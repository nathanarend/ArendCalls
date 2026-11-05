package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeAgent faz o papel do WebSocket da ElevenLabs; script roda depois do
// conversation_initiation_client_data.
type fakeAgent struct {
	init chan map[string]any
	ts   *httptest.Server
}

func newFakeAgent(t *testing.T, script func(ctx context.Context, c *websocket.Conn)) *fakeAgent {
	t.Helper()
	f := &fakeAgent{init: make(chan map[string]any, 1)}
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(1 << 20)
		_, data, err := c.Read(r.Context())
		if err != nil {
			return
		}
		var msg map[string]any
		_ = json.Unmarshal(data, &msg)
		f.init <- msg
		script(r.Context(), c)
	}))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeAgent) url() string { return "ws" + strings.TrimPrefix(f.ts.URL, "http") }

func sendAgentJSON(ctx context.Context, c *websocket.Conn, v any) {
	data, _ := json.Marshal(v)
	_ = c.Write(ctx, websocket.MessageText, data)
}

func sendMetadata(ctx context.Context, c *websocket.Conn, format string) {
	sendAgentJSON(ctx, c, map[string]any{
		"type": "conversation_initiation_metadata",
		"conversation_initiation_metadata_event": map[string]any{
			"conversation_id": "conv_1", "agent_output_audio_format": format, "user_input_audio_format": format,
		},
	})
}

func sendAudio(ctx context.Context, c *websocket.Conn, ms int, eventID int64) {
	pcm := make([]byte, ms*32)
	for i := range pcm {
		pcm[i] = 1
	}
	sendAgentJSON(ctx, c, map[string]any{
		"type":        "audio",
		"audio_event": map[string]any{"audio_base_64": base64.StdEncoding.EncodeToString(pcm), "event_id": eventID},
	})
}

type legRecorder struct {
	mu       sync.Mutex
	samples  int
	statuses []string
	convID   string
	errMsg   string
	terminal chan struct{}
	once     sync.Once
}

func recordLeg(leg *AgentLeg) *legRecorder {
	rec := &legRecorder{terminal: make(chan struct{})}
	leg.OnAudio = func(pcm []float32) {
		rec.mu.Lock()
		rec.samples += len(pcm)
		rec.mu.Unlock()
	}
	leg.OnTerminal = func() { rec.once.Do(func() { close(rec.terminal) }) }
	leg.OnStatus = func(status, conversationID, errMsg string) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.statuses = append(rec.statuses, status)
		if conversationID != "" {
			rec.convID = conversationID
		}
		if errMsg != "" {
			rec.errMsg = errMsg
		}
	}
	return rec
}

func (r *legRecorder) playedMs() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.samples / 16
}

func waitTerminal(t *testing.T, rec *legRecorder, d time.Duration) {
	t.Helper()
	select {
	case <-rec.terminal:
	case <-time.After(d):
		t.Fatal("OnTerminal não disparou")
	}
}

