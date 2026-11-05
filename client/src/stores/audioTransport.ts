import { create } from "zustand";

// Transporte de áudio das chamadas deste navegador. Automático (padrão) tenta
// WebRTC e cai para WebSocket sem encerrar a chamada; os outros dois fixam um
// transporte. Lido em openCall() a cada chamada.

export type AudioTransport = "auto" | "webrtc" | "websocket";

const KEY = "arendcalls.audioTransport";

const read = (): AudioTransport => {
  try {
    const stored = localStorage.getItem(KEY);
    return stored === "webrtc" || stored === "websocket" ? stored : "auto";
  } catch {
    return "auto";
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
