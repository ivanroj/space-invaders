// wasm.js is the bridge between the page and main.wasm.
//
// A WebAssembly module cannot touch the DOM, the network or the clock. It
// can only call functions it was given at instantiation ("imports") and
// offer functions of its own. For Go the imports come from wasm_exec.js,
// which ships with the compiler; the Go program then publishes its API as
// the global object SpaceInvaders (see cmd/wasm/main.go).
//
// Nothing else on the page talks to Wasm. app.js sees only the Engine class
// below, so the rest of the front end is ordinary JavaScript.

const API_NAME = 'SpaceInvaders';

/**
 * Downloads, compiles and starts the module.
 * @param {string} url
 * @param {(stage: 'download' | 'start') => void} [onStage]
 * @returns {Promise<Engine>}
 */
export async function loadEngine(url, onStage = () => {}) {
  if (typeof WebAssembly !== 'object') {
    throw new Error('Этот браузер не поддерживает WebAssembly.');
  }
  if (typeof globalThis.Go !== 'function') {
    throw new Error('Не найден wasm_exec.js. Запустите ./build.sh.');
  }
  const go = new globalThis.Go();

  onStage('download');
  const { instance } = await instantiate(url, go.importObject);

  onStage('start');
  // run() resolves only when the Go program exits, and ours never does: it
  // parks in `select {}` to keep serving calls. By the time run() yields
  // back here, main() has already published the API.
  const exited = go.run(instance);
  exited.then(() => console.error('main.wasm exited; the game cannot continue'));

  const api = globalThis[API_NAME];
  if (!api) {
    throw new Error('main.wasm запустился, но не опубликовал API.');
  }
  return new Engine(api, url);
}

// instantiateStreaming compiles while the bytes are still arriving, but it
// insists on the Content-Type application/wasm. Some static servers do not
// send it, so fall back to compiling a downloaded buffer.
async function instantiate(url, imports) {
  if (WebAssembly.instantiateStreaming) {
    try {
      return await WebAssembly.instantiateStreaming(fetch(url), imports);
    } catch (err) {
      console.warn('streaming compilation failed, retrying from a buffer:', err);
    }
  }
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`Не удалось загрузить ${url}: HTTP ${response.status}. Запустите ./build.sh.`);
  }
  return WebAssembly.instantiate(await response.arrayBuffer(), imports);
}

/**
 * Engine is the Go game seen from JavaScript.
 *
 * Calling into Wasm has a cost, so the boundary is crossed once per frame:
 * frame() advances the simulation, draws the picture and fills two buffers
 * that JavaScript owns. Reading the score or the events afterwards is plain
 * array access.
 */
export class Engine {
  #api;
  #s; // offsets of the fields in the state block

  constructor(api, url) {
    this.#api = api;
    this.#s = api.state;
    this.url = url;
    this.width = api.width;
    this.height = api.height;
    this.version = api.version;
    this.tickRate = api.tickRate;

    /** Event, phase, mode and input codes, by name. They come from Go so the
     *  two languages cannot disagree. */
    this.events = Object.freeze({ ...api.events });
    this.phases = Object.freeze({ ...api.phases });
    this.modes = Object.freeze({ ...api.modes });
    this.inputs = Object.freeze({ ...api.inputs });

    // The picture: RGBA bytes that Go overwrites every frame. Wrapping the
    // same memory in an ImageData lets the canvas draw it without a copy on
    // the JavaScript side.
    this.pixels = new Uint8ClampedArray(this.width * this.height * 4);
    this.image = new ImageData(this.pixels, this.width, this.height);

    // The numbers: little-endian int32s, viewed through an Int32Array.
    const bytes = new Uint8Array(api.stateLength * 4);
    this.state = new Int32Array(bytes.buffer);
    api.bind(this.pixels, bytes);
  }

  /** Advances the game by dtMs milliseconds with the given input bits. */
  frame(dtMs, input) {
    this.#api.frame(dtMs, input);
  }

  /** Starts a recorded game. The seed is a hex string: a 64-bit number does
   *  not survive a trip through a JavaScript double. */
  start(seedHex) {
    if (!this.#api.start(seedHex)) throw new Error(`bad seed: ${seedHex}`);
  }

  /** Plays back a recording made by replay(). */
  watch(seedHex, recording) {
    if (!this.#api.watch(seedHex, recording)) throw new Error('bad recording');
  }

  /** Returns to the demo that runs behind the menu. */
  demo() {
    this.#api.demo();
  }

  /** Pauses or resumes; returns the resulting state. */
  setPaused(paused) {
    return this.#api.setPaused(paused);
  }

  setHiScore(score) {
    this.#api.setHiScore(score);
  }

  /** The inputs of the current game as bytes the server can replay. */
  replay() {
    return this.#api.replay();
  }

  get mode() { return this.state[this.#s.mode]; }
  get phase() { return this.state[this.#s.phase]; }
  get score() { return this.state[this.#s.score]; }
  get lives() { return this.state[this.#s.lives]; }
  get wave() { return this.state[this.#s.wave]; }
  get tick() { return this.state[this.#s.tick]; }
  get ufoActive() { return this.state[this.#s.ufo] !== 0; }
  get bossActive() { return this.state[this.#s.boss] !== 0; }
  get shots() { return this.state[this.#s.shots]; }
  get hits() { return this.state[this.#s.hits]; }
  get kills() { return this.state[this.#s.kills]; }
  get maxCombo() { return this.state[this.#s.maxCombo]; }
  get over() { return this.phase === this.phases.gameOver; }

  /** Calls fn(kind, a, x) for each thing that happened in the last frame. */
  forEachEvent(fn) {
    const s = this.#s;
    const n = this.state[s.eventCount];
    for (let i = 0; i < n; i++) {
      const at = s.events + i * s.eventFields;
      fn(this.state[at], this.state[at + 1], this.state[at + 2]);
    }
  }
}
