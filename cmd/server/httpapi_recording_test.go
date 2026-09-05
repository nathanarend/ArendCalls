package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// newRecTestServer builds a server wired with the recording store/controller and
// one pre-registered session, without the whatsmeow client plumbing.
func newRecTestServer(t *testing.T) (*server, *httptest.Server, string) {
	t.Helper()
	ctx := context.Background()
	db, err := openDB(filepath.Join(t.TempDir(), "rec_http.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	sstore, err := newSessionStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	rstore, err := newRecordingStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	broker := NewBroker()
	mgr := newSessionManager(ctx, container, broker, sstore, waLog.Noop, log, 0)
	rec := newRecordingController(ctx, rstore, newSecretBox(log), t.TempDir(), log)
	mgr.rec = rec

	sid := "sess-http-1"
	if err := sstore.insert(ctx, sid, "HTTP Rec"); err != nil {
		t.Fatal(err)
	}
	mgr.register(&Session{id: sid, name: "HTTP Rec", mgr: mgr, log: log, reg: newCallRegistry()})

	srv := &server{sessions: mgr, broker: broker, recStore: rstore, rec: rec, log: log, apiKey: "k"}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return srv, ts, sid
}

func recDo(t *testing.T, ts *httptest.Server, method, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rdr)
	req.Header.Set("X-Api-Key", "k")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	_ = json.Unmarshal(raw, &out)
	return resp, out
}

func TestRecordingConfigEndpointPartialUpdateAndRedaction(t *testing.T) {
	_, ts, sid := newRecTestServer(t)
	base := "/api/sessions/" + sid + "/recording-config"

	// Initial: disabled, nothing set.
	resp, got := recDo(t, ts, http.MethodGet, base, nil)
	if resp.StatusCode != 200 || got["enabled"] != false {
		t.Fatalf("initial GET: %d %v", resp.StatusCode, got)
	}

	// PATCH B2 creds + enable.
	resp, got = recDo(t, ts, http.MethodPatch, base, map[string]any{
		"enabled": true, "b2Endpoint": "https://s3.us-west-004.backblazeb2.com",
		"b2Bucket": "recs", "b2KeyId": "kid", "b2AppKey": "supersecret", "urlTtlSeconds": 600,
	})
	if resp.StatusCode != 200 {
		t.Fatalf("PATCH failed: %d %v", resp.StatusCode, got)
	}
	if got["b2AppKeySet"] != true || got["b2AppKey"] != nil {
		t.Errorf("app key should be redacted: %v", got)
	}

	// PATCH only webhookUrl — B2 creds must survive.
	resp, _ = recDo(t, ts, http.MethodPatch, base, map[string]any{"webhookUrl": "https://mocho.example/hook"})
	if resp.StatusCode != 200 {
		t.Fatal("second PATCH failed")
	}
	_, got = recDo(t, ts, http.MethodGet, base, nil)
	if got["b2Bucket"] != "recs" || got["b2AppKeySet"] != true {
		t.Errorf("partial PATCH clobbered B2 config: %v", got)
	}
	if got["webhookUrl"] != "https://mocho.example/hook" {
		t.Errorf("webhookUrl not persisted: %v", got)
	}
	if got["enabled"] != true || got["urlTtlSeconds"] != float64(600) {
		t.Errorf("scalars not persisted: %v", got)
	}
}

func TestRecordingInfoEndpoint(t *testing.T) {
	srv, ts, sid := newRecTestServer(t)
	ctx := context.Background()

	// Unknown call -> 404.
	resp, _ := recDo(t, ts, http.MethodGet, "/api/sessions/"+sid+"/calls/nope/recording-info", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("unknown recording: got %d", resp.StatusCode)
	}

	_ = srv.recStore.begin(ctx, RecordingRow{
		CallID: "c-info", SessionID: sid, ClinicID: "cl-1", LocalPath: "/x.wav",
		Channels: "stereo", Direction: "inbound", StartedAt: 1_757_070_000_000,
	})
	_ = srv.recStore.finishRecording(ctx, "c-info", 90_000, 1_757_070_090_000, RecStatusUploading)
	_ = srv.recStore.markUploaded(ctx, "c-info", "recordings/cl-1/2026/09/c-info.wav", "https://signed.example/x")

	resp, got := recDo(t, ts, http.MethodGet, "/api/sessions/"+sid+"/calls/c-info/recording-info", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("recording-info: got %d %v", resp.StatusCode, got)
	}
	if got["status"] != "ready" || got["durationSeconds"] != float64(90) ||
		got["recordingKey"] != "recordings/cl-1/2026/09/c-info.wav" || got["channels"] != "stereo" {
		t.Errorf("recording-info payload wrong: %v", got)
	}

	// A recording that belongs to another session must not leak.
	resp, _ = recDo(t, ts, http.MethodGet, "/api/sessions/other/calls/c-info/recording-info", nil)
	if resp.StatusCode == 200 {
		t.Error("recording-info leaked across sessions")
	}
}

func TestRecordingConfigDecryptsSecretThroughStore(t *testing.T) {
	srv, ts, sid := newRecTestServer(t)
	base := "/api/sessions/" + sid + "/recording-config"
	resp, _ := recDo(t, ts, http.MethodPatch, base, map[string]any{"b2AppKey": "roundtrip-me", "webhookSecret": "hmac-key"})
	if resp.StatusCode != 200 {
		t.Fatal("patch failed")
	}
	cfg, err := srv.recStore.config(context.Background(), sid, srv.rec.secrets)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.B2AppKey != "roundtrip-me" || cfg.WebhookSecret != "hmac-key" {
		t.Errorf("secret round-trip failed: %+v", cfg)
	}
}
