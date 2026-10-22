package main

import (
	"testing"
	"time"
)

func TestAudioTickets(t *testing.T) {
	now := time.Now()
	tk := newAudioTickets()
	tk.now = func() time.Time { return now }

	t.Run("uso único", func(t *testing.T) {
		ticket := tk.issue("call-1")
		if len(ticket) != 64 {
			t.Fatalf("ticket deveria ter 64 hex, tem %d", len(ticket))
		}
		if !tk.consume(ticket, "call-1") {
			t.Fatal("primeiro uso deveria valer")
		}
		if tk.consume(ticket, "call-1") {
			t.Fatal("segundo uso não deveria valer")
		}
	})

	t.Run("outra chamada falha e queima", func(t *testing.T) {
		ticket := tk.issue("call-1")
		if tk.consume(ticket, "call-2") {
			t.Fatal("ticket de outra chamada não deveria valer")
		}
		if tk.consume(ticket, "call-1") {
			t.Fatal("ticket já tentado deveria estar queimado")
		}
	})

	t.Run("expirado", func(t *testing.T) {
		ticket := tk.issue("call-1")
		now = now.Add(audioTicketTTL + time.Second)
		if tk.consume(ticket, "call-1") {
			t.Fatal("ticket expirado não deveria valer")
		}
	})

	t.Run("limpeza preguiçosa", func(t *testing.T) {
		tk.issue("velho")
		now = now.Add(audioTicketTTL + time.Second)
		tk.issue("novo")
		tk.mu.Lock()
		n := len(tk.tickets)
		tk.mu.Unlock()
		if n != 1 {
			t.Fatalf("esperava só o ticket novo, há %d", n)
		}
	})
}
