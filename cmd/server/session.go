package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"wacalls/internal/voip/call"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/signaling"
	"wacalls/internal/voip/wanode"
	"wacalls/internal/wa"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type Session struct {
	id         string
	name       string
	webhookURL string
	mgr        *SessionManager
	log        *slog.Logger

	client *whatsmeow.Client
	reg    *callRegistry

	mu   sync.Mutex
	auth AuthSnapshot
}

func newSession(mgr *SessionManager, id, name, webhookURL string, client *whatsmeow.Client) *Session {
	s := &Session{
		id:         id,
		name:       name,
		webhookURL: webhookURL,
		mgr:        mgr,
		log:        mgr.log.With("session", id),
		client:     client,
		auth:       AuthSnapshot{State: "connecting"},
		reg:        newCallRegistry(),
	}
	client.AddEventHandler(s.handleEvent)
	return s
}

func (s *Session) createCall(callID string) *call.CallManager {
	cm := call.NewCallManager(wa.NewSocket(s.client), s.log)
	s.wireCall(cm, callID)
	s.reg.add(callID, &activeCall{cm: cm})
	return cm
}

func (s *Session) resolvePeerJID(peerStr string) string {
	peerJID, err := types.ParseJID(peerStr)
	if err != nil {
		return peerStr
	}
	if peerJID.Server == "lid" {
		pnJID, err := s.client.Store.LIDs.GetPNForLID(context.Background(), peerJID)
		if err == nil && !pnJID.IsEmpty() {
			return pnJID.String()
		}
	}
	return peerStr
}

func (s *Session) resolvePeerName(peerStr string) string {
	peerJID, err := types.ParseJID(peerStr)
	if err != nil {
		return ""
	}
	if peerJID.Server == "lid" {
		pnJID, err := s.client.Store.LIDs.GetPNForLID(context.Background(), peerJID)
		if err == nil && !pnJID.IsEmpty() {
			peerJID = pnJID
		}
	}
	
	if s.client.Store.Contacts != nil {
		contactInfo, err := s.client.Store.Contacts.GetContact(context.Background(), peerJID)
		if err == nil && contactInfo.Found {
			if contactInfo.FullName != "" {
				return contactInfo.FullName
			}
			if contactInfo.FirstName != "" {
				return contactInfo.FirstName
			}
			if contactInfo.PushName != "" {
				return contactInfo.PushName
			}
			if contactInfo.BusinessName != "" {
				return contactInfo.BusinessName
			}
		}
	}
	return ""
}

