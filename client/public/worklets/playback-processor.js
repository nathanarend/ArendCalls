// Minimal playback ring buffer for 16 kHz mono PCM from the server data channel.
// No pre-buffer and no re-prime: on underrun it emits silence and resumes on the
// next sample. A deep buffer / re-arm turned brief network gaps into long
// dropouts, so this deliberately stays dumb (matches upstream WaCalls).
const RING_SIZE = 16000 * 2; // 2 s
const REPORT_QUANTA = 250; // ~2 s of 128-sample render quanta @ 16 kHz

class PlaybackProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.ring = new Float32Array(RING_SIZE);
    this.read = 0;
    this.write = 0;
    this.available = 0;

    // lightweight telemetry (reported to the main thread ~every 2 s)
    this.underrunSamples = 0;
    this.minAvailable = RING_SIZE;
    this.quanta = 0;

    this.port.onmessage = (e) => {
      const data = e.data;
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

    for (let i = 0; i < out.length; i += 1) {
      if (this.available > 0) {
        out[i] = this.ring[this.read];
        this.read = (this.read + 1) % RING_SIZE;
        this.available -= 1;
      } else {
        out[i] = 0;
        this.underrunSamples += 1;
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
