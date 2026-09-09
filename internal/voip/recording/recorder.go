// Package recording captures the audio of a live call on the server side.
//
// The call relay already handles both voice legs as 16 kHz, 16-bit, mono PCM.
// A Recorder taps those samples on a parallel branch — it never sits in the live
// media path, so feeding it can neither add latency nor drop a conversation
// frame. The two legs arrive on independent callbacks at their own pace; a
// wall-clock ticker interleaves them into a streaming stereo WAV:
//
//	left  channel (L) = operator  (atendente)
//	right channel (R) = peer      (paciente/cliente)
//
// When a leg has no samples ready for a slice of wall-clock time (mute, hold,
// jitter) that slice is padded with silence, keeping both channels aligned to
// real time regardless of arrival jitter.
package recording

import (
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WavHeaderBytes is the fixed size of the WAV header written before any samples.
const WavHeaderBytes = 44

const (
	sampleRate   = 16000
	channels     = 2
	tickInterval = 20 * time.Millisecond
	// nsPerSample is the wall-clock duration of one mono sample frame.
	nsPerSample = int64(time.Second) / sampleRate
	// maxPendingSamples caps each per-leg buffer (~30 s) so a stuck consumer or a
	// misbehaving producer can never grow memory without bound.
	maxPendingSamples = sampleRate * 30
)

// Options configures a Recorder.
type Options struct {
	// Path is the destination .wav file. Parent dirs are created.
	Path string
	// MaxDuration auto-stops the recording once reached (call left off-hook).
	// Zero means no cap.
	MaxDuration time.Duration
	// OnLimit is invoked (once, in its own goroutine) when MaxDuration is hit.
	OnLimit func()
	Log     *slog.Logger
}

// Recorder mixes two mono PCM legs into a stereo WAV file.
type Recorder struct {
	log       *slog.Logger
	onLimit   func()
	maxFrames int64

	mu        sync.Mutex
	opBuf     []float32
	peerBuf   []float32
	written   int64 // stereo sample-frames committed to the WAV so far
	startedAt time.Time
	closed    bool
	limitHit  bool

	wav    *wavWriter
	stopCh chan struct{}
	doneCh chan struct{}
}

// New creates and starts a Recorder. The returned Recorder is writing to disk
// until Close is called.
func New(opts Options) (*Recorder, error) {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o755); err != nil {
		return nil, err
	}
	ww, err := newWavWriter(opts.Path, channels, sampleRate)
	if err != nil {
		return nil, err
	}
	r := &Recorder{
		log:       opts.Log,
		onLimit:   opts.OnLimit,
		wav:       ww,
		startedAt: time.Now(),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
	if opts.MaxDuration > 0 {
		r.maxFrames = int64(opts.MaxDuration / (time.Duration(nsPerSample) * time.Nanosecond))
	}
	go r.run()
	return r, nil
}

// WriteOperator feeds mono 16 kHz PCM captured from the operator (browser mic).
// Safe to call from the media callback; never blocks on I/O.
func (r *Recorder) WriteOperator(pcm []float32) { r.appendLeg(&r.opBuf, pcm) }

// WritePeer feeds mono 16 kHz PCM decoded from the peer (WhatsApp) leg.
func (r *Recorder) WritePeer(pcm []float32) { r.appendLeg(&r.peerBuf, pcm) }

func (r *Recorder) appendLeg(buf *[]float32, pcm []float32) {
	if len(pcm) == 0 {
		return
	}
	r.mu.Lock()
	if !r.closed && len(*buf) < maxPendingSamples {
		*buf = append(*buf, pcm...) // copies sample values; caller may reuse pcm
	}
	r.mu.Unlock()
}

func (r *Recorder) run() {
	defer close(r.doneCh)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			r.flush(true)
			return
		case <-ticker.C:
			if r.flush(false) {
				r.flush(true)
				if r.onLimit != nil {
					go r.onLimit()
				}
				return
			}
		}
	}
}

// flush commits sample-frames up to the current wall-clock position (or, when
// final, whatever remains buffered). It returns true when the duration cap is
// reached and the recorder should stop.
func (r *Recorder) flush(final bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}

	var target int64
	if final {
		rem := len(r.opBuf)
		if len(r.peerBuf) > rem {
			rem = len(r.peerBuf)
		}
		target = r.written + int64(rem)
	} else {
		target = time.Since(r.startedAt).Nanoseconds() / nsPerSample
	}
	if r.maxFrames > 0 && target > r.maxFrames {
		target = r.maxFrames
		r.limitHit = true
	}
	n := target - r.written
	if n <= 0 {
		return r.limitHit
	}

	left := consume(&r.opBuf, int(n))
	right := consume(&r.peerBuf, int(n))
	inter := make([]int16, 0, n*2)
	for i := int64(0); i < n; i++ {
		inter = append(inter, f2i(left[i]), f2i(right[i]))
	}
	if err := r.wav.writeSamples(inter); err != nil {
		r.log.Warn("recording: wav write failed", "err", err)
	}
	r.written += n
	return r.limitHit
}

// Close stops the mixer, finalizes the WAV and reports the exact recorded
// duration measured from committed samples.
func (r *Recorder) Close() (duration time.Duration, err error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return r.durationLocked(), nil
	}
	r.mu.Unlock()

	close(r.stopCh)
	<-r.doneCh

	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	d := r.durationLocked()
	if cerr := r.wav.Close(); cerr != nil {
		return d, cerr
	}
	return d, nil
}

func (r *Recorder) durationLocked() time.Duration {
	return time.Duration(r.written) * time.Duration(nsPerSample)
}

// consume returns exactly n samples from *buf, padding the tail with silence
// when the buffer is short, and compacts the remainder in place.
func consume(buf *[]float32, n int) []float32 {
	out := make([]float32, n)
	m := copy(out, *buf)
	if m < len(*buf) {
		*buf = append((*buf)[:0], (*buf)[m:]...)
	} else {
		*buf = (*buf)[:0]
	}
	return out
}

// ApproxDurationMs estimates a stereo recording's length from its WAV file size.
// Used to salvage a file whose writer was killed before it could report the
// exact duration (server restart mid-call).
func ApproxDurationMs(fileBytes int64) int64 {
	data := fileBytes - WavHeaderBytes
	if data <= 0 {
		return 0
	}
	return data * 1000 / (sampleRate * channels * 2)
}

func f2i(s float32) int16 {
	switch {
	case math.IsNaN(float64(s)):
		return 0
	case s >= 1:
		return math.MaxInt16
	case s <= -1:
		return math.MinInt16
	}
	return int16(s * 32767)
}