// Rajada de áudio sai em tempo real, e o fim da conversa só encerra a chamada
// depois de tocar a fila: a despedida do agente não pode ser cortada.
func TestAgentLegPacesBurstAndDrainsBeforeTerminal(t *testing.T) {
	const burstMs = 400
	f := newFakeAgent(t, func(ctx context.Context, c *websocket.Conn) {
		sendMetadata(ctx, c, agentAudioFormat)
		sendAudio(ctx, c, burstMs, 1)
		_ = c.Close(websocket.StatusNormalClosure, "end_call")
	})
	leg := newAgentLeg(f.url(), agentCallConfig{AgentID: "a1", DynamicVariables: map[string]any{"nome": "Maria"}}, slog.Default())
	rec := recordLeg(leg)
	t.Cleanup(leg.Close)

	start := time.Now()
	leg.Answer()
	waitTerminal(t, rec, 5*time.Second)
	if elapsed := time.Since(start); elapsed < (burstMs-100)*time.Millisecond {
		t.Fatalf("rajada entregue em %v: sem ritmo de tempo real", elapsed)
	}
	if got := rec.playedMs(); got != burstMs {
		t.Fatalf("tocou %d ms, want %d", got, burstMs)
	}
	if init := <-f.init; init["type"] != "conversation_initiation_client_data" ||
		init["dynamic_variables"].(map[string]any)["nome"] != "Maria" {
		t.Fatalf("init = %v", init)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.convID != "conv_1" || len(rec.statuses) != 1 || rec.statuses[0] != agentStatusConnected {
		t.Fatalf("status = %v conv = %q", rec.statuses, rec.convID)
	}
}

func TestAgentLegInterruptDropsQueueAndStaleAudio(t *testing.T) {
	interrupted := make(chan struct{})
	release := make(chan struct{})
	f := newFakeAgent(t, func(ctx context.Context, c *websocket.Conn) {
		sendMetadata(ctx, c, agentAudioFormat)
		sendAudio(ctx, c, 5000, 5)
		time.Sleep(100 * time.Millisecond)
		sendAgentJSON(ctx, c, map[string]any{"type": "interruption", "interruption_event": map[string]any{"event_id": 7}})
		sendAudio(ctx, c, 1000, 6) // anterior à interrupção, em trânsito
		sendAudio(ctx, c, 100, 7)  // resposta nova
		close(interrupted)
		<-release
		_ = c.Close(websocket.StatusNormalClosure, "")
	})
	leg := newAgentLeg(f.url(), agentCallConfig{AgentID: "a1"}, slog.Default())
	rec := recordLeg(leg)
	t.Cleanup(leg.Close)
	leg.Answer()

	<-interrupted
	close(release)
	waitTerminal(t, rec, 3*time.Second)
	// ~100 ms tocados antes da interrupção + 100 ms da resposta nova.
	if got := rec.playedMs(); got > 800 {
		t.Fatalf("tocou %d ms: fila antiga não foi descartada", got)
	}
}

// Fala do contato sobe em base64, o ping é respondido, e a pausa (DTX) vira
// silêncio em vez de buraco no fluxo.
func TestAgentLegUplinkPongAndSilence(t *testing.T) {
	var chunks, silent, pongs atomic.Int64
	done := make(chan struct{})
	f := newFakeAgent(t, func(ctx context.Context, c *websocket.Conn) {
		sendMetadata(ctx, c, agentAudioFormat)
		sendAgentJSON(ctx, c, map[string]any{"type": "ping", "ping_event": map[string]any{"event_id": 42}})
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var msg struct {
				Type    string `json:"type"`
				EventID int64  `json:"event_id"`
				Chunk   string `json:"user_audio_chunk"`
			}
			_ = json.Unmarshal(data, &msg)
			if msg.Type == "pong" && msg.EventID == 42 {
				pongs.Add(1)
			}
			if msg.Chunk != "" {
				chunks.Add(1)
				pcm, _ := base64.StdEncoding.DecodeString(msg.Chunk)
				if len(pcm) == agentSilenceBytes {
					silent.Add(1)
				}
			}
			if pongs.Load() > 0 && silent.Load() >= 2 {
				close(done)
				return
			}
		}
	})
	leg := newAgentLeg(f.url(), agentCallConfig{AgentID: "a1"}, slog.Default())
	recordLeg(leg)
	t.Cleanup(leg.Close)

	if err := leg.WritePCM(make([]float32, 960)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("chunks=%d silent=%d pongs=%d", chunks.Load(), silent.Load(), pongs.Load())
	}
}

func TestAgentLegRejectsWrongFormat(t *testing.T) {
	f := newFakeAgent(t, func(ctx context.Context, c *websocket.Conn) {
		sendMetadata(ctx, c, "ulaw_8000")
		_, _, _ = c.Read(ctx)
	})
	leg := newAgentLeg(f.url(), agentCallConfig{AgentID: "a1"}, slog.Default())
	rec := recordLeg(leg)
	t.Cleanup(leg.Close)
	leg.Answer()

	waitTerminal(t, rec, 3*time.Second)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.statuses) != 1 || rec.statuses[0] != agentStatusFailed || !strings.Contains(rec.errMsg, "ulaw_8000") {
		t.Fatalf("status=%v err=%q", rec.statuses, rec.errMsg)
	}
}

