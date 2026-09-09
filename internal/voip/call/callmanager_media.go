package call

import (
	"time"
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
	"wacalls/internal/voip/transport"
)

// rxFailoverSilence: how long the locked peer SSRC may be fully silent before a
// different arriving SSRC is allowed to take over — but only if it decodes.
const rxFailoverSilence = 2 * time.Second

func (m *CallManager) initCodec() {
	if m.codec != nil {
		return
	}
	codec, err := media.NewMLowCodec(media.DefaultCodecOptions)
	if err != nil {
		m.log.Warn("MLow codec unavailable — call will run signaling-only (no audio)", "err", err)
		return
	}
	m.codec = codec
}

func (m *CallManager) FeedCapturedPCM(data []float32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isHold || m.codec == nil || m.rtpSession == nil || m.srtpSession == nil || !m.relay.HasConnection() {
		return
	}
	m.lastCaptureAt = time.Now()
	frameSize := m.codec.FrameSize()
	if m.encodeBuf == nil {
		m.encodeBuf = make([]float32, frameSize)
		m.encodeBufPos = 0
	}

	offset := 0
	for offset < len(data) {
		toCopy := min(len(data)-offset, frameSize-m.encodeBufPos)
		copy(m.encodeBuf[m.encodeBufPos:], data[offset:offset+toCopy])
		m.encodeBufPos += toCopy
		offset += toCopy
		if m.encodeBufPos < frameSize {
			break
		}
		frame := make([]float32, frameSize)
		copy(frame, m.encodeBuf)
		m.encodeBufPos = 0

		opus, err := m.codec.Encode(frame)
		if err != nil {
			m.log.Debug("encode error", "err", err)
			continue
		}
		m.sendOpusFrameLocked(opus)
	}
}

func (m *CallManager) sendOpusFrameLocked(opus []byte) {
	if m.rtpSession == nil || m.srtpSession == nil {
		return
	}
	marker := !m.firstPacketSent
	pkt := m.rtpSession.CreatePacketWithDuration(opus, m.codec.FrameSize(), marker)
	if m.debeEnabled {
		pkt.Header.Extension = true
		pkt.Header.ExtensionProfile = 0xbede
		pkt.Header.ExtensionData = nil
	}
	m.firstPacketSent = true

	srtp, err := m.srtpSession.Protect(pkt)
	if err != nil {
		m.log.Debug("srtp protect error", "err", err)
		return
	}
	m.relay.Broadcast(srtp)
}

func (m *CallManager) startSilenceKeepaliveLocked() {
	if m.keepaliveStop != nil || m.codec == nil {
		return
	}
	stop := make(chan struct{})
	m.keepaliveStop = stop
	frameSize := m.codec.FrameSize()
	go func() {
		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()
		silence := make([]float32, frameSize)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				m.mu.Lock()
				ready := !m.isHold && m.codec != nil && m.rtpSession != nil && m.srtpSession != nil && m.relay.HasConnection()
				idle := time.Since(m.lastCaptureAt) > 120*time.Millisecond
				if ready && idle {
					if opus, err := m.codec.Encode(silence); err == nil {
						m.sendOpusFrameLocked(opus)
					}
				}
				m.mu.Unlock()
			}
		}
	}()
}

