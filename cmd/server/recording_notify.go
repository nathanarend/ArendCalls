package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// recordingReadyPayload is the body POSTed to the Mocho webhook. Field names
// match docs/gravacao-server-side.md §8.
type recordingReadyPayload struct {
	CallID          string `json:"callId"`
	SessionID       string `json:"sessionId"`
	ClinicID        string `json:"clinicId"`
	RecordingKey    string `json:"recordingKey"`
	RecordingURL    string `json:"recordingUrl,omitempty"`
	DurationSeconds int    `json:"durationSeconds"`
	Channels        string `json:"channels"`
	MimeType        string `json:"mimeType"`
	StartedAt       string `json:"startedAt"`
	EndedAt         string `json:"endedAt"`
}

// notify delivers the "recording ready" webhook to Mocho. A non-2xx or transport
// error leaves notified_at at 0 so the retry worker tries again with backoff.
func (c *recordingController) notify(ctx context.Context, row RecordingRow, cfg RecordingConfig) {
	if cfg.WebhookURL == "" || cfg.WebhookSecret == "" {
		// Config guarantees both are present before we get here; this is a
		// safety net if the config was cleared after the upload.
		c.log.Warn("recording notify: webhook not configured", "call", row.CallID, "session", row.SessionID)
		return
	}
	_ = c.store.incNotifyAttempts(ctx, row.CallID)

	body, _ := json.Marshal(recordingReadyPayload{
		CallID:          row.CallID,
		SessionID:       row.SessionID,
		ClinicID:        row.ClinicID,
		RecordingKey:    row.B2Key,
		RecordingURL:    row.B2URL,
		DurationSeconds: int((time.Duration(row.DurationMs) * time.Millisecond).Seconds()),
		Channels:        row.Channels,
		MimeType:        "audio/wav",
		StartedAt:       msToRFC3339(row.StartedAt),
		EndedAt:         msToRFC3339(row.EndedAt),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		c.log.Error("recording notify: bad request", "call", row.CallID, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ArendCalls-Recording/1")
	if cfg.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.WebhookSecret))
		mac.Write(body)
		req.Header.Set("X-ArendCalls-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		c.log.Warn("recording notify: transport error, will retry", "call", row.CallID, "err", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		c.log.Warn("recording notify: non-2xx, will retry", "call", row.CallID, "status", resp.Status)
		return
	}
	if err := c.store.markNotified(ctx, row.CallID); err != nil {
		c.log.Error("recording notify: cannot persist ack", "call", row.CallID, "err", err)
		return
	}
	c.log.Info("recording notify: Mocho acked", "call", row.CallID)
}

// retryBackoff is the delay before attempt N (1-indexed). After the table it
// stays at 1h. Matches docs/gravacao-server-side.md §8.2.
var retryBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

const maxHandoffAttempts = 24 // ~24h of retries once at the 1h step

func backoffFor(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt <= len(retryBackoff) {
		return retryBackoff[attempt-1]
	}
	return retryBackoff[len(retryBackoff)-1]
}

// retryLoop re-queues failed uploads and un-acked webhooks on a backoff
// schedule. Started once by (*recordingController).start.
func (c *recordingController) retryLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.appCtx.Done():
			return
		case <-ticker.C:
			c.retrySweep()
		}
	}
}

func (c *recordingController) retrySweep() {
	rows, err := c.store.pendingHandoff(c.appCtx)
	if err != nil {
		c.log.Warn("recording retry: sweep query failed", "err", err)
		return
	}
	now := time.Now()
	for _, r := range rows {
		attempts := r.UploadAttempts
		if r.NotifyAttempts > attempts {
			attempts = r.NotifyAttempts
		}
		if attempts >= maxHandoffAttempts {
			continue // give up quietly; recording-info still serves the row
		}
		if r.LastAttemptAt != 0 && now.Sub(time.UnixMilli(r.LastAttemptAt)) < backoffFor(attempts) {
			continue
		}
		// Session not fully configured yet — the WAV just waits, not a failure
		// to churn on. A later sweep picks it up once B2 + webhook are set.
		if cfg, err := c.store.config(c.appCtx, r.SessionID, c.secrets); err == nil && !cfg.complete() {
			continue
		}
		c.enqueue(r.CallID) // dedupe drops it if a worker already has it
	}
}

func msToRFC3339(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// recordingInfo is the payload returned by GET .../recording-info for Mocho to
// reconcile recordings whose webhook it never received.
func recordingInfoJSON(row RecordingRow) ([]byte, error) {
	if row.Channels == "" {
		row.Channels = "stereo"
	}
	return json.MarshalIndent(map[string]any{
		"callId":          row.CallID,
		"sessionId":       row.SessionID,
		"clinicId":        row.ClinicID,
		"status":          string(row.Status),
		"recordingKey":    row.B2Key,
		"recordingUrl":    row.B2URL,
		"durationSeconds": int((time.Duration(row.DurationMs) * time.Millisecond).Seconds()),
		"channels":        row.Channels,
		"mimeType":        "audio/wav",
		"startedAt":       msToRFC3339(row.StartedAt),
		"endedAt":         msToRFC3339(row.EndedAt),
		"error":           row.Err,
	}, "", "  ")
}
