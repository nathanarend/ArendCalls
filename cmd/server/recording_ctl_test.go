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
	ctl := newRecordingController(context.Background(), store, newSecretBox(log), dir, log)
	return ctl, store, dir
}

func TestRecordingControllerWantRecording(t *testing.T) {
	ctl, store, _ := newTestRecCtl(t)

	// No config row: default off.
	if ctl.wantRecording("sess-1", nil) {
		t.Error("expected recording off by default")
	}
	// Explicit true from the Mocho POST wins.
	tru := true
	if !ctl.wantRecording("sess-1", &tru) {
		t.Error("explicit record=true should force recording")
	}
	// Per-session default on.
	if err := store.saveConfig(context.Background(), RecordingConfig{SessionID: "sess-1", Enabled: true}, ctl.secrets); err != nil {
		t.Fatal(err)
	}
	if !ctl.wantRecording("sess-1", nil) {
		t.Error("session default should enable recording for inbound")
	}
}

func TestRecordingControllerLifecycle(t *testing.T) {
	ctl, store, dir := newTestRecCtl(t)
	ctx := context.Background()
	const callID = "call-abc"

	ctl.arm(recMeta{callID: callID, sessionID: "sess-1", clinicID: "clinic-9", direction: "outbound", peer: "55110001@s.whatsapp.net"})
	if !ctl.armed(callID) {
		t.Fatal("call should be armed")
	}

	rec := ctl.onMediaConnected(callID)
	if rec == nil {
		t.Fatal("onMediaConnected returned nil recorder")
	}
	// Second call is a no-op and returns the same recorder.
	if got := ctl.onMediaConnected(callID); got != rec {
		t.Error("onMediaConnected should be idempotent")
	}

	frame := make([]float32, 320)
	for i := range frame {
		frame[i] = 0.3
	}
	for k := 0; k < 15; k++ {
		rec.WriteOperator(frame)
		rec.WritePeer(frame)
		time.Sleep(20 * time.Millisecond)
	}

	ctl.onCallEnded(callID)
	// onCallEnded fires the handoff in a goroutine; give it a beat.
	time.Sleep(100 * time.Millisecond)

	row, err := store.get(ctx, callID)
	if err != nil || row == nil {
		t.Fatalf("recording row missing: %v", err)
	}
	if row.Status != RecStatusUploading {
		t.Errorf("status = %q, want uploading (no B2 config)", row.Status)
	}
	if row.ClinicID != "clinic-9" {
		t.Errorf("clinic id = %q", row.ClinicID)
	}
	if row.DurationMs < 200 {
		t.Errorf("duration_ms = %d, want >=200", row.DurationMs)
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

func TestRecordingControllerReapStale(t *testing.T) {
	ctl, store, _ := newTestRecCtl(t)
	ctx := context.Background()

	if err := store.begin(ctx, RecordingRow{
		CallID: "stale-1", SessionID: "s", LocalPath: "/x.wav", Channels: "stereo",
		StartedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	ctl.resumePendingHandoffs()

	row, err := store.get(ctx, "stale-1")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != RecStatusFailed {
		t.Errorf("stale recording status = %q, want failed", row.Status)
	}
}
