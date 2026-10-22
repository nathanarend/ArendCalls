package main

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"wacalls/internal/voip/core"
)

// Transporte de áudio por WebSocket, ao lado do WebRTC (/webrtc). Mesmo PCM do
// DataChannel; existe para servidor atrás de proxy/tunnel, sem UDP até o
// navegador. O cliente escolhe o transporte ao conectar o áudio.

// wsReconnectGrace: carência para o cliente reconectar depois de uma queda do
// WebSocket, como o ICE tolera instabilidade no WebRTC.
const wsReconnectGrace = 15 * time.Second

// wsReconnectGraceOverride (ns) troca a carência nos testes. Atômica: timers de
// outros testes ainda leem enquanto um teste troca o valor.
var wsReconnectGraceOverride atomic.Int64

func reconnectGrace() time.Duration {
	if d := wsReconnectGraceOverride.Load(); d > 0 {
		return time.Duration(d)
	}
	return wsReconnectGrace
}

// audioPrebuffer: atraso de reprodução (ms) no início de cada fala, que o
// cliente aplica ao áudio do contato. Com o buffer vazio após uma pausa (DTX),
// o primeiro quadro tocaria na hora e qualquer atraso do seguinte viraria
// picote na primeira sílaba. Um valor por transporte: no WebSocket (TCP) o
// jitter é maior. O servidor só repassa; quem aplica é o cliente.
type audioPrebuffer struct {
	WebRTC    int
	WebSocket int
}

const maxPrebufferMs = 500

func prebufferFromEnv(log *slog.Logger) audioPrebuffer {
	return audioPrebuffer{
		WebRTC:    envPrebufferMs(log, "AUDIO_PREBUFFER_MS_WEBRTC", 0),
		WebSocket: envPrebufferMs(log, "AUDIO_PREBUFFER_MS_WEBSOCKET", 60),
	}
}

func envPrebufferMs(log *slog.Logger, key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > maxPrebufferMs {
		log.Warn("invalid audio prebuffer, using default", "env", key, "value", v, "max", maxPrebufferMs, "default", def)
		return def
	}
	return n
}

// isAudioWSPath: /api/sessions/{sid}/calls/{id}/audio.
func isAudioWSPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/sessions/")
	if !ok {
		return false
	}
	parts := strings.Split(rest, "/")
	return len(parts) == 4 && parts[0] != "" && parts[1] == "calls" && parts[2] != "" && parts[3] == "audio"
}

// findCall acha a chamada na sessão da URL ou, como doWebRTC, na sessão que de
// fato a tem.
func (s *server) findCall(sess *Session, callID string) (*Session, *activeCall, bool) {
	if ac, ok := sess.reg.get(callID); ok {
		return sess, ac, true
	}
	if other, found := s.sessions.FindSessionByCall(callID); found {
		if ac, ok := other.reg.get(callID); ok {
			return other, ac, true
		}
	}
	return nil, nil, false
}

func (s *server) handleAudioTicket(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	callID := r.PathValue("id")
	if _, _, ok := s.findCall(sess, callID); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":    s.tickets.issue(callID),
		"expiresIn": int(audioTicketTTL.Seconds()),
	})
}

func (s *server) handleAudioWS(w http.ResponseWriter, r *http.Request) {
	callID := r.PathValue("id")
	opts := &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled}
	// withAuth deixa passar sem credencial quando há ?ticket=; a validação é
	// aqui, antes de qualquer consulta à sessão.
	switch q := r.URL.Query(); {
	case q.Has("ticket"):
		if !s.tickets.consume(q.Get("ticket"), callID) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired ticket"})
			return
		}
		opts.InsecureSkipVerify = true
	case s.panelCookieOK(r):
		// O navegador manda o cookie em handshake aberto por página de qualquer
		// origem (CSWSH): fica a checagem padrão da lib, Origin igual ao Host.
	case s.apiKeyOK(r):
		opts.InsecureSkipVerify = true
	default:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized or invalid api key / admin cookie"})
		return
	}

	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	sess, ac, ok := s.findCall(sess, callID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}

	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		// Accept já respondeu (ex.: 403 de Origin).
		s.log.Info("audio websocket rejected", "callID", callID, "err", err)
		return
	}
	// Antes de qualquer áudio: a config da reprodução.
	if err := sendWSConfig(conn, s.prebuffer.WebSocket); err != nil {
		s.log.Info("audio websocket config failed", "callID", callID, "err", err)
		conn.CloseNow()
		return
	}
	leg := newWSBridge(conn, s.log.With("callID", callID))
	leg.OnBrowserPCM = func(pcm []float32) {
		ac.cm.FeedCapturedPCM(pcm)
		if r := ac.rec.Load(); r != nil {
			r.WriteOperator(pcm)
		}
	}
	leg.OnTerminal = func() { s.onAudioLegDropped(sess, callID, leg) }
	// Registra antes de ligar os loops: uma queda imediata precisa já achar a
	// perna como atual para armar a carência.
	sess.setBridge(callID, leg)
	leg.start()
	// O handler segura a conexão sequestrada enquanto o WebSocket existir.
	<-leg.Done()
}

// onAudioLegDropped: o WebSocket da chamada caiu. Não encerra na hora — dá
// wsReconnectGrace para o cliente reconectar. Se nesse prazo nenhuma outra
// conexão de áudio assumiu, encerra; em espera (transferência), não.
func (s *server) onAudioLegDropped(sess *Session, callID string, leg browserLeg) {
	if cur, ok := sess.reg.bridgeOf(callID); !ok || cur != leg {
		return
	}
	grace := reconnectGrace()
	time.AfterFunc(grace, func() {
		if cur, ok := sess.reg.bridgeOf(callID); !ok || cur != leg {
			return
		}
		ac, ok := sess.reg.get(callID)
		if !ok {
			return
		}
		if ac.cm.IsHold() {
			s.log.Info("audio websocket closed while on HOLD (transfer in progress), keeping WhatsApp call alive", "callID", callID)
			return
		}
		s.log.Info("audio websocket not reconnected, ending call", "callID", callID, "grace", grace)
		sess.terminateCall(callID, core.EndCallReasonUserEnded)
	})
}
