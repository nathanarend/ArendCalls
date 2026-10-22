package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"wacalls/internal/voip/media"

	"github.com/coder/websocket"
)

const (
	// wsReadLimit: um quadro PCM tem poucas centenas de bytes; o limite só
	// barra lixo.
	wsReadLimit = 64 << 10
	// wsSendQueue: ~1 s de áudio. Fila cheia = navegador não está lendo; o
	// quadro é descartado para não travar o loop de recepção do WhatsApp.
	wsSendQueue    = 32
	wsWriteTimeout = 5 * time.Second
	// wsPingInterval: o Cloudflare derruba WebSocket ocioso após ~100 s e em
	// espera pode não haver áudio nenhum.
	wsPingInterval = 20 * time.Second
	wsPingTimeout  = 10 * time.Second

	wsCloseCallEnded = "call_ended"
	wsCloseReplaced  = "replaced"
)

// wsConfig: primeira mensagem do servidor, em texto (binário é só áudio).
type wsConfig struct {
	Type        string `json:"type"` // "config"
	PrebufferMs int    `json:"prebufferMs"`
}

func sendWSConfig(conn *websocket.Conn, prebufferMs int) error {
	msg, err := json.Marshal(wsConfig{Type: "config", PrebufferMs: prebufferMs})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, msg)
}

var errWSQueueFull = errors.New("websocket send queue full")
var errWSClosed = errors.New("websocket closed")

// WSBridge é a perna do navegador por WebSocket: o mesmo PCM Int16 LE 16 kHz
// mono do DataChannel, uma mensagem binária por quadro. Existe para servidores
// atrás de proxy/tunnel (Cloudflare Tunnel), onde não há UDP até o navegador.
type WSBridge struct {
	conn   *websocket.Conn
	log    *slog.Logger
	out    chan []byte
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// Callbacks: definidos antes de start; Detach os zera sob mu.
	mu sync.Mutex
	// OnBrowserPCM recebe o PCM 16 kHz mono capturado do microfone do navegador.
	OnBrowserPCM func(pcm []float32)
	// OnTerminal dispara uma vez quando a conexão cai sem o servidor ter
	// fechado (queda de rede, navegador fechou, ping sem resposta).
	OnTerminal  func()
	closeReason string

	closeOnce    sync.Once
	failOnce     sync.Once
	serverClosed atomic.Bool
}

func newWSBridge(conn *websocket.Conn, log *slog.Logger) *WSBridge {
	conn.SetReadLimit(wsReadLimit)
	ctx, cancel := context.WithCancel(context.Background())
	return &WSBridge{
		conn:        conn,
		log:         log,
		out:         make(chan []byte, wsSendQueue),
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
		closeReason: wsCloseCallEnded,
	}
}

// start liga os loops de leitura, escrita e ping. Os callbacks já têm de
// estar definidos.
func (b *WSBridge) start() {
	go b.readLoop()
	go b.writeLoop()
	go b.pingLoop()
}

// Done fecha quando a conexão termina (o handler HTTP espera por ele).
func (b *WSBridge) Done() <-chan struct{} { return b.done }

func (b *WSBridge) readLoop() {
	defer close(b.done)
	for {
		typ, data, err := b.conn.Read(b.ctx)
		if err != nil {
			b.fail(err)
			return
		}
		// Texto é reservado para versões futuras do protocolo: ignora.
		if typ != websocket.MessageBinary || len(data) == 0 {
			continue
		}
		b.mu.Lock()
		cb := b.OnBrowserPCM
		b.mu.Unlock()
		if cb != nil {
			cb(media.PCMInt16LEToFloat32(data))
		}
	}
}

func (b *WSBridge) writeLoop() {
	for {
		select {
		case <-b.ctx.Done():
			return
		case frame := <-b.out:
			ctx, cancel := context.WithTimeout(b.ctx, wsWriteTimeout)
			err := b.conn.Write(ctx, websocket.MessageBinary, frame)
			cancel()
			if err != nil {
				b.fail(err)
				return
			}
		}
	}
}

// pingLoop fica fora do writeLoop: o Ping espera o pong e seguraria o áudio.
func (b *WSBridge) pingLoop() {
	t := time.NewTicker(wsPingInterval)
	defer t.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(b.ctx, wsPingTimeout)
			err := b.conn.Ping(ctx)
			cancel()
			if err != nil {
				b.fail(err)
				return
			}
		}
	}
}

// fail derruba a conexão e avisa OnTerminal, uma vez só. Depois de um Close do
// servidor a leitura também termina em erro; aí não é queda e não avisa.
func (b *WSBridge) fail(err error) {
	b.failOnce.Do(func() {
		b.cancel()
		b.conn.CloseNow()
		if b.serverClosed.Load() {
			return
		}
		b.log.Info("browser websocket dropped", "err", err)
		b.mu.Lock()
		cb := b.OnTerminal
		b.mu.Unlock()
		if cb != nil {
			cb()
		}
	})
}

// WritePCM enfileira o quadro para o navegador sem bloquear: roda no loop de
// recepção do WhatsApp. Fila cheia descarta o quadro e devolve erro.
func (b *WSBridge) WritePCM(pcm []float32) error {
	if len(pcm) == 0 {
		return nil
	}
	if b.ctx.Err() != nil {
		return errWSClosed
	}
	select {
	case b.out <- media.PCMFloat32ToInt16LE(pcm):
		return nil
	default:
		return errWSQueueFull
	}
}

// Detach: a perna foi substituída por outra conexão de áudio. Zera os
// callbacks e faz o Close seguinte avisar o navegador com "replaced".
func (b *WSBridge) Detach() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.OnBrowserPCM = nil
	b.OnTerminal = nil
	b.closeReason = wsCloseReplaced
}

// Close fecha com 1000 e o motivo (call_ended ou replaced). Não bloqueia: é
// chamado com o lock do CallManager preso (finishCall → removeCall), e o
// handshake de fechamento pode levar segundos.
func (b *WSBridge) Close() {
	b.closeOnce.Do(func() {
		b.serverClosed.Store(true)
		b.mu.Lock()
		reason := b.closeReason
		b.mu.Unlock()
		go func() {
			_ = b.conn.Close(websocket.StatusNormalClosure, reason)
			b.cancel()
		}()
	})
}
