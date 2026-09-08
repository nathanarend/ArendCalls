package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func newTestServer(t *testing.T, apiKey, adminToken string) (*server, *httptest.Server) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "http_test.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := newSessionStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	broker := NewBroker()
	mgr := newSessionManager(ctx, container, broker, store, waLog.Noop, slog.Default(), 0)

	srv := &server{
		sessions:   mgr,
		broker:     broker,
		log:        slog.Default(),
		apiKey:     apiKey,
		adminToken: adminToken,
	}

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(func() { ts.Close() })

	return srv, ts
}

func TestAPIAuthentication(t *testing.T) {
	apiKey := "super-secret-global-key"
	adminToken := "admin-token-123"
	_, ts := newTestServer(t, apiKey, adminToken)

	t.Run("Unauthorized - No Token", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/api/sessions")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", res.StatusCode)
		}
	})

	t.Run("Unauthorized - Wrong API Key", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", res.StatusCode)
		}
	})

	t.Run("Authorized - Bearer Token Header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", res.StatusCode)
		}
	})

	t.Run("Authorized - Query Param apikey", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/api/sessions?apikey=" + apiKey)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", res.StatusCode)
		}
	})

	t.Run("Authorized - Admin Token Cookie", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
		req.AddCookie(&http.Cookie{Name: "wacalls_admin_token", Value: adminToken})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", res.StatusCode)
		}
	})
}

func TestAPISessionCRUD(t *testing.T) {
	apiKey := "test-key"
	_, ts := newTestServer(t, apiKey, "admin-token")

	doReq := func(method, path string, body any) (*http.Response, map[string]any) {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, ts.URL+path, &buf)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if res.Header.Get("Content-Type") == "application/json" {
			_ = json.NewDecoder(res.Body).Decode(&out)
		}
		return res, out
	}

	// 1. Create session
	res, data := doReq("POST", "/api/sessions", map[string]string{"name": "Conta Teste API"})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create session failed: status %d", res.StatusCode)
	}
	sid, ok := data["id"].(string)
	if !ok || sid == "" {
		t.Fatalf("expected session ID, got %+v", data)
	}

	// 2. List sessions
	res, listData := doReq("GET", "/api/sessions", nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list sessions failed: status %d", res.StatusCode)
	}
	sessions, ok := listData["sessions"].([]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	// 3. Rename session
	res, _ = doReq("PATCH", "/api/sessions/"+sid, map[string]string{"name": "Novo Nome"})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rename session failed: status %d", res.StatusCode)
	}

	// 4. Update webhook URL
	res, _ = doReq("PATCH", "/api/sessions/"+sid+"/webhook", map[string]string{"webhook_url": "https://example.com/hook"})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update webhook failed: status %d", res.StatusCode)
	}

	// 5. Get history
	res, histData := doReq("GET", "/api/sessions/"+sid+"/history", nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get history failed: status %d", res.StatusCode)
	}
	if _, ok := histData["rows"]; !ok {
		t.Fatalf("expected rows field in history response")
	}

	// 6. Delete session
	res, _ = doReq("DELETE", "/api/sessions/"+sid, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete session failed: status %d", res.StatusCode)
	}

	// 7. Verify empty list
	res, listDataAfter := doReq("GET", "/api/sessions", nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list after delete failed: status %d", res.StatusCode)
	}
	sessionsAfter := listDataAfter["sessions"].([]any)
	if len(sessionsAfter) != 0 {
		t.Fatalf("expected 0 sessions after delete, got %d", len(sessionsAfter))
	}
}

func TestAPICallEndpointsValidation(t *testing.T) {
	apiKey := "test-key"
	srv, ts := newTestServer(t, apiKey, "admin-token")

	doReq := func(method, path string, body any) *http.Response {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, ts.URL+path, &buf)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	// Create a dummy session
	res := doReq("POST", "/api/sessions", map[string]string{"name": "Conta Chamadas"})
	var createData map[string]any
	json.NewDecoder(res.Body).Decode(&createData)
	res.Body.Close()
	sid := createData["id"].(string)

	t.Run("Start Call - Session Not Paired", func(t *testing.T) {
		res := doReq("POST", "/api/sessions/"+sid+"/calls", map[string]string{"phone": "5511999999999"})
		defer res.Body.Close()
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected status 503 (not paired), got %d", res.StatusCode)
		}
	})

	t.Run("Start Call - Missing Phone (Paired Session)", func(t *testing.T) {
		sess, _ := srv.sessions.Get(sid)
		sess.client.Store.ID = &types.JID{User: "5511999999999", Server: types.DefaultUserServer}
		res := doReq("POST", "/api/sessions/"+sid+"/calls", map[string]string{})
		defer res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", res.StatusCode)
		}
	})

	t.Run("WebRTC Offer - Call Not Found", func(t *testing.T) {
		res := doReq("POST", "/api/sessions/"+sid+"/calls/nonexistent/webrtc", map[string]string{"sdp_offer": "v=0..."})
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", res.StatusCode)
		}
	})

	t.Run("Accept Call - Call Not Found", func(t *testing.T) {
		res := doReq("POST", "/api/sessions/"+sid+"/calls/nonexistent/accept", nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", res.StatusCode)
		}
	})

	t.Run("Reject Call - Nonexistent", func(t *testing.T) {
		res := doReq("POST", "/api/sessions/"+sid+"/calls/nonexistent/reject", nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("expected status 404 for nonexistent reject, got %d", res.StatusCode)
		}
	})

	t.Run("End Call - Nonexistent", func(t *testing.T) {
		res := doReq("DELETE", "/api/sessions/"+sid+"/calls/nonexistent", nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("expected status 404 for nonexistent end call, got %d", res.StatusCode)
		}
	})
}

func TestAPICORS(t *testing.T) {
	_, ts := newTestServer(t, "key", "token")
	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/sessions", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS preflight, got %d", res.StatusCode)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing or invalid Access-Control-Allow-Origin header")
	}
}

func TestAPISystemMetrics(t *testing.T) {
	_, ts := newTestServer(t, "test-super-key", "test-token")
	req, _ := http.NewRequest("GET", ts.URL+"/api/system/metrics", nil)
	req.Header.Set("X-Api-Key", "test-super-key")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var data SystemMetricsResponse
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode metrics response: %v", err)
	}

	if data.Process.Goroutines <= 0 {
		t.Errorf("expected positive goroutines, got %d", data.Process.Goroutines)
	}
	if data.Process.UptimeFormatted == "" {
		t.Errorf("expected non-empty uptime formatted string")
	}
	if data.Host.CPUCores <= 0 {
		t.Errorf("expected positive CPU cores, got %d", data.Host.CPUCores)
	}
}

func TestIsSamePhoneNumber(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"Exato igual", "556285358653", "556285358653", true},
		{"Brasil com 9 vs sem 9", "556285358653", "5562985358653", true},
		{"Brasil sem 9 vs com 9", "5562985358653", "556285358653", true},
		{"Brasil com formatação", "+55 (62) 98535-8653", "556285358653", true},
		{"DDD diferente", "556285358653", "551185358653", false},
		{"DDD diferente com 9", "556285358653", "5511985358653", false},
		{"Números diferentes", "556285358653", "556281112222", false},
		{"Internacional", "12025550123", "12025550123", true},
		{"Internacional diferente", "12025550123", "12025550124", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isSamePhoneNumber(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("isSamePhoneNumber(%q, %q) = %v; want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
