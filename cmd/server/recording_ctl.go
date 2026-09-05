package main

import (
	"context"
	"fmt"
	"log/slog"
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
	rec      *recording.Recorder // nil until media connects
	started  bool
	stopOnce sync.Once
}

// recordingController owns the server-side recording lifecycle: it decides which
// calls to record, mixes both legs into a stereo WAV while the call is up, and
// on hang-up hands the file off (B2 upload + Mocho webhook — see recording_handoff.go).
type recordingController struct {
	appCtx  context.Context
	store   *recordingStore
	secrets *secretBox
	dir     string
	log     *slog.Logger

	mu        sync.Mutex
	live      map[string]*liveRecording
	retryOnce sync.Once
}

func newRecordingController(ctx context.Context, store *recordingStore, secrets *secretBox, dir string, log *slog.Logger) *recordingController {
	return &recordingController{
		appCtx:  ctx,
		store:   store,
		secrets: secrets,
		dir:     dir,
		log:     log,
		live:    map[string]*liveRecording{},
	}
}

// wantRecording resolves whether a call should be recorded: an explicit flag from
// the Mocho POST wins; otherwise the per-session default applies.
func (c *recordingController) wantRecording(sessionID string, explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	cfg, err := c.store.config(c.appCtx, sessionID, c.secrets)
	if err != nil {
		c.log.Warn("recording: cannot read session config", "session", sessionID, "err", err)
		return false
	}
	return cfg.Enabled
}

// arm records the intent to capture a call. Call it once the call object exists
// (outbound: on POST; inbound: on offer) and before media connects.
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

// onMediaConnected starts the WAV capture. Idempotent: safe to call from every
// relay-connected notification (mirror sessions, renegotiation).
func (c *recordingController) onMediaConnected(callID string) *recording.Recorder {
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
	c.mu.Unlock()

	path := c.pathFor(meta)
	rec, err := recording.New(recording.Options{
		Path:        path,
		MaxDuration: maxRecordingDuration,
		OnLimit:     func() { c.log.Warn("recording: 40min cap reached, auto-closing", "call", callID) },
		Log:         nil,
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
		// The call ended while we were setting up (disk + DB I/O). Finalize the
		// short recording ourselves so it still reaches the handoff.
		c.mu.Unlock()
		dur, _ := rec.Close()
		_ = c.store.finishRecording(c.appCtx, callID, dur.Milliseconds(), time.Now().UnixMilli(), RecStatusUploading)
		c.log.Warn("recording: call ended during startup, finalized early", "call", callID, "duration", dur.String())
		go c.handoff(callID)
		return nil
	}
	lr.rec = rec
	c.mu.Unlock()
	c.log.Info("recording started", "call", callID, "session", meta.sessionID, "path", path)
	return rec
}

// onCallEnded finalizes the WAV, records the exact server-measured duration and
// triggers the asynchronous handoff.
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
			// Armed but media never connected — nothing was recorded.
			return
		}
		dur, err := lr.rec.Close()
		if err != nil {
			c.log.Error("recording: finalize failed", "call", callID, "err", err)
			_ = c.store.setStatus(c.appCtx, callID, RecStatusFailed, "finalize: "+err.Error())
			return
		}
		endedAt := time.Now().UnixMilli()
		if err := c.store.finishRecording(c.appCtx, callID, dur.Milliseconds(), endedAt, RecStatusUploading); err != nil {
			c.log.Error("recording: cannot persist finish", "call", callID, "err", err)
		}
		c.log.Info("recording finalized", "call", callID, "duration", dur.String())
		go c.handoff(callID)
	})
}

func (c *recordingController) pathFor(meta recMeta) string {
	now := time.Now().UTC()
	sub := filepath.Join(c.dir, meta.sessionID, fmt.Sprintf("%04d-%02d", now.Year(), int(now.Month())))
	return filepath.Join(sub, meta.callID+".wav")
}
