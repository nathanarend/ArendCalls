package main

import (
	"sync"
	"sync/atomic"

	"wacalls/internal/voip/call"
	"wacalls/internal/voip/recording"
	"wacalls/internal/voip/wanode"

	"go.mau.fi/whatsmeow/types"
)

// browserLeg é a perna navegador ↔ servidor de uma chamada: WebRTC
// (DataChannel, Bridge) ou WebSocket (WSBridge). O núcleo da chamada só vê PCM
// float32.
type browserLeg interface {
	WritePCM(pcm []float32) error
	// Detach desliga os callbacks de queda antes do Close, para a troca de
	// perna (unhold, reconexão) não disparar o encerramento da chamada.
	Detach()
	Close()
}

type activeCall struct {
	cm     *call.CallManager
	bridge browserLeg
	// rec is the live call recorder, set once the call is answered when it is
	// armed for recording. Loaded lock-free on the audio hot path.
	rec atomic.Pointer[recording.Recorder]
	// recTried: o primeiro áudio do paciente já tentou iniciar a gravação.
	// Evita lock/lookup em recordingController a cada pacote (~16/s).
	recTried atomic.Bool
	// stopRinging para o timer anti-zumbi da chamada (ver wireCall).
	stopRinging func()
	// bridgeSendErrors: falhas de WritePCM da perna (servidor → navegador).
	// Fica na chamada, não na perna, porque a perna é trocada na reconexão.
	bridgeSendErrors atomic.Int64
}

// callAudioStats é a telemetria de áudio de uma chamada: logada no fim e
// enviada no evento call-ended. Buraco aqui e no Mocho → lado WhatsApp;
// buraco só no Mocho → trecho servidor → navegador.
type callAudioStats struct {
	RxPackets        int64  `json:"rxPackets"`
	RxGapsSpeech     int64  `json:"rxGapsSpeech"`
	RxMaxGapSpeechMs int64  `json:"rxMaxGapSpeechMs"`
	RxUndecodable    int64  `json:"rxUndecodable"`
	RxRelock         int64  `json:"rxRelock"`
	RelayUsed        string `json:"relayUsed,omitempty"`
	BridgeSendErrors int64  `json:"bridgeSendErrors"`
}

func (ac *activeCall) audioStats() callAudioStats {
	rx := ac.cm.RxStats()
	return callAudioStats{
		RxPackets:        rx.Packets,
		RxGapsSpeech:     rx.SpeechGaps,
		RxMaxGapSpeechMs: rx.MaxSpeechGap.Milliseconds(),
		RxUndecodable:    rx.Undecodable,
		RxRelock:         rx.Relocks,
		RelayUsed:        rx.Relay,
		BridgeSendErrors: ac.bridgeSendErrors.Load(),
	}
}

type callRegistry struct {
	mu    sync.Mutex
	calls map[string]*activeCall
}

func newCallRegistry() *callRegistry {
	return &callRegistry{calls: map[string]*activeCall{}}
}

func (r *callRegistry) add(callID string, ac *activeCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls[callID] = ac
}

func (r *callRegistry) get(callID string) (*activeCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ac, ok := r.calls[callID]
	return ac, ok
}

func (r *callRegistry) getByPeer(peer types.JID) (*activeCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	peerStr := peer.ToNonAD().String()
	for _, ac := range r.calls {
		if c := ac.cm.CurrentCall(); c != nil {
			if wanode.MustJID(c.PeerJid).ToNonAD().String() == peerStr {
				return ac, true
			}
		}
	}
	return nil, false
}

func (r *callRegistry) remove(callID string) (*activeCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ac, ok := r.calls[callID]
	if !ok {
		return nil, false
	}
	delete(r.calls, callID)
	return ac, true
}

func (r *callRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// bridgeOf devolve a perna atual da chamada. ok=false: a chamada já saiu.
func (r *callRegistry) bridgeOf(callID string) (browserLeg, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ac, ok := r.calls[callID]
	if !ok {
		return nil, false
	}
	return ac.bridge, true
}

func (r *callRegistry) setBridge(callID string, b browserLeg) (browserLeg, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ac, ok := r.calls[callID]
	if !ok {
		return nil, false
	}
	oldB := ac.bridge
	ac.bridge = b
	return oldB, true
}

func (r *callRegistry) drain() []*activeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*activeCall, 0, len(r.calls))
	for _, ac := range r.calls {
		out = append(out, ac)
	}
	r.calls = map[string]*activeCall{}
	return out
}