func (s *Session) wireCall(cm *call.CallManager, callID string) {
	var timeoutTimer *time.Timer
	var timeoutMu sync.Mutex

	stopTimeout := func() {
		timeoutMu.Lock()
		defer timeoutMu.Unlock()
		if timeoutTimer != nil {
			timeoutTimer.Stop()
			timeoutTimer = nil
		}
	}

	startTimeout := func() {
		timeoutMu.Lock()
		defer timeoutMu.Unlock()
		if timeoutTimer != nil {
			timeoutTimer.Stop()
		}
		timeoutTimer = time.AfterFunc(90*time.Second, func() {
			// Antes de encerrar, verificar se a chamada já está ativa em outra sessão.
			// Isso evita que sessões-espelho (multi-device) derrubem chamadas legítimas.
			if s.mgr.broker.isCallConnected(callID) {
				s.log.Info("call ringing timeout: call already active in broker, skipping EndCall", "call_id", callID)
				s.removeCall(callID)
				return
			}
			s.log.Info("call ringing timeout reached (90s), ending stale call", "call_id", callID)
			_ = cm.EndCall(context.Background(), core.EndCallReason("timeout"))
		})
	}

	// Inicia o timer de 60s para evitar chamada zumbi
	startTimeout()

	cm.OnIncoming = func(c *call.CallInfo) {
		peer := s.resolvePeerJID(c.PeerJid)
		peerName := s.resolvePeerName(c.PeerJid)
		s.mgr.broker.upsertCall(CallRecord{
			SessionID: s.id, CallID: c.CallID, Direction: "inbound", Peer: peer, PeerName: peerName,
			StartedAt: time.Now().UnixMilli(), Status: StatusRinging,
		})
		s.mgr.broker.emitIncoming(s.id, c.CallID, peer, peerName)
	}
	cm.OnStateChange = func(c *call.CallInfo) {
		if c.IsEnded() {
			stopTimeout()
			s.removeCall(c.CallID)
			s.mgr.broker.endCall(c.CallID, string(c.StateData.EndReason))
			return
		}
		if c.StateData.State == core.CallStateActive || c.StateData.State == core.CallStateConnecting {
			stopTimeout()
		}
		dir := "outbound"
		if c.Direction == core.CallDirectionIncoming {
			dir = "inbound"
		}
		existing, _ := s.mgr.broker.getCall(c.CallID)
		peer := s.resolvePeerJID(c.PeerJid)
		peerName := s.resolvePeerName(c.PeerJid)
		rec := CallRecord{
			SessionID: s.id, CallID: c.CallID, Direction: dir, Peer: peer, PeerName: peerName,
			StartedAt: time.Now().UnixMilli(), Status: mapStatus(c.StateData.State),
		}
		s.log.Info("call state change", "call_id", c.CallID, "raw_state", c.StateData.State, "mapped_status", rec.Status)
		if existing != nil {
			rec.Owner = existing.Owner
			rec.StartedAt = existing.StartedAt
		}
		s.mgr.broker.upsertCall(rec)
	}
	cm.OnEnded = func(c *call.CallInfo) {
		stopTimeout()
		s.removeCall(c.CallID)
		s.mgr.broker.endCall(c.CallID, string(c.StateData.EndReason))
	}
	cm.OnPeerAudio = func(pcm16 []float32) {
		ac, ok := s.reg.get(callID)
		if !ok || ac.bridge == nil {
			return
		}
		_ = ac.bridge.WritePCM(pcm16)
	}
	// Cancela o timer anti-zombie quando o relay de mídia conecta.
	// Crucial para sessões-espelho (multi-device) que ficam em IncomingRinging
	// enquanto outra sessão já aceitou a chamada — sem isso o timer derruba a ligação.
	cm.OnRelayConnected = func() {
		s.log.Info("relay connected: cancelling ringing timeout", "call_id", callID)
		stopTimeout()
	}
}

func (s *Session) startOutgoing(ctx context.Context, peer types.JID, isVideo bool) (string, error) {
	callID := signaling.GenerateCallID()
	cm := s.createCall(callID)
	if err := cm.StartCall(ctx, callID, peer, isVideo); err != nil {
		s.removeCall(callID)
		return "", err
	}
	return callID, nil
}

func (s *Session) callForEvent(from types.JID, data *waBinary.Node) (*activeCall, bool) {
	callID := callIDFromNode(wrapCall(from, data))
	if callID == "" {
		return nil, false
	}
	return s.reg.get(callID)
}

func (s *Session) onIncomingOffer(ctx context.Context, evt *events.CallOffer) {
	node := wrapCall(evt.From, evt.Data)
	callID := callIDFromNode(node)
	if callID == "" {
		return
	}
	// Se a chamada já existe (foi iniciada como saída por esta sessão), ignorar oferta duplicada
	if _, exists := s.reg.get(callID); exists {
		return
	}
	if s.client.Store.ID != nil {
		info := signaling.ExtractNodeInfo(node)
		if info != nil {
			creator := wanode.AttrString(info.InnerNode.Attrs, "call-creator")
			if creator != "" && (creator == s.client.Store.ID.String() || wanode.MustJID(creator).User == s.client.Store.ID.User) {
				s.log.Info("ignoring self-originated outgoing call offer", "call_id", callID)
				return
			}
		}
	}
	if !evt.Timestamp.IsZero() && time.Since(evt.Timestamp) > 2*time.Minute {
		s.log.Info("ignoring stale inbound call offer", "call_id", callID, "age", time.Since(evt.Timestamp))
		return
	}
	if max := s.mgr.maxCalls; max > 0 && s.reg.count() >= max {
		s.rejectOffer(ctx, node, evt.From)
		return
	}
	cm := s.createCall(callID)
	cm.HandleCallOffer(ctx, node, evt.From)
}

