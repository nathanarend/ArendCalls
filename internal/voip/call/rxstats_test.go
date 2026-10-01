package call

import (
	"testing"
	"time"
)

func frameOf(v float32) []float32 {
	f := make([]float32, 320)
	for i := range f {
		f[i] = v
	}
	return f
}

func TestRxStatsCountsOnlySpeechToSpeechGaps(t *testing.T) {
	var s rxStats
	speech, silence := frameOf(0.3), frameOf(0)
	ms := func(n int64) int64 { return n * int64(time.Millisecond) }

	s.decoded(ms(1000), speech, "gru")
	s.decoded(ms(1020), speech, "gru")
	s.decoded(ms(1820), speech, "gru")  // fala → fala, 800 ms: buraco
	s.decoded(ms(3000), silence, "gru") // fala → silêncio: DTX
	s.decoded(ms(4000), speech, "lga")  // silêncio → fala: DTX
	s.decoded(ms(4700), speech, "lga")  // fala → fala, 700 ms: buraco
	s.decoded(ms(4500), speech, "gru")  // fora de ordem: não mexe no relógio
	s.dropped()
	s.relocked()

	got := s.snapshot()
	if got.Packets != 7 {
		t.Errorf("packets = %d, want 7", got.Packets)
	}
	if got.SpeechGaps != 2 {
		t.Errorf("speech gaps = %d, want 2", got.SpeechGaps)
	}
	if got.MaxSpeechGap != 800*time.Millisecond {
		t.Errorf("max gap = %v, want 800ms", got.MaxSpeechGap)
	}
	if got.Undecodable != 1 || got.Relocks != 1 {
		t.Errorf("undecodable/relocks = %d/%d, want 1/1", got.Undecodable, got.Relocks)
	}
	if got.Relay != "gru" {
		t.Errorf("relay = %q, want gru (5 de 7 pacotes)", got.Relay)
	}
}

func TestIsSpeechThreshold(t *testing.T) {
	if isSpeech(frameOf(0.005)) {
		t.Error("-46 dBFS should be silence")
	}
	if !isSpeech(frameOf(0.02)) {
		t.Error("-34 dBFS should be speech")
	}
	if isSpeech(nil) {
		t.Error("empty frame should be silence")
	}
}
