package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"

	"wacalls/internal/voip/media"
)

const (
	// agentFrameDur/agentFrameBytes: 20 ms de PCM16 16 kHz mono.
	agentFrameDur   = 20 * time.Millisecond
	agentFrameBytes = 640
	// agentAudioFormat: mesmo PCM do núcleo da chamada, sem reamostragem. O
	// formato fica na configuração do agente; outro valor derruba a conversa.
	agentAudioFormat = "pcm_16000"
	// agentUplinkQueue: alguns segundos de fala do contato; segura o "alô"
	// enquanto a conexão com o agente abre.
	agentUplinkQueue = 64
	// agentSilenceAfter: sem quadro do contato (DTX) por este tempo, manda
	// silêncio. O agente precisa de áudio contínuo para notar o fim da fala.
	agentSilenceAfter = 100 * time.Millisecond
	agentSilenceBytes = 3200 // 100 ms
	// agentHandshakeTimeout limita a abertura até o metadata da conversa.
	agentHandshakeTimeout = 10 * time.Second
	agentWriteTimeout     = 5 * time.Second
	// agentProbeSettle: quanto o teste espera, depois do metadata, por uma recusa
	// da conversa. Agente sem primeira fala fica calado e passa neste prazo.
	agentProbeSettle = 2 * time.Second
	// agentReadLimit: um evento de áudio traz segundos de PCM em base64.
	agentReadLimit = 4 << 20
	// agentPadAfterTicks: resto menor que um quadro espera mais áudio por estes
	// ciclos; depois sai completado com silêncio.
	agentPadAfterTicks = 3
	// agentMaxLag: atraso do playout (CPU, GC) acima disto é descartado em vez
	// de virar rajada.
	agentMaxLag = 200 * time.Millisecond

	agentStatusConnected = "connected"
	agentStatusFailed    = "failed"
)

// agentCallConfig é o campo "agent" do POST /calls.
type agentCallConfig struct {
	AgentID          string         `json:"agentId"`
	DynamicVariables map[string]any `json:"dynamicVariables,omitempty"`
}

// AgentLeg é a perna de uma chamada atendida por um agente ElevenLabs, no lugar
// do navegador. Abre a conversa só no atendimento (toque não gera custo nem
// saudação no vazio). O agente manda áudio mais rápido que o tempo real, então
// o envio ao WhatsApp sai daqui em ritmo de 20 ms: FeedCapturedPCM não tem fila.
type AgentLeg struct {
	signedURL string
	agent     agentCallConfig
	log       *slog.Logger
	up        chan []byte
	// stop corta áudio e laços na hora; ctx/cancel seguem a conexão até o
	// handshake de fechamento terminar.
	stop   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc

	startOnce sync.Once
	closeOnce sync.Once

	mu   sync.Mutex
	conn *websocket.Conn
	// queue: PCM do agente ainda não tocado; interruption o esvazia.
	queue         []byte
	starvedTicks  int
	interruptedAt int64
	// remoteGone: a conversa acabou; a fila ainda toca até o fim, senão a
	// despedida do agente é cortada.
	remoteGone bool
	// Callbacks: definidos antes do início; Detach os zera sob mu.
	OnAudio    func(pcm []float32)
	OnTerminal func()
	OnStatus   func(status, conversationID, errMsg string)
}

func newAgentLeg(signedURL string, agent agentCallConfig, log *slog.Logger) *AgentLeg {
	ctx, cancel := context.WithCancel(context.Background())
	return &AgentLeg{
		signedURL: signedURL,
		agent:     agent,
		log:       log,
		up:        make(chan []byte, agentUplinkQueue),
		stop:      make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (l *AgentLeg) stopped() bool {
	select {
	case <-l.stop:
		return true
	default:
		return false
	}
}

// Answer abre a conversa com o agente. Idempotente: dispara no estado Active e
// no primeiro áudio do contato, o que vier antes. Não bloqueia.
func (l *AgentLeg) Answer() {
	if l.stopped() {
		return
	}
	l.startOnce.Do(func() {
		go l.playout()
		go l.run()
	})
}

// WritePCM recebe a fala do contato, no loop de recepção do WhatsApp: não
// bloqueia. O WhatsApp não manda áudio durante o toque, então o primeiro quadro
// significa atendimento.
func (l *AgentLeg) WritePCM(pcm []float32) error {
	if len(pcm) == 0 {
		return nil
	}
	if l.stopped() {
		return errWSClosed
	}
	l.Answer()
	select {
	case l.up <- media.PCMFloat32ToInt16LE(pcm):
		return nil
	default:
		return errWSQueueFull
	}
}

// Detach: outra perna assumiu a chamada (ex.: atendente pelo painel).
func (l *AgentLeg) Detach() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.OnAudio = nil
	l.OnTerminal = nil
	l.OnStatus = nil
}

// Close não bloqueia: roda com o lock do CallManager preso (finishCall →
// removeCall), e o handshake de fechamento pode levar segundos.
func (l *AgentLeg) Close() {
	l.closeOnce.Do(func() {
		close(l.stop)
		l.mu.Lock()
		conn := l.conn
		l.mu.Unlock()
		go func() {
			if conn != nil {
				_ = conn.Close(websocket.StatusNormalClosure, wsCloseCallEnded)
			}
			l.cancel()
		}()
	})
}

func (l *AgentLeg) run() {
	err := l.converse()
	if !l.stopped() && err != nil {
		if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			l.log.Info("agent conversation ended")
		} else {
			l.log.Warn("agent conversation failed", "err", err)
			l.emitStatus(agentStatusFailed, "", err.Error())
		}
	}
	l.mu.Lock()
	l.remoteGone = true
	l.mu.Unlock()
}

