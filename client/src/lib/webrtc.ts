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

/** Abre o áudio pelo transporte escolhido neste navegador (padrão WebRTC). */
export const openCall = async (
  sid: string,
  callId: string,
  micDeviceId: string | null,
): Promise<OpenCall> => {
  const micStream = await navigator.mediaDevices.getUserMedia({
    audio: micDeviceId ? { deviceId: { exact: micDeviceId } } : true,
  });
  try {
    return useAudioTransport.getState().transport === "websocket"
      ? await openCallWS(sid, callId, micStream)
      : await openCallWebRTC(sid, callId, micStream);
  } catch (err) {
    micStream.getTracks().forEach((t) => t.stop());
    throw err;
  }
};

const openCallWebRTC = async (sid: string, callId: string, micStream: MediaStream): Promise<OpenCall> => {
  const pc = new RTCPeerConnection({ iceServers: [] });

  const dc = pc.createDataChannel(PCM_CHANNEL_LABEL, { ordered: true });
  dc.binaryType = "arraybuffer";

  const graph = await buildAudioGraph(micStream, {
    send: (frame) => {
      if (dc.readyState !== "open") return false;
      dc.send(frame);
      return true;
    },
    txBufferedBytes: () => dc.bufferedAmount,
  });
  dc.onmessage = (e: MessageEvent<ArrayBuffer>) => graph.playPeerFrame(e.data);

  const offer = await pc.createOffer();
  await pc.setLocalDescription(offer);
  await new Promise<void>((resolve) => {
    if (pc.iceGatheringState === "complete") resolve();
    else
      pc.addEventListener("icegatheringstatechange", () => {
        if (pc.iceGatheringState === "complete") resolve();
      });
  });

  const { sdp_answer, prebuffer_ms } = await apiPost<{ sdp_answer: string; prebuffer_ms?: number }>(
    `/api/sessions/${sid}/calls/${callId}/webrtc`,
    { sdp_offer: pc.localDescription!.sdp },
  );
  graph.setPrebufferMs(prebuffer_ms ?? 0);
  await pc.setRemoteDescription({ type: "answer", sdp: sdp_answer });

  return {
    micStream,
    remoteStream: graph.remoteStream,
    close: () => {
      graph.close();
      try {
        micStream.getTracks().forEach((t) => t.stop());
      } catch {}
      try {
        pc.close();
      } catch {}
    },
  };
};
