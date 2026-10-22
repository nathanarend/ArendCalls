import { buildAudioGraph } from "./audio-graph";
import type { OpenCall } from "./webrtc";

// Transporte de áudio por WebSocket: o mesmo PCM Int16 LE 16 kHz mono do
// DataChannel, uma mensagem binária por quadro. Funciona atrás de proxy/tunnel
// (Cloudflare Tunnel), onde não há UDP até o servidor. O cookie do login
// autentica o handshake (mesma origem). Uso de teste: sem reconexão — a queda
// deixa a chamada sem áudio e o servidor a encerra se ninguém reconectar.

// Acima disto na fila o navegador não está dando vazão: descarta o quadro do
// microfone em vez de acumular atraso (mesmo limite do DataChannel no Mocho).
const WS_TX_HIGH_WATER = 64 * 1024;

/** Mensagem de texto do servidor: {"type":"config","prebufferMs":N}. null = outra coisa. */
const parsePrebufferMs = (text: string): number | null => {
  try {
    const msg = JSON.parse(text) as { type?: string; prebufferMs?: unknown };
    return msg.type === "config" && typeof msg.prebufferMs === "number" ? msg.prebufferMs : null;
  } catch {
    return null;
  }
};

export const openCallWS = async (sid: string, callId: string, micStream: MediaStream): Promise<OpenCall> => {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/api/sessions/${sid}/calls/${callId}/audio`);
  ws.binaryType = "arraybuffer";

  const opened = new Promise<void>((resolve, reject) => {
    ws.onopen = () => resolve();
    ws.onclose = (e) => reject(new Error(`audio websocket closed before open (${e.code})`));
  });

  // A config chega logo após abrir, possivelmente antes de o grafo ficar pronto.
  let prebufferMs = 0;
  ws.onmessage = (e: MessageEvent) => {
    if (typeof e.data === "string") prebufferMs = parsePrebufferMs(e.data) ?? prebufferMs;
  };

  const graph = await buildAudioGraph(micStream, {
    send: (frame) => {
      if (ws.readyState !== WebSocket.OPEN || ws.bufferedAmount > WS_TX_HIGH_WATER) return false;
      ws.send(frame);
      return true;
    },
    txBufferedBytes: () => ws.bufferedAmount,
  });
  graph.setPrebufferMs(prebufferMs);
  ws.onmessage = (e: MessageEvent) => {
    // Binário é áudio; texto é config do servidor.
    if (e.data instanceof ArrayBuffer) graph.playPeerFrame(e.data);
    else if (typeof e.data === "string") {
      const ms = parsePrebufferMs(e.data);
      if (ms !== null) graph.setPrebufferMs(ms);
    }
  };

  try {
    await opened;
  } catch (err) {
    graph.close();
    throw err;
  }
  ws.onclose = (e) => {
    // eslint-disable-next-line no-console
    if (e.code !== 1000) console.warn(`[arendcalls] audio websocket dropped (${e.code} ${e.reason})`);
  };

  return {
    micStream,
    remoteStream: graph.remoteStream,
    close: () => {
      graph.close();
      try {
        micStream.getTracks().forEach((t) => t.stop());
      } catch {}
      try {
        ws.close(1000);
      } catch {}
    },
  };
};
