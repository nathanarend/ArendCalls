package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"wacalls/internal/voip/recording"
)

// maxRecordingDuration auto-closes a recording for a call left off-hook so a
// forgotten call cannot produce a giant file. Audio up to the cap is kept.
const maxRecordingDuration = 40 * time.Minute

type recMeta struct {
	callID    string
	sessionID string
	clinicID  string
	direction string
	peer      string
}

type liveRecording struct {
	meta     recMeta
	rec      *recording.Recorder // nil until the call is answered
	path     string
	started  bool
	stopOnce sync.Once
}

// recordingController owns the server-side recording lifecycle:
//
//   - capture: mixes both call legs into a stereo WAV on local disk while the
//     call is up (only after it is answered);
//   - queue: finished recordings sit in the call_recordings table (the durable
//     queue) and are drained by a fixed pool of upload workers — a burst of
//     call-ends never fans out into a burst of uploads;
//   - handoff: each worker uploads to B2, verifies it landed, deletes the local
//     WAV, then POSTs the signed webhook to Mocho, retrying on a backoff.
type recordingController struct {
	appCtx  context.Context
	store   *recordingStore
	secrets *secretBox
	dir     string
	log     *slog.Logger
	workers int

	mu        sync.Mutex
	live      map[string]*liveRecording
	inflight  map[string]bool // callIDs queued or being processed — upload dedupe
	jobs      chan string
	startOnce sync.Once
}

func newRecordingController(ctx context.Context, store *recordingStore, secrets *secretBox, dir string, workers int, log *slog.Logger) *recordingController {
	if workers < 1 {
		workers = 3
	}
	return &recordingController{
		appCtx:   ctx,
		store:    store,
		secrets:  secrets,
		dir:      dir,
		log:      log,
		workers:  workers,
		live:     map[string]*liveRecording{},
		inflight: map[string]bool{},
		jobs:     make(chan string, 512),
	}
}

// start launches the upload worker pool, the retry sweeper and the boot
// recovery. Call once, after construction.
func (c *recordingController) start() {
	c.startOnce.Do(func() {
		for i := 0; i < c.workers; i++ {
			go c.worker()
		}
		go c.retryLoop()
		go c.resumeOnBoot()
		c.log.Info("recording: handoff pool started", "workers", c.workers)
	})
}

// arm records the intent to capture a call. Whether a call is recorded is
// decided solely by the caller of POST /api/sessions/{sid}/calls (the `record`
// field); arm is invoked only when that flag is true, before the call connects.
func (c *recordingController) arm(meta recMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.live[meta.callID]; ok {
		return
	}
	c.live[meta.callID] = &liveRecording{meta: meta}
}

// armed reports whether arm was called for this call.
func (c *recordingController) armed(callID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.live[callID]
	return ok
}

// onAnswered starts the WAV capture. Called when the call is answered / media is
// live — never while it is still ringing. Idempotent.
func (c *recordingController) onAnswered(callID string) *recording.Recorder {
	c.mu.Lock()
	lr, ok := c.live[callID]
	if !ok || lr.started {
		var r *recording.Recorder
		if ok {
			r = lr.rec
		}
		c.mu.Unlock()
		return r
	}
	lr.started = true
	meta := lr.meta
	path := c.pathFor(meta)
	lr.path = path
	c.mu.Unlock()

	rec, err := recording.New(recording.Options{
		Path:        path,
		MaxDuration: maxRecordingDuration,
		OnLimit:     func() { c.log.Warn("recording: 40min cap reached, auto-closing", "call", callID) },
	})
	if err != nil {
		c.log.Error("recording: cannot start", "call", callID, "err", err)
		c.mu.Lock()
		delete(c.live, callID)
		c.mu.Unlock()
		return nil
	}

	if err := c.store.begin(c.appCtx, RecordingRow{
		CallID: callID, SessionID: meta.sessionID, ClinicID: meta.clinicID,
		LocalPath: path, Channels: "stereo", Direction: meta.direction, Peer: meta.peer,
		StartedAt: time.Now().UnixMilli(),
	}); err != nil {
		c.log.Error("recording: cannot persist start", "call", callID, "err", err)
	}

	c.mu.Lock()
	if cur, ok := c.live[callID]; !ok || cur != lr {
		// The call ended while we were setting up (disk + DB I/O). Finalize it
		// ourselves so it still reaches the queue (or gets skipped if < 5s).
		c.mu.Unlock()
		c.finalizeRecording(callID, path, rec)
		return nil
	}
	lr.rec = rec
	c.mu.Unlock()
	c.log.Info("recording started", "call", callID, "session", meta.sessionID, "path", path)
	return rec
}

