package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
)

type sessionRow struct {
	ID           string
	Name         string
	JID          string
	WebhookURL   string
	PanelInbound bool // show/ring incoming calls for this account in the panel UI
}

type sessionStore struct{ db *sql.DB }

func newSessionStore(ctx context.Context, db *sql.DB) (*sessionStore, error) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sessions (
		id   TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		jid  TEXT,
		webhook_url TEXT,
		panel_inbound INTEGER DEFAULT 1
	)`)
	if err != nil {
		return nil, err
	}
	// Add columns if they don't exist (for existing DBs).
	db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN webhook_url TEXT`)
	db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN panel_inbound INTEGER DEFAULT 1`)
	return &sessionStore{db: db}, nil
}

func newSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *sessionStore) list(ctx context.Context) ([]sessionRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(jid, ''), COALESCE(webhook_url, ''), COALESCE(panel_inbound, 1) FROM sessions ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sessionRow
	for rows.Next() {
		var r sessionRow
		var panelInbound int
		if err := rows.Scan(&r.ID, &r.Name, &r.JID, &r.WebhookURL, &panelInbound); err != nil {
			return nil, err
		}
		r.PanelInbound = panelInbound != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sessionStore) insert(ctx context.Context, id, name string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, name, jid) VALUES (?, ?, NULL)`, id, name)
	return err
}

func (s *sessionStore) setJID(ctx context.Context, id, jid string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET jid = ? WHERE id = ?`, jid, id)
	return err
}

func (s *sessionStore) delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *sessionStore) updateName(ctx context.Context, id, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET name = ? WHERE id = ?`, name, id)
	return err
}

func (s *sessionStore) setWebhookURL(ctx context.Context, id, webhookURL string) error {
	var err error
	if webhookURL == "" {
		_, err = s.db.ExecContext(ctx, `UPDATE sessions SET webhook_url = NULL WHERE id = ?`, id)
	} else {
		_, err = s.db.ExecContext(ctx, `UPDATE sessions SET webhook_url = ? WHERE id = ?`, webhookURL, id)
	}
	return err
}

func (s *sessionStore) setPanelInbound(ctx context.Context, id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET panel_inbound = ? WHERE id = ?`, v, id)
	return err
}