func (m *CallManager) onRelayData(data []byte) {
	if transport.IsStunPacket(data) {
		return
	}
	if !transport.IsRtpPacket(data) {
		return
	}
	if len(data) < 12 {
		return
	}
	pt := data[1] & 0x7f
	if pt != core.PayloadTypeWhatsAppOpus {
		return
	}

	m.mu.Lock()
	if m.srtpSession == nil || m.codec == nil {
		m.mu.Unlock()
		return
	}
	ssrc := uint32(data[8])<<24 | uint32(data[9])<<16 | uint32(data[10])<<8 | uint32(data[11])
	if ssrc == m.selfSsrc {
		m.mu.Unlock()
		return
	}

	nowNs := time.Now().UnixNano()

	// (C1) Forward exactly one peer stream. rxLockedSsrc is committed (in C2,
	// after the decode) to the first SSRC that decodes. A different SSRC is
	// dropped here — before spending a decode — UNLESS the locked stream has
	// been fully silent past rxFailoverSilence; then this packet is let through
	// as a failover candidate and only takes over the lock if it actually
	// decodes. A stream of undecodable packets (e.g. a peer device whose
	// per-JID SRTP key we don't hold) must never capture the lock, or the call
	// would go permanently silent with no recovery.
	failoverTry := false
	if m.rxLockedSsrc != 0 && ssrc != m.rxLockedSsrc {
		if nowNs-m.rxLockedLastNs > int64(rxFailoverSilence) {
			failoverTry = true
		} else {
			m.mu.Unlock()
			return
		}
	}
	if ssrc == m.rxLockedSsrc {
		m.rxLockedLastNs = nowNs
	}

	// (B) Drop exact relay copies. Several relays forward the same stream, so the
	// same (ssrc, seq) arrives 2-3x. Sliding window keyed by ssrc<<16|seq.
	seq := uint16(data[2])<<8 | uint16(data[3])
	dkey := uint64(ssrc)<<16 | uint64(seq)
	if m.rxDedup == nil {
		m.rxDedup = make(map[uint64]struct{}, len(m.rxDedupRing))
	}
	if _, dup := m.rxDedup[dkey]; dup {
		m.mu.Unlock()
		return
	}
	if m.rxDedupFilled {
		delete(m.rxDedup, m.rxDedupRing[m.rxDedupIdx])
	}
	m.rxDedupRing[m.rxDedupIdx] = dkey
	m.rxDedup[dkey] = struct{}{}
	m.rxDedupIdx++
	if m.rxDedupIdx == len(m.rxDedupRing) {
		m.rxDedupIdx = 0
		m.rxDedupFilled = true
	}

	if !m.actualPeerSet {
		m.actualPeerSet = true
		if !containsSsrc(m.peerSsrcs, ssrc) {
			m.peerSsrcs = []uint32{ssrc}
			m.relay.SetSubscriptionSsrc(ssrc)
			go m.relay.ResendSubscriptions()
		}
	}
	srtp := m.srtpSession
	codec := m.codec
	m.mu.Unlock()

	pkt, err := srtp.Unprotect(data)
	if err != nil {
		m.log.Debug("srtp unprotect error", "err", err)
		return
	}
	if len(pkt.Payload) == 0 {
		return
	}
	pcm, err := codec.Decode(pkt.Payload)
	if err != nil {
		return // decode failed → lock unchanged, failover not taken
	}

	// (C2) Commit the lock only now that the packet decoded: the first SSRC that
	// ever decodes, or a failover SSRC that just proved it decodes while the
	// previous lock stayed silent. Echo never reaches here (it fails
	// srtp.Unprotect — separate send/recv keys).
	if m.rxLockedSsrc == 0 || failoverTry {
		m.mu.Lock()
		if m.rxLockedSsrc == 0 {
			m.rxLockedSsrc = ssrc
			m.rxLockedLastNs = nowNs
			m.log.Debug("rx peer ssrc locked", "ssrc", ssrc)
		} else if failoverTry && ssrc != m.rxLockedSsrc &&
			nowNs-m.rxLockedLastNs > int64(rxFailoverSilence) {
			m.log.Debug("rx peer ssrc re-lock (previous went silent)", "from", m.rxLockedSsrc, "to", ssrc)
			m.rxLockedSsrc = ssrc
			m.rxLockedLastNs = nowNs
		}
		m.mu.Unlock()
	}

	pcm = media.NormalizeFrame(pcm, codec.FrameSize())
	if m.OnPeerAudio != nil {
		m.OnPeerAudio(pcm)
	}
}
