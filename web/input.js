// Keyboard, touch and gamepad, merged into the three input bits the engine
// understands. Keys that are not movement or fire are reported as actions.

const KEY_BITS = {
  ArrowLeft: 'left', KeyA: 'left',
  ArrowRight: 'right', KeyD: 'right',
  Space: 'fire', ArrowUp: 'fire', KeyW: 'fire', KeyZ: 'fire',
};

const KEY_ACTIONS = {
  Enter: 'start', NumpadEnter: 'start',
  KeyP: 'pause', Escape: 'back',
  KeyM: 'mute', KeyF: 'fullscreen',
  Digit1: 'speed1', Digit2: 'speed2', Digit4: 'speed4',
};

const STICK_DEADZONE = 0.4;

export class Input {
  #bits; // name -> bit, from the engine
  #held = new Set(); // names held on the keyboard
  #touch = new Set(); // names held on the touch buttons
  #padStart = false;

  /** While false, game keys keep their normal browser meaning (so Space
   *  still presses a focused button in a menu). */
  capture = false;

  /** @param {{left:number,right:number,fire:number}} bits
   *  @param {(action: string) => void} onAction */
  constructor(bits, onAction) {
    this.#bits = bits;
    this.onAction = onAction;

    addEventListener('keydown', (e) => this.#key(e, true));
    addEventListener('keyup', (e) => this.#key(e, false));
    // A key released while the window is not focused never sends keyup.
    addEventListener('blur', () => this.release());
  }

  #key(e, down) {
    if (e.ctrlKey || e.altKey || e.metaKey) return;
    if (e.target instanceof Element && e.target.closest('input, dialog')) return;

    const name = KEY_BITS[e.code];
    if (name) {
      if (!down) this.#held.delete(name);
      else if (this.capture) this.#held.add(name);
      if (this.capture) e.preventDefault(); // no page scrolling mid-game
      return;
    }
    const action = KEY_ACTIONS[e.code];
    if (!action || !down || e.repeat) return;
    // Enter on a focused button already clicks it.
    if (action === 'start' && e.target instanceof Element && e.target.closest('button')) return;
    this.onAction(action);
  }

  /** Makes an element a hold-to-press button for one input. */
  bindTouch(el, name) {
    const set = (down) => {
      if (down) this.#touch.add(name);
      else this.#touch.delete(name);
      el.classList.toggle('down', down);
    };
    el.addEventListener('pointerdown', (e) => {
      e.preventDefault();
      el.setPointerCapture(e.pointerId);
      set(true);
    });
    for (const type of ['pointerup', 'pointercancel', 'lostpointercapture']) {
      el.addEventListener(type, () => set(false));
    }
    el.addEventListener('contextmenu', (e) => e.preventDefault());
  }

  release() {
    this.#held.clear();
    this.#touch.clear();
  }

  /** The input bits for this frame. */
  read() {
    let bits = 0;
    for (const name of this.#held) bits |= this.#bits[name];
    for (const name of this.#touch) bits |= this.#bits[name];
    return bits | this.#gamepad();
  }

  #gamepad() {
    if (!navigator.getGamepads) return 0;
    let bits = 0;
    let start = false;
    for (const pad of navigator.getGamepads()) {
      if (!pad || !pad.connected) continue;
      const pressed = (i) => Boolean(pad.buttons[i] && pad.buttons[i].pressed);
      const x = pad.axes[0] || 0;
      if (x < -STICK_DEADZONE || pressed(14)) bits |= this.#bits.left;
      if (x > STICK_DEADZONE || pressed(15)) bits |= this.#bits.right;
      if (pressed(0) || pressed(2) || pressed(7)) bits |= this.#bits.fire;
      start ||= pressed(9);
    }
    if (start && !this.#padStart) this.onAction('start');
    this.#padStart = start;
    return bits;
  }
}
