package main

import (
	"context"
	"database/sql"
	"time"
)

// RecordingStatus tracks a call recording through its lifecycle.
type RecordingStatus string

const (
	RecStatusRecording RecordingStatus = "recording" // capture in progress
	RecStatusUploading RecordingStatus = "uploading" // finished, queued for B2
	RecStatusReady     RecordingStatus = "ready"     // in B2, webhook maybe still owed
	RecStatusFailed    RecordingStatus = "failed"    // last attempt errored, will retry
	RecStatusSkipped   RecordingStatus = "skipped"   // too short (<5s) — not uploaded
)

// minRecordingDuration: recordings shorter than this are discarded, not uploaded.
const minRecordingMs = 5000

// RecordingRow is one row of the call_recordings table. It survives restarts so
// the upload/notify workers can resume interrupted handoffs.
type RecordingRow struct {
	CallID         string
	SessionID      string
	ClinicID       string
	Status         RecordingStatus
	LocalPath      string
	B2Key          string
	B2URL          string
	DurationMs     int64
	Channels       string
	Direction      string
	Peer           string
	StartedAt      int64 // unix ms
	EndedAt        int64 // unix ms, 0 while recording
	Err            string
	UploadAttempts int
	NotifyAttempts int
	NotifiedAt     int64 // unix ms, 0 until Mocho acked
	LastAttemptAt  int64 // unix ms of the last upload/notify try
	CreatedAt      int64
}

type recordingStore struct{ db *sql.DB }

func newRecordingStore(ctx context.Context, db *sql.DB) (*recordingStore, error) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS call_recordings (
		call_id         TEXT PRIMARY KEY,
		session_id      TEXT NOT NULL,
		clinic_id       TEXT,
		status          TEXT NOT NULL,
		local_path      TEXT,
		b2_key          TEXT,
		b2_url          TEXT,
		duration_ms     INTEGER DEFAULT 0,
		channels        TEXT,
		direction       TEXT,
		peer            TEXT,
		started_at      INTEGER,
		ended_at        INTEGER DEFAULT 0,
		error           TEXT,
		upload_attempts INTEGER DEFAULT 0,
		notify_attempts INTEGER DEFAULT 0,
		notified_at     INTEGER DEFAULT 0,
		last_attempt_at INTEGER DEFAULT 0,
		created_at      INTEGER
	)`)
	if err != nil {
		return nil, err
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS recording_config (
		session_id         TEXT PRIMARY KEY,
		b2_endpoint        TEXT,
		b2_region          TEXT,
		b2_bucket          TEXT,
		b2_key_id          TEXT,
		b2_app_key_enc     TEXT,
		b2_prefix          TEXT,
		webhook_url        TEXT,
		webhook_secret_enc TEXT,
		url_ttl_seconds    INTEGER DEFAULT 0,
		updated_at         INTEGER
	)`)
	if err != nil {
		return nil, err
	}
	return &recordingStore{db: db}, nil
}

// RecordingConfig is the per-session recording setup. Secret fields
// (b2_app_key, webhook_secret) are stored encrypted at rest and are returned
// here already decrypted.
type RecordingConfig struct {
	SessionID     string
	B2Endpoint    string
	B2Region      string
	B2Bucket      string
	B2KeyID       string
	B2AppKey      string
	B2Prefix      string
	WebhookURL    string
	WebhookSecret string
	URLTTLSeconds int
}

// complete reports whether the session can actually deliver a recording: B2 and
// the Mocho webhook are mutually required, so both must be fully set.
func (c RecordingConfig) complete() bool {
	return c.B2Endpoint != "" && c.B2Bucket != "" && c.B2KeyID != "" && c.B2AppKey != "" &&
		c.WebhookURL != "" && c.WebhookSecret != ""
}

// isEmpty reports whether no destination field is set (config never configured,
// or explicitly cleared). Anything between empty and complete is rejected.
func (c RecordingConfig) isEmpty() bool {
	return c.B2Endpoint == "" && c.B2Region == "" && c.B2Bucket == "" && c.B2KeyID == "" &&
		c.B2AppKey == "" && c.B2Prefix == "" && c.WebhookURL == "" && c.WebhookSecret == "" &&
		c.URLTTLSeconds == 0
}

