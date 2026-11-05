package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"wacalls/internal/voip/core"
)

const elevenLabsAPIBase = "https://api.elevenlabs.io"

// elevenLabsHTTP não segue redirect: o header xi-api-key iria junto para o
// destino, seja qual for.
var elevenLabsHTTP = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (s *server) handleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	key, err := s.sessions.store.elevenLabsAPIKey(r.Context(), sess.id, s.rec.secrets)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, redactedAgentConfig(sess.id, key))
}

// handleSetAgentConfig: mesma regra dos segredos da gravação. Campo omitido
// mantém o valor; "" apaga.
func (s *server) handleSetAgentConfig(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	var body struct {
		ElevenLabsAPIKey *string `json:"elevenLabsApiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	store := s.sessions.store
	if body.ElevenLabsAPIKey != nil {
		if err := store.setElevenLabsAPIKey(r.Context(), sess.id, strings.TrimSpace(*body.ElevenLabsAPIKey), s.rec.secrets); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	key, err := store.elevenLabsAPIKey(r.Context(), sess.id, s.rec.secrets)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, redactedAgentConfig(sess.id, key))
}

// handleAgentTest confirma key, agente e formato de áudio sem ligar para ninguém.
func (s *server) handleAgentTest(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	var agent agentCallConfig
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	signed, err := s.prepareAgent(r.Context(), sess.id, &agent)
	if err != nil {
		var se *agentSetupError
		errors.As(err, &se)
		writeJSON(w, se.status, map[string]string{"error": se.msg})
		return
	}
	conversationID, err := newAgentLeg(signed, agent, s.log.With("agentId", agent.AgentID)).probe()
	if err != nil {
		s.log.Warn("agent test failed", "session", sess.id, "agentId", agent.AgentID, "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "conversationId": conversationID, "audioFormat": agentAudioFormat})
}

func redactedAgentConfig(sessionID, key string) map[string]any {
	return map[string]any{"sessionId": sessionID, "elevenLabsApiKeySet": key != ""}
}

// agentSetupError carrega o status HTTP da resposta ao POST /calls.
type agentSetupError struct {
	status int
	msg    string
}

func (e *agentSetupError) Error() string { return e.msg }

// prepareAgent valida o agente e obtém a signed URL antes de discar: uma falha
// aqui vira erro para quem chamou a API, não um contato atendendo no silêncio.
// A URL vale 15 min para iniciar a conversa, folga de sobra para o toque.
func (s *server) prepareAgent(ctx context.Context, sessionID string, agent *agentCallConfig) (string, error) {
	agent.AgentID = strings.TrimSpace(agent.AgentID)
	if agent.AgentID == "" {
		return "", &agentSetupError{http.StatusBadRequest, "agent.agentId required"}
	}
	key, err := s.sessions.store.elevenLabsAPIKey(ctx, sessionID, s.rec.secrets)
	if err != nil {
		return "", &agentSetupError{http.StatusInternalServerError, err.Error()}
	}
	if key == "" {
		return "", &agentSetupError{http.StatusBadRequest, "API key da ElevenLabs não configurada nesta conta"}
	}
	signed, err := fetchSignedURL(ctx, cmp.Or(s.agentAPIBase, elevenLabsAPIBase), key, agent.AgentID)
	if err != nil {
		return "", &agentSetupError{http.StatusBadGateway, "falha ao preparar o agente: " + err.Error()}
	}
	return signed, nil
}

func fetchSignedURL(ctx context.Context, base, apiKey, agentID string) (string, error) {
	u := base + "/v1/convai/conversation/get-signed-url?" + url.Values{"agent_id": {agentID}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("xi-api-key", apiKey)
	res, err := elevenLabsHTTP.Do(req)
	if err != nil {
		return "", scrubURLError(err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return "", errors.New("API key recusada pela ElevenLabs (401)")
	case http.StatusForbidden:
		return "", errors.New("API key sem permissão para este agente (403)")
	case http.StatusNotFound:
		return "", errors.New("agente não encontrado nesta conta da ElevenLabs (404)")
	default:
		return "", fmt.Errorf("ElevenLabs respondeu %d", res.StatusCode)
	}
	var out struct {
		SignedURL string `json:"signed_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.SignedURL == "" {
		return "", errors.New("ElevenLabs não devolveu signed_url")
	}
	return out.SignedURL, nil
}

// attachAgent põe o agente como perna da chamada. Trocar de perna depois (ex.:
// atendente pelo painel) encerra a conversa com o agente e mantém a ligação.
func (s *server) attachAgent(sess *Session, callID, signedURL string, agent agentCallConfig) {
	ac, ok := sess.reg.get(callID)
	if !ok {
		return
	}
	leg := newAgentLeg(signedURL, agent, s.log.With("callID", callID, "agentId", agent.AgentID))
	leg.OnAudio = func(pcm []float32) {
		ac.cm.FeedCapturedPCM(pcm)
		if r := ac.rec.Load(); r != nil {
			r.WriteOperator(pcm)
		}
	}
	// Sem carência de reconexão: a conversa do agente não é retomada.
	leg.OnTerminal = func() {
		if cur, ok := sess.reg.bridgeOf(callID); !ok || cur != browserLeg(leg) {
			return
		}
		sess.terminateCall(callID, core.EndCallReasonUserEnded)
	}
	// Vai por SSE e pelo webhook da conta; conversationId liga a chamada ao
	// post-call webhook da ElevenLabs.
	leg.OnStatus = func(status, conversationID, errMsg string) {
		s.broker.broadcast(map[string]any{
			"type": "call-agent", "sessionId": sess.id, "id": callID, "agentId": agent.AgentID,
			"status": status, "conversationId": conversationID, "error": errMsg,
		})
	}
	sess.setBridge(callID, leg)
}