// A signed URL é credencial: não pode aparecer no erro que vai para log e webhook.
func TestAgentLegDialFailureHidesSignedURL(t *testing.T) {
	const secret = "conversation_signature=SEGREDO"
	leg := newAgentLeg("ws://127.0.0.1:1/x?"+secret, agentCallConfig{AgentID: "a1"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := recordLeg(leg)
	t.Cleanup(leg.Close)
	leg.Answer()

	waitTerminal(t, rec, 3*time.Second)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.errMsg == "" || strings.Contains(rec.errMsg, "SEGREDO") {
		t.Fatalf("errMsg = %q", rec.errMsg)
	}
}

// Encerrar antes do atendimento não abre conversa nem dispara OnTerminal.
func TestAgentLegCloseBeforeAnswer(t *testing.T) {
	var dials atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { dials.Add(1) }))
	defer ts.Close()
	leg := newAgentLeg("ws"+strings.TrimPrefix(ts.URL, "http"), agentCallConfig{AgentID: "a1"}, slog.Default())
	rec := recordLeg(leg)
	leg.Close()
	leg.Close()
	leg.Answer()
	if err := leg.WritePCM(make([]float32, 320)); !errors.Is(err, errWSClosed) {
		t.Fatalf("err = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if dials.Load() != 0 {
		t.Fatal("abriu conversa sem atendimento")
	}
	select {
	case <-rec.terminal:
		t.Fatal("OnTerminal disparou depois de Close")
	default:
	}
}

func TestAgentConfigAndPrepareAgent(t *testing.T) {
	srv, ts, sid := newRecTestServer(t)
	var gotKey, gotAgent string
	signedStatus := http.StatusOK
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotAgent = r.Header.Get("xi-api-key"), r.URL.Query().Get("agent_id")
		w.WriteHeader(signedStatus)
		_ = json.NewEncoder(w).Encode(map[string]string{"signed_url": "wss://signed"})
	}))
	defer api.Close()
	srv.agentAPIBase = api.URL
	ctx := context.Background()

	patch := func(body string) string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/sessions/"+sid+"/agent-config", strings.NewReader(body))
		req.Header.Set("X-Api-Key", "k")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("PATCH %s: %d %s", body, res.StatusCode, out)
		}
		return string(out)
	}
	setupErr := func(err error, status int) {
		t.Helper()
		var se *agentSetupError
		if !errors.As(err, &se) || se.status != status {
			t.Fatalf("err = %v, want status %d", err, status)
		}
	}

	if _, err := srv.prepareAgent(ctx, sid, &agentCallConfig{AgentID: " "}); err == nil {
		t.Fatal("agentId vazio deveria falhar")
	} else {
		setupErr(err, http.StatusBadRequest)
	}
	_, err := srv.prepareAgent(ctx, sid, &agentCallConfig{AgentID: "a1"})
	setupErr(err, http.StatusBadRequest) // sem API key: falha antes de discar

	if out := patch(`{"elevenLabsApiKey":" xi-secret "}`); strings.Contains(out, "xi-secret") || !strings.Contains(out, `"elevenLabsApiKeySet":true`) {
		t.Fatalf("resposta deveria redigir a key: %s", out)
	}
	if out := patch(`{}`); !strings.Contains(out, `"elevenLabsApiKeySet":true`) {
		t.Fatalf("PATCH vazio deveria manter a key: %s", out)
	}
	signed, err := srv.prepareAgent(ctx, sid, &agentCallConfig{AgentID: " a1 "})
	if err != nil || signed != "wss://signed" || gotKey != "xi-secret" || gotAgent != "a1" {
		t.Fatalf("signed=%q err=%v key=%q agent=%q", signed, err, gotKey, gotAgent)
	}

	signedStatus = http.StatusUnauthorized
	_, err = srv.prepareAgent(ctx, sid, &agentCallConfig{AgentID: "a1"})
	setupErr(err, http.StatusBadGateway)

	if out := patch(`{"elevenLabsApiKey":""}`); !strings.Contains(out, `"elevenLabsApiKeySet":false`) {
		t.Fatalf("\"\" deveria apagar a key: %s", out)
	}
}

func TestReservePeerAllowsOneCallPerNumber(t *testing.T) {
	r := newCallRegistry()
	if _, ok := r.reservePeer("5511"); !ok {
		t.Fatal("primeira reserva deveria passar")
	}
	if id, ok := r.reservePeer("5511"); ok || id != "" {
		t.Fatalf("discagem em preparo deveria bloquear: id=%q ok=%v", id, ok)
	}
	if _, ok := r.reservePeer("5522"); !ok {
		t.Fatal("outro número não deveria ser bloqueado")
	}

	r.releasePeer("5511")
	if _, ok := r.reservePeer("5511"); !ok {
		t.Fatal("falha antes de discar deveria liberar o número")
	}

	r.add("c1", &activeCall{})
	r.bindPeer("5511", "c1")
	if id, ok := r.reservePeer("5511"); ok || id != "c1" {
		t.Fatalf("chamada ativa deveria bloquear e informar o id: id=%q ok=%v", id, ok)
	}
	r.remove("c1")
	if _, ok := r.reservePeer("5511"); !ok {
		t.Fatal("fim da chamada deveria liberar o número")
	}

	// Chamada que acabou antes do bind não pode prender o número.
	r.bindPeer("5511", "ja-acabou")
	if _, ok := r.reservePeer("5511"); !ok {
		t.Fatal("bind de chamada encerrada deveria liberar o número")
	}
}

