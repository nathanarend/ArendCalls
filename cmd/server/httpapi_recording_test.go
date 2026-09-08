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
	rec := newRecordingController(ctx, rstore, newSecretBox(log), t.TempDir(), 2, log)
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

var completeConfigBody = map[string]any{
	"b2Endpoint": "https://s3.us-west-004.backblazeb2.com",
	"b2Bucket":   "recs", "b2KeyId": "kid", "b2AppKey": "supersecret",
	"webhookUrl": "https://mocho.example/hook", "webhookSecret": "hmac-key",
	"urlTtlSeconds": 600,
}

const recCfgBase = "/api/recording-config"

func TestRecordingConfigRejectsPartial(t *testing.T) {
	_, ts, _ := newRecTestServer(t)

	// B2 without the webhook → rejected.
	resp, got := recDo(t, ts, http.MethodPatch, recCfgBase, map[string]any{
		"b2Endpoint": "https://x", "b2Bucket": "b", "b2KeyId": "k", "b2AppKey": "a",
	})
	if resp.StatusCode != 400 {
		t.Fatalf("partial config: got %d, want 400 (%v)", resp.StatusCode, got)
	}
	miss, _ := got["missing"].([]any)
	if len(miss) != 2 { // webhookUrl, webhookSecret
		t.Errorf("missing = %v, want [webhookSecret webhookUrl]", got["missing"])
	}

	// Nothing was persisted.
	_, got = recDo(t, ts, http.MethodGet, recCfgBase, nil)
	if got["b2Bucket"] != "" || got["complete"] != false {
		t.Errorf("rejected config leaked into storage: %v", got)
	}
}

func TestRecordingConfigAcceptsCompleteAndEmpty(t *testing.T) {
	_, ts, _ := newRecTestServer(t)

	resp, got := recDo(t, ts, http.MethodPatch, recCfgBase, completeConfigBody)
	if resp.StatusCode != 200 {
		t.Fatalf("complete config rejected: %d %v", resp.StatusCode, got)
	}
	if got["complete"] != true || got["b2AppKeySet"] != true || got["webhookSecretSet"] != true || got["b2AppKey"] != nil {
		t.Errorf("redacted config wrong: %v", got)
	}

	// Clearing everything is allowed.
	resp, _ = recDo(t, ts, http.MethodPatch, recCfgBase, map[string]any{
		"b2Endpoint": "", "b2Bucket": "", "b2KeyId": "", "b2AppKey": "",
		"webhookUrl": "", "webhookSecret": "", "urlTtlSeconds": 0,
	})
	if resp.StatusCode != 200 {
		t.Fatalf("clearing config rejected: %d", resp.StatusCode)
	}
	_, got = recDo(t, ts, http.MethodGet, recCfgBase, nil)
	if got["complete"] != false || got["b2Bucket"] != "" {
		t.Errorf("config not cleared: %v", got)
	}
}

func TestRecordingConfigPartialUpdateKeepsComplete(t *testing.T) {
	srv, ts, _ := newRecTestServer(t)

	if resp, got := recDo(t, ts, http.MethodPatch, recCfgBase, completeConfigBody); resp.StatusCode != 200 {
		t.Fatalf("seed config: %d %v", resp.StatusCode, got)
	}
	// Change only the bucket + toggle recordInbound — secrets must survive and stay complete.
	if resp, _ := recDo(t, ts, http.MethodPatch, recCfgBase, map[string]any{"b2Bucket": "recs-2", "recordInbound": true}); resp.StatusCode != 200 {
		t.Fatal("partial PATCH failed")
	}
	_, got := recDo(t, ts, http.MethodGet, recCfgBase, nil)
	if got["b2Bucket"] != "recs-2" || got["complete"] != true || got["recordInbound"] != true {
		t.Errorf("partial PATCH broke config: %v", got)
	}
	cfg, err := srv.recStore.globalConfig(context.Background(), srv.rec.secrets)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.B2AppKey != "supersecret" || cfg.WebhookSecret != "hmac-key" || !cfg.RecordInbound {
		t.Errorf("secrets/toggle lost on partial PATCH: %+v", cfg)
	}
}

func TestRecordingInfoEndpoint(t *testing.T) {
	srv, ts, sid := newRecTestServer(t)
	ctx := context.Background()

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

	resp, _ = recDo(t, ts, http.MethodGet, "/api/sessions/other/calls/c-info/recording-info", nil)
	if resp.StatusCode == 200 {
		t.Error("recording-info leaked across sessions")
	}
}

func TestListRecordingsEndpoint(t *testing.T) {
	srv, ts, sid := newRecTestServer(t)
	ctx := context.Background()

	_ = srv.recStore.begin(ctx, RecordingRow{CallID: "r1", SessionID: sid, LocalPath: "/a.wav", Channels: "stereo", Direction: "outbound", Peer: "551199@s.whatsapp.net", StartedAt: 1000})
	_ = srv.recStore.finishRecording(ctx, "r1", 12000, 2000, RecStatusUploading)
	_ = srv.recStore.begin(ctx, RecordingRow{CallID: "r2", SessionID: sid, LocalPath: "/b.wav", Channels: "stereo", StartedAt: 3000})
	_ = srv.recStore.markSkipped(ctx, "r2", 2000, 4000, "recording shorter than 5s")

	resp, got := recDo(t, ts, http.MethodGet, "/api/sessions/"+sid+"/recordings", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("list: %d %v", resp.StatusCode, got)
	}
	if got["configured"] != false {
		t.Errorf("configured = %v, want false", got["configured"])
	}
	recs, _ := got["recordings"].([]any)
	if len(recs) != 2 {
		t.Fatalf("recordings len = %d, want 2", len(recs))
	}
	stats, _ := got["stats"].(map[string]any)
	if stats["uploading"] != float64(1) || stats["skipped"] != float64(1) {
		t.Errorf("stats = %v", stats)
	}
}