// onCallEnded finalizes the WAV and either skips it (<5s) or queues it for upload.
func (c *recordingController) onCallEnded(callID string) {
	c.mu.Lock()
	lr, ok := c.live[callID]
	if !ok {
		c.mu.Unlock()
		return
	}
	delete(c.live, callID)
	c.mu.Unlock()

	lr.stopOnce.Do(func() {
		if lr.rec == nil {
			return // armed but never answered — nothing was recorded
		}
		c.finalizeRecording(callID, lr.path, lr.rec)
	})
}

// finalizeRecording closes the WAV, measures the exact duration, and routes it:
// shorter than 5s → discarded; otherwise → onto the upload queue.
func (c *recordingController) finalizeRecording(callID, path string, rec *recording.Recorder) {
	dur, err := rec.Close()
	endedAt := time.Now().UnixMilli()
	if err != nil {
		c.log.Error("recording: finalize failed", "call", callID, "err", err)
		_ = c.store.setStatus(c.appCtx, callID, RecStatusFailed, "finalize: "+err.Error())
		return
	}
	if dur.Milliseconds() < minRecordingMs {
		_ = c.store.markSkipped(c.appCtx, callID, dur.Milliseconds(), endedAt, "recording shorter than 5s")
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			c.log.Warn("recording: could not delete skipped WAV", "call", callID, "err", rmErr)
		}
		c.log.Info("recording skipped (<5s)", "call", callID, "duration", dur.String())
		return
	}
	if err := c.store.finishRecording(c.appCtx, callID, dur.Milliseconds(), endedAt, RecStatusUploading); err != nil {
		c.log.Error("recording: cannot persist finish", "call", callID, "err", err)
	}
	c.log.Info("recording finalized, queued for upload", "call", callID, "duration", dur.String())
	c.enqueue(callID)
}

// enqueue hands a callID to the upload pool, unless it is already queued or being
// processed. If the queue is momentarily full the claim is released and the
// retry sweep will pick the row up again.
func (c *recordingController) enqueue(callID string) {
	c.mu.Lock()
	if c.inflight[callID] {
		c.mu.Unlock()
		return
	}
	c.inflight[callID] = true
	c.mu.Unlock()

	select {
	case c.jobs <- callID:
	default:
		c.mu.Lock()
		delete(c.inflight, callID)
		c.mu.Unlock()
		c.log.Warn("recording: upload queue full, deferring to retry sweep", "call", callID)
	}
}

func (c *recordingController) worker() {
	for {
		select {
		case <-c.appCtx.Done():
			return
		case callID := <-c.jobs:
			c.processRecording(callID)
			c.mu.Lock()
			delete(c.inflight, callID)
			c.mu.Unlock()
		}
	}
}

func (c *recordingController) pathFor(meta recMeta) string {
	now := time.Now().UTC()
	sub := filepath.Join(c.dir, meta.sessionID, fmt.Sprintf("%04d-%02d", now.Year(), int(now.Month())))
	return filepath.Join(sub, meta.callID+".wav")
}

// diskUsageBytes reports how much the recordings directory is holding — a health
// signal (it should stay near zero; it only grows while B2 is unreachable).
func (c *recordingController) diskUsageBytes() int64 {
	var total int64
	_ = filepath.Walk(c.dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
