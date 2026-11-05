import { apiPost } from "./api";
import { buildAudioGraph } from "./audio-graph";
import { openCallWS } from "./ws-audio";
import { useAudioTransport } from "@/stores/audioTransport";
import { PCM_CHANNEL_LABEL } from "../constants/audio";

// Conexão de áudio de uma chamada, independente do transporte.
export type OpenCall = {
  micStream: MediaStream;
  remoteStream: MediaStream | null;
  close: () => void;
};

type WebRTCAttempt = {
  call: OpenCall;
  audioOpen: Promise<void>;
  /** Desfaz a conexão mantendo o microfone, que o próximo transporte reaproveita. */
  release: () => void;
};

// No Mocho, o p99 de abertura do DataChannel foi ~3 s; acima de 4 s ficou <1%.
const WEBRTC_OPEN_TIMEOUT_MS = 4_000;
const WEBRTC_ANSWER_TIMEOUT_MS = 8_000;
const WEBSOCKET_OPEN_TIMEOUT_MS = 5_000;
const ICE_GATHERING_TIMEOUT_MS = 2_000;
// Depois de um fallback, as próximas chamadas deste navegador já começam pelo WebSocket.
const WS_STICKY_MS = 30 * 60_000;

let wsStickyUntil = 0;

/** Abre o áudio pelo transporte escolhido neste navegador (padrão automático). */
export const openCall = async (
  sid: string,
  callId: string,
  micDeviceId: string | null,
): Promise<OpenCall> => {
  const micStream = await navigator.mediaDevices.getUserMedia({
    audio: micDeviceId ? { deviceId: { exact: micDeviceId } } : true,
  });
  try {
    switch (useAudioTransport.getState().transport) {
      case "websocket":
        return await openCallWS(sid, callId, micStream);
      case "webrtc":
        return (await connectWebRTC(sid, callId, micStream)).call;
      default:
        return await openCallAuto(sid, callId, micStream);
    }
  } catch (err) {
    micStream.getTracks().forEach((t) => t.stop());
    throw err;
  }
};

// O servidor aceita a troca: a perna nova substitui a anterior sem encerrar a chamada.
const openCallAuto = async (sid: string, callId: string, micStream: MediaStream): Promise<OpenCall> => {
  if (Date.now() < wsStickyUntil) {
    try {
      return await openCallWS(sid, callId, micStream, WEBSOCKET_OPEN_TIMEOUT_MS);
    } catch (err) {
      console.warn("[arendcalls] WebSocket não conectou, trocando para WebRTC", err);
      wsStickyUntil = 0;
      return await openConfirmedWebRTC(sid, callId, micStream);
    }
  }
  try {
    return await openConfirmedWebRTC(sid, callId, micStream);
  } catch (err) {
    console.warn("[arendcalls] WebRTC não conectou, trocando para WebSocket", err);
  }
  const ws = await openCallWS(sid, callId, micStream, WEBSOCKET_OPEN_TIMEOUT_MS);
  wsStickyUntil = Date.now() + WS_STICKY_MS;
  return ws;
};

/** Só devolve a conexão com o DataChannel aberto; senão, desfaz e falha. */
const openConfirmedWebRTC = async (sid: string, callId: string, micStream: MediaStream): Promise<OpenCall> => {
  const attempt = await connectWebRTC(sid, callId, micStream, AbortSignal.timeout(WEBRTC_ANSWER_TIMEOUT_MS));
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      attempt.audioOpen,
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error("data channel timeout")), WEBRTC_OPEN_TIMEOUT_MS);
      }),
    ]);
    return attempt.call;
  } catch (err) {
    attempt.release();
    throw err;
  } finally {
    clearTimeout(timer);
  }
};

const connectWebRTC = async (
  sid: string,
  callId: string,
  micStream: MediaStream,
  answerSignal?: AbortSignal,
): Promise<WebRTCAttempt> => {
  const pc = new RTCPeerConnection({ iceServers: [] });

  const dc = pc.createDataChannel(PCM_CHANNEL_LABEL, { ordered: true });
  dc.binaryType = "arraybuffer";

  const audioOpen = new Promise<void>((resolve, reject) => {
    dc.onopen = () => resolve();
    dc.onclose = () => reject(new Error("data channel closed"));
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === "failed") reject(new Error("peer connection failed"));
    };
  });
  // Quem fixou WebRTC não espera esta promise.
  audioOpen.catch(() => {});

  const graph = await buildAudioGraph(micStream, {
    send: (frame) => {
      if (dc.readyState !== "open") return false;
      dc.send(frame);
      return true;
    },
    txBufferedBytes: () => dc.bufferedAmount,
  });
  dc.onmessage = (e: MessageEvent<ArrayBuffer>) => graph.playPeerFrame(e.data);

  const release = () => {
    graph.close();
    try {
      pc.close();
    } catch {}
  };

  try {
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    await new Promise<void>((resolve) => {
      if (pc.iceGatheringState === "complete") return resolve();
      const cap = setTimeout(resolve, ICE_GATHERING_TIMEOUT_MS);
      pc.addEventListener("icegatheringstatechange", () => {
        if (pc.iceGatheringState === "complete") {
          clearTimeout(cap);
          resolve();
        }
      });
    });

    const { sdp_answer, prebuffer_ms } = await apiPost<{ sdp_answer: string; prebuffer_ms?: number }>(
      `/api/sessions/${sid}/calls/${callId}/webrtc`,
      { sdp_offer: pc.localDescription!.sdp },
      answerSignal,
    );
    graph.setPrebufferMs(prebuffer_ms ?? 0);
    await pc.setRemoteDescription({ type: "answer", sdp: sdp_answer });
  } catch (err) {
    release();
    throw err;
  }

  return {
    audioOpen,
    release,
    call: {
      micStream,
      remoteStream: graph.remoteStream,
      close: () => {
        release();
        try {
          micStream.getTracks().forEach((t) => t.stop());
        } catch {}
      },
    },
  };
};
