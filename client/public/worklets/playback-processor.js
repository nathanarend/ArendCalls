// Minimal playback ring buffer for 16 kHz mono PCM from the server data channel.
// On underrun it emits silence and resumes on the next sample. A deep buffer /
// re-arm turned brief network gaps into long dropouts, so this stays dumb.
// Optional prebuffer (server-configured, default 0 = off): when the ring runs
// dry (e.g. the contact paused, DTX), the next talkspurt is held for that many
// ms before playing, so jitter on its second frame doesn't clip the first
// syllable. It's time-based, not a fill target: a lone frame still plays.
const RING_SIZE = 16000 * 2; // 2 s
const REPORT_QUANTA = 250; // ~2 s of 128-sample render quanta @ 16 kHz

class PlaybackProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.ring = new Float32Array(RING_SIZE);
    this.read = 0;
    this.write = 0;
    this.available = 0;

    this.prebufferSamples = 0;
    this.priming = true;
    this.primeWait = 0;

    // lightweight telemetry (reported to the main thread ~every 2 s)
    this.underrunSamples = 0;
    this.minAvailable = RING_SIZE;
    this.quanta = 0;

    this.port.onmessage = (e) => {
      const data = e.data;
      if (data && data.t === "prebuffer") {
        this.prebufferSamples = Math.max(0, Math.round(data.ms * 16)); // 16 samples/ms @ 16 kHz
        return;
      }
      if (!data || !data.length) return;
      for (let i = 0; i < data.length; i += 1) {
        this.ring[this.write] = data[i];
        this.write = (this.write + 1) % RING_SIZE;
        if (this.available < RING_SIZE) {
          this.available += 1;
        } else {
          this.read = (this.read + 1) % RING_SIZE; // full: drop oldest
        }
      }
    };
  }

  process(_inputs, outputs) {
    const out = outputs[0] && outputs[0][0];
    if (!out) return true;

    if (this.available < this.minAvailable) this.minAvailable = this.available;

    if (this.priming && this.available > 0) {
      if (this.primeWait >= this.prebufferSamples) this.priming = false;
      else this.primeWait += out.length;
    }

    for (let i = 0; i < out.length; i += 1) {
      if (this.available > 0 && !this.priming) {
        out[i] = this.ring[this.read];
        this.read = (this.read + 1) % RING_SIZE;
        this.available -= 1;
        if (this.available === 0) {
          this.priming = true;
          this.primeWait = 0;
        }
      } else {
        out[i] = 0;
        // Held on purpose while priming isn't an underrun.
        if (this.available === 0) this.underrunSamples += 1;
      }
    }

    this.quanta += 1;
    if (this.quanta >= REPORT_QUANTA) {
      this.port.postMessage({
        t: "pb",
        underrunMs: Math.round(this.underrunSamples / 16), // 16 samples/ms @ 16 kHz
        minFillMs: Math.round(this.minAvailable / 16),
        fillMs: Math.round(this.available / 16),
      });
      this.underrunSamples = 0;
      this.minAvailable = RING_SIZE;
      this.quanta = 0;
    }
    return true;
  }
}

registerProcessor("playback-processor", PlaybackProcessor);
