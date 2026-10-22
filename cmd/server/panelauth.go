package main

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

// Login do painel web: um admin só, senha guardada como hash bcrypt em
// ADMIN_PASSWORD_HASH. Sem o hash, o painel segue aberto como antes (cookie
// entregue ao abrir "/") — o servidor só passa a exigir login quando a
// variável existe.

const (
	panelSessionCookie = "arendcalls_session"
	panelSessionTTL    = 7 * 24 * time.Hour
	panelHashCost      = 12

	// Depois de loginMaxFails erros seguidos, o IP espera loginBaseLock,
	// dobrando a cada novo erro até loginMaxLock.
	loginMaxFails = 5
	loginBaseLock = time.Minute
	loginMaxLock  = 15 * time.Minute
)

type loginFails struct {
	count       int
	lockedUntil time.Time
}

type panelAuth struct {
	user string
	hash []byte
	now  func() time.Time

	mu       sync.Mutex
	sessions map[string]time.Time // token → expira em
	fails    map[string]*loginFails
}

func newPanelAuth(user, hash string) (*panelAuth, error) {
	if user == "" {
		user = "admin"
	}
	if _, err := bcrypt.Cost([]byte(hash)); err != nil {
		return nil, fmt.Errorf("ADMIN_PASSWORD_HASH inválido (gere com -hash-password): %w", err)
	}
	return &panelAuth{
		user:     user,
		hash:     []byte(hash),
		now:      time.Now,
		sessions: map[string]time.Time{},
		fails:    map[string]*loginFails{},
	}, nil
}

// check compara usuário e senha. Roda o bcrypt mesmo com usuário errado, pra
// resposta não denunciar pelo tempo qual dos dois falhou.
func (p *panelAuth) check(user, password string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(p.user)) == 1
	passOK := bcrypt.CompareHashAndPassword(p.hash, []byte(password)) == nil
	return userOK && passOK
}

// lockedFor: quanto falta para o IP poder tentar de novo (0 = liberado).
func (p *panelAuth) lockedFor(ip string) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	f := p.fails[ip]
	if f == nil {
		return 0
	}
	return max(0, f.lockedUntil.Sub(p.now()))
}

func (p *panelAuth) recordFailure(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f := p.fails[ip]
	if f == nil {
		f = &loginFails{}
		p.fails[ip] = f
	}
	f.count++
	if f.count >= loginMaxFails {
		lock := loginBaseLock << (f.count - loginMaxFails)
		if lock <= 0 || lock > loginMaxLock {
			lock = loginMaxLock
		}
		f.lockedUntil = p.now().Add(lock)
	}
}

func (p *panelAuth) newSession(ip string) (string, time.Time) {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	now := p.now()
	expires := now.Add(panelSessionTTL)

	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.fails, ip)
	for t, exp := range p.sessions {
		if now.After(exp) {
			delete(p.sessions, t)
		}
	}
	p.sessions[token] = expires
	return token, expires
}

func (p *panelAuth) valid(token string) bool {
	if token == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	exp, ok := p.sessions[token]
	if !ok {
		return false
	}
	if p.now().After(exp) {
		delete(p.sessions, token)
		return false
	}
	return true
}

func (p *panelAuth) revoke(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sessions, token)
}

// panelCookieOK: cookie de sessão válido (com login ligado) ou o
// cookie de admin automático (painel aberto).
func (s *server) panelCookieOK(r *http.Request) bool {
	if s.panel != nil {
		c, err := r.Cookie(panelSessionCookie)
		return err == nil && s.panel.valid(c.Value)
	}
	c, err := r.Cookie("wacalls_admin_token")
	return err == nil && s.adminToken != "" && c.Value == s.adminToken
}

// clientIP: atrás do Cloudflare Tunnel toda conexão vem do cloudflared local,
// então o IP real só existe no CF-Connecting-IP.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"authRequired":  s.panel != nil,
		"authenticated": s.panelCookieOK(r),
	})
}

func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.panel == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
		return
	}
	ip := clientIP(r)
	if wait := s.panel.lockedFor(ip); wait > 0 {
		secs := int(wait.Round(time.Second).Seconds())
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": "muitas tentativas, aguarde", "retryAfter": secs,
		})
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "requisição inválida"})
		return
	}
	if !s.panel.check(body.Username, body.Password) {
		s.panel.recordFailure(ip)
		s.log.Warn("panel login failed", "ip", ip, "user", body.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "usuário ou senha inválidos"})
		return
	}

	token, expires := s.panel.newSession(ip)
	http.SetCookie(w, &http.Cookie{
		Name:     panelSessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	s.log.Info("panel login", "ip", ip)
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (s *server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if s.panel != nil {
		if c, err := r.Cookie(panelSessionCookie); err == nil {
			s.panel.revoke(c.Value)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     panelSessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

// runHashPassword: `-hash-password` lê a senha (sem eco no terminal) e
// imprime o hash para colar em ADMIN_PASSWORD_HASH.
func runHashPassword(in *os.File, out io.Writer) error {
	var password string
	if term.IsTerminal(int(in.Fd())) {
		fmt.Fprint(out, "Senha: ")
		p1, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return err
		}
		fmt.Fprint(out, "Repita a senha: ")
		p2, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return err
		}
		if string(p1) != string(p2) {
			return errors.New("as senhas não conferem")
		}
		password = string(p1)
	} else {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		password = strings.TrimRight(line, "\r\n")
	}
	if len(password) < 10 {
		return errors.New("use uma senha com pelo menos 10 caracteres")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), panelHashCost)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(hash))
	return nil
}
