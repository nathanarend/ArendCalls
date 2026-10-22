package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type SessionManager struct {
	appCtx    context.Context
	container *sqlstore.Container
	broker    *Broker
	store     *sessionStore
	rec       *recordingController
	waLogger  waLog.Logger
	log       *slog.Logger
	maxCalls  int

	mu               sync.RWMutex
	sessions         map[string]*Session
	order            []string
	panelInboundCall atomic.Bool // global switch: panel shows incoming calls

	// blocked: migração exige tudo parado. Nenhuma conta conecta nem pareia.
	blocked atomic.Bool
}

var errMigrationBlocked = errors.New("instância bloqueada por migração")

func newSessionManager(ctx context.Context, container *sqlstore.Container, broker *Broker, store *sessionStore, waLogger waLog.Logger, log *slog.Logger, maxCalls int) *SessionManager {
	m := &SessionManager{
		appCtx:    ctx,
		container: container,
		broker:    broker,
		store:     store,
		waLogger:  waLogger,
		log:       log,
		maxCalls:  maxCalls,
		sessions:  map[string]*Session{},
	}
	m.panelInboundCall.Store(true)
	m.startPresenceKeepalive()
	return m
}

func (m *SessionManager) register(s *Session) {
	m.mu.Lock()
	m.sessions[s.id] = s
	m.order = append(m.order, s.id)
	m.mu.Unlock()
}

func (m *SessionManager) unregister(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	for i, x := range m.order {
		if x == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
}

func (m *SessionManager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[id]; ok {
		return s, true
	}
	for _, s := range m.sessions {
		if strings.EqualFold(s.name, id) || strings.EqualFold(s.id, id) {
			return s, true
		}
	}
	return nil, false
}

func (m *SessionManager) FindSessionByCall(callID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if _, ok := s.reg.get(callID); ok {
			return s, true
		}
	}
	return nil, false
}

func (m *SessionManager) infos() []SessionInfo {
	m.mu.RLock()
	ordered := make([]*Session, 0, len(m.order))
	for _, id := range m.order {
		if s, ok := m.sessions[id]; ok {
			ordered = append(ordered, s)
		}
	}
	m.mu.RUnlock()
	out := make([]SessionInfo, 0, len(ordered))
	for _, s := range ordered {
		out = append(out, s.info())
	}
	return out
}

func (m *SessionManager) snapshotEvents() []any {
	return []any{map[string]any{
		"type": "session-list", "sessions": m.infos(),
		"panelInboundCalls": m.PanelInboundCalls(),
	}}
}

func (m *SessionManager) getWebhookURL(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[id]; ok {
		return s.webhookURL
	}
	return ""
}

// PanelInboundCalls is the global "panel shows incoming calls" switch.
func (m *SessionManager) PanelInboundCalls() bool { return m.panelInboundCall.Load() }

// SetPanelInboundCalls flips the global switch and broadcasts the change.
func (m *SessionManager) SetPanelInboundCalls(ctx context.Context, enabled bool) error {
	if err := m.store.setPanelInboundCalls(ctx, enabled); err != nil {
		return err
	}
	m.panelInboundCall.Store(enabled)
	m.broker.emitSessionList(m.infos())
	return nil
}

func (m *SessionManager) Restore(ctx context.Context) error {
	if enabled, err := m.store.panelInboundCalls(ctx); err == nil {
		m.panelInboundCall.Store(enabled)
	} else {
		m.panelInboundCall.Store(true)
	}
	rows, err := m.store.list(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.JID == "" {
			device := m.container.NewDevice()
			client := whatsmeow.NewClient(device, m.waLogger)
			s := newSession(m, row.ID, row.Name, row.WebhookURL, row.PanelInbound, client)
			s.auth = AuthSnapshot{State: "logged_out", Paired: false}
			m.register(s)
			continue
		}
		jid, parseErr := types.ParseJID(row.JID)
		if parseErr != nil {
			m.log.Warn("session has unparseable jid; preserving session in logged_out state", "session", row.ID, "jid", row.JID)
			device := m.container.NewDevice()
			client := whatsmeow.NewClient(device, m.waLogger)
			s := newSession(m, row.ID, row.Name, row.WebhookURL, row.PanelInbound, client)
			s.auth = AuthSnapshot{State: "logged_out", Paired: false}
			m.register(s)
			continue
		}
		device, err := m.container.GetDevice(ctx, jid)
		if err != nil || device == nil {
			m.log.Warn("session device not found; preserving session in logged_out state", "session", row.ID, "jid", row.JID)
			device = m.container.NewDevice()
			client := whatsmeow.NewClient(device, m.waLogger)
			s := newSession(m, row.ID, row.Name, row.WebhookURL, row.PanelInbound, client)
			s.auth = AuthSnapshot{State: "logged_out", Paired: false}
			m.register(s)
			continue
		}
		client := whatsmeow.NewClient(device, m.waLogger)
		s := newSession(m, row.ID, row.Name, row.WebhookURL, row.PanelInbound, client)
		m.register(s)
		m.log.Info("session restored from database", "session", row.ID, "name", row.Name, "jid", row.JID)
		if m.blocked.Load() {
			s.auth = AuthSnapshot{State: "stopped", Paired: true}
			continue
		}
		if err := s.connect(ctx); err != nil {
			m.log.Error("session connect failed", "session", row.ID, "err", err)
		}
	}
	m.broker.emitSessionList(m.infos())
	m.log.Info("sessions restored", "count", len(m.infos()))
	return nil
}

