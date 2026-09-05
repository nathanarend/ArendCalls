package recording

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readWav(t *testing.T, path string) (fmtChannels, fmtRate int, samples []int16) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read wav: %v", err)
	}
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatalf("not a RIFF/WAVE file")
	}
	if got := binary.LittleEndian.Uint32(b[4:8]); got != uint32(36+len(b)-44) {
		t.Errorf("RIFF size = %d, want %d", got, 36+len(b)-44)
	}
	fmtChannels = int(binary.LittleEndian.Uint16(b[22:24]))
	fmtRate = int(binary.LittleEndian.Uint32(b[24:28]))
	dataBytes := int(binary.LittleEndian.Uint32(b[40:44]))
	if dataBytes != len(b)-44 {
		t.Errorf("data size = %d, want %d", dataBytes, len(b)-44)
	}
	for i := 44; i+1 < len(b); i += 2 {
		samples = append(samples, int16(binary.LittleEndian.Uint16(b[i:])))
	}
	return
}

func TestRecorderInterleavesStereo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.wav")
	r, err := New(Options{Path: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// One 20 ms frame of full-scale positive on the operator leg, full-scale
	// negative on the peer leg.
	op := make([]float32, 320)
	peer := make([]float32, 320)
	for i := range op {
		op[i] = 0.5
		peer[i] = -0.5
	}
	for k := 0; k < 25; k++ {
		r.WriteOperator(op)
		r.WritePeer(peer)
		time.Sleep(20 * time.Millisecond)
	}

	dur, err := r.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if dur < 300*time.Millisecond {
		t.Fatalf("duration too short: %v", dur)
	}

	ch, rate, samples := readWav(t, path)
	if ch != 2 || rate != 16000 {
		t.Fatalf("format: channels=%d rate=%d", ch, rate)
	}
	if len(samples) < 2 {
		t.Fatal("no samples written")
	}
	// Find a frame where both legs have real audio (skip leading silence pad).
	var checked bool
	for i := 0; i+1 < len(samples); i += 2 {
		l, rr := samples[i], samples[i+1]
		if l > 1000 && rr < -1000 {
			checked = true
			break
		}
	}
	if !checked {
		t.Error("expected an interleaved frame with L>0 (operator) and R<0 (peer)")
	}
}

func TestRecorderPadsSilenceForMissingLeg(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.wav")
	r, err := New(Options{Path: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Only the operator leg produces audio; peer stays silent (hold scenario).
	op := make([]float32, 320)
	for i := range op {
		op[i] = 0.8
	}
	for k := 0; k < 10; k++ {
		r.WriteOperator(op)
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, _, samples := readWav(t, path)
	for i := 1; i < len(samples); i += 2 {
		if samples[i] != 0 {
			t.Fatalf("peer channel sample %d = %d, want silence", i, samples[i])
		}
	}
}

func TestRecorderDurationCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.wav")
	var limitFired bool
	done := make(chan struct{})
	r, err := New(Options{
		Path:        path,
		MaxDuration: 200 * time.Millisecond,
		OnLimit: func() {
			limitFired = true
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	op := make([]float32, 320)
	go func() {
		for k := 0; k < 100; k++ {
			r.WriteOperator(op)
			r.WritePeer(op)
			time.Sleep(20 * time.Millisecond)
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("OnLimit never fired")
	}
	if !limitFired {
		t.Fatal("limit callback not marked")
	}
	dur, err := r.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if dur > 400*time.Millisecond {
		t.Errorf("recording exceeded cap: %v", dur)
	}
	_, _, samples := readWav(t, path)
	if len(samples) == 0 {
		t.Fatal("capped recording has no audio")
	}
}
