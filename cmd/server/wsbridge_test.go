package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// newWSBridgePair sobe um servidor com uma WSBridge e devolve a ponte e o
// cliente (o "navegador"). setup roda antes de start, para ligar callbacks.
func newWSBridgePair(t *testing.T, setup func(b *WSBridge)) (*WSBridge, *websocket.Conn) {
	t.Helper()
	bridges := make(chan *WSBridge, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		b := newWSBridge(conn, slog.Default())
		if setup != nil {
			setup(b)
		}
		b.start()
		bridges <- b
		<-b.Done()
	}))
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.CloseNow() })
	select {
	case b := <-bridges:
		t.Cleanup(b.Close)
		return b, client
	case <-time.After(5 * time.Second):
		t.Fatal("ponte não subiu")
		return nil, nil
	}
}

func TestWSBridgeWritePCMReachesBrowser(t *testing.T) {
	b, client := newWSBridgePair(t, nil)
	if err := b.WritePCM([]float32{0.5, -0.5}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, data, err := client.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageBinary || len(data) != 4 {
		t.Fatalf("esperava binário de 4 bytes, veio %v %v", typ, data)
	}
	// 0.5 e -0.5 em Int16 LE (escala 32767).
	s0 := int16(uint16(data[0]) | uint16(data[1])<<8)
	s1 := int16(uint16(data[2]) | uint16(data[3])<<8)
	if s0 < 16000 || s0 > 16500 || s1 > -16000 || s1 < -16500 {
		t.Fatalf("amostras inesperadas: %d %d", s0, s1)
	}
}

func TestWSBridgeBrowserPCMSurfaces(t *testing.T) {
	got := make(chan []float32, 1)
	_, client := newWSBridgePair(t, func(b *WSBridge) {
		b.OnBrowserPCM = func(pcm []float32) { got <- pcm }
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Texto é ignorado; só o binário vira PCM.
	_ = client.Write(ctx, websocket.MessageText, []byte("oi"))
	if err := client.Write(ctx, websocket.MessageBinary, []byte{0x00, 0x40, 0x00, 0xC0}); err != nil {
		t.Fatal(err)
	}
	select {
	case pcm := <-got:
		if len(pcm) != 2 || pcm[0] < 0.4 || pcm[0] > 0.6 || pcm[1] > -0.4 || pcm[1] < -0.6 {
			t.Fatalf("amostras inesperadas: %v", pcm)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PCM do navegador não chegou")
	}
}

// Navegador não lê: a fila enche e WritePCM descarta sem bloquear.
func TestWSBridgeQueueFullDropsWithoutBlocking(t *testing.T) {
	b, _ := newWSBridgePair(t, nil)
	frame := make([]float32, 8000)
	done := make(chan error, 1)
	go func() {
		var last error
		for range 2000 {
			if err := b.WritePCM(frame); err != nil {
				last = err
			}
		}
		done <- last
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errWSQueueFull) {
			t.Fatalf("esperava errWSQueueFull, veio %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WritePCM bloqueou")
	}
}

func TestWSBridgeClientCloseFiresTerminalOnce(t *testing.T) {
	var calls atomic.Int32
	fired := make(chan struct{}, 2)
	_, client := newWSBridgePair(t, func(b *WSBridge) {
		b.OnTerminal = func() { calls.Add(1); fired <- struct{}{} }
	})
	client.Close(websocket.StatusGoingAway, "")
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("OnTerminal não disparou")
	}
	time.Sleep(100 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("OnTerminal disparou %d vezes", n)
	}
}

func TestWSBridgeDetachSilencesTerminal(t *testing.T) {
	var calls atomic.Int32
	b, client := newWSBridgePair(t, func(b *WSBridge) {
		b.OnTerminal = func() { calls.Add(1) }
	})
	b.Detach()
	client.CloseNow()
	select {
	case <-b.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("ponte não terminou")
	}
	if calls.Load() != 0 {
		t.Fatal("OnTerminal disparou depois do Detach")
	}
}

// Close do servidor: o navegador recebe 1000 com o motivo; não é queda.
func TestWSBridgeServerCloseReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		detach bool
		reason string
	}{
		{"fim da chamada", false, wsCloseCallEnded},
		{"substituída", true, wsCloseReplaced},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			b, client := newWSBridgePair(t, func(b *WSBridge) {
				b.OnTerminal = func() { calls.Add(1) }
			})
			if tc.detach {
				b.Detach()
			}
			b.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _, err := client.Read(ctx)
			var ce websocket.CloseError
			if !errors.As(err, &ce) || ce.Code != websocket.StatusNormalClosure || ce.Reason != tc.reason {
				t.Fatalf("esperava 1000 %q, veio %v", tc.reason, err)
			}
			<-b.Done()
			if calls.Load() != 0 {
				t.Fatal("Close do servidor não deveria disparar OnTerminal")
			}
		})
	}
}
