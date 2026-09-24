package main

import (
	"context"
	"testing"
	"time"

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
