const RING_SIZE = 16000 * 4; // 4 seconds buffer
const PREBUFFER_SAMPLES = 16000 * 0.05; // 50ms pre-buffer threshold
const RESYNC_THRESHOLD = 16000 * 0.03; // 30ms re-sync threshold after underflow

class PlaybackProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.ring = new Float32Array(RING_SIZE);
    this.read = 0;
    this.write = 0;
    this.available = 0;
    this.isPlaying = false;
    this.lastSample = 0;

    this.port.onmessage = (e) => {
      const data = e.data;
      if (!data || !data.length) return;

      for (let i = 0; i < data.length; i += 1) {
        this.ring[this.write] = data[i];
        this.write = (this.write + 1) % RING_SIZE;
        if (this.available < RING_SIZE) {
          this.available += 1;
        } else {
          // Overflow: advance read pointer to drop oldest sample
          this.read = (this.read + 1) % RING_SIZE;
        }
      }

      if (!this.isPlaying && this.available >= PREBUFFER_SAMPLES) {
        this.isPlaying = true;
      }
    };
  }

  process(_inputs, outputs) {
    const out = outputs[0] && outputs[0][0];
    if (!out) return true;

    if (!this.isPlaying) {
      if (this.available >= PREBUFFER_SAMPLES) {
        this.isPlaying = true;
      } else {
        // Output comfort silence / ramp down last sample
        for (let i = 0; i < out.length; i += 1) {
          this.lastSample *= 0.95;
          out[i] = this.lastSample;
        }
        return true;
      }
    }

    for (let i = 0; i < out.length; i += 1) {
      if (this.available > 0) {
        this.lastSample = this.ring[this.read];
        out[i] = this.lastSample;
        this.read = (this.read + 1) % RING_SIZE;
        this.available -= 1;
      } else {
        // Underflow: smooth ramp to silence and re-arm prebuffer
        this.lastSample *= 0.92;
        out[i] = this.lastSample;
        this.isPlaying = false;
      }
    }
    return true;
  }
}

registerProcessor("playback-processor", PlaybackProcessor);