func (m *SessionManager) Create(name string) (string, error) {
	if m.blocked.Load() {
		return "", errMigrationBlocked
	}
	id := newSessionID()
	if err := m.store.insert(m.appCtx, id, name); err != nil {
		return "", err
	}
	device := m.container.NewDevice()
	client := whatsmeow.NewClient(device, m.waLogger)
	// panelInbound nasce desligado, igual ao DEFAULT 0 do banco: é override.
	s := newSession(m, id, name, "", false, client)
	m.register(s)
	m.broker.emitSessionList(m.infos())
	if err := s.startPairing(m.appCtx); err != nil {
		m.log.Error("start pairing failed", "session", id, "err", err)
		return "", fmt.Errorf("start pairing: %w", err)
	}
	m.log.Info("session created", "session", id, "name", name)
	return id, nil
}

func (m *SessionManager) Rename(ctx context.Context, id, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("no session %s", id)
	}

	if err := m.store.updateName(ctx, id, name); err != nil {
		return fmt.Errorf("update name in store: %w", err)
	}

	s.name = name
	m.broker.emitSessionList(m.infosLocked())
	return nil
}

func (m *SessionManager) infosLocked() []SessionInfo {
	ordered := make([]*Session, 0, len(m.order))
	for _, id := range m.order {
		if s, ok := m.sessions[id]; ok {
			ordered = append(ordered, s)
		}
	}
	out := make([]SessionInfo, 0, len(ordered))
	for _, s := range ordered {
		out = append(out, s.info())
	}
	return out
}

func (m *SessionManager) Delete(ctx context.Context, id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if s.client.Store.ID != nil {
		if err := s.client.Logout(ctx); err != nil {
			m.log.Warn("logout failed; deleting locally", "session", id, "err", err)
			_ = m.container.DeleteDevice(ctx, s.client.Store)
		}
	} else {
		s.client.Disconnect()
		_ = m.container.DeleteDevice(ctx, s.client.Store)
	}
	s.teardownAllCalls()
	m.unregister(id)
	_ = m.store.delete(ctx, id)
	m.broker.emitSessionList(m.infos())
	m.log.Info("session deleted", "session", id)
	return nil
}

func (m *SessionManager) Logout(ctx context.Context, id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if s.client.Store.ID != nil {
		if err := s.client.Logout(ctx); err != nil {
			m.log.Warn("logout failed", "session", id, "err", err)
		}
	}
	s.replaceClient(whatsmeow.NewClient(m.container.NewDevice(), m.waLogger))
	_ = m.store.setJID(ctx, id, "")
	s.setAuth(AuthSnapshot{State: "logged_out", Paired: false})
	m.log.Info("session disconnected", "session", id)
	return nil
}

func (m *SessionManager) Stop(ctx context.Context, id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	s.shutdown()
	s.setAuth(AuthSnapshot{State: "stopped", Paired: s.client.Store.ID != nil})
	m.log.Info("session stopped", "session", id)
	return nil
}

func (m *SessionManager) Start(ctx context.Context, id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if m.blocked.Load() {
		return errMigrationBlocked
	}
	if s.client.IsConnected() {
		return nil
	}
	if s.client.Store.ID != nil {
		s.setAuth(AuthSnapshot{State: "connecting", Paired: true})
		if err := s.client.Connect(); err != nil {
			m.log.Error("session start connect failed", "session", id, "err", err)
			return fmt.Errorf("connect failed: %w", err)
		}
		return nil
	}
	return m.Pair(id)
}

func (m *SessionManager) Pair(id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if m.blocked.Load() {
		return errMigrationBlocked
	}
	if s.client.Store.ID != nil {
		return fmt.Errorf("session already paired")
	}
	s.replaceClient(whatsmeow.NewClient(m.container.NewDevice(), m.waLogger))
	if err := s.startPairing(m.appCtx); err != nil {
		return fmt.Errorf("start pairing: %w", err)
	}
	m.broker.emitSessionList(m.infos())
	m.log.Info("session re-pairing", "session", id)
	return nil
}

func (m *SessionManager) disconnectAll() {
	m.mu.RLock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.RUnlock()
	for _, s := range all {
		s.shutdown()
	}
}