func (s *Session) rejectOffer(ctx context.Context, node *waBinary.Node, from types.JID) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	creator := wanode.AttrString(info.InnerNode.Attrs, "call-creator")
	if creator == "" {
		creator = from.String()
	}
	reject := signaling.BuildRejectStanza(from, info.CallID, wanode.MustJID(creator))
	_ = wa.NewSocket(s.client).SendNode(ctx, reject)
	s.log.Info("inbound call rejected: session at capacity", "call_id", info.CallID)
}

func (s *Session) handleEvent(rawEvt any) {
	ctx := context.Background()
	switch evt := rawEvt.(type) {
	case *events.Connected:
		if id := s.client.Store.ID; id != nil {
			_ = s.mgr.store.setJID(s.mgr.appCtx, s.id, id.String())
		}
		s.setAuth(AuthSnapshot{State: "open", Paired: true})
	case *events.LoggedOut:
		s.setAuth(AuthSnapshot{State: "logged_out", Paired: false})
	case *events.CallOffer:
		s.onIncomingOffer(ctx, evt)
	case *events.CallAccept:
		node := wrapCall(evt.From, evt.Data)
		if ac, ok := s.callForEvent(evt.From, evt.Data); ok {
			ac.cm.HandleCallAccept(ctx, node, evt.From)
		} else {
			// Fallback: busca pelo JID do peer — cobre o caso em que evt.Data
			// é nil ou não carrega o call-id (igual ao tratamento de CallTerminate).
			peerJID := evt.From
			if peerJID.Server == "lid" {
				if pn, err := s.client.Store.LIDs.GetPNForLID(context.Background(), peerJID); err == nil && !pn.IsEmpty() {
					peerJID = pn
				}
			}
			if ac, ok := s.reg.getByPeer(peerJID); ok {
				ac.cm.HandleCallAccept(ctx, node, evt.From)
			} else if ac, ok := s.reg.getByPeer(evt.From); ok {
				ac.cm.HandleCallAccept(ctx, node, evt.From)
			} else {
				s.log.Warn("CallAccept: chamada não encontrada por callId nem por peer", "from", evt.From)
			}
		}
	case *events.CallTransport:
		if ac, ok := s.callForEvent(evt.From, evt.Data); ok {
			ac.cm.HandleCallTransport(ctx, wrapCall(evt.From, evt.Data), evt.From)
		}
	case *events.CallTerminate:
		node := wrapCall(evt.From, evt.Data)
		if ac, ok := s.callForEvent(evt.From, evt.Data); ok {
			ac.cm.HandleCallTerminate(node)
		} else {
			peerJID := evt.From
			if peerJID.Server == "lid" {
				if pn, err := s.client.Store.LIDs.GetPNForLID(context.Background(), peerJID); err == nil && !pn.IsEmpty() {
					peerJID = pn
				}
			}
			if ac, ok := s.reg.getByPeer(peerJID); ok {
				ac.cm.HandleCallTerminate(node)
			} else if ac, ok := s.reg.getByPeer(evt.From); ok {
				ac.cm.HandleCallTerminate(node)
			}
		}
	case *events.CallReject:
		node := wrapCall(evt.From, evt.Data)
		if ac, ok := s.callForEvent(evt.From, evt.Data); ok {
			ac.cm.HandleCallTerminate(node)
		} else {
			peerJID := evt.From
			if peerJID.Server == "lid" {
				if pn, err := s.client.Store.LIDs.GetPNForLID(context.Background(), peerJID); err == nil && !pn.IsEmpty() {
					peerJID = pn
				}
			}
			if ac, ok := s.reg.getByPeer(peerJID); ok {
				ac.cm.HandleCallTerminate(node)
			} else if ac, ok := s.reg.getByPeer(evt.From); ok {
				ac.cm.HandleCallTerminate(node)
			}
		}
	case *events.Receipt:
		s.log.Info("receipt received", "type", string(evt.Type), "sender", evt.Sender.String())
		if evt.Type == types.ReceiptTypeDelivered || string(evt.Type) == "ringer" {
			peerJID := evt.Sender
			if peerJID.Server == "lid" {
				if pn, err := s.client.Store.LIDs.GetPNForLID(context.Background(), peerJID); err == nil && !pn.IsEmpty() {
					peerJID = pn
				}
			}
			if ac, ok := s.reg.getByPeer(peerJID); ok {
				ac.cm.HandleCallRinging()
			} else if ac, ok := s.reg.getByPeer(evt.Sender); ok {
				ac.cm.HandleCallRinging()
			}
		}
	}
}