func (l *AgentLeg) converse() error {
	conn, err := l.dial()
	if err != nil {
		return err
	}
	return l.downlink(conn)
}

func (l *AgentLeg) dial() (*websocket.Conn, error) {
	ctx, cancel := context.WithTimeout(l.ctx, agentHandshakeTimeout)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, l.signedURL, &websocket.DialOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return nil, fmt.Errorf("conectar ao agente: %w", scrubURLError(err))
	}
	conn.SetReadLimit(agentReadLimit)
	l.mu.Lock()
	l.conn = conn
	l.mu.Unlock()
	if l.stopped() {
		// Close rodou durante o dial e não viu esta conexão.
		conn.CloseNow()
		return nil, errWSClosed
	}
	client := map[string]any{"type": "conversation_initiation_client_data"}
	// Sem variáveis, o campo fica de fora: com null a ElevenLabs manda o metadata
	// e logo fecha a conexão com "Invalid message received".
	if len(l.agent.DynamicVariables) > 0 {
		client["dynamic_variables"] = l.agent.DynamicVariables
	}
	init, err := json.Marshal(client)
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	if err := conn.Write(ctx, websocket.MessageText, init); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("iniciar conversa: %w", err)
	}
	return conn, nil
}

// scrubURLError tira a URL do erro: a signed URL é credencial e não vai para
// log nem para webhook.
func scrubURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func checkAgentFormats(input, output string) error {
	if output != agentAudioFormat || input != agentAudioFormat {
		return fmt.Errorf("agente deve usar %s na entrada e na saída (entrada=%q saída=%q)", agentAudioFormat, input, output)
	}
	return nil
}

// probe confere key, agente e formato sem ligação. O metadata chega antes de a
// ElevenLabs validar o resto, então só conta como aprovado depois do primeiro
// áudio do agente ou de agentProbeSettle sem recusa. Fica registrada como uma
// conversa curta na ElevenLabs.
func (l *AgentLeg) probe() (conversationID string, err error) {
	defer l.Close()
	conn, err := l.dial()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(l.ctx, agentHandshakeTimeout)
	defer cancel()
	for conversationID == "" {
		ev, err := readAgentEvent(ctx, conn)
		if err != nil {
			return "", fmt.Errorf("agente não iniciou a conversa: %w", err)
		}
		if ev.Type != "conversation_initiation_metadata" {
			continue
		}
		md := ev.Metadata
		if err := checkAgentFormats(md.InputFormat, md.OutputFormat); err != nil {
			return "", err
		}
		conversationID = md.ConversationID
	}
	if err := conn.Write(ctx, websocket.MessageText, agentAudioChunk(make([]byte, agentSilenceBytes))); err != nil {
		return "", fmt.Errorf("enviar áudio ao agente: %w", err)
	}
	settle, stop := context.WithTimeout(ctx, agentProbeSettle)
	defer stop()
	for {
		ev, err := readAgentEvent(settle, conn)
		if settle.Err() != nil {
			return conversationID, nil
		}
		if err != nil {
			return "", fmt.Errorf("agente encerrou a conversa: %w", err)
		}
		if ev.Type == "audio" {
			return conversationID, nil
		}
	}
}

// readAgentEvent devolve o próximo evento; mensagem que não é JSON vira evento vazio.
func readAgentEvent(ctx context.Context, conn *websocket.Conn) (agentEvent, error) {
	var ev agentEvent
	_, data, err := conn.Read(ctx)
	if err != nil {
		return ev, err
	}
	_ = json.Unmarshal(data, &ev)
	return ev, nil
}

type agentEvent struct {
	Type     string `json:"type"`
	Metadata struct {
		ConversationID string `json:"conversation_id"`
		OutputFormat   string `json:"agent_output_audio_format"`
		InputFormat    string `json:"user_input_audio_format"`
	} `json:"conversation_initiation_metadata_event"`
	Audio struct {
		AudioBase64 string `json:"audio_base_64"`
		EventID     int64  `json:"event_id"`
	} `json:"audio_event"`
	Interruption struct {
		EventID int64 `json:"event_id"`
	} `json:"interruption_event"`
	Ping struct {
		EventID int64 `json:"event_id"`
	} `json:"ping_event"`
}

