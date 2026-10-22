import { create } from "zustand";

// Transporte de áudio das chamadas deste navegador. WebRTC (padrão) precisa de
// UDP direto até o servidor; WebSocket funciona atrás de proxy/tunnel
// (Cloudflare Tunnel). Lido em openCall() a cada chamada.

export type AudioTransport = "webrtc" | "websocket";

const KEY = "arendcalls.audioTransport";

const read = (): AudioTransport => {
  try {
    return localStorage.getItem(KEY) === "websocket" ? "websocket" : "webrtc";
  } catch {
    return "webrtc";
  }
};

export const useAudioTransport = create<{
  transport: AudioTransport;
  setTransport: (t: AudioTransport) => void;
}>((set) => ({
  transport: read(),
  setTransport: (transport) => {
    try {
      localStorage.setItem(KEY, transport);
    } catch {}
    set({ transport });
  },
}));