func (s *Session) connect(ctx context.Context) error {
	if s.client.Store.ID != nil {
		return s.client.Connect()
	}
	return s.startPairing(ctx)
}

func (s *Session) startPairing(ctx context.Context) error {
	qrChan, err := s.client.GetQRChannel(ctx)
	if err != nil {
		return err
	}
	if err := s.client.Connect(); err != nil {
		return err
	}
	go func() {
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				s.log.Info("scan the QR code to pair this session")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				s.setAuth(AuthSnapshot{State: "qr", QR: evt.Code})
				s.mgr.broker.emitSessionQR(s.id, evt.Code)
			case "success":
				if id := s.client.Store.ID; id != nil {
					_ = s.mgr.store.setJID(s.mgr.appCtx, s.id, id.String())
				}
				s.setAuth(AuthSnapshot{State: "open", Paired: true})
			case "timeout":
				s.setAuth(AuthSnapshot{State: "logged_out", Paired: false})
			}
		}
	}()
	return nil
}

func (s *Session) setAuth(a AuthSnapshot) {
	s.mu.Lock()
	s.auth = a
	s.mu.Unlock()
	s.mgr.broker.emitAuthState(s.id, a)
	s.mgr.broker.emitSessionList(s.mgr.infos())
}

func (s *Session) info() SessionInfo {
	s.mu.Lock()
	a := s.auth
	webhookURL := s.webhookURL
	s.mu.Unlock()
	jid := ""
	if id := s.client.Store.ID; id != nil {
		jid = id.String()
	}
	return SessionInfo{ID: s.id, Name: s.name, JID: jid, State: a.State, Paired: a.Paired || jid != "", QR: a.QR, WebhookURL: webhookURL}
}

func (s *Session) setBridge(callID string, b *Bridge) {
	oldB, found := s.reg.setBridge(callID, b)
	if !found {
		b.Close()
		return
	}
	if oldB != nil {
		oldB.OnTerminalICE = nil
		oldB.Close()
	}
}

func (s *Session) removeCall(callID string) {
	ac, ok := s.reg.remove(callID)
	if !ok {
		return
	}
	if ac.bridge != nil {
		ac.bridge.Close()
	}
}

func (s *Session) terminateCall(callID string, reason core.EndCallReason) {
	ac, ok := s.reg.get(callID)
	if !ok {
		return
	}
	_ = ac.cm.EndCall(context.Background(), reason)
}

func (s *Session) teardownAllCalls() {
	for _, ac := range s.reg.drain() {
		_ = ac.cm.EndCall(context.Background(), core.EndCallReasonUserEnded)
		if ac.bridge != nil {
			ac.bridge.Close()
		}
	}
}

func (s *Session) replaceClient(client *whatsmeow.Client) {
	s.teardownAllCalls()
	s.client.Disconnect()
	s.client = client
	client.AddEventHandler(s.handleEvent)
}

func (s *Session) shutdown() {
	s.teardownAllCalls()
	s.client.Disconnect()
}

func mapStatus(state core.CallState) CallStatus {
	switch state {
	case core.CallStateActive, core.CallStateConnecting, core.CallStateOnHold:
		return StatusConnected
	case core.CallStateEnded:
		return StatusEnded
	case core.CallStateInitiating:
		return StatusStarting
	default:
		return StatusRinging
	}
}