func (m *SessionManager) all() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.order))
	for _, id := range m.order {
		if s, ok := m.sessions[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

func (m *SessionManager) activeCallCount() int {
	n := 0
	for _, s := range m.all() {
		n += s.reg.count()
	}
	return n
}

func (m *SessionManager) pairedCount() int {
	n := 0
	for _, s := range m.all() {
		if s.currentClient().Store.ID != nil {
			n++
		}
	}
	return n
}

// stopForMigration derruba todas as conexões sem logout. Auto-reconnect
// desligado para nenhum reconnect em voo religar a conta depois do Disconnect.
func (m *SessionManager) stopForMigration() {
	for _, s := range m.all() {
		c := s.currentClient()
		c.EnableAutoReconnect = false
		s.shutdown()
		s.setAuth(AuthSnapshot{State: "stopped", Paired: c.Store.ID != nil})
	}
}

// connectedCount: contas pareadas conectadas agora e as que conectaram em algum
// momento desde o registro (connectedOnce).
func (m *SessionManager) connectedCount() (now, ever, paired int, missing []string) {
	for _, s := range m.all() {
		c := s.currentClient()
		if c.Store.ID == nil {
			continue
		}
		paired++
		if c.IsConnected() && c.IsLoggedIn() {
			now++
		}
		if s.connectedOnce.Load() {
			ever++
		} else {
			missing = append(missing, s.name)
		}
	}
	return now, ever, paired, missing
}

// reconnectAll religa as contas pareadas (fim do bloqueio de migração).
func (m *SessionManager) reconnectAll() {
	for _, s := range m.all() {
		c := s.currentClient()
		if c.Store.ID == nil {
			continue
		}
		c.EnableAutoReconnect = true
		s.setAuth(AuthSnapshot{State: "connecting", Paired: true})
		if err := c.Connect(); err != nil {
			m.log.Error("session reconnect failed", "session", s.id, "err", err)
		}
	}
}

// dropAll derruba e esquece todas as sessões em memória (o banco fica como está).
func (m *SessionManager) dropAll() {
	for _, s := range m.all() {
		s.currentClient().EnableAutoReconnect = false
		s.shutdown()
	}
	m.mu.Lock()
	m.sessions = map[string]*Session{}
	m.order = nil
	m.mu.Unlock()
}

// reload troca o container (cache de LID novo) e recarrega as sessões do banco.
func (m *SessionManager) reload(ctx context.Context, container *sqlstore.Container) error {
	m.dropAll()
	m.mu.Lock()
	m.container = container
	m.mu.Unlock()
	return m.Restore(ctx)
}

func (m *SessionManager) SetWebhookURL(ctx context.Context, id, webhookURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("no session %s", id)
	}

	if err := m.store.setWebhookURL(ctx, id, webhookURL); err != nil {
		return fmt.Errorf("update webhook_url in store: %w", err)
	}

	s.mu.Lock()
	s.webhookURL = webhookURL
	s.mu.Unlock()
	m.broker.emitSessionList(m.infosLocked())
	return nil
}

func (m *SessionManager) SetPanelInbound(ctx context.Context, id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if err := m.store.setPanelInbound(ctx, id, enabled); err != nil {
		return fmt.Errorf("update panel_inbound in store: %w", err)
	}
	s.mu.Lock()
	s.panelInbound = enabled
	s.mu.Unlock()
	m.broker.emitSessionList(m.infosLocked())
	return nil
}

func (m *SessionManager) SessionCounts() (total int, connected int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total = len(m.sessions)
	for _, s := range m.sessions {
		s.mu.Lock()
		client := s.client
		s.mu.Unlock()
		if client != nil && client.IsConnected() {
			connected++
		}
	}
	return total, connected
}

// presencePulseDuration é quanto tempo a sessão fica "available" em cada pulso
// do keepalive antes de voltar para unavailable.
const presencePulseDuration = 30 * time.Second

// presencePulseMinGap evita pulsar em cada reconexão de uma rajada (queda de
// rede): dentro dele o connect só manda unavailable.
const presencePulseMinGap = time.Hour

func (m *SessionManager) startPresenceKeepalive() {
	interval := 24 * time.Hour
	if durVal := os.Getenv("WA_PRESENCE_INTERVAL"); durVal != "" {
		if d, err := time.ParseDuration(durVal); err == nil && d > 0 {
			interval = d
		}
	} else if envVal := os.Getenv("WA_PRESENCE_INTERVAL_HOURS"); envVal != "" {
		if h, err := strconv.Atoi(envVal); err == nil && h > 0 {
			interval = time.Duration(h) * time.Hour
		}
	}
	m.log.Info("presence keepalive worker started", "interval", interval)
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-m.appCtx.Done():
				return
			case <-ticker.C:
				m.pulseAll()
			}
		}
	}()
}

func (m *SessionManager) pulseAll() {
	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()

	for _, s := range sessions {
		s.mu.Lock()
		client := s.client
		s.mu.Unlock()
		if client != nil && client.IsConnected() {
			go s.presencePulse(m.appCtx)
		}
	}
}
