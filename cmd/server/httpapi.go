package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"wacalls/internal/voip/core"
)

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/sessions", s.handleSessionList)
	mux.HandleFunc("POST /api/sessions", s.handleSessionCreate)
	mux.HandleFunc("PATCH /api/sessions/{sid}", s.handleSessionRename)
	mux.HandleFunc("PATCH /api/sessions/{sid}/webhook", s.handleSessionWebhook)
	mux.HandleFunc("DELETE /api/sessions/{sid}", s.handleSessionDelete)
	mux.HandleFunc("POST /api/sessions/{sid}/logout", s.handleSessionLogout)
	mux.HandleFunc("POST /api/sessions/{sid}/pair", s.handleSessionPair)
	mux.HandleFunc("POST /api/sessions/{sid}/start", s.handleSessionStart)
	mux.HandleFunc("POST /api/sessions/{sid}/restart", s.handleSessionRestart)
	mux.HandleFunc("POST /api/sessions/{sid}/stop", s.handleSessionStop)
	mux.HandleFunc("POST /api/sessions/{sid}/calls", s.handleStartCall)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/webrtc", s.handleWebRTC)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/accept", s.handleAccept)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/reject", s.handleReject)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/hold", s.handleHold)
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/unhold", s.handleUnhold)
	mux.HandleFunc("DELETE /api/sessions/{sid}/calls/{id}", s.handleEndCall)
	mux.HandleFunc("GET /api/sessions/{sid}/history", s.handleHistory)
	mux.HandleFunc("POST /api/sessions/{sid}/check-number", s.handleCheckNumber)

	mux.HandleFunc("GET /api/events", s.handleEvents)

	mux.HandleFunc("GET /api/sessions/{sid}/events", s.handleSessionEvents)

	if s.staticDir != "" {
		if _, err := os.Stat(s.staticDir); err == nil {
			fs := http.FileServer(http.Dir(s.staticDir))
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					http.SetCookie(w, &http.Cookie{
						Name:     "wacalls_admin_token",
						Value:    s.adminToken,
						Path:     "/",
						HttpOnly: true,
						SameSite: http.SameSiteLaxMode,
					})
				}
				fs.ServeHTTP(w, r)
			})
		}
	}
	return withCORS(s.withAuth(mux))
}

func (s *server) withAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			h.ServeHTTP(w, r)
			return
		}

		// 1. Web Panel Admin Cookie
		if cookie, err := r.Cookie("wacalls_admin_token"); err == nil && cookie.Value == s.adminToken {
			h.ServeHTTP(w, r)
			return
		}

		// Extract Authorization header, X-Api-Key header or query param
		auth := r.Header.Get("X-Api-Key")
		if auth == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				auth = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}
		if auth == "" {
			auth = r.URL.Query().Get("apikey")
		}

		// 2. Global Super-User API Key
		if s.apiKey != "" && auth == s.apiKey {
			h.ServeHTTP(w, r)
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized or invalid api key / admin cookie"})
	})
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Client-Id, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func clientID(r *http.Request) string {
	if id := r.Header.Get("X-Client-Id"); id != "" {
		return id
	}
	return r.URL.Query().Get("clientId")
}

func (s *server) sessionByID(w http.ResponseWriter, sid string) *Session {
	sess, ok := s.sessions.Get(sid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such session"})
		return nil
	}
	return sess
}

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	s.broker.serveSSE(w, r, clientID(r))
}

func (s *server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	sess, ok := s.sessions.Get(sid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	s.broker.serveSSESession(w, r, clientID(r), sid, sess)
}

func (s *server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.sessions.infos()})
}

func (s *server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Session"
	}
	id, err := s.sessions.Create(name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *server) handleSessionRename(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if err := s.sessions.Rename(r.Context(), sid, name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleSessionWebhook(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	var body struct {
		WebhookURL string `json:"webhook_url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.sessions.SetWebhookURL(r.Context(), sid, strings.TrimSpace(body.WebhookURL)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Delete(r.Context(), r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}


func (s *server) handleSessionLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Logout(r.Context(), r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSessionStop(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Stop(r.Context(), r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Start(r.Context(), r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSessionRestart(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	_ = s.sessions.Stop(r.Context(), sid)
	time.Sleep(500 * time.Millisecond)
	if err := s.sessions.Start(r.Context(), sid); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSessionPair(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Pair(r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleStartCall(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doStartCall(sess, w, r)
	}
}

func (s *server) handleWebRTC(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doWebRTC(sess, w, r)
	}
}

func (s *server) handleAccept(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doAccept(sess, w, r)
	}
}

func (s *server) handleReject(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doReject(sess, w, r)
	}
}

func (s *server) handleHold(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doHold(sess, w, r, true)
	}
}

func (s *server) handleUnhold(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doHold(sess, w, r, false)
	}
}

func (s *server) doHold(sess *Session, w http.ResponseWriter, r *http.Request, hold bool) {
	id := r.PathValue("id")
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	var mode string
	if hold && r.Body != nil {
		var req struct {
			Mode string `json:"mode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mode = req.Mode
	}
	ac.cm.SetHold(hold, mode)
	writeJSON(w, http.StatusOK, map[string]any{"hold": hold, "callId": id, "mode": mode})
}

func (s *server) handleEndCall(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		s.doEndCall(sess, w, r)
	}
}

func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r.PathValue("sid")); sess != nil {
		writeJSON(w, http.StatusOK, map[string]any{"rows": s.broker.historyRows(sess.id, 50)})
	}
}

