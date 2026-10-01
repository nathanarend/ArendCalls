package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
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
	id           string
	name         string
	webhookURL   string
	panelInbound bool
	mgr          *SessionManager
	log          *slog.Logger

	client *whatsmeow.Client
	reg    *callRegistry

	mu   sync.Mutex
	auth AuthSnapshot
}

func newSession(mgr *SessionManager, id, name, webhookURL string, panelInbound bool, client *whatsmeow.Client) *Session {
	s := &Session{
		id:           id,
		name:         name,
		webhookURL:   webhookURL,
		panelInbound: panelInbound,
		mgr:          mgr,
		log:          mgr.log.With("session", id),
		client:       client,
		auth:         AuthSnapshot{State: "connecting"},
		reg:          newCallRegistry(),
	}
	client.AddEventHandler(s.handleEvent)
	return s
}

// ringingTimeout: tempo máximo de uma chamada sem atender/conectar antes do
// timer anti-zumbi encerrá-la. Variável (não const) para os testes.
var ringingTimeout = 90 * time.Second

func (s *Session) createCall(callID string) *call.CallManager {
	cm := call.NewCallManager(wa.NewSocket(s.client), s.log)
	stopRinging := s.wireCall(cm, callID)
	s.reg.add(callID, &activeCall{cm: cm, stopRinging: stopRinging})
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

// wireCall liga os callbacks do CallManager à sessão e arma o timer
// anti-zumbi. Devolve a função que para o timer (guardada no activeCall).
func (s *Session) wireCall(cm *call.CallManager, callID string) (stopRinging func()) {
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
		timeoutTimer = time.AfterFunc(ringingTimeout, func() {
			// Já saiu do registro (fim normal, discagem que falhou): nada a fazer.
			if _, ok := s.reg.get(callID); !ok {
				return
			}
			// Antes de encerrar, verificar se a chamada já está ativa em outra sessão.
			// Isso evita que sessões-espelho (multi-device) derrubem chamadas legítimas.
			if s.mgr.broker.isCallConnected(callID) {
				s.log.Info("call ringing timeout: call already active in broker, skipping EndCall", "call_id", callID)
				s.removeCall(callID)
				return
			}
			s.log.Info("call ringing timeout reached, ending stale call", "call_id", callID, "after", ringingTimeout)
			_ = cm.EndCall(context.Background(), core.EndCallReason("timeout"))
			// EndCall não faz nada se a oferta nunca virou chamada (currentCall
			// nil); sem isto a entrada vaza no registro.
			s.removeCall(callID)
		})
	}

	// Timer anti-zumbi: encerra a chamada que não atende nem conecta.
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
			s.finishCall(c)
			return
		}
		if c.StateData.State == core.CallStateActive || c.StateData.State == core.CallStateConnecting {
			stopTimeout()
		}
		// Atendida (accept + mídia). Junto com o 1º áudio do paciente
		// (OnPeerAudio), é o que inicia a gravação — nunca o toque.
		if c.StateData.State == core.CallStateActive {
			s.startRecordingIfArmed(c.CallID)
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
		s.finishCall(c)
	}
	cm.OnPeerAudio = func(pcm16 []float32) {
		ac, ok := s.reg.get(callID)
		if !ok {
			return
		}
		// Primeiro áudio do paciente = atendeu: o WhatsApp não manda áudio do
		// peer durante o toque (no silêncio manda ruído de conforto, que também
		// chega aqui). Antes do WritePeer para o 1º frame entrar no WAV.
		if ac.recTried.CompareAndSwap(false, true) {
			s.startRecordingIfArmed(callID)
		}
		if ac.bridge != nil {
			if err := ac.bridge.WritePCM(pcm16); err != nil {
				ac.bridgeSendErrors.Add(1)
			}
		}
		if r := ac.rec.Load(); r != nil {
			r.WritePeer(pcm16)
		}
	}
	// Cancela o timer anti-zombie quando o relay de mídia conecta.
	// Crucial para sessões-espelho (multi-device) que ficam em IncomingRinging
	// enquanto outra sessão já aceitou a chamada — sem isso o timer derruba a ligação.
	// Relay conectado NÃO é atendimento: na saída ele conecta antes do toque,
	// então não inicia gravação aqui.
	var relayLogged atomic.Bool
	cm.OnRelayConnected = func() {
		// O callback dispara uma vez por relay (~3 por ligação); loga só o 1º.
		if relayLogged.CompareAndSwap(false, true) {
			s.log.Info("relay connected: cancelling ringing timeout", "call_id", callID)
		}
		stopTimeout()
	}
	return stopTimeout
}

