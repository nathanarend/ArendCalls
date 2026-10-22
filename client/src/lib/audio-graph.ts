import { float32ToInt16LE, int16LEToFloat32 } from "./pcm";
import { audioDebugEnabled, useAudioDebug } from "@/stores/audioDebug";
import {
  CAPTURE_PROCESSOR_NAME,
  CAPTURE_WORKLET_URL,
  PLAYBACK_PROCESSOR_NAME,
  PLAYBACK_WORKLET_URL,
  SAMPLE_RATE,
} from "../constants/audio";

// Grafo de áudio de uma chamada, igual para WebRTC (DataChannel) e WebSocket:
// microfone → worklet de captura → PCM Int16 LE para o transporte; PCM do
// transporte → worklet de reprodução → remoteStream. Telemetria de debug junto.

export type AudioGraphTransport = {
  /** Envia um quadro do microfone. false = não enviado (canal fechado/cheio). */
  send: (frame: ArrayBuffer) => boolean;
  /** Bytes do microfone ainda na fila do transporte (telemetria). */
  txBufferedBytes: () => number;
};

export type AudioGraph = {
  remoteStream: MediaStream;
  /** Entrega à reprodução um quadro PCM Int16 LE recebido do servidor. */
  playPeerFrame: (data: ArrayBuffer) => void;
  /** Atraso (ms) no início de cada fala do contato, configurado no servidor. 0 = toca na hora. */
  setPrebufferMs: (ms: number) => void;
  close: () => void;
};

export const buildAudioGraph = async (
  micStream: MediaStream,
  transport: AudioGraphTransport,
): Promise<AudioGraph> => {
  const ctx = new AudioContext({ sampleRate: SAMPLE_RATE });
  await ctx.audioWorklet.addModule(CAPTURE_WORKLET_URL);
  await ctx.audioWorklet.addModule(PLAYBACK_WORKLET_URL);
  await ctx.resume();

  const micSource = ctx.createMediaStreamSource(micStream);
  const captureNode = new AudioWorkletNode(ctx, CAPTURE_PROCESSOR_NAME);
  let micFrames = 0;
  captureNode.port.onmessage = (e: MessageEvent<Float32Array>) => {
    if (transport.send(float32ToInt16LE(e.data))) micFrames += 1;
  };
  micSource.connect(captureNode);
  // Conectar a um GainNode zerado para manter o clock do AudioWorklet ativo sem reproduzir o próprio microfone
  const muteGain = ctx.createGain();
  muteGain.gain.value = 0;
  captureNode.connect(muteGain);
  muteGain.connect(ctx.destination);

  const playbackNode = new AudioWorkletNode(ctx, PLAYBACK_PROCESSOR_NAME);
  const streamDest = ctx.createMediaStreamDestination();
  playbackNode.connect(streamDest);
  let peerFrames = 0;
  const playPeerFrame = (data: ArrayBuffer) => {
    playbackNode.port.postMessage(int16LEToFloat32(data));
    peerFrames += 1;
  };

  // --- telemetry: one console line every ~2 s ---
  let pb: { underrunMs: number; minFillMs: number; fillMs: number } = { underrunMs: 0, minFillMs: 0, fillMs: 0 };
  playbackNode.port.onmessage = (e: MessageEvent) => {
    if (e.data && e.data.t === "pb") pb = e.data;
  };
  // AudioContext render load (Chrome 123+): fraction of the audio quantum budget used.
  let audioLoad = -1;
  let audioUnderrun = -1;
  try {
    const rc = (ctx as unknown as { renderCapacity?: { start: (o: { updateInterval: number }) => void; addEventListener: (t: string, cb: (ev: { averageLoad: number; peakLoad: number; underrunRatio: number }) => void) => void } }).renderCapacity;
    if (rc) {
      rc.start({ updateInterval: 2 });
      rc.addEventListener("update", (ev) => {
        audioLoad = ev.averageLoad;
        audioUnderrun = ev.underrunRatio;
      });
    }
  } catch {}
  let expected = Date.now() + 2000;
  const statTimer = setInterval(() => {
    const now = Date.now();
    const lag = Math.round(now - expected); // event-loop lag: how late this fire is
    expected = now + 2000;
    const peerPerSec = peerFrames;
    const micPerSec = micFrames;
    peerFrames = 0;
    micFrames = 0;

    if (!audioDebugEnabled()) return; // off by default — near-zero cost when disabled

    const mem = (performance as unknown as { memory?: { usedJSHeapSize: number } }).memory;
    const jsHeapMb = mem ? mem.usedJSHeapSize / 1048576 : -1;
    const sample = {
      ts: now,
      peerPerSec,
      micPerSec,
      dcTxBufBytes: transport.txBufferedBytes(),
      underrunMs: pb.underrunMs,
      minFillMs: pb.minFillMs,
      fillMs: pb.fillMs,
      audioLoadPct: audioLoad < 0 ? -1 : audioLoad * 100,
      audioUnderrunPct: audioUnderrun < 0 ? -1 : audioUnderrun * 100,
      jsHeapMb,
      loopLagMs: lag,
    };
    useAudioDebug.getState().push(sample);
    // eslint-disable-next-line no-console
    console.info(
      `[arendcalls] peer=${peerPerSec}/2s mic=${micPerSec}/2s dcTxBuf=${sample.dcTxBufBytes}B | ` +
        `underrun=${sample.underrunMs}ms minFill=${sample.minFillMs}ms fill=${sample.fillMs}ms | ` +
        `audioLoad=${sample.audioLoadPct < 0 ? "n/a" : sample.audioLoadPct.toFixed(0) + "%"} ` +
        `audioUnderrun=${sample.audioUnderrunPct < 0 ? "n/a" : sample.audioUnderrunPct.toFixed(1) + "%"} | ` +
        `jsHeap=${jsHeapMb < 0 ? "n/a" : jsHeapMb.toFixed(1) + "MB"} loopLag=${lag}ms`,
    );
  }, 2000);

  return {
    remoteStream: streamDest.stream,
    playPeerFrame,
    setPrebufferMs: (ms) => {
      playbackNode.port.postMessage({ t: "prebuffer", ms });
      // eslint-disable-next-line no-console
      console.info(`[arendcalls] prebuffer=${ms}ms`);
    },
    close: () => {
      try {
        clearInterval(statTimer);
      } catch {}
      try {
        ctx.close();
      } catch {}
    },
  };
};
