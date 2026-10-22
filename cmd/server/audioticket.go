package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// audioTicketTTL: o cliente pede o ticket e abre o WebSocket logo em seguida.
const audioTicketTTL = 30 * time.Second

// audioTickets autentica o WebSocket de áudio vindo de outra origem (Mocho):
// a API do WebSocket do navegador não manda header Authorization, e a API key
// não pode ir para o navegador. O backend do cliente pede o ticket com a API
// key e o navegador o usa uma vez, só para aquela chamada.
type audioTickets struct {
	now func() time.Time

	mu      sync.Mutex
	tickets map[string]audioTicket
}

type audioTicket struct {
	callID  string
	expires time.Time
}

func newAudioTickets() *audioTickets {
	return &audioTickets{now: time.Now, tickets: map[string]audioTicket{}}
}

func (t *audioTickets) issue(callID string) string {
	b := make([]byte, 32)
	rand.Read(b)
	ticket := hex.EncodeToString(b)
	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()
	// Limpeza preguiçosa: tickets não usados só vencem aqui.
	for k, v := range t.tickets {
		if now.After(v.expires) {
			delete(t.tickets, k)
		}
	}
	t.tickets[ticket] = audioTicket{callID: callID, expires: now.Add(audioTicketTTL)}
	return ticket
}

// consume valida e queima o ticket. Queima mesmo quando a chamada não confere:
// ticket é de uso único, válido ou não.
func (t *audioTickets) consume(ticket, callID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	tk, ok := t.tickets[ticket]
	if !ok {
		return false
	}
	delete(t.tickets, ticket)
	return tk.callID == callID && !t.now().After(tk.expires)
}