// finishCall encerra a chamada na sessão: fecha a gravação, tira do registro,
// loga a telemetria de áudio e emite call-ended. Chamado por OnStateChange
// (estado Ended) e por OnEnded — só a primeira passada acha a chamada no
// registro, então o log e as stats saem uma vez. Roda com o m.mu do
// CallManager travado (emitState): nada aqui pode chamar o CallManager além
// de RxStats.
func (s *Session) finishCall(c *call.CallInfo) {
	if s.mgr.rec != nil {
		s.mgr.rec.onCallEnded(c.CallID)
	}
	var stats *callAudioStats
	if ac := s.removeCall(c.CallID); ac != nil {
		st := ac.audioStats()
		stats = &st
		s.log.Info("call audio stats", "call_id", c.CallID,
			"rx_packets", st.RxPackets, "rx_gaps_speech", st.RxGapsSpeech,
			"rx_max_gap_speech_ms", st.RxMaxGapSpeechMs, "rx_undecodable", st.RxUndecodable,
			"rx_relock", st.RxRelock, "relay_used", st.RelayUsed,
			"bridge_send_errors", st.BridgeSendErrors)
	}
	s.mgr.broker.endCall(c.CallID, string(c.StateData.EndReason), stats)
}

// startRecordingIfArmed begins the WAV capture for a call that asked for
// recording, once it is answered. Idempotent — fires from both the Active
// state change and the first peer audio packet, whichever lands first.
func (s *Session) startRecordingIfArmed(callID string) {
	if s.mgr.rec == nil || !s.mgr.rec.armed(callID) {
		return
	}
	ac, ok := s.reg.get(callID)
	if !ok {
		return
	}
	if r := s.mgr.rec.onAnswered(callID); r != nil {
		ac.rec.Store(r)
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
	// Chamada entre duas sessões deste servidor: quem ligou já é dono deste
	// callID. Se esta sessão também registrasse a oferta, as duas disputariam o
	// mesmo registro no broker (indexado só por callID) e o card sumiria do
	// painel de quem ligou. Aqui a chamada toca só no celular.
	if origin, found := s.mgr.FindSessionByCall(callID); found {
		if ac, ok := origin.reg.get(callID); ok {
			if c := ac.cm.CurrentCall(); c != nil && c.Direction == core.CallDirectionOutgoing {
				s.log.Info("ignoring inbound offer for call placed by another session", "call_id", callID, "origin_session", origin.id)
				return
			}
		}
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
	// Panic aqui some: o whatsmeow recupera panic de event handler e loga no
	// logger dele, que é Noop fora do -debug. A chamada ficava no registro sem
	// currentCall e o timer de toque nunca a removia (chamada fantasma).
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("inbound offer handling panicked, dropping call", "call_id", callID,
				"panic", r, "children", call.ChildTagSummary(evt.Data), "stack", string(debug.Stack()))
			s.removeCall(callID)
			s.mgr.broker.endCall(callID, "error", nil)
		}
	}()
	if !cm.HandleCallOffer(ctx, node, evt.From) {
		s.log.Warn("inbound offer not handled, dropping call", "call_id", callID,
			"children", call.ChildTagSummary(evt.Data))
		s.removeCall(callID)
	}
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
	// Mesmo motivo do recover em onIncomingOffer: sem isto um panic aqui só
	// apareceria com -debug.
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("event handler panicked", "event", fmt.Sprintf("%T", rawEvt),
				"panic", r, "stack", string(debug.Stack()))
		}
	}()
	ctx := context.Background()
	switch evt := rawEvt.(type) {
	case *events.Connected:
		if id := s.client.Store.ID; id != nil {
			_ = s.mgr.store.setJID(s.mgr.appCtx, s.id, id.String())
		}
		s.setAuth(AuthSnapshot{State: "open", Paired: true})
		// Unavailable já leva o pushname (evita o "-" nos contatos). Não usar
		// available aqui: o aparelho ficaria "online" direto e o servidor passa
		// a empurrar chatstate/presence, que disputam a fila de eventos com a
		// sinalização das chamadas.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.client.SendPresence(ctx, types.PresenceUnavailable); err != nil {
				s.log.Warn("failed to send initial presence on connect", "err", err)
			} else {
				s.log.Debug("initial presence unavailable sent on connect")
			}
		}()
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
		s.log.Debug("receipt received", "type", string(evt.Type), "sender", evt.Sender.String())
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
	panelInbound := s.panelInbound
	s.mu.Unlock()
	jid := ""
	if id := s.client.Store.ID; id != nil {
		jid = id.String()
	}
	return SessionInfo{ID: s.id, Name: s.name, JID: jid, State: a.State, Paired: a.Paired || jid != "", QR: a.QR, WebhookURL: webhookURL, PanelInbound: panelInbound}
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

// removeCall tira a chamada do registro, para o timer de toque e fecha o
// Bridge. Devolve a chamada removida, ou nil se ela já tinha saído.
func (s *Session) removeCall(callID string) *activeCall {
	ac, ok := s.reg.remove(callID)
	if !ok {
		return nil
	}
	if ac.stopRinging != nil {
		ac.stopRinging()
	}
	if ac.bridge != nil {
		ac.bridge.Close()
	}
	return ac
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
		if ac.stopRinging != nil {
			ac.stopRinging()
		}
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

func (s *Session) SendPresence(ctx context.Context, state types.Presence) error {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil || !client.IsConnected() {
		return fmt.Errorf("session not connected")
	}
	return client.SendPresence(ctx, state)
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
