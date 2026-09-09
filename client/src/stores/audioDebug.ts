import { create } from "zustand";

// Per-browser audio/telemetry debug switch. Off by default so it is safe to ship
// to production; flip it on from the "Recursos & VPS" modal when a call needs
// investigating. When on, openCall() logs a `[arendcalls]` line to the console
// every 2 s and pushes the same sample here for the modal's live readout.

export type AudioDebugSample = {
  ts: number;
  peerPerSec: number; // peer audio frames received /2s (nominal ~33)
  micPerSec: number; // mic frames sent /2s (nominal ~250)
  dcTxBufBytes: number; // mic bytes queued on the data channel
  underrunMs: number; // playback buffer ran dry this long in the window (>0 = audible)
  minFillMs: number;
  fillMs: number;
  audioLoadPct: number; // AudioContext render load, -1 if unsupported
  audioUnderrunPct: number; // -1 if unsupported
  jsHeapMb: number; // -1 if unsupported
  loopLagMs: number; // event-loop lag on the 2 s timer
};

const KEY = "wacalls.audioDebug";
const MAX_SAMPLES = 40;

type State = {
  enabled: boolean;
  samples: AudioDebugSample[];
  setEnabled: (v: boolean) => void;
  push: (s: AudioDebugSample) => void;
  clear: () => void;
};

export const useAudioDebug = create<State>((set) => ({
  enabled: localStorage.getItem(KEY) === "1",
  samples: [],
  setEnabled: (v) => {
    try {
      localStorage.setItem(KEY, v ? "1" : "0");
    } catch {}
    set(v ? { enabled: true } : { enabled: false, samples: [] });
  },
  push: (s) =>
    set((st) => ({ samples: [...st.samples.slice(-(MAX_SAMPLES - 1)), s] })),
  clear: () => set({ samples: [] }),
}));

/** Cheap check for hot paths that must not subscribe. */
export const audioDebugEnabled = (): boolean => useAudioDebug.getState().enabled;