func (s *recordingStore) config(ctx context.Context, sessionID string, dec secretDecryptor) (RecordingConfig, error) {
	c := RecordingConfig{SessionID: sessionID}
	var appKeyEnc, secretEnc string
	err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(b2_endpoint,''), COALESCE(b2_region,''), COALESCE(b2_bucket,''),
		COALESCE(b2_key_id,''), COALESCE(b2_app_key_enc,''), COALESCE(b2_prefix,''),
		COALESCE(webhook_url,''), COALESCE(webhook_secret_enc,''), COALESCE(url_ttl_seconds,0)
		FROM recording_config WHERE session_id=?`, sessionID).Scan(
		&c.B2Endpoint, &c.B2Region, &c.B2Bucket, &c.B2KeyID, &appKeyEnc,
		&c.B2Prefix, &c.WebhookURL, &secretEnc, &c.URLTTLSeconds)
	if err == sql.ErrNoRows {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if c.B2AppKey, err = dec.Decrypt(appKeyEnc); err != nil {
		return c, err
	}
	if c.WebhookSecret, err = dec.Decrypt(secretEnc); err != nil {
		return c, err
	}
	return c, nil
}

func (s *recordingStore) saveConfig(ctx context.Context, c RecordingConfig, enc secretEncryptor) error {
	appKeyEnc, err := enc.Encrypt(c.B2AppKey)
	if err != nil {
		return err
	}
	secretEnc, err := enc.Encrypt(c.WebhookSecret)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO recording_config
		(session_id, b2_endpoint, b2_region, b2_bucket, b2_key_id, b2_app_key_enc,
		 b2_prefix, webhook_url, webhook_secret_enc, url_ttl_seconds, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			b2_endpoint=excluded.b2_endpoint, b2_region=excluded.b2_region,
			b2_bucket=excluded.b2_bucket, b2_key_id=excluded.b2_key_id, b2_app_key_enc=excluded.b2_app_key_enc,
			b2_prefix=excluded.b2_prefix, webhook_url=excluded.webhook_url,
			webhook_secret_enc=excluded.webhook_secret_enc, url_ttl_seconds=excluded.url_ttl_seconds,
			updated_at=excluded.updated_at`,
		c.SessionID, c.B2Endpoint, c.B2Region, c.B2Bucket, c.B2KeyID, appKeyEnc,
		c.B2Prefix, c.WebhookURL, secretEnc, c.URLTTLSeconds, time.Now().UnixMilli())
	return err
}

func (s *recordingStore) begin(ctx context.Context, r RecordingRow) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO call_recordings
		(call_id, session_id, clinic_id, status, local_path, channels, direction, peer, started_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(call_id) DO UPDATE SET
			session_id=excluded.session_id, clinic_id=excluded.clinic_id, status=excluded.status,
			local_path=excluded.local_path, channels=excluded.channels, direction=excluded.direction,
			peer=excluded.peer, started_at=excluded.started_at`,
		r.CallID, r.SessionID, r.ClinicID, string(RecStatusRecording), r.LocalPath,
		r.Channels, r.Direction, r.Peer, r.StartedAt, now)
	return err
}

// finishRecording marks the capture done and stores the exact server-measured
// duration; the recording is now a candidate for upload.
func (s *recordingStore) finishRecording(ctx context.Context, callID string, durationMs, endedAt int64, next RecordingStatus) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_recordings
		SET status=?, duration_ms=?, ended_at=? WHERE call_id=?`,
		string(next), durationMs, endedAt, callID)
	return err
}

func (s *recordingStore) setStatus(ctx context.Context, callID string, st RecordingStatus, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_recordings SET status=?, error=? WHERE call_id=?`,
		string(st), errMsg, callID)
	return err
}

func (s *recordingStore) markUploaded(ctx context.Context, callID, b2Key, b2URL string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_recordings
		SET status=?, b2_key=?, b2_url=?, error='' WHERE call_id=?`,
		string(RecStatusReady), b2Key, b2URL, callID)
	return err
}

