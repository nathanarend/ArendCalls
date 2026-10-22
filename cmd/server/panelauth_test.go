package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const testPanelPassword = "senha-forte-123"

func enablePanelLogin(t *testing.T, srv *server) *panelAuth {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(testPanelPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	p, err := newPanelAuth("admin", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	srv.panel = p
	return p
}

func doLogin(t *testing.T, url, user, password, ip string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": password})
	req, _ := http.NewRequest("POST", url+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.Header.Set("CF-Connecting-IP", ip)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func getWithCookie(t *testing.T, url string, c *http.Cookie) int {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if c != nil {
		req.AddCookie(c)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func sessionCookie(res *http.Response) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == panelSessionCookie {
			return c
		}
	}
	return nil
}

func TestPanelAuthStatus(t *testing.T) {
	srv, ts := newTestServer(t, "", "admin-token")

	var st map[string]bool
	res, _ := http.Get(ts.URL + "/api/auth/status")
	json.NewDecoder(res.Body).Decode(&st)
	res.Body.Close()
	if st["authRequired"] {
		t.Fatalf("sem hash, login não deveria ser exigido: %v", st)
	}

	enablePanelLogin(t, srv)
	res, _ = http.Get(ts.URL + "/api/auth/status")
	json.NewDecoder(res.Body).Decode(&st)
	res.Body.Close()
	if !st["authRequired"] || st["authenticated"] {
		t.Fatalf("com hash: esperado authRequired=true authenticated=false, veio %v", st)
	}
}

func TestPanelLoginFlow(t *testing.T) {
	srv, ts := newTestServer(t, "api-key", "admin-token")
	enablePanelLogin(t, srv)

	if code := getWithCookie(t, ts.URL+"/api/sessions", nil); code != http.StatusUnauthorized {
		t.Fatalf("sem cookie: esperado 401, veio %d", code)
	}
	// Com login ligado, o cookie automático antigo não vale mais.
	if code := getWithCookie(t, ts.URL+"/api/sessions", &http.Cookie{Name: "wacalls_admin_token", Value: "admin-token"}); code != http.StatusUnauthorized {
		t.Fatalf("cookie automático: esperado 401, veio %d", code)
	}

	if res := doLogin(t, ts.URL, "admin", "errada", ""); res.StatusCode != http.StatusUnauthorized || sessionCookie(res) != nil {
		t.Fatalf("senha errada: esperado 401 sem cookie, veio %d", res.StatusCode)
	}
	if res := doLogin(t, ts.URL, "outro", testPanelPassword, ""); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("usuário errado: esperado 401, veio %d", res.StatusCode)
	}

	res := doLogin(t, ts.URL, "admin", testPanelPassword, "")
	c := sessionCookie(res)
	if res.StatusCode != http.StatusOK || c == nil {
		t.Fatalf("login certo: esperado 200 com cookie, veio %d", res.StatusCode)
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie sem HttpOnly/SameSite=Strict: %+v", c)
	}
	if code := getWithCookie(t, ts.URL+"/api/sessions", c); code != http.StatusOK {
		t.Fatalf("com sessão: esperado 200, veio %d", code)
	}

	// API key segue valendo para integrações.
	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer api-key")
	apiRes, _ := http.DefaultClient.Do(req)
	apiRes.Body.Close()
	if apiRes.StatusCode != http.StatusOK {
		t.Fatalf("API key: esperado 200, veio %d", apiRes.StatusCode)
	}

	logoutReq, _ := http.NewRequest("POST", ts.URL+"/api/auth/logout", nil)
	logoutReq.AddCookie(c)
	logoutRes, _ := http.DefaultClient.Do(logoutReq)
	logoutRes.Body.Close()
	if code := getWithCookie(t, ts.URL+"/api/sessions", c); code != http.StatusUnauthorized {
		t.Fatalf("após logout: esperado 401, veio %d", code)
	}
}

func TestPanelLoginLockout(t *testing.T) {
	srv, ts := newTestServer(t, "", "admin-token")
	enablePanelLogin(t, srv)

	for i := 0; i < loginMaxFails; i++ {
		doLogin(t, ts.URL, "admin", "errada", "203.0.113.7")
	}
	res := doLogin(t, ts.URL, "admin", testPanelPassword, "203.0.113.7")
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Fatalf("IP bloqueado: esperado 429 com Retry-After, veio %d", res.StatusCode)
	}
	if res := doLogin(t, ts.URL, "admin", testPanelPassword, "198.51.100.9"); res.StatusCode != http.StatusOK {
		t.Fatalf("outro IP: esperado 200, veio %d", res.StatusCode)
	}
}

func TestPanelLockoutGrows(t *testing.T) {
	p := &panelAuth{fails: map[string]*loginFails{}, now: time.Now}
	for i := 0; i < loginMaxFails; i++ {
		p.recordFailure("ip")
	}
	first := p.lockedFor("ip")
	p.recordFailure("ip")
	if second := p.lockedFor("ip"); second <= first {
		t.Fatalf("bloqueio deveria crescer: %v → %v", first, second)
	}
	for i := 0; i < 40; i++ {
		p.recordFailure("ip")
	}
	if got := p.lockedFor("ip"); got > loginMaxLock {
		t.Fatalf("bloqueio passou do teto: %v", got)
	}
}

func TestPanelSessionExpires(t *testing.T) {
	now := time.Now()
	p := &panelAuth{sessions: map[string]time.Time{}, fails: map[string]*loginFails{}, now: func() time.Time { return now }}
	token, _ := p.newSession("ip")
	if !p.valid(token) {
		t.Fatal("sessão nova deveria valer")
	}
	now = now.Add(panelSessionTTL + time.Second)
	if p.valid(token) {
		t.Fatal("sessão expirada não deveria valer")
	}
}

func TestPanelInvalidHash(t *testing.T) {
	if _, err := newPanelAuth("admin", "senha-em-texto"); err == nil {
		t.Fatal("hash inválido deveria falhar na subida")
	}
}

func TestPanelStaticCookieOnlyWhenOpen(t *testing.T) {
	srv, _ := newTestServer(t, "", "admin-token")
	srv.staticDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(srv.staticDir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	hasAdminCookie := func() bool {
		ts := httptest.NewServer(srv.routes())
		defer ts.Close()
		res, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		for _, c := range res.Cookies() {
			if strings.HasPrefix(c.Name, "wacalls_admin_token") {
				return true
			}
		}
		return false
	}

	if !hasAdminCookie() {
		t.Fatal("sem login: painel deveria entregar o cookie automático")
	}
	enablePanelLogin(t, srv)
	if hasAdminCookie() {
		t.Fatal("com login: painel não pode entregar o cookie automático")
	}
}
