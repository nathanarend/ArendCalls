package main

// Handoff of a finished recording to Backblaze B2 and the Mocho webhook.
//
// Commit 1 leaves the finalized WAV on local disk in status "uploading"; the B2
// upload, presigned-URL generation, HMAC-signed webhook and retry worker land in
// the follow-up commits. Keeping this seam here means onCallEnded already calls
// the right entry point.

// handoff moves one finished recording off the box. Safe to call from a
// goroutine; it never blocks call teardown.
func (c *recordingController) handoff(callID string) {
	row, err := c.store.get(c.appCtx, callID)
	if err != nil || row == nil {
		c.log.Warn("recording handoff: row missing", "call", callID, "err", err)
		return
	}
	cfg, err := c.store.config(c.appCtx, row.SessionID, c.secrets)
	if err != nil {
		c.log.Error("recording handoff: config read failed", "call", callID, "err", err)
		return
	}
	if !cfg.b2Ready() {
		c.log.Warn("recording handoff: B2 not configured for session, WAV kept on local disk",
			"call", callID, "session", row.SessionID, "path", row.LocalPath)
		return
	}
	// B2 upload + webhook implemented in the follow-up commit.
	c.log.Info("recording handoff: pending B2 upload", "call", callID, "path", row.LocalPath)
}

// resumePendingHandoffs is invoked at startup to recover recordings whose
// handoff was interrupted by a restart.
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
		c.log.Info("recording: pending handoff on startup", "call", r.CallID, "status", string(r.Status))
	}
}
