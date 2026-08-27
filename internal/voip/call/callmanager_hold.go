package call

import (
	_ "embed"
	"encoding/binary"
	"strings"
	"time"
)

//go:embed sounds/hold.pcm
var holdPCMBytes []byte

//go:embed sounds/transfer.pcm
var transferPCMBytes []byte

var (
	holdFloatSamples     []float32
	transferFloatSamples []float32
)

func init() {
	holdFloatSamples = pcmBytesToFloat32(holdPCMBytes)
	transferFloatSamples = pcmBytesToFloat32(transferPCMBytes)
}

func pcmBytesToFloat32(data []byte) []float32 {
	if len(data) < 2 {
		return nil
	}
	numSamples := len(data) / 2
	out := make([]float32, numSamples)
	for i := 0; i < numSamples; i++ {
		val := int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
		out[i] = float32(val) / 32768.0
	}
	return out
}

const holdSampleRate = 16000

// SetHold coloca ou retira a chamada do modo de espera (com áudio de voz de espera ou transferência)
func (m *CallManager) SetHold(hold bool, mode ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	targetMode := "hold"
	if len(mode) > 0 && strings.TrimSpace(mode[0]) != "" {
		targetMode = strings.ToLower(strings.TrimSpace(mode[0]))
	}

	if m.isHold == hold {
		return
	}
	m.isHold = hold

	if hold {
		m.log.Info("chamada colocada em HOLD", "call_id", m.currentCallIdLocked(), "mode", targetMode)
		if m.holdStop != nil {
			close(m.holdStop)
		}
		stop := make(chan struct{})
		m.holdStop = stop

		var audioSamples []float32
		if targetMode == "transfer" && len(transferFloatSamples) > 0 {
			audioSamples = transferFloatSamples
		} else if len(holdFloatSamples) > 0 {
			audioSamples = holdFloatSamples
		}

		go m.runHoldAudioLoop(stop, audioSamples)
	} else {
		m.log.Info("chamada retirada de HOLD (retomando áudio normal)", "call_id", m.currentCallIdLocked())
		if m.holdStop != nil {
			close(m.holdStop)
			m.holdStop = nil
		}
		m.lastCaptureAt = time.Now()
	}
}

func (m *CallManager) IsHold() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isHold
}

func (m *CallManager) currentCallIdLocked() string {
	if m.currentCall != nil {
		return m.currentCall.CallID
	}
	return ""
}

// runHoldAudioLoop transmite o áudio de espera ou transferência em loop contínuo via RTP/Opus para o WhatsApp
func (m *CallManager) runHoldAudioLoop(stop chan struct{}, audioSamples []float32) {
	if m.codec == nil || len(audioSamples) == 0 {
		return
	}
	frameSize := m.codec.FrameSize()
	if frameSize <= 0 {
		frameSize = 320 // 20ms @ 16kHz
	}

	frameDuration := time.Duration(float64(frameSize)/float64(holdSampleRate)*1000) * time.Millisecond
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	sampleIdx := 0
	totalSamples := len(audioSamples)
	pauseSamples := 3 * holdSampleRate // 3 segundos de silêncio
	loopLength := totalSamples + pauseSamples

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		m.mu.Lock()
		ready := m.isHold && m.codec != nil && m.rtpSession != nil && m.srtpSession != nil && m.relay.HasConnection()
		if !ready {
			m.mu.Unlock()
			continue
		}

		frame := make([]float32, frameSize)
		for i := 0; i < frameSize; i++ {
			seqIdx := sampleIdx % loopLength
			if seqIdx < totalSamples {
				frame[i] = audioSamples[seqIdx]
			} else {
				frame[i] = 0.0
			}
			sampleIdx++
		}

		opus, err := m.codec.Encode(frame)
		if err == nil {
			m.sendOpusFrameLocked(opus)
		}
		m.mu.Unlock()
		}
	}
}
