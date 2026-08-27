package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

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
	waLogger  waLog.Logger
	log       *slog.Logger
	maxCalls  int

	mu       sync.RWMutex
	sessions map[string]*Session
	order    []string
}

func newSessionManager(ctx context.Context, container *sqlstore.Container, broker *Broker, store *sessionStore, waLogger waLog.Logger, log *slog.Logger, maxCalls int) *SessionManager {
	return &SessionManager{
		appCtx:    ctx,
		container: container,
		broker:    broker,
		store:     store,
		waLogger:  waLogger,
		log:       log,
		maxCalls:  maxCalls,
		sessions:  map[string]*Session{},
	}
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
	return []any{map[string]any{"type": "session-list", "sessions": m.infos()}}
}

func (m *SessionManager) getWebhookURL(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[id]; ok {
		return s.webhookURL
	}
	return ""
}

func (m *SessionManager) Restore(ctx context.Context) error {
	rows, err := m.store.list(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.JID == "" {
			device := m.container.NewDevice()
			client := whatsmeow.NewClient(device, m.waLogger)
			s := newSession(m, row.ID, row.Name, row.WebhookURL, client)
			s.auth = AuthSnapshot{State: "logged_out", Paired: false}
			m.register(s)
			continue
		}
		jid, parseErr := types.ParseJID(row.JID)
		if parseErr != nil {
			m.log.Warn("session has unparseable jid; preserving session in logged_out state", "session", row.ID, "jid", row.JID)
			device := m.container.NewDevice()
			client := whatsmeow.NewClient(device, m.waLogger)
			s := newSession(m, row.ID, row.Name, row.WebhookURL, client)
			s.auth = AuthSnapshot{State: "logged_out", Paired: false}
			m.register(s)
			continue
		}
		device, err := m.container.GetDevice(ctx, jid)
		if err != nil || device == nil {
			m.log.Warn("session device not found; preserving session in logged_out state", "session", row.ID, "jid", row.JID)
			device = m.container.NewDevice()
			client := whatsmeow.NewClient(device, m.waLogger)
			s := newSession(m, row.ID, row.Name, row.WebhookURL, client)
			s.auth = AuthSnapshot{State: "logged_out", Paired: false}
			m.register(s)
			continue
		}
		client := whatsmeow.NewClient(device, m.waLogger)
		s := newSession(m, row.ID, row.Name, row.WebhookURL, client)
		m.register(s)
		if err := s.connect(ctx); err != nil {
			m.log.Error("session connect failed", "session", row.ID, "err", err)
		}
	}
	m.broker.emitSessionList(m.infos())
	m.log.Info("sessions restored", "count", len(m.infos()))
	return nil
}

func (m *SessionManager) Create(name string) (string, error) {
	id := newSessionID()
	if err := m.store.insert(m.appCtx, id, name); err != nil {
		return "", err
	}
	device := m.container.NewDevice()
	client := whatsmeow.NewClient(device, m.waLogger)
	s := newSession(m, id, name, "", client)
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
