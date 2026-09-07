package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"wacalls/internal/voip/recording"
)

// processRecording runs one recording all the way off the box: upload the WAV to
// the session's B2 bucket, verify it landed, delete the local file, then POST the
// signed webhook to Mocho. Every terminal state is persisted; a failure just
// returns and the retry sweep re-queues the row later. Called only by pool
// workers, one recording at a time per worker.
func (c *recordingController) processRecording(callID string) {
	ctx, cancel := context.WithTimeout(c.appCtx, 20*time.Minute)
	defer cancel()

	row, err := c.store.get(ctx, callID)
	if err != nil || row == nil {
		c.log.Warn("recording handoff: row missing", "call", callID, "err", err)
		return
	}
	if row.Status == RecStatusSkipped || (row.Status == RecStatusReady && row.NotifiedAt != 0) {
		return // nothing left to do
	}

	cfg, err := c.store.config(ctx, row.SessionID, c.secrets)
	if err != nil {
		c.log.Error("recording handoff: config read failed", "call", callID, "err", err)
		return
	}
	if !cfg.complete() {
		// B2 and the webhook are configured together or not at all. Until the
		// session gets both, the WAV waits on disk — nothing is lost.
		c.log.Warn("recording handoff: session B2/webhook not configured, WAV kept on local disk",
			"call", callID, "session", row.SessionID, "path", row.LocalPath)
		return
	}

	if row.Status == RecStatusUploading || row.Status == RecStatusFailed {
		updated, err := c.uploadAndVerify(ctx, *row, cfg)
		if err != nil {
			c.log.Warn("recording handoff: upload failed, will retry", "call", callID, "err", err)
			return
		}
		row = updated
	}

	if row.Status == RecStatusReady && row.NotifiedAt == 0 {
		c.notify(ctx, *row, cfg)
	}
}

// uploadAndVerify streams the local WAV to B2, confirms it is really there with a
// matching size, then deletes the local file. Returns the updated row.
func (c *recordingController) uploadAndVerify(ctx context.Context, row RecordingRow, cfg RecordingConfig) (*RecordingRow, error) {
	_ = c.store.incUploadAttempts(ctx, row.CallID)

	f, err := os.Open(row.LocalPath)
	if err != nil {
		_ = c.store.setStatus(ctx, row.CallID, RecStatusFailed, "local file unreadable: "+err.Error())
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		_ = c.store.setStatus(ctx, row.CallID, RecStatusFailed, "local stat: "+err.Error())
		return nil, err
	}
	size := st.Size()

	b2, err := recording.NewB2Client(recording.B2Config{
		Endpoint: cfg.B2Endpoint, Region: cfg.B2Region, Bucket: cfg.B2Bucket,
		KeyID: cfg.B2KeyID, AppKey: cfg.B2AppKey,
	})
	if err != nil {
		_ = c.store.setStatus(ctx, row.CallID, RecStatusFailed, "b2 client: "+err.Error())
		return nil, err
	}

	key := b2KeyFor(cfg.B2Prefix, row)
	if err := b2.PutObject(ctx, key, "audio/wav", f, size); err != nil {
		_ = c.store.setStatus(ctx, row.CallID, RecStatusFailed, "b2 put: "+err.Error())
		return nil, err
	}
	remote, err := b2.HeadObject(ctx, key)
	if err != nil {
		_ = c.store.setStatus(ctx, row.CallID, RecStatusFailed, "b2 verify: "+err.Error())
		return nil, err
	}
	if remote != size {
		msg := fmt.Sprintf("b2 verify: size mismatch remote=%d local=%d", remote, size)
		_ = c.store.setStatus(ctx, row.CallID, RecStatusFailed, msg)
		return nil, fmt.Errorf("%s", msg)
	}

	url := ""
	if cfg.URLTTLSeconds > 0 {
		url = b2.PresignGet(key, time.Duration(cfg.URLTTLSeconds)*time.Second)
	}
	if err := c.store.markUploaded(ctx, row.CallID, key, url); err != nil {
		c.log.Error("recording handoff: cannot persist upload result", "call", row.CallID, "err", err)
		return nil, err
	}

	f.Close()
	if err := os.Remove(row.LocalPath); err != nil && !os.IsNotExist(err) {
		c.log.Warn("recording handoff: could not delete local WAV after verified upload", "call", row.CallID, "err", err)
	}
	c.log.Info("recording uploaded + verified in B2", "call", row.CallID, "key", key, "bytes", size)

	row.Status = RecStatusReady
	row.B2Key = key
	row.B2URL = url
	return &row, nil
}

// b2KeyFor builds [{prefix}/]recordings/{clinicId}/{YYYY}/{MM}/{callId}.wav,
// using the call's server start time.
func b2KeyFor(prefix string, row RecordingRow) string {
	clinic := row.ClinicID
	if clinic == "" {
		clinic = "unknown"
	}
	t := time.UnixMilli(row.StartedAt).UTC()
	if row.StartedAt == 0 {
		t = time.Now().UTC()
	}
	key := fmt.Sprintf("recordings/%s/%04d/%02d/%s.wav", clinic, t.Year(), int(t.Month()), row.CallID)
	if prefix != "" {
		key = trimSlash(prefix) + "/" + key
	}
	return key
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	for len(s) > 0 && s[0] == '/' {
		s = s[1:]
	}
	return s
}

// resumeOnBoot recovers recordings interrupted by a restart: a row left in
// "recording" is salvaged if its WAV is intact on disk (estimating the duration
// from file size), otherwise marked failed. Everything still pending is then
// re-queued.
func (c *recordingController) resumeOnBoot() {
	stale, err := c.store.staleRecordings(c.appCtx)
	if err != nil {
		c.log.Warn("recording: staleRecordings query failed", "err", err)
	}
	now := time.Now().UnixMilli()
	for _, r := range stale {
		fi, statErr := os.Stat(r.LocalPath)
		if statErr != nil || fi.Size() <= recording.WavHeaderBytes {
			_ = c.store.setStatus(c.appCtx, r.CallID, RecStatusFailed, "restart mid-capture, no usable file")
			c.log.Warn("recording: lost after restart (no file on disk)", "call", r.CallID)
			continue
		}
		estMs := recording.ApproxDurationMs(fi.Size())
		if estMs < minRecordingMs {
			_ = c.store.markSkipped(c.appCtx, r.CallID, estMs, now, "restart mid-capture, <5s of audio")
			_ = os.Remove(r.LocalPath)
			continue
		}
		_ = c.store.finishRecording(c.appCtx, r.CallID, estMs, now, RecStatusUploading)
		c.log.Info("recording: salvaged after restart", "call", r.CallID, "bytes", fi.Size(), "est_ms", estMs)
	}

	rows, err := c.store.pendingHandoff(c.appCtx)
	if err != nil {
		c.log.Warn("recording: pendingHandoff query failed", "err", err)
		return
	}
	for _, r := range rows {
		c.enqueue(r.CallID)
	}
	if len(rows) > 0 {
		c.log.Info("recording: re-queued pending handoffs on startup", "count", len(rows))
	}
}
