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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeDummyWav(t *testing.T, dir, name string, payloadBytes int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	body := append([]byte("RIFF\x24\x00\x00\x00WAVE"), make([]byte, payloadBytes)...)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeB2 answers PUT (store size) and HEAD (echo size) like a bucket does.
func fakeB2(t *testing.T) (*httptest.Server, func() (method, path string, body []byte)) {
	t.Helper()
	var mu sync.Mutex
	var size int
	var lastMethod, lastPath string
	var lastBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		lastMethod, lastPath = r.Method, r.URL.Path
		switch r.Method {
		case http.MethodPut:
			lastBody, _ = io.ReadAll(r.Body)
			size = len(lastBody)
			w.WriteHeader(http.StatusOK)
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(size))
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	return srv, func() (string, string, []byte) {
		mu.Lock()
		defer mu.Unlock()
		return lastMethod, lastPath, lastBody
	}
}

func TestRecordingHandoffUploadsVerifiesAndNotifies(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()
	const callID = "call-h1"

	b2, b2State := fakeB2(t)
	defer b2.Close()

	var gotSig string
	var payload recordingReadyPayload
	mocho := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-ArendCalls-Signature")
		mac := hmac.New(sha256.New, []byte("s3cr3t"))
		mac.Write(body)
		if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); gotSig != want {
			t.Errorf("bad signature: got %s want %s", gotSig, want)
		}
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer mocho.Close()

	if err := store.saveGlobalConfig(ctx, RecordingConfig{

		B2Endpoint: b2.URL, B2Region: "us-west-004", B2Bucket: "recs",
		B2KeyID: "kid", B2AppKey: "akey", B2Prefix: "tenantA",
		WebhookURL: mocho.URL, WebhookSecret: "s3cr3t", URLTTLSeconds: 900,
	}, ctl.secrets); err != nil {
		t.Fatal(err)
	}

	local := writeDummyWav(t, dir, callID+".wav", 4096)
	if err := store.begin(ctx, RecordingRow{
		CallID: callID, SessionID: "sess-1", ClinicID: "clinic-7", LocalPath: local,
		Channels: "stereo", Direction: "outbound", StartedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.finishRecording(ctx, callID, 143000, time.Now().UnixMilli(), RecStatusUploading); err != nil {
		t.Fatal(err)
	}

	ctl.processRecording(callID)

	method, path, body := b2State()
	if method != http.MethodHead {
		t.Errorf("last B2 call = %s, want HEAD (verify after PUT)", method)
	}
	if path != "/recs/tenantA/recordings/clinic-7/2026/09/"+callID+".wav" {
		t.Errorf("B2 key path = %s", path)
	}
	if !strings.HasPrefix(string(body), "RIFF") {
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
		t.Error("local WAV should be deleted after verified upload")
	}
}

func TestRecordingHandoffFailsOnSizeMismatch(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()
	const callID = "call-mismatch"

	// B2 PUT succeeds but HEAD reports a different size.
	b2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "999999")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer b2.Close()
	mocho := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer mocho.Close()

	_ = store.saveGlobalConfig(ctx, RecordingConfig{
		B2Endpoint: b2.URL, B2Bucket: "recs", B2KeyID: "k", B2AppKey: "a",
		WebhookURL: mocho.URL, WebhookSecret: "x",
	}, ctl.secrets)
	local := writeDummyWav(t, dir, callID+".wav", 100)
	_ = store.begin(ctx, RecordingRow{CallID: callID, SessionID: "sess-1", LocalPath: local, Channels: "stereo", StartedAt: time.Now().UnixMilli()})
	_ = store.finishRecording(ctx, callID, 8000, time.Now().UnixMilli(), RecStatusUploading)

	ctl.processRecording(callID)

	row, _ := store.get(ctx, callID)
	if row.Status != RecStatusFailed {
		t.Errorf("status = %q, want failed on size mismatch", row.Status)
	}
	if !strings.Contains(row.Err, "size mismatch") {
		t.Errorf("error = %q, want size mismatch", row.Err)
	}
	if _, err := os.Stat(local); err != nil {
		t.Error("local WAV must be kept when verification fails")
	}
}

func TestRecordingHandoffRetriesOnWebhookFailure(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()
	const callID = "call-h2"

	b2, _ := fakeB2(t)
	defer b2.Close()
	mochoFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer mochoFail.Close()

	_ = store.saveGlobalConfig(ctx, RecordingConfig{
		B2Endpoint: b2.URL, B2Bucket: "recs", B2KeyID: "k", B2AppKey: "a",
		WebhookURL: mochoFail.URL, WebhookSecret: "x",
	}, ctl.secrets)
	local := writeDummyWav(t, dir, callID+".wav", 512)
	_ = store.begin(ctx, RecordingRow{CallID: callID, SessionID: "sess-1", LocalPath: local, Channels: "stereo", StartedAt: time.Now().UnixMilli()})
	_ = store.finishRecording(ctx, callID, 9000, time.Now().UnixMilli(), RecStatusUploading)

	ctl.processRecording(callID)

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

func TestRecordingHandoffIncompleteConfigKeepsLocal(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()
	const callID = "call-h3"

	// Only B2, no webhook — config is not complete.
	_ = store.saveGlobalConfig(ctx, RecordingConfig{
		B2Endpoint: "https://x", B2Bucket: "b", B2KeyID: "k", B2AppKey: "a",
	}, ctl.secrets)
	local := writeDummyWav(t, dir, callID+".wav", 256)
	_ = store.begin(ctx, RecordingRow{CallID: callID, SessionID: "sess-x", LocalPath: local, Channels: "stereo", StartedAt: time.Now().UnixMilli()})
	_ = store.finishRecording(ctx, callID, 7000, time.Now().UnixMilli(), RecStatusUploading)

	ctl.processRecording(callID)

	row, _ := store.get(ctx, callID)
	if row.Status != RecStatusUploading {
		t.Errorf("status = %q, want uploading (config incomplete, awaiting setup)", row.Status)
	}
	if _, err := os.Stat(local); err != nil {
		t.Error("local WAV must be kept when the destination is not fully configured")
	}
}