func (s *recordingStore) markSkipped(ctx context.Context, callID string, durationMs, endedAt int64, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_recordings
		SET status=?, duration_ms=?, ended_at=?, error=? WHERE call_id=?`,
		string(RecStatusSkipped), durationMs, endedAt, reason, callID)
	return err
}

func (s *recordingStore) incUploadAttempts(ctx context.Context, callID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_recordings
		SET upload_attempts = upload_attempts + 1, last_attempt_at = ? WHERE call_id=?`,
		time.Now().UnixMilli(), callID)
	return err
}

func (s *recordingStore) incNotifyAttempts(ctx context.Context, callID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_recordings
		SET notify_attempts = notify_attempts + 1, last_attempt_at = ? WHERE call_id=?`,
		time.Now().UnixMilli(), callID)
	return err
}

func (s *recordingStore) markNotified(ctx context.Context, callID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_recordings SET notified_at=? WHERE call_id=?`,
		time.Now().UnixMilli(), callID)
	return err
}

func (s *recordingStore) get(ctx context.Context, callID string) (*RecordingRow, error) {
	row := s.db.QueryRowContext(ctx, recordingSelectCols+` WHERE call_id=?`, callID)
	r, err := scanRecording(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// pendingHandoff returns recordings that still need work: local captures that
// finished but were never uploaded, or uploaded ones Mocho never acked.
func (s *recordingStore) pendingHandoff(ctx context.Context) ([]RecordingRow, error) {
	rows, err := s.db.QueryContext(ctx, recordingSelectCols+`
		WHERE (status IN (?, ?)) OR (status = ? AND notified_at = 0)
		ORDER BY created_at`,
		string(RecStatusUploading), string(RecStatusFailed), string(RecStatusReady))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordingRow
	for rows.Next() {
		r, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// staleRecordings returns rows left in "recording" by a restart. The caller
// decides per row: salvage the WAV if it is intact on disk, else mark failed.
func (s *recordingStore) staleRecordings(ctx context.Context) ([]RecordingRow, error) {
	rows, err := s.db.QueryContext(ctx, recordingSelectCols+` WHERE status=?`, string(RecStatusRecording))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordingRow
	for rows.Next() {
		r, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// listBySession returns the most recent recordings for a session, newest first,
// for the per-session monitoring view.
func (s *recordingStore) listBySession(ctx context.Context, sessionID string, limit int) ([]RecordingRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, recordingSelectCols+`
		WHERE session_id=? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordingRow
	for rows.Next() {
		r, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// sessionRecordingStats summarizes a session's recordings for the monitoring header.
func (s *recordingStore) sessionRecordingStats(ctx context.Context, sessionID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM call_recordings
		WHERE session_id=? GROUP BY status`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

const recordingSelectCols = `SELECT call_id, session_id, COALESCE(clinic_id,''), status,
	COALESCE(local_path,''), COALESCE(b2_key,''), COALESCE(b2_url,''), duration_ms,
	COALESCE(channels,''), COALESCE(direction,''), COALESCE(peer,''), started_at, ended_at,
	COALESCE(error,''), upload_attempts, notify_attempts, notified_at, last_attempt_at, created_at
	FROM call_recordings`

type scannable interface {
	Scan(dest ...any) error
}

func scanRecording(sc scannable) (*RecordingRow, error) {
	var r RecordingRow
	var status string
	err := sc.Scan(&r.CallID, &r.SessionID, &r.ClinicID, &status, &r.LocalPath, &r.B2Key,
		&r.B2URL, &r.DurationMs, &r.Channels, &r.Direction, &r.Peer, &r.StartedAt, &r.EndedAt,
		&r.Err, &r.UploadAttempts, &r.NotifyAttempts, &r.NotifiedAt, &r.LastAttemptAt, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	r.Status = RecordingStatus(status)
	return &r, nil
}