func (s *server) doStartCall(sess *Session, w http.ResponseWriter, r *http.Request) {
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	var body struct {
		Phone      string `json:"phone"`
		DurationMs int    `json:"duration_ms"`
		Record     bool   `json:"record"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Phone) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone required"})
		return
	}
	owner := clientID(r)
	if other := s.broker.ownerActiveCall(owner); other != "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "operator already on a call"})
		return
	}
	if max := s.sessions.maxCalls; max > 0 && sess.reg.count() >= max {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "max concurrent calls"})
		return
	}
	norm := normalizePhone(body.Phone)
	res, err := sess.client.IsOnWhatsApp(r.Context(), []string{"+" + norm})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "falha ao verificar número na rede: " + err.Error()})
		return
	}
	if len(res) == 0 || !res[0].IsIn {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "este número não possui WhatsApp ativo"})
		return
	}
	peer := res[0].JID

	callID, err := sess.startOutgoing(r.Context(), peer, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	existing, _ := s.broker.getCall(callID)
	status := StatusStarting
	startedAt := time.Now().UnixMilli()
	if existing != nil {
		status = existing.Status
		startedAt = existing.StartedAt
	}
	s.broker.upsertCall(CallRecord{
		SessionID: sess.id, CallID: callID, Owner: &owner, Direction: "outbound", Peer: peer.String(),
		StartedAt: startedAt, Status: status,
	})
	writeJSON(w, http.StatusOK, map[string]any{"call": map[string]string{"callId": callID}})
}

func (s *server) doWebRTC(sess *Session, w http.ResponseWriter, r *http.Request) {
	callID := r.PathValue("id")
	ac, ok := sess.reg.get(callID)
	if !ok {
		if actualSess, found := s.sessions.FindSessionByCall(callID); found {
			sess = actualSess
			ac, ok = sess.reg.get(callID)
		}
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	var body struct {
		SDPOffer string `json:"sdp_offer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SDPOffer == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sdp_offer required"})
		return
	}
	bridge, answer, err := NewBridge(body.SDPOffer, s.log)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	bridge.OnBrowserPCM = func(pcm []float32) {
		ac.cm.FeedCapturedPCM(pcm)
	}
	bridge.OnTerminalICE = func() {
		if cur, ok := sess.reg.get(callID); ok && cur.bridge == bridge {
			if cur.cm.IsHold() {
				s.log.Info("Call ICE closed while on HOLD (transfer in progress), keeping WhatsApp call alive", "callID", callID)
				return
			}
			go sess.terminateCall(callID, core.EndCallReasonUserEnded)
		}
	}
	sess.setBridge(callID, bridge)
	writeJSON(w, http.StatusOK, map[string]string{"sdp_answer": answer})
}

func (s *server) doAccept(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, ok := sess.reg.get(id)
	if !ok {
		if actualSess, found := s.sessions.FindSessionByCall(id); found {
			sess = actualSess
			ac, ok = sess.reg.get(id)
		}
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	owner := clientID(r)
	if other := s.broker.ownerActiveCall(owner); other != "" && other != id {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "operator already on a call"})
		return
	}
	if !s.broker.setOwner(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "claimed by another client"})
		return
	}
	s.broker.emitIncomingClaimed(sess.id, id, owner)
	if err := ac.cm.AcceptCall(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"call": map[string]string{"callId": id}})
}

func (s *server) doReject(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, ok := sess.reg.get(id)
	if !ok {
		if actualSess, found := s.sessions.FindSessionByCall(id); found {
			sess = actualSess
			ac, ok = sess.reg.get(id)
		}
	}
	if ok {
		_ = ac.cm.RejectCall(r.Context(), id, core.EndCallReasonDeclined)
	}
	sess.removeCall(id)
	s.broker.endCall(id, string(core.EndCallReasonDeclined))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) doEndCall(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, ok := sess.reg.get(id)
	if !ok {
		if actualSess, found := s.sessions.FindSessionByCall(id); found {
			sess = actualSess
			ac, ok = sess.reg.get(id)
		}
	}
	if ok {
		_ = ac.cm.EndCall(r.Context(), core.EndCallReasonUserEnded)
	}
	sess.removeCall(id)
	s.broker.endCall(id, string(core.EndCallReasonUserEnded))
	w.WriteHeader(http.StatusNoContent)
}

func normalizePhone(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "+")
	var b strings.Builder
	for _, c := range p {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func (s *server) handleCheckNumber(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	var body struct {
		Numbers []string `json:"numbers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Numbers) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "numbers array required"})
		return
	}

	var phones []string
	for _, n := range body.Numbers {
		phones = append(phones, "+"+normalizePhone(n))
	}

	res, err := sess.client.IsOnWhatsApp(r.Context(), phones)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type result struct {
		Query string `json:"query"`
		JID   string `json:"jid"`
		IsIn  bool   `json:"is_in"`
	}
	var out []result
	for _, item := range res {
		out = append(out, result{
			Query: item.Query,
			JID:   item.JID.String(),
			IsIn:  item.IsIn,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

