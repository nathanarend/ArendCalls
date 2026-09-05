package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"wacalls/internal/voip/recording"
)

// handoff moves one finished recording off the box: upload the WAV to the
// session's Backblaze B2 bucket, then notify Mocho (see recording_notify.go).
// Safe to run in a goroutine; never blocks call teardown. Every terminal state
// is persisted so a restart can resume via resumePendingHandoffs.
func (c *recordingController) handoff(callID string) {
	ctx, cancel := context.WithTimeout(c.appCtx, 10*time.Minute)
	defer cancel()

	row, err := c.store.get(ctx, callID)
	if err != nil || row == nil {
		c.log.Warn("recording handoff: row missing", "call", callID, "err", err)
		return
	}
	cfg, err := c.store.config(ctx, row.SessionID, c.secrets)
	if err != nil {
		c.log.Error("recording handoff: config read failed", "call", callID, "err", err)
		return
	}

	if row.Status == RecStatusUploading || row.Status == RecStatusFailed {
		row, err = c.upload(ctx, *row, cfg)
		if err != nil {
			c.log.Warn("recording handoff: upload failed, will retry", "call", callID, "err", err)
			return
		}
	}

	if row.Status == RecStatusReady && row.NotifiedAt == 0 {
		c.notify(ctx, *row, cfg)
	}
}

// upload pushes the local WAV to B2 and marks the row ready. It returns the
// updated row on success.
func (c *recordingController) upload(ctx context.Context, row RecordingRow, cfg RecordingConfig) (*RecordingRow, error) {
	if !cfg.b2Ready() {
		c.log.Warn("recording handoff: B2 not configured for session, WAV kept on local disk",
			"call", row.CallID, "session", row.SessionID, "path", row.LocalPath)
		return &row, errors.New("b2 not configured")
	}
	_ = c.store.incUploadAttempts(ctx, row.CallID)

	data, err := os.ReadFile(row.LocalPath)
	if err != nil {
		_ = c.store.setStatus(ctx, row.CallID, RecStatusFailed, "local file unreadable: "+err.Error())
		return nil, err
	}

	b2, err := recording.NewB2Client(recording.B2Config{
		Endpoint: cfg.B2Endpoint, Region: cfg.B2Region, Bucket: cfg.B2Bucket,
		KeyID: cfg.B2KeyID, AppKey: cfg.B2AppKey,
	})
	if err != nil {
		_ = c.store.setStatus(ctx, row.CallID, RecStatusFailed, "b2 client: "+err.Error())
		return nil, err
	}

	key := b2KeyFor(cfg.B2Prefix, row)
	if err := b2.PutObject(ctx, key, "audio/wav", data); err != nil {
		_ = c.store.setStatus(ctx, row.CallID, RecStatusFailed, "b2 put: "+err.Error())
		return nil, err
	}

	url := ""
	if cfg.URLTTLSeconds > 0 {
		url = b2.PresignGet(key, time.Duration(cfg.URLTTLSeconds)*time.Second)
	}
	if err := c.store.markUploaded(ctx, row.CallID, key, url); err != nil {
		c.log.Error("recording handoff: cannot persist upload result", "call", row.CallID, "err", err)
		return nil, err
	}
	if err := os.Remove(row.LocalPath); err != nil && !os.IsNotExist(err) {
		c.log.Warn("recording handoff: could not delete local WAV after upload", "call", row.CallID, "err", err)
	}
	c.log.Info("recording uploaded to B2", "call", row.CallID, "key", key)

	row.Status = RecStatusReady
	row.B2Key = key
	row.B2URL = url
	return &row, nil
}

// b2KeyFor builds recordings/{clinicId}/{YYYY}/{MM}/{callId}.wav, optionally
// under a per-session prefix, using the call's server start time.
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

// resumePendingHandoffs recovers recordings whose handoff was interrupted by a
// restart: stale "recording" rows (audio buffer lost) become failed captures,
// and everything still owing an upload or a Mocho ack is re-driven.
func (c *recordingController) resumePendingHandoffs() {
	if err := c.store.reapStaleRecording(c.appCtx); err != nil {
		c.log.Warn("recording: reap stale rows failed", "err", err)
	}
	rows, err := c.store.pendingHandoff(c.appCtx)
	if err != nil {
		c.log.Warn("recording: list pending handoffs failed", "err", err)
		return
	}
	for _, r := range rows {
		c.log.Info("recording: resuming handoff on startup", "call", r.CallID, "status", string(r.Status))
		go c.handoff(r.CallID)
	}
	c.startRetryWorker()
}
