package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeDummyWav(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, append([]byte("RIFF\x24\x00\x00\x00WAVE"), make([]byte, 64)...), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRecordingHandoffUploadsAndNotifies(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()
	const callID = "call-h1"

	var b2Method, b2Path string
	var b2Body []byte
	b2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b2Method, b2Path = r.Method, r.URL.Path
		b2Body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer b2.Close()

	var gotSig string
	var payload recordingReadyPayload
	mocho := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-ArendCalls-Signature")
		mac := hmac.New(sha256.New, []byte("s3cr3t"))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if gotSig != want {
			t.Errorf("bad signature: got %s want %s", gotSig, want)
		}
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer mocho.Close()

	if err := store.saveConfig(ctx, RecordingConfig{
		SessionID: "sess-1", Enabled: true,
		B2Endpoint: b2.URL, B2Region: "us-west-004", B2Bucket: "recs",
		B2KeyID: "kid", B2AppKey: "akey", B2Prefix: "tenantA",
		WebhookURL: mocho.URL, WebhookSecret: "s3cr3t", URLTTLSeconds: 900,
	}, ctl.secrets); err != nil {
		t.Fatal(err)
	}

	local := writeDummyWav(t, dir, callID+".wav")
	if err := store.begin(ctx, RecordingRow{
		CallID: callID, SessionID: "sess-1", ClinicID: "clinic-7", LocalPath: local,
		Channels: "stereo", Direction: "outbound", StartedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.finishRecording(ctx, callID, 143000, time.Now().UnixMilli(), RecStatusUploading); err != nil {
		t.Fatal(err)
	}

	ctl.handoff(callID)

	if b2Method != http.MethodPut {
		t.Errorf("B2 method = %s", b2Method)
	}
	if b2Path != "/recs/tenantA/recordings/clinic-7/2026/09/"+callID+".wav" {
		t.Errorf("B2 key path = %s", b2Path)
	}
	if len(b2Body) == 0 || !strings.HasPrefix(string(b2Body), "RIFF") {
		t.Errorf("B2 did not receive the WAV bytes")
	}

	row, err := store.get(ctx, callID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != RecStatusReady {
		t.Errorf("status = %q, want ready", row.Status)
	}
	if row.NotifiedAt == 0 {
		t.Error("notified_at not set after 2xx from Mocho")
	}
	if row.B2Key != "tenantA/recordings/clinic-7/2026/09/"+callID+".wav" {
		t.Errorf("b2_key = %q", row.B2Key)
	}
	if payload.DurationSeconds != 143 || payload.ClinicID != "clinic-7" || payload.Channels != "stereo" {
		t.Errorf("webhook payload wrong: %+v", payload)
	}
	if payload.RecordingURL == "" || !strings.Contains(payload.RecordingURL, "X-Amz-Signature=") {
		t.Errorf("expected presigned recordingUrl, got %q", payload.RecordingURL)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Error("local WAV should be deleted after successful upload")
	}
}

func TestRecordingHandoffRetriesOnWebhookFailure(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()
	const callID = "call-h2"

	b2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer b2.Close()
	mochoFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer mochoFail.Close()

	if err := store.saveConfig(ctx, RecordingConfig{
		SessionID: "sess-1", Enabled: true,
		B2Endpoint: b2.URL, B2Bucket: "recs", B2KeyID: "k", B2AppKey: "a",
		WebhookURL: mochoFail.URL, WebhookSecret: "x",
	}, ctl.secrets); err != nil {
		t.Fatal(err)
	}
	local := writeDummyWav(t, dir, callID+".wav")
	_ = store.begin(ctx, RecordingRow{CallID: callID, SessionID: "sess-1", LocalPath: local, Channels: "stereo", StartedAt: time.Now().UnixMilli()})
	_ = store.finishRecording(ctx, callID, 5000, time.Now().UnixMilli(), RecStatusUploading)

	ctl.handoff(callID)

	row, _ := store.get(ctx, callID)
	if row.Status != RecStatusReady {
		t.Errorf("status = %q, want ready (upload ok, only webhook failed)", row.Status)
	}
	if row.NotifiedAt != 0 {
		t.Error("notified_at should stay 0 after webhook 500")
	}
	if row.NotifyAttempts != 1 {
		t.Errorf("notify_attempts = %d, want 1", row.NotifyAttempts)
	}
	// The row is still pending and will be retried by the sweep.
	pend, _ := store.pendingHandoff(ctx)
	found := false
	for _, p := range pend {
		if p.CallID == callID {
			found = true
		}
	}
	if !found {
		t.Error("un-acked recording should appear in pendingHandoff")
	}
}

func TestBackoffSchedule(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour, time.Hour}
	for i, w := range want {
		if got := backoffFor(i + 1); got != w {
			t.Errorf("backoffFor(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestRecordingHandoffNoB2ConfigKeepsLocal(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()
	const callID = "call-h3"

	local := writeDummyWav(t, dir, callID+".wav")
	_ = store.begin(ctx, RecordingRow{CallID: callID, SessionID: "sess-x", LocalPath: local, Channels: "stereo", StartedAt: time.Now().UnixMilli()})
	_ = store.finishRecording(ctx, callID, 1000, time.Now().UnixMilli(), RecStatusUploading)

	ctl.handoff(callID)

	row, _ := store.get(ctx, callID)
	if row.Status != RecStatusUploading {
		t.Errorf("status = %q, want uploading (no B2 config, awaiting setup)", row.Status)
	}
	if _, err := os.Stat(local); err != nil {
		t.Error("local WAV must be kept when B2 is not configured")
	}
}
