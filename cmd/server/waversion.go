package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
)

// O whatsmeow fixa a versão do cliente WA no código. Quando o WhatsApp passa a
// recusá-la (405 client outdated), todas as contas param até sair um build com
// a lib nova. Adotar a versão publicada no web.whatsapp.com cobre esse
// intervalo; mudanças de protocolo continuam exigindo atualizar a lib.

const (
	waVersionTimeout     = 15 * time.Second
	waVersionMinInterval = 10 * time.Minute
)

var (
	waVersionMu        sync.Mutex
	waVersionLastCheck time.Time
)

// refreshWAVersion adota a versão do web.whatsapp.com se for mais nova que a
// em uso. Nunca rebaixa. Depois de uma consulta bem-sucedida, as próximas em
// menos de waVersionMinInterval não saem: várias contas recebem o 405 juntas.
func refreshWAVersion(ctx context.Context, log *slog.Logger) {
	waVersionMu.Lock()
	defer waVersionMu.Unlock()
	if time.Since(waVersionLastCheck) < waVersionMinInterval {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, waVersionTimeout)
	defer cancel()
	cur := store.GetWAVersion()
	latest, err := whatsmeow.GetLatestVersion(ctx, nil)
	if err != nil {
		log.Warn("wa version check failed; keeping current", "wa_version", cur.String(), "err", err)
		return
	}
	waVersionLastCheck = time.Now()
	if !waVersionNewer(*latest, cur) {
		log.Info("wa version check", "wa_version", cur.String(), "web", latest.String())
		return
	}
	store.SetWAVersion(*latest)
	log.Info("wa version updated from web.whatsapp.com", "from", cur.String(), "to", latest.String())
}

func waVersionNewer(a, b store.WAVersionContainer) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// retryAfterOutdated reconecta a conta depois de um 405 se a versão mudou
// desde a falha. O whatsmeow não reconecta sozinho nesse caso.
func (s *Session) retryAfterOutdated(failed store.WAVersionContainer) {
	refreshWAVersion(s.mgr.appCtx, s.log)
	if store.GetWAVersion() == failed {
		s.log.Error("client outdated and no newer version on web.whatsapp.com; update ArendCalls",
			"wa_version", failed.String())
		return
	}
	queueReconnect(s.mgr.appCtx, s, func() bool {
		s.mu.Lock()
		state := s.auth.State
		s.mu.Unlock()
		// Parada pelo painel ou migração em curso enquanto esperava na fila.
		if state != "connecting" || s.mgr.blocked.Load() {
			return false
		}
		c := s.currentClient()
		if c.Store.ID == nil || c.IsConnected() {
			return false
		}
		s.log.Info("reconnecting with updated wa version", "wa_version", store.GetWAVersion().String())
		if err := c.Connect(); err != nil {
			s.log.Error("reconnect after client outdated failed", "err", err)
			return false
		}
		return true
	})
}

const (
	waReconnectGap  = 15 * time.Second
	waReconnectWait = 30 * time.Second
)

// waReconnectSlot serializa as reconexões por troca de versão (madrugada e
// 405): uma conta por vez, esperando ela abrir e mais um intervalo, para não
// abrir todas as conexões com o WhatsApp no mesmo instante.
var waReconnectSlot = make(chan struct{}, 1)

// queueReconnect roda fn quando chegar a vez da conta. fn retorna se iniciou
// uma conexão; só nesse caso a fila espera a conta abrir e o intervalo.
func queueReconnect(ctx context.Context, s *Session, fn func() bool) {
	select {
	case waReconnectSlot <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-waReconnectSlot }()
	if !fn() {
		return
	}
	if !s.waitOpen(ctx, waReconnectWait) {
		s.log.Warn("reconnect for wa version: not open yet; moving on", "waited", waReconnectWait)
	}
	select {
	case <-time.After(waReconnectGap):
	case <-ctx.Done():
	}
}

func (s *Session) waitOpen(ctx context.Context, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		s.mu.Lock()
		state := s.auth.State
		s.mu.Unlock()
		if state == "open" {
			return true
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

const (
	waVersionDefaultHour = 3
	waVersionRetryCall   = 10 * time.Minute
	waVersionMaxRetries  = 12
)

// startWAVersionWorker checa a versão toda madrugada. Se mudou, reconecta as
// contas abertas para já usarem a nova, em vez de esperar o WhatsApp recusar a
// antiga no meio do dia. Hora local configurável por WA_VERSION_CHECK_HOUR (0–23).
func (s *server) startWAVersionWorker(ctx context.Context) {
	hour := waVersionDefaultHour
	if v := os.Getenv("WA_VERSION_CHECK_HOUR"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h >= 0 && h <= 23 {
			hour = h
		} else {
			s.log.Warn("invalid WA_VERSION_CHECK_HOUR; using default", "value", v, "default", hour)
		}
	}
	s.log.Info("wa version worker started", "hour", hour)
	go func() {
		for {
			timer := time.NewTimer(time.Until(nextLocalHour(time.Now(), hour)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.nightlyWAVersionCheck(ctx)
			}
		}
	}()
}

func nextLocalHour(now time.Time, hour int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (s *server) nightlyWAVersionCheck(ctx context.Context) {
	if s.migrationActive() {
		s.log.Info("wa version check skipped: migration in progress")
		return
	}
	before := store.GetWAVersion()
	refreshWAVersion(ctx, s.log)
	if store.GetWAVersion() == before {
		return
	}
	pending := s.sessions.all()
	for attempt := 0; len(pending) > 0; attempt++ {
		if attempt > 0 {
			if attempt > waVersionMaxRetries {
				s.log.Warn("wa version: sessions still in call; new version applies on their next reconnect",
					"count", len(pending))
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(waVersionRetryCall):
			}
			if s.migrationActive() {
				s.log.Info("wa version reconnect aborted: migration in progress")
				return
			}
		}
		var busy []*Session
		for _, sess := range pending {
			if sess.reg.count() > 0 {
				busy = append(busy, sess)
				continue
			}
			inCall := false
			queueReconnect(ctx, sess, func() bool {
				// A ligação pode ter começado enquanto a conta esperava na fila.
				if sess.reg.count() > 0 {
					inCall = true
					return false
				}
				return !s.migrationActive() && sess.reconnectForVersion()
			})
			if inCall {
				busy = append(busy, sess)
			}
		}
		pending = busy
	}
}

// migrationActive: bloqueio de contas ou job de migração ainda rodando.
func (s *server) migrationActive() bool {
	if s.sessions.blocked.Load() {
		return true
	}
	st, _, running := s.mig.snapshot()
	return running || st.blocksSessions() || st.targetBusy()
}

// reconnectForVersion derruba e religa a conta para o handshake usar a versão
// nova. Só mexe em conta aberta: parada pelo painel, deslogada ou ainda
// conectando fica como está. Retorna se iniciou a conexão.
func (s *Session) reconnectForVersion() bool {
	s.mu.Lock()
	state := s.auth.State
	s.mu.Unlock()
	c := s.currentClient()
	if state != "open" || c.Store.ID == nil || !c.IsConnected() {
		return false
	}
	s.log.Info("reconnecting to apply new wa version", "wa_version", store.GetWAVersion().String())
	c.Disconnect()
	s.setAuth(AuthSnapshot{State: "connecting", Paired: true})
	if err := c.Connect(); err != nil {
		s.log.Error("reconnect for wa version failed", "err", err)
		return false
	}
	return true
}