// downlink lê os eventos do agente até a conversa acabar. O áudio do contato só
// começa a subir depois do metadata.
func (l *AgentLeg) downlink(conn *websocket.Conn) error {
	ready := false
	for {
		ctx, cancel := l.ctx, context.CancelFunc(func() {})
		if !ready {
			ctx, cancel = context.WithTimeout(l.ctx, agentHandshakeTimeout)
		}
		_, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			return err
		}
		var ev agentEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			l.log.Debug("agent event ignored", "err", err)
			continue
		}
		switch ev.Type {
		case "conversation_initiation_metadata":
			if ready {
				continue
			}
			md := ev.Metadata
			if err := checkAgentFormats(md.InputFormat, md.OutputFormat); err != nil {
				conn.CloseNow()
				return err
			}
			ready = true
			go l.uplink(conn)
			l.emitStatus(agentStatusConnected, md.ConversationID, "")
		case "audio":
			pcm, err := base64.StdEncoding.DecodeString(ev.Audio.AudioBase64)
			if err != nil {
				l.log.Debug("agent audio ignored", "err", err)
				continue
			}
			l.enqueue(pcm, ev.Audio.EventID)
		case "interruption":
			l.interrupt(ev.Interruption.EventID)
		case "ping":
			pong, _ := json.Marshal(map[string]any{"type": "pong", "event_id": ev.Ping.EventID})
			wctx, wcancel := context.WithTimeout(l.ctx, agentWriteTimeout)
			err := conn.Write(wctx, websocket.MessageText, pong)
			wcancel()
			if err != nil {
				return err
			}
		}
	}
}

func (l *AgentLeg) uplink(conn *websocket.Conn) {
	silence := agentAudioChunk(make([]byte, agentSilenceBytes))
	idle := time.NewTimer(agentSilenceAfter)
	defer idle.Stop()
	for {
		var msg []byte
		select {
		case <-l.stop:
			return
		case <-l.ctx.Done():
			return
		case pcm := <-l.up:
			msg = agentAudioChunk(pcm)
		case <-idle.C:
			msg = silence
		}
		idle.Reset(agentSilenceAfter)
		ctx, cancel := context.WithTimeout(l.ctx, agentWriteTimeout)
		err := conn.Write(ctx, websocket.MessageText, msg)
		cancel()
		if err != nil {
			// A leitura também cai e encerra a conversa em run.
			conn.CloseNow()
			return
		}
	}
}

func agentAudioChunk(pcm []byte) []byte {
	msg, _ := json.Marshal(map[string]string{"user_audio_chunk": base64.StdEncoding.EncodeToString(pcm)})
	return msg
}

func (l *AgentLeg) enqueue(pcm []byte, eventID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Áudio anterior à interrupção ainda pode estar em trânsito.
	if eventID < l.interruptedAt {
		return
	}
	l.queue = append(l.queue, pcm...)
}

func (l *AgentLeg) interrupt(eventID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.queue = nil
	l.starvedTicks = 0
	l.interruptedAt = max(l.interruptedAt, eventID)
}

// nextFrame devolve o próximo quadro (nil = nada agora). done: a conversa
// acabou e a fila esvaziou.
func (l *AgentLeg) nextFrame() (frame []byte, done bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch n := len(l.queue); {
	case n >= agentFrameBytes:
		frame, l.queue = l.queue[:agentFrameBytes:agentFrameBytes], l.queue[agentFrameBytes:]
		l.starvedTicks = 0
		return frame, false
	case n > 0:
		l.starvedTicks++
		if l.starvedTicks < agentPadAfterTicks && !l.remoteGone {
			return nil, false
		}
		frame = make([]byte, agentFrameBytes)
		copy(frame, l.queue)
		l.queue = nil
		l.starvedTicks = 0
		return frame, false
	default:
		return nil, l.remoteGone
	}
}

// playout entrega a fila em tempo real. Prazo absoluto em vez de ticker: um
// atraso pequeno é recuperado, sem encolher a duração total do áudio.
func (l *AgentLeg) playout() {
	next := time.Now().Add(agentFrameDur)
	timer := time.NewTimer(agentFrameDur)
	defer timer.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-timer.C:
		}
		now := time.Now()
		if now.Sub(next) > agentMaxLag {
			next = now
		}
		for !now.Before(next) {
			if l.stopped() {
				return
			}
			frame, done := l.nextFrame()
			if done {
				l.fireTerminal()
				return
			}
			if frame != nil {
				l.deliver(frame)
			}
			next = next.Add(agentFrameDur)
		}
		timer.Reset(time.Until(next))
	}
}

func (l *AgentLeg) deliver(frame []byte) {
	l.mu.Lock()
	cb := l.OnAudio
	l.mu.Unlock()
	if cb != nil {
		cb(media.PCMInt16LEToFloat32(frame))
	}
}

func (l *AgentLeg) fireTerminal() {
	l.mu.Lock()
	cb := l.OnTerminal
	l.mu.Unlock()
	if cb != nil && !l.stopped() {
		cb()
	}
}

func (l *AgentLeg) emitStatus(status, conversationID, errMsg string) {
	l.mu.Lock()
	cb := l.OnStatus
	l.mu.Unlock()
	if cb != nil {
		cb(status, conversationID, errMsg)
	}
}
