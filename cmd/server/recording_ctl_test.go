package main

import (
	"context"
	"encoding/binary"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestRecCtl(t *testing.T) (*recordingController, *recordingStore, string) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "rec.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := newRecordingStore(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctl := newRecordingController(context.Background(), store, newSecretBox(log), dir, 2, log)
	return ctl, store, dir
}

func TestRecordingControllerLifecycle(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()
	const callID = "call-abc"

	ctl.arm(recMeta{callID: callID, sessionID: "sess-1", clinicID: "clinic-9", direction: "outbound", peer: "55110001@s.whatsapp.net"})
	if !ctl.armed(callID) {
		t.Fatal("call should be armed")
	}

	rec := ctl.onAnswered(callID)
	if rec == nil {
		t.Fatal("onAnswered returned nil recorder")
	}
	if got := ctl.onAnswered(callID); got != rec {
		t.Error("onAnswered should be idempotent")
	}

	frame := make([]float32, 320)
	for i := range frame {
		frame[i] = 0.3
	}
	for k := 0; k < 320; k++ { // ~6.4s of audio, over the 5s floor
		rec.WriteOperator(frame)
		rec.WritePeer(frame)
		time.Sleep(20 * time.Millisecond)
	}

	ctl.onCallEnded(callID)

	row, err := store.get(ctx, callID)
	if err != nil || row == nil {
		t.Fatalf("recording row missing: %v", err)
	}
	if row.Status != RecStatusUploading {
		t.Errorf("status = %q, want uploading (queued, no B2 config)", row.Status)
	}
	if row.ClinicID != "clinic-9" {
		t.Errorf("clinic id = %q", row.ClinicID)
	}
	if row.DurationMs < 5000 {
		t.Errorf("duration_ms = %d, want >=5000", row.DurationMs)
	}
	if row.Channels != "stereo" {
		t.Errorf("channels = %q", row.Channels)
	}

	wantPath := filepath.Join(dir, "sess-1")
	b, err := os.ReadFile(row.LocalPath)
	if err != nil {
		t.Fatalf("wav not on disk: %v", err)
	}
	if string(b[0:4]) != "RIFF" || binary.LittleEndian.Uint16(b[22:24]) != 2 {
		t.Error("local file is not a stereo WAV")
	}
	if filepath.Dir(filepath.Dir(row.LocalPath)) != wantPath {
		t.Errorf("path layout = %q, want under %q/<month>/", row.LocalPath, wantPath)
	}
	if ctl.armed(callID) {
		t.Error("call should be disarmed after end")
	}
}

func TestRecordingControllerSkipsShortCall(t *testing.T) {
	ctl, store, _ := newTestRecCtl(t)
	const callID = "call-short"
	ctl.arm(recMeta{callID: callID, sessionID: "sess-1", direction: "outbound"})
	rec := ctl.onAnswered(callID)
	if rec == nil {
		t.Fatal("onAnswered nil")
	}
	frame := make([]float32, 320)
	for k := 0; k < 40; k++ { // ~0.8s — under the 5s floor
		rec.WriteOperator(frame)
		time.Sleep(20 * time.Millisecond)
	}
	started, _ := store.get(context.Background(), callID)
	local := started.LocalPath
	ctl.onCallEnded(callID)

	row, _ := store.get(context.Background(), callID)
	if row == nil || row.Status != RecStatusSkipped {
		t.Fatalf("status = %v, want skipped", row)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Error("short recording WAV should be deleted")
	}
}

func TestRecordingControllerSalvagesOnRestart(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()

	// A row left in "recording" with an intact WAV on disk must be salvaged,
	// not failed.
	goodPath := filepath.Join(dir, "sess-1", "2026-09", "good.wav")
	_ = os.MkdirAll(filepath.Dir(goodPath), 0o755)
	// 44-byte header + ~6s of stereo 16k audio (64000 B/s).
	_ = os.WriteFile(goodPath, make([]byte, 44+64000*6), 0o644)
	_ = store.begin(ctx, RecordingRow{CallID: "good", SessionID: "sess-1", LocalPath: goodPath, Channels: "stereo", StartedAt: time.Now().UnixMilli()})

	// A row whose file is gone → failed.
	_ = store.begin(ctx, RecordingRow{CallID: "gone", SessionID: "sess-1", LocalPath: "/nope/x.wav", Channels: "stereo", StartedAt: time.Now().UnixMilli()})

	ctl.resumeOnBoot()

	good, _ := store.get(ctx, "good")
	if good.Status != RecStatusUploading {
		t.Errorf("salvaged recording status = %q, want uploading", good.Status)
	}
	if good.DurationMs < 5000 {
		t.Errorf("salvaged duration_ms = %d, want ~6000", good.DurationMs)
	}
	gone, _ := store.get(ctx, "gone")
	if gone.Status != RecStatusFailed {
		t.Errorf("lost recording status = %q, want failed", gone.Status)
	}
}
