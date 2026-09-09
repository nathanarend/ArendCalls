package recording

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
)

// wavWriter streams 16-bit PCM samples into a WAV container. The RIFF/data chunk
// sizes are written as placeholders up front and patched on Close, so the file is
// valid even if the process dies mid-call (only the trailing size fields are off).
type wavWriter struct {
	f          *os.File
	w          *bufio.Writer
	channels   int
	sampleRate int
	dataBytes  uint32
	closed     bool
}

func newWavWriter(path string, channels, sampleRate int) (*wavWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	ww := &wavWriter{
		f:          f,
		w:          bufio.NewWriterSize(f, 64*1024),
		channels:   channels,
		sampleRate: sampleRate,
	}
	if err := ww.writeHeader(); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return ww, nil
}

func (ww *wavWriter) writeHeader() error {
	const bitsPerSample = 16
	byteRate := ww.sampleRate * ww.channels * bitsPerSample / 8
	blockAlign := ww.channels * bitsPerSample / 8

	var h [44]byte
	copy(h[0:4], "RIFF")
	binary.LittleEndian.PutUint32(h[4:8], 0) // patched on close: 36 + dataBytes
	copy(h[8:12], "WAVE")
	copy(h[12:16], "fmt ")
	binary.LittleEndian.PutUint32(h[16:20], 16) // PCM fmt chunk size
	binary.LittleEndian.PutUint16(h[20:22], 1)  // audio format: PCM
	binary.LittleEndian.PutUint16(h[22:24], uint16(ww.channels))
	binary.LittleEndian.PutUint32(h[24:28], uint32(ww.sampleRate))
	binary.LittleEndian.PutUint32(h[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(h[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(h[34:36], bitsPerSample)
	copy(h[36:40], "data")
	binary.LittleEndian.PutUint32(h[40:44], 0) // patched on close: dataBytes

	_, err := ww.w.Write(h[:])
	return err
}

// writeSamples appends interleaved int16 samples (L,R,L,R,... for stereo).
func (ww *wavWriter) writeSamples(samples []int16) error {
	if len(samples) == 0 {
		return nil
	}
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	n, err := ww.w.Write(buf)
	ww.dataBytes += uint32(n)
	return err
}

// Close flushes buffered data and patches the RIFF/data sizes.
func (ww *wavWriter) Close() error {
	if ww.closed {
		return nil
	}
	ww.closed = true
	if err := ww.w.Flush(); err != nil {
		ww.f.Close()
		return err
	}
	if _, err := ww.f.WriteAt(le32(36+ww.dataBytes), 4); err != nil {
		ww.f.Close()
		return err
	}
	if _, err := ww.f.WriteAt(le32(ww.dataBytes), 40); err != nil {
		ww.f.Close()
		return err
	}
	if err := ww.f.Sync(); err != nil {
		ww.f.Close()
		return fmt.Errorf("wav sync: %w", err)
	}
	return ww.f.Close()
}

func le32(v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return b[:]
}