func TestAgentTestEndpoint(t *testing.T) {
	srv, ts, sid := newRecTestServer(t)
	format := agentAudioFormat
	agent := newFakeAgent(t, func(ctx context.Context, c *websocket.Conn) {
		sendMetadata(ctx, c, format)
		sendAgentJSON(ctx, c, map[string]any{"type": "audio", "audio_event": map[string]any{"audio_base_64": "", "event_id": 1}})
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	})
	signedStatus := http.StatusOK
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(signedStatus)
		_ = json.NewEncoder(w).Encode(map[string]string{"signed_url": agent.url()})
	}))
	defer api.Close()
	srv.agentAPIBase = api.URL

	post := func(body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/sessions/"+sid+"/agent-test", strings.NewReader(body))
		req.Header.Set("X-Api-Key", "k")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(out)
	}

	if code, out := post(`{"agentId":"a1"}`); code != http.StatusBadRequest {
		t.Fatalf("sem key: %d %s", code, out)
	}
	if err := srv.sessions.store.setElevenLabsAPIKey(context.Background(), sid, "xi", srv.rec.secrets); err != nil {
		t.Fatal(err)
	}
	if code, out := post(`{"agentId":"a1","dynamicVariables":{"nome":"Maria"}}`); code != http.StatusOK || !strings.Contains(out, "conv_1") {
		t.Fatalf("agente ok: %d %s", code, out)
	}
	select {
	case init := <-agent.init:
		vars, _ := init["dynamic_variables"].(map[string]any)
		if vars["nome"] != "Maria" {
			t.Fatalf("variáveis não chegaram: %v", init)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agente não recebeu o init")
	}

	format = "ulaw_8000"
	if code, out := post(`{"agentId":"a1"}`); code != http.StatusBadGateway || !strings.Contains(out, agentAudioFormat) {
		t.Fatalf("formato errado: %d %s", code, out)
	}

	signedStatus = http.StatusNotFound
	if code, out := post(`{"agentId":"a1"}`); code != http.StatusBadGateway || !strings.Contains(out, "não encontrado") {
		t.Fatalf("agente inexistente: %d %s", code, out)
	}
}

func TestAgentInitOmitsEmptyVariables(t *testing.T) {
	agent := newFakeAgent(t, func(ctx context.Context, c *websocket.Conn) {
		sendMetadata(ctx, c, agentAudioFormat)
		sendAgentJSON(ctx, c, map[string]any{"type": "audio", "audio_event": map[string]any{"audio_base_64": "", "event_id": 1}})
		_, _, _ = c.Read(ctx)
	})
	if _, err := newAgentLeg(agent.url(), agentCallConfig{AgentID: "a1"}, slog.New(slog.DiscardHandler)).probe(); err != nil {
		t.Fatal(err)
	}
	if init := <-agent.init; init["type"] != "conversation_initiation_client_data" {
		t.Fatalf("init = %v", init)
	} else if _, present := init["dynamic_variables"]; present {
		t.Fatalf("dynamic_variables vazio deveria ficar de fora (a ElevenLabs recusa null): %v", init)
	}
}

// A recusa real da ElevenLabs vem logo depois do metadata.
func TestAgentProbeDetectsRejectionAfterMetadata(t *testing.T) {
	agent := newFakeAgent(t, func(ctx context.Context, c *websocket.Conn) {
		sendMetadata(ctx, c, agentAudioFormat)
		_ = c.Close(websocket.StatusPolicyViolation, "Invalid message received")
	})
	_, err := newAgentLeg(agent.url(), agentCallConfig{AgentID: "a1"}, slog.New(slog.DiscardHandler)).probe()
	if err == nil || !strings.Contains(err.Error(), "Invalid message received") {
		t.Fatalf("err = %v, want recusa da conversa", err)
	}
}

// Agente sem primeira fala fica calado: não é falha.
func TestAgentProbeAcceptsSilentAgent(t *testing.T) {
	agent := newFakeAgent(t, func(ctx context.Context, c *websocket.Conn) {
		sendMetadata(ctx, c, agentAudioFormat)
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	})
	id, err := newAgentLeg(agent.url(), agentCallConfig{AgentID: "a1"}, slog.New(slog.DiscardHandler)).probe()
	if err != nil || id != "conv_1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}
