package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"wacalls/internal/voip/call"
	"wacalls/internal/voip/core"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Ligação entre duas contas do mesmo servidor: a oferta que chega na conta de
// destino não pode tomar o registro da chamada da conta que ligou.
func TestInboundOfferForCallPlacedBySiblingIsIgnored(t *testing.T) {
	m := newTestManager(t)
	a := m.addUnconnected(t, "Account A")
	b := m.addUnconnected(t, "Account B")

	const callID = "ABCDEF0123456789ABCDEF0123456789"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Sem conexão o envio da oferta falha, mas a chamada já fica marcada como
	// de saída, que é o estado que a sessão B encontra na vida real.
	cm := a.createCall(callID)
	_ = cm.StartCall(ctx, callID, types.NewJID("5511999999999", types.DefaultUserServer), false)

	from := types.NewJID("5511888888888", types.DefaultUserServer)
	b.onIncomingOffer(ctx, &events.CallOffer{
		BasicCallMeta: types.BasicCallMeta{From: from, CallID: callID, Timestamp: time.Now()},
		Data:          &waBinary.Node{Tag: "offer", Attrs: waBinary.Attrs{"call-id": callID, "call-creator": from.String()}},
	})

	if _, ok := b.reg.get(callID); ok {
		t.Fatal("session B registered a call already placed by session A")
	}
	if rec, ok := m.broker.getCall(callID); !ok || rec.SessionID != a.id {
		t.Fatalf("broker record should stay with session A, got %+v", rec)
	}
}

// syncBuffer: destino de log seguro para o goroutine do timer escrever
// enquanto o teste lê.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func withRingingTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := ringingTimeout
	ringingTimeout = d
	t.Cleanup(func() { ringingTimeout = prev })
}

func captureLog(s *Session) *syncBuffer {
	var buf syncBuffer
	s.log = slog.New(slog.NewTextHandler(&buf, nil))
	return &buf
}

// waitFor espera cond virar true (até 2 s).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRemoveCallStopsRingingTimer(t *testing.T) {
	withRingingTimeout(t, 30*time.Millisecond)
	m := newTestManager(t)
	s := m.addUnconnected(t, "A")
	logs := captureLog(s)

	const callID = "11112222333344445555666677778888"
	s.createCall(callID)
	s.removeCall(callID)

	time.Sleep(120 * time.Millisecond)
	if strings.Contains(logs.String(), "ringing timeout") {
		t.Fatalf("timer fired after removeCall:\n%s", logs)
	}
}

// Oferta que nunca virou chamada (currentCall nil): EndCall não faz nada, então
// o timer precisa tirar a entrada do registro ele mesmo.
func TestRingingTimerReapsGhostCall(t *testing.T) {
	withRingingTimeout(t, 30*time.Millisecond)
	m := newTestManager(t)
	s := m.addUnconnected(t, "A")
	logs := captureLog(s)

	const callID = "99998888777766665555444433332222"
	s.createCall(callID)

	waitFor(t, "ghost call reaped", func() bool {
		_, ok := s.reg.get(callID)
		return !ok
	})
	if !strings.Contains(logs.String(), "ending stale call") {
		t.Fatalf("expected the stale-call log line:\n%s", logs)
	}
}

// Discagem que falha depois de criar a chamada: sem o stopRinging o timer
// mandava um terminate para a oferta que falhou, 90 s depois.
func TestFailedDialStopsRingingTimer(t *testing.T) {
	withRingingTimeout(t, 30*time.Millisecond)
	m := newTestManager(t)
	s := m.addUnconnected(t, "A")
	logs := captureLog(s)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := s.startOutgoing(ctx, types.NewJID("5511999999999", types.DefaultUserServer), false); err == nil {
		t.Fatal("dial on an unconnected session should fail")
	}
	if n := s.reg.count(); n != 0 {
		t.Fatalf("failed dial left %d call(s) in the registry", n)
	}

	time.Sleep(120 * time.Millisecond)
	if strings.Contains(logs.String(), "ringing timeout") {
		t.Fatalf("timer fired for a failed dial:\n%s", logs)
	}
}

// Relay conectado não é atendimento (na saída conecta antes do toque); o
// primeiro áudio do paciente é.
func TestRecordingStartsOnFirstPeerAudioNotRelayConnect(t *testing.T) {
	m := newTestManager(t)
	ctl, store, _ := newTestRecCtl(t)
	m.rec = ctl
	s := m.addUnconnected(t, "A")

	const callID = "AAAABBBBCCCCDDDDEEEEFFFF00001111"
	cm := s.createCall(callID)
	ctl.arm(recMeta{callID: callID, sessionID: s.id, direction: "outbound", peer: "5511999999999@s.whatsapp.net"})
	ac, _ := s.reg.get(callID)

	cm.OnRelayConnected()
	cm.OnRelayConnected() // um por relay
	if ac.rec.Load() != nil {
		t.Fatal("recording started on relay connect (before the phone rang)")
	}

	frame := make([]float32, 320)
	cm.OnPeerAudio(frame)
	r := ac.rec.Load()
	if r == nil {
		t.Fatal("recording did not start on the first peer audio")
	}
	cm.OnPeerAudio(frame)
	if ac.rec.Load() != r {
		t.Fatal("second peer packet replaced the recorder")
	}

	ctl.onCallEnded(callID)
	row, err := store.get(context.Background(), callID)
	if err != nil || row == nil {
		t.Fatalf("recording row missing: %v", err)
	}
	if row.Status != RecStatusSkipped || row.Err != "recording shorter than 5s" {
		t.Fatalf("short answered call: got %s / %q", row.Status, row.Err)
	}
}

func TestCallEndedEventCarriesAudioStats(t *testing.T) {
	m := newTestManager(t)
	s := m.addUnconnected(t, "A")
	sub := m.broker.subscribe("test")
	defer m.broker.unsubscribe(sub)

	const callID = "0000111122223333444455556666777F"
	s.createCall(callID)
	m.broker.upsertCall(CallRecord{SessionID: s.id, CallID: callID, Direction: "inbound", Status: StatusConnected})
	ac, _ := s.reg.get(callID)
	ac.bridgeSendErrors.Add(3)

	c := call.NewIncomingCall(callID, "5511999999999@s.whatsapp.net", "", "", core.CallMediaTypeAudio)
	c.StateData.EndReason = core.EndCallReasonUserEnded
	s.finishCall(c)
	s.finishCall(c) // OnStateChange + OnEnded: a 2ª passada não acha mais a chamada

	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-sub.ch:
			var ev struct {
				Type       string          `json:"type"`
				AudioStats *callAudioStats `json:"audioStats"`
			}
			if err := json.Unmarshal(raw, &ev); err != nil || ev.Type != "call-ended" {
				continue
			}
			if ev.AudioStats == nil || ev.AudioStats.BridgeSendErrors != 3 {
				t.Fatalf("call-ended without the audio stats: %s", raw)
			}
			if _, ok := s.reg.get(callID); ok {
				t.Fatal("finishCall left the call in the registry")
			}
			return
		case <-deadline:
			t.Fatal("no call-ended event")
		}
	}
}
