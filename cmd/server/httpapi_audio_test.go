package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"go.mau.fi/whatsmeow/types"
)

func TestIsAudioWSPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/sessions/s1/calls/c1/audio":        true,
		"/api/sessions/s1/calls/c1/audio-ticket": false,
		"/api/sessions/s1/calls/c1/webrtc":       false,
		"/api/sessions//calls/c1/audio":          false,
		"/api/sessions/s1/calls/c1/audio/x":      false,
		"/api/sessions":                          false,
	} {
		if got := isAudioWSPath(path); got != want {
			t.Errorf("isAudioWSPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestAudioWebSocketAuth(t *testing.T) {
	const apiKey = "test-key"
	const adminToken = "admin-token"
	srv, ts := newTestServer(t, apiKey, adminToken)
	sess := srv.sessions.addUnconnected(t, "Conta Áudio")
	const callID = "CALLAUDIO0123456789"
	sess.createCall(callID)
	t.Cleanup(func() { sess.removeCall(callID) })

	base := "/api/sessions/" + sess.id + "/calls/" + callID
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + base + "/audio"

	issueTicket := func(t *testing.T, path string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader(nil))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out struct {
			Ticket    string `json:"ticket"`
			ExpiresIn int    `json:"expiresIn"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode == http.StatusOK && out.ExpiresIn != 30 {
			t.Fatalf("expiresIn = %d", out.ExpiresIn)
		}
		return res.StatusCode, out.Ticket
	}

	dial := func(t *testing.T, url string, h http.Header) int {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, res, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
		if err == nil {
			conn.Close(websocket.StatusNormalClosure, "")
			return http.StatusSwitchingProtocols
		}
		if res == nil {
			t.Fatalf("dial sem resposta: %v", err)
		}
		return res.StatusCode
	}

	t.Run("ticket de chamada inexistente → 404", func(t *testing.T) {
		if code, _ := issueTicket(t, "/api/sessions/"+sess.id+"/calls/nope/audio-ticket"); code != http.StatusNotFound {
			t.Fatalf("esperava 404, veio %d", code)
		}
	})

	t.Run("ticket sem credencial → 401", func(t *testing.T) {
		res, err := http.Post(ts.URL+base+"/audio-ticket", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("esperava 401, veio %d", res.StatusCode)
		}
	})

	t.Run("sem credencial → 401", func(t *testing.T) {
		if code := dial(t, wsURL, nil); code != http.StatusUnauthorized {
			t.Fatalf("esperava 401, veio %d", code)
		}
	})

	t.Run("ticket válido, depois reutilizado", func(t *testing.T) {
		code, ticket := issueTicket(t, base+"/audio-ticket")
		if code != http.StatusOK || ticket == "" {
			t.Fatalf("emissão falhou: %d", code)
		}
		// Origin de outro site: com ticket não importa (é o Mocho).
		h := http.Header{"Origin": {"https://mocho.example"}}
		if code := dial(t, wsURL+"?ticket="+ticket, h); code != http.StatusSwitchingProtocols {
			t.Fatalf("esperava upgrade, veio %d", code)
		}
		if code := dial(t, wsURL+"?ticket="+ticket, h); code != http.StatusUnauthorized {
			t.Fatalf("ticket reutilizado: esperava 401, veio %d", code)
		}
	})

	t.Run("ticket não libera outras rotas", func(t *testing.T) {
		_, ticket := issueTicket(t, base+"/audio-ticket")
		res, err := http.Get(ts.URL + "/api/sessions?ticket=" + ticket)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("esperava 401, veio %d", res.StatusCode)
		}
	})

	t.Run("API key", func(t *testing.T) {
		h := http.Header{"Origin": {"https://outro.example"}}
		if code := dial(t, wsURL+"?apikey="+apiKey, h); code != http.StatusSwitchingProtocols {
			t.Fatalf("esperava upgrade, veio %d", code)
		}
	})

	t.Run("API key, chamada inexistente → 404", func(t *testing.T) {
		url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/" + sess.id + "/calls/nope/audio?apikey=" + apiKey
		if code := dial(t, url, nil); code != http.StatusNotFound {
			t.Fatalf("esperava 404, veio %d", code)
		}
	})

	t.Run("cookie com Origin de outro site → recusado", func(t *testing.T) {
		h := http.Header{
			"Cookie": {"wacalls_admin_token=" + adminToken},
			"Origin": {"https://evil.example"},
		}
		if code := dial(t, wsURL, h); code != http.StatusForbidden {
			t.Fatalf("esperava 403, veio %d", code)
		}
	})

	t.Run("cookie com Origin do próprio host", func(t *testing.T) {
		h := http.Header{
			"Cookie": {"wacalls_admin_token=" + adminToken},
			"Origin": {ts.URL},
		}
		if code := dial(t, wsURL, h); code != http.StatusSwitchingProtocols {
			t.Fatalf("esperava upgrade, veio %d", code)
		}
	})
}

// fakeLeg: perna sem transporte, para testar a carência de reconexão. O campo
// existe para cada &fakeLeg{} ter endereço próprio (struct vazia pode repetir).
type fakeLeg struct{ _ byte }

func (fakeLeg) WritePCM([]float32) error { return nil }
func (fakeLeg) Detach()                  {}
func (fakeLeg) Close()                   {}

func withWSReconnectGrace(t *testing.T, d time.Duration) {
	t.Helper()
	prev := wsReconnectGraceOverride.Swap(int64(d))
	t.Cleanup(func() { wsReconnectGraceOverride.Store(prev) })
}

func TestAudioLegDropGrace(t *testing.T) {
	withWSReconnectGrace(t, 50*time.Millisecond)
	srv, _ := newTestServer(t, "k", "t")

	// Chamada de saída sem conexão: o envio falha, mas currentCall existe e o
	// EndCall da carência tira a chamada do registro.
	newCall := func(t *testing.T, callID string) *Session {
		t.Helper()
		sess := srv.sessions.addUnconnected(t, "Conta "+callID)
		cm := sess.createCall(callID)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = cm.StartCall(ctx, callID, types.NewJID("5511999999999", types.DefaultUserServer), false)
		t.Cleanup(func() { sess.removeCall(callID) })
		return sess
	}

	t.Run("queda sem reconexão encerra", func(t *testing.T) {
		const callID = "GRACE0000000000000000000000000001"
		sess := newCall(t, callID)
		leg := &fakeLeg{}
		sess.setBridge(callID, leg)
		srv.onAudioLegDropped(sess, callID, leg)
		if _, ok := sess.reg.get(callID); !ok {
			t.Fatal("não deveria encerrar antes da carência")
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, ok := sess.reg.get(callID); !ok {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("chamada não encerrou depois da carência")
	})

	t.Run("queda seguida de nova perna não encerra", func(t *testing.T) {
		const callID = "GRACE0000000000000000000000000002"
		sess := newCall(t, callID)
		old := &fakeLeg{}
		sess.setBridge(callID, old)
		srv.onAudioLegDropped(sess, callID, old)
		sess.setBridge(callID, &fakeLeg{})
		time.Sleep(300 * time.Millisecond)
		if _, ok := sess.reg.get(callID); !ok {
			t.Fatal("reconexão dentro da carência não deveria encerrar a chamada")
		}
	})
}

func TestPrebufferFromEnv(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	if got := prebufferFromEnv(log); got != (audioPrebuffer{WebRTC: 0, WebSocket: 60}) {
		t.Fatalf("padrões: %+v", got)
	}
	for v, want := range map[string]int{"0": 0, " 120 ": 120, "500": 500, "501": 60, "-1": 60, "abc": 60} {
		t.Setenv("AUDIO_PREBUFFER_MS_WEBSOCKET", v)
		if got := prebufferFromEnv(log).WebSocket; got != want {
			t.Errorf("AUDIO_PREBUFFER_MS_WEBSOCKET=%q → %d, want %d", v, got, want)
		}
	}
}

func TestAudioWebSocketSendsConfigFirst(t *testing.T) {
	const apiKey = "test-key"
	srv, ts := newTestServer(t, apiKey, "admin-token")
	srv.prebuffer = audioPrebuffer{WebSocket: 80}
	sess := srv.sessions.addUnconnected(t, "Conta Config")
	const callID = "CALLCONFIG0123456789"
	sess.createCall(callID)
	t.Cleanup(func() { sess.removeCall(callID) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/" + sess.id + "/calls/" + callID + "/audio"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"X-Api-Key": {apiKey}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cfg wsConfig
	if typ != websocket.MessageText || json.Unmarshal(data, &cfg) != nil || cfg != (wsConfig{Type: "config", PrebufferMs: 80}) {
		t.Fatalf("primeira mensagem: %v %s", typ, data)
	}
}
