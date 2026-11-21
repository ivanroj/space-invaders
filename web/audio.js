// Sound effects synthesised with the Web Audio API: oscillators and filtered
// noise, no audio files. The engine reports what happened as events; this
// file decides what each one sounds like.

const STORAGE_KEY = 'si.muted';
const STEP_NOTES = [98, 87, 78, 73]; // the four-note march of the invaders

export class Sound {
  #ctx = null;
  #master = null;
  #noise = null;
  #siren = null;
  #handlers;

  constructor(events) {
    this.muted = localStorage.getItem(STORAGE_KEY) === '1';
    const up = (notes, opts) => notes.forEach((f, i) => this.#tone({ from: f, dur: 0.09, at: i * 0.07, ...opts }));

    /** @type {Record<number, (a: number, pan: number) => void>} */
    this.#handlers = {
      [events.shoot]: (a, pan) => {
        this.#tone({ from: 1300, to: 320, dur: 0.09, vol: 0.1, pan });
        if (a > 1) this.#tone({ from: 1700, to: 500, dur: 0.07, vol: 0.06, pan });
      },
      [events.invaderKilled]: (a, pan) => {
        this.#burst({ dur: 0.13, vol: 0.22, from: 3200, to: 400, pan });
        this.#tone({ from: 320, to: 70, dur: 0.11, vol: 0.12, pan });
      },
      [events.playerHit]: (a, pan) => {
        this.#burst({ dur: 0.8, vol: 0.5, from: 2600, to: 60, pan });
        this.#tone({ type: 'sawtooth', from: 220, to: 28, dur: 0.7, vol: 0.25, pan });
      },
      [events.ufoKilled]: (a, pan) => up([660, 880, 1320, 1760], { vol: 0.12, pan }),
      [events.powerup]: (a, pan) => up(a === 4 ? [523, 659, 784, 1047, 1319] : [523, 784, 1047], { type: 'triangle', vol: 0.2, pan }),
      [events.bunkerHit]: (a, pan) => this.#burst({ dur: 0.05, vol: 0.07, from: 1500, to: 300, pan }),
      [events.bulletClash]: (a, pan) => this.#tone({ from: 1900, to: 900, dur: 0.04, vol: 0.07, pan }),
      [events.step]: (a) => this.#tone({ from: STEP_NOTES[a & 3], dur: 0.08, vol: 0.2 }),
      [events.enemyShoot]: (a, pan) => this.#tone({ type: 'triangle', from: 520, to: 190, dur: 0.09, vol: 0.07, pan }),
      [events.waveStart]: () => up([392, 523, 659], { type: 'triangle', vol: 0.16 }),
      [events.waveClear]: () => up([523, 659, 784, 1047, 1319], { vol: 0.12 }),
      [events.gameOver]: () => {
        this.#tone({ type: 'sawtooth', from: 440, to: 50, dur: 1.5, vol: 0.22 });
        [330, 262, 196, 131].forEach((f, i) => this.#tone({ from: f, dur: 0.28, at: 0.2 + i * 0.3, vol: 0.12 }));
      },
      [events.bossHit]: (a, pan) => this.#tone({ from: 150, to: 70, dur: 0.06, vol: 0.18, pan }),
      [events.bossKilled]: (a, pan) => {
        this.#burst({ dur: 1.6, vol: 0.55, from: 3000, to: 40, pan });
        [0, 0.15, 0.3, 0.5].forEach((at) => this.#tone({ type: 'sawtooth', from: 180, to: 30, dur: 0.5, at, vol: 0.2, pan }));
        up([523, 659, 784, 1047, 1319, 1568], { at: 0.9, vol: 0.12 });
      },
      [events.bossShoot]: (a, pan) => this.#tone({ type: 'sawtooth', from: 300, to: 110, dur: 0.11, vol: 0.07, pan }),
      [events.extraLife]: () => up([784, 988, 1175, 1568], { vol: 0.14 }),
      [events.shieldBreak]: (a, pan) => {
        this.#tone({ type: 'triangle', from: 1500, to: 180, dur: 0.25, vol: 0.2, pan });
        this.#burst({ dur: 0.15, vol: 0.15, from: 5000, to: 1000, pan });
      },
    };
  }

  /** Browsers start audio only after a click or a key press, so this is
   *  called from the handlers that start a game. */
  unlock() {
    if (!this.#ctx) {
      const Ctx = globalThis.AudioContext || globalThis.webkitAudioContext;
      if (!Ctx) return;
      const ctx = new Ctx();
      const compressor = ctx.createDynamicsCompressor(); // keeps a busy screen from clipping
      this.#master = ctx.createGain();
      this.#master.gain.value = this.muted ? 0 : 0.6;
      this.#master.connect(compressor).connect(ctx.destination);

      // One second of white noise, reused for every explosion.
      this.#noise = ctx.createBuffer(1, ctx.sampleRate, ctx.sampleRate);
      const data = this.#noise.getChannelData(0);
      for (let i = 0; i < data.length; i++) data[i] = Math.random() * 2 - 1;
      this.#ctx = ctx;
    }
    if (this.#ctx.state === 'suspended') this.#ctx.resume();
  }

  setMuted(muted) {
    this.muted = muted;
    localStorage.setItem(STORAGE_KEY, muted ? '1' : '0');
    if (this.#master) this.#master.gain.value = muted ? 0 : 0.6;
  }

  /** Plays the sound of one engine event. x is where it happened, 0..width. */
  play(kind, a, x, width) {
    if (this.#ctx?.state !== 'running' || this.muted) return;
    const handler = this.#handlers[kind];
    if (handler) handler(a, Math.max(-1, Math.min(1, (x / width) * 2 - 1)) * 0.7);
  }

  /** The warble of the mystery ship, on for as long as it is on screen. */
  siren(on) {
    if (this.#ctx?.state !== 'running' || on === Boolean(this.#siren)) return;
    const ctx = this.#ctx;
    if (!on) {
      this.#siren.gain.gain.setTargetAtTime(0, ctx.currentTime, 0.03);
      this.#siren.osc.stop(ctx.currentTime + 0.2);
      this.#siren.lfo.stop(ctx.currentTime + 0.2);
      this.#siren = null;
      return;
    }
    const osc = ctx.createOscillator();
    const lfo = ctx.createOscillator();
    const depth = ctx.createGain();
    const gain = ctx.createGain();
    osc.type = 'triangle';
    osc.frequency.value = 620;
    lfo.frequency.value = 7;
    depth.gain.value = 160;
    gain.gain.value = 0.06;
    lfo.connect(depth).connect(osc.frequency); // the LFO bends the pitch
    osc.connect(gain).connect(this.#master);
    osc.start();
    lfo.start();
    this.#siren = { osc, lfo, gain };
  }

  #out(node, pan) {
    if (pan && this.#ctx.createStereoPanner) {
      const panner = this.#ctx.createStereoPanner();
      panner.pan.value = pan;
      node.connect(panner).connect(this.#master);
    } else {
      node.connect(this.#master);
    }
  }

  // A note whose pitch slides from `from` to `to` while it fades out.
  #tone({ type = 'square', from, to = from, dur, vol = 0.15, at = 0, pan = 0 }) {
    const ctx = this.#ctx;
    const t = ctx.currentTime + at;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = type;
    osc.frequency.setValueAtTime(from, t);
    if (to !== from) osc.frequency.exponentialRampToValueAtTime(to, t + dur);
    gain.gain.setValueAtTime(vol, t);
    gain.gain.exponentialRampToValueAtTime(0.0001, t + dur);
    osc.connect(gain);
    this.#out(gain, pan);
    osc.start(t);
    osc.stop(t + dur + 0.02);
  }

  // Noise through a low-pass filter that closes: the sound of an explosion.
  #burst({ dur, vol, from, to, at = 0, pan = 0 }) {
    const ctx = this.#ctx;
    const t = ctx.currentTime + at;
    const src = ctx.createBufferSource();
    const filter = ctx.createBiquadFilter();
    const gain = ctx.createGain();
    src.buffer = this.#noise;
    src.loop = true;
    filter.type = 'lowpass';
    filter.frequency.setValueAtTime(from, t);
    filter.frequency.exponentialRampToValueAtTime(to, t + dur);
    gain.gain.setValueAtTime(vol, t);
    gain.gain.exponentialRampToValueAtTime(0.0001, t + dur);
    src.connect(filter).connect(gain);
    this.#out(gain, pan);
    src.start(t, Math.random() * 0.5);
    src.stop(t + dur + 0.02);
  }
}
