package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// recordingConfigBody is the PATCH payload for per-session recording setup.
// Every field is a pointer so a PATCH touches only what it sends; secrets left
// out keep their stored value, secrets sent as "" are cleared.
type recordingConfigBody struct {
	Enabled       *bool   `json:"enabled"`
	B2Endpoint    *string `json:"b2Endpoint"`
	B2Region      *string `json:"b2Region"`
	B2Bucket      *string `json:"b2Bucket"`
	B2KeyID       *string `json:"b2KeyId"`
	B2AppKey      *string `json:"b2AppKey"`
	B2Prefix      *string `json:"b2Prefix"`
	WebhookURL    *string `json:"webhookUrl"`
	WebhookSecret *string `json:"webhookSecret"`
	URLTTLSeconds *int    `json:"urlTtlSeconds"`
}

func (s *server) handleSetRecordingConfig(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	var body recordingConfigBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	cur, err := s.recStore.config(r.Context(), sess.id, s.rec.secrets)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if body.Enabled != nil {
		cur.Enabled = *body.Enabled
	}
	setStr(&cur.B2Endpoint, body.B2Endpoint)
	setStr(&cur.B2Region, body.B2Region)
	setStr(&cur.B2Bucket, body.B2Bucket)
	setStr(&cur.B2KeyID, body.B2KeyID)
	setStr(&cur.B2AppKey, body.B2AppKey)
	setStr(&cur.B2Prefix, body.B2Prefix)
	setStr(&cur.WebhookURL, body.WebhookURL)
	setStr(&cur.WebhookSecret, body.WebhookSecret)
	if body.URLTTLSeconds != nil {
		cur.URLTTLSeconds = *body.URLTTLSeconds
	}
	cur.SessionID = sess.id

	if err := s.recStore.saveConfig(r.Context(), cur, s.rec.secrets); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.rec.startRetryWorker()
	writeJSON(w, http.StatusOK, redactedConfig(cur))
}

func (s *server) handleGetRecordingConfig(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	cfg, err := s.recStore.config(r.Context(), sess.id, s.rec.secrets)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, redactedConfig(cfg))
}

func (s *server) handleRecordingInfo(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	callID := r.PathValue("id")
	row, err := s.recStore.get(r.Context(), callID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if row == nil || row.SessionID != sess.id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no recording for this call"})
		return
	}
	payload, err := recordingInfoJSON(*row)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func setStr(dst *string, src *string) {
	if src != nil {
		*dst = strings.TrimSpace(*src)
	}
}

// redactedConfig echoes the config back without leaking secret values.
func redactedConfig(c RecordingConfig) map[string]any {
	return map[string]any{
		"sessionId":        c.SessionID,
		"enabled":          c.Enabled,
		"b2Endpoint":       c.B2Endpoint,
		"b2Region":         c.B2Region,
		"b2Bucket":         c.B2Bucket,
		"b2KeyId":          c.B2KeyID,
		"b2AppKeySet":      c.B2AppKey != "",
		"b2Prefix":         c.B2Prefix,
		"webhookUrl":       c.WebhookURL,
		"webhookSecretSet": c.WebhookSecret != "",
		"urlTtlSeconds":    c.URLTTLSeconds,
	}
}
