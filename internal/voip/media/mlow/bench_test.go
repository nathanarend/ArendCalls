package mlow

import (
	"math"
	"math/rand"
	"testing"
)

// Bench fixtures: one 60 ms / 960-sample frame @ 16 kHz for each signal class the
// encoder hot path can hit. voiced (harmonic tone) exercises the full pitch +
// CELP search; unvoiced (white noise) skips the closed-loop pitch search but
// still runs the codebook search; silent is the keepalive / speech-gap frame.

func voicedFrame(frameIdx int) []float32 {
	pcm := make([]float32, 960)
	for i := 0; i < 960; i++ {
		t := float64(frameIdx*960+i) / 16000.0
		// fundamental + a couple of harmonics ≈ voiced speech
		pcm[i] = float32(0.45*math.Sin(2*math.Pi*180*t) +
			0.20*math.Sin(2*math.Pi*360*t) +
			0.10*math.Sin(2*math.Pi*540*t))
	}
	return pcm
}

func unvoicedFrame(seed int64) []float32 {
	r := rand.New(rand.NewSource(seed))
	pcm := make([]float32, 960)
	for i := range pcm {
		pcm[i] = float32((r.Float64()*2 - 1) * 0.3)
	}
	return pcm
}

func silentFrame() []float32 { return make([]float32, 960) }

// realisticStream is ~50 % speech / ~50 % silence, the mix a live call produces.
func realisticStream(n int) [][]float32 {
	out := make([][]float32, n)
	for i := 0; i < n; i++ {
		switch {
		case i%8 < 3:
			out[i] = voicedFrame(i)
		case i%8 == 3:
			out[i] = unvoicedFrame(int64(i))
		default:
			out[i] = silentFrame()
		}
	}
	return out
}

func BenchmarkEncodeVoiced(b *testing.B) {
	enc := NewMlowEncoder()
	f := voicedFrame(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Encode(f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeUnvoiced(b *testing.B) {
	enc := NewMlowEncoder()
	f := unvoicedFrame(1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Encode(f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeSilent(b *testing.B) {
	enc := NewMlowEncoder()
	f := silentFrame()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Encode(f); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncodeStream is the headline number: one encoder chewing a realistic
// speech/silence mix, ns/op ≈ cost of one outbound frame on an active call.
func BenchmarkEncodeStream(b *testing.B) {
	enc := NewMlowEncoder()
	frames := realisticStream(240) // ~14.4 s of audio
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Encode(frames[i%len(frames)]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncodeParallel models many concurrent calls: GOMAXPROCS encoders each
// on their own stream. Throughput scaling here is the density proxy.
func BenchmarkEncodeParallel(b *testing.B) {
	frames := realisticStream(240)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		enc := NewMlowEncoder()
		i := 0
		for pb.Next() {
			if _, err := enc.Encode(frames[i%len(frames)]); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

func BenchmarkDecodeStream(b *testing.B) {
	enc := NewMlowEncoder()
	src := realisticStream(240)
	wire := make([][]byte, len(src))
	for i, f := range src {
		w, err := enc.Encode(f)
		if err != nil {
			b.Fatal(err)
		}
		cp := make([]byte, len(w))
		copy(cp, w)
		wire[i] = cp
	}
	dec := NewMlowDecoder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dec.Decode(wire[i%len(wire)])
	}
}
