// The page: screens, buttons, the frame loop. The game itself is in
// main.wasm; this file only feeds it input and shows what it draws.

import { loadEngine } from './wasm.js';
import { Input } from './input.js';
import { Sound } from './audio.js';
import { Api } from './api.js';

const $ = (id) => document.getElementById(id);

const screenEl = $('screen');
const stageEl = $('stage');
const canvas = $('canvas');
const ctx2d = canvas.getContext('2d', { alpha: false });
const views = ['loading', 'error', 'menu', 'over', 'pause'];

const BEST_KEY = 'si.best';
const CRT_KEY = 'si.crt';
const OVER_DELAY_MS = 1400; // let the last explosion play out
const WATCH_END_MS = 2500;

const api = new Api();
/** @type {import('./wasm.js').Engine} */
let engine;
/** @type {Input} */
let input;
/** @type {Sound} */
let sound;

// Which screen is up: 'menu' (demo behind it), 'play', 'watch' or 'over'.
let scene = 'loading';
let paused = false;
let starting = false;
let ticket = null; // the server's ticket for the game being played
let pending = null; // a finished game the server has not accepted yet
let overSince = 0;
let watchSpeed = 1;
let boardTop = 0;
let crt = localStorage.getItem(CRT_KEY) !== '0';

function show(view) {
  for (const name of views) $(`view-${name}`).hidden = name !== view;
}

function fail(err) {
  console.error(err);
  $('error-text').textContent = err.message || String(err);
  show('error');
}

// ---------- layout ----------

// The picture is scaled by a whole number where possible: at 2.5x some game
// pixels would be two screen pixels wide and others three.
function layout() {
  const box = stageEl.getBoundingClientRect();
  const touch = $('touch');
  const below = touch.offsetHeight ? touch.offsetHeight + 12 : 0;
  const fullscreen = document.fullscreenElement === stageEl;
  const availW = fullscreen ? innerWidth : box.width;
  const availH = (fullscreen ? innerHeight : innerHeight - box.top - 16) - below;
  let scale = Math.min(availW / canvas.width, availH / canvas.height);
  scale = scale >= 2 ? Math.floor(scale) : Math.max(scale, 1);
  screenEl.style.setProperty('--px', `${scale}px`);
  screenEl.classList.toggle('crt', crt && scale >= 2); // scan lines need whole pixels
}

// ---------- scenes ----------

function setScene(next) {
  scene = next;
  paused = false;
  overSince = 0;
  input.capture = next === 'play';
  input.release();
  $('watch-banner').hidden = next !== 'watch';
  show(next === 'menu' || next === 'over' ? next : null);
}

function toMenu() {
  engine.demo();
  setScene('menu');
  updateAccount();
  $('btn-play').focus({ preventScroll: true });
}

function randomSeed() {
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

async function startGame() {
  if (starting) return;
  starting = true;
  sound.unlock();
  let seed = randomSeed();
  ticket = null;
  // A signed-in player gets the seed from the server, inside a ticket that
  // makes the score verifiable. A guest just plays.
  if (api.user) {
    try {
      const game = await api.newGame();
      if (game.engine !== engine.version) throw new Error('engine version mismatch');
      ({ seed, ticket } = game);
    } catch (err) {
      console.warn('playing without a ticket:', err);
    }
  }
  try {
    engine.start(seed);
    setScene('play');
  } finally {
    starting = false;
  }
}

async function watch(entry) {
  try {
    sound.unlock();
    const rec = await api.replay(entry.userId);
    engine.watch(rec.seed, rec.replay);
    watchSpeed = 1;
    $('watch-text').textContent = `Запись: ${rec.name} · ${rec.score}`;
    setScene('watch');
  } catch (err) {
    $('board-note').textContent = err.message;
  }
}

function togglePause() {
  if ((scene !== 'play' && scene !== 'watch') || engine.over) return;
  paused = engine.setPaused(!paused);
  input.release();
  show(paused && scene === 'play' ? 'pause' : null);
  if (paused && scene === 'play') $('btn-resume').focus({ preventScroll: true });
}

function onAction(action) {
  switch (action) {
    case 'start':
      if (scene === 'menu' || scene === 'over') startGame();
      else if (paused) togglePause();
      break;
    case 'pause':
      togglePause();
      break;
    case 'back':
      if (scene === 'play') togglePause();
      else if (scene === 'watch' || scene === 'over') toMenu();
      break;
    case 'mute':
      setMuted(!sound.muted);
      break;
    case 'fullscreen':
      toggleFullscreen();
      break;
    case 'speed1':
    case 'speed2':
    case 'speed4':
      if (scene === 'watch') watchSpeed = Number(action.slice(-1));
      break;
  }
}

// ---------- game over and the score ----------

function gameOver() {
  const { score, wave, shots, hits, maxCombo } = engine;
  $('over-score').textContent = score.toLocaleString('ru-RU');
  $('over-wave').textContent = wave;
  $('over-acc').textContent = `${shots ? Math.min(100, Math.round((hits * 100) / shots)) : 0}%`;
  $('over-combo').textContent = maxCombo;
  if (score > localBest()) localStorage.setItem(BEST_KEY, String(score));
  syncHiScore();

  setScene('over');
  $('btn-again').focus({ preventScroll: true });

  if (ticket) {
    pending = { ticket, replay: engine.replay() };
    ticket = null;
    submitPending();
  } else if (api.user) {
    verdict('', 'Результат не отправлен: в начале игры не было связи с сервером.');
  } else {
    verdict('', 'Гостевой результат не сохраняется. Войдите до начала игры, чтобы попасть в таблицу рекордов.');
  }
}

function verdict(kind, text) {
  const el = $('over-verdict');
  el.className = `verdict ${kind}`;
  el.textContent = text;
}

async function submitPending() {
  if (!pending) return;
  const { ticket: t, replay } = pending;
  verdict('', `Сервер переигрывает запись: ${replay.length.toLocaleString('ru-RU')} байт нажатий…`);
  try {
    const res = await api.submit(t, replay);
    pending = null;
    const best = res.personalBest ? 'Новый личный рекорд!' : `Личный рекорд: ${res.best.toLocaleString('ru-RU')}.`;
    verdict('ok', `Проверено сервером: ${res.score.toLocaleString('ru-RU')} очков. Место в таблице: ${res.rank}. ${best}`);
    loadBoard();
    api.me().catch(() => {});
  } catch (err) {
    // After a long game the session may be gone; signing in again retries.
    const retry = err.status === 401 ? ' Войдите снова, и результат будет отправлен.' : '';
    if (err.status !== 401 && err.status !== 0) pending = null;
    verdict('bad', err.message + retry);
  }
}

function localBest() {
  return Number(localStorage.getItem(BEST_KEY)) || 0;
}

function syncHiScore() {
  engine.setHiScore(Math.max(localBest(), boardTop, api.user?.best || 0));
}

// ---------- leaderboard ----------

async function loadBoard() {
  const list = $('board');
  const note = $('board-note');
  try {
    const entries = await api.leaderboard(10);
    boardTop = entries[0]?.score || 0;
    syncHiScore();
    list.replaceChildren(...entries.map(boardRow));
    note.textContent = entries.length ? '' : 'Пока пусто. Станьте первым!';
    note.hidden = entries.length > 0;
  } catch (err) {
    list.replaceChildren();
    note.hidden = false;
    note.textContent = err.message;
  }
}

// Names come from other users, so rows are built with textContent, never
// from an HTML string.
function boardRow(entry) {
  const li = document.createElement('li');
  li.classList.toggle('me', entry.userId === api.user?.id);
  const cell = (cls, text) => {
    const span = document.createElement('span');
    span.className = cls;
    span.textContent = text;
    return span;
  };
  const play = document.createElement('button');
  play.type = 'button';
  play.textContent = '▶';
  play.title = `Смотреть запись игры: ${entry.name}`;
  play.setAttribute('aria-label', play.title);
  play.addEventListener('click', () => watch(entry));
  li.title = `Волна ${entry.wave} · точность ${entry.accuracy}% · серия ${entry.maxCombo}`;
  li.append(cell('rank', entry.rank), cell('name', entry.name), cell('pts', entry.score.toLocaleString('ru-RU')), play);
  return li;
}

// ---------- account ----------

function updateAccount() {
  const user = api.user;
  $('account-name').hidden = !user;
  $('account-name').textContent = user ? user.name : '';
  $('btn-login').hidden = Boolean(user);
  $('btn-logout').hidden = !user;
  $('menu-hint').textContent = user
    ? `Пилот ${user.name}${user.best ? ` · рекорд ${user.best.toLocaleString('ru-RU')}` : ''}. Результат попадёт в таблицу.`
    : 'Вы играете как гость. Войдите, чтобы попасть в таблицу рекордов.';
  for (const li of $('board').children) li.classList.remove('me');
  if (engine) syncHiScore();
}

let registering = false;

function setAuthMode(register) {
  registering = register;
  $('tab-login').setAttribute('aria-selected', String(!register));
  $('tab-register').setAttribute('aria-selected', String(register));
  $('auth-title').textContent = register ? 'Регистрация' : 'Вход';
  $('auth-submit').textContent = register ? 'СОЗДАТЬ' : 'ВОЙТИ';
  $('auth-password').autocomplete = register ? 'new-password' : 'current-password';
  $('auth-hint').hidden = !register;
  $('auth-error').hidden = true;
}

function openAuth() {
  if (scene === 'play' && !paused) togglePause();
  setAuthMode(false);
  $('auth-password').value = '';
  $('auth').showModal();
  $('auth-name').focus();
}

async function submitAuth(e) {
  e.preventDefault();
  const name = $('auth-name').value.trim();
  const password = $('auth-password').value;
  const button = $('auth-submit');
  button.disabled = true;
  $('auth-error').hidden = true;
  try {
    await (registering ? api.register(name, password) : api.login(name, password));
    $('auth').close();
    $('auth-password').value = '';
    loadBoard();
    if (pending) submitPending();
    if (scene === 'menu') $('btn-play').focus({ preventScroll: true });
  } catch (err) {
    $('auth-error').textContent = err.message;
    $('auth-error').hidden = false;
  } finally {
    button.disabled = false;
  }
}

// ---------- settings ----------

function setMuted(muted) {
  sound.unlock();
  sound.setMuted(muted);
  $('btn-sound').setAttribute('aria-pressed', String(!muted));
}

function setCrt(on) {
  crt = on;
  localStorage.setItem(CRT_KEY, on ? '1' : '0');
  $('btn-crt').setAttribute('aria-pressed', String(on));
  layout();
}

function toggleFullscreen() {
  if (document.fullscreenElement) document.exitFullscreen();
  else stageEl.requestFullscreen?.().catch(() => {});
}

// ---------- the frame loop ----------

const perf = { frames: 0, goMs: 0, since: 0 };
let lastFrame = 0;

function frame(now) {
  requestAnimationFrame(frame);
  const dt = Math.min(now - lastFrame, 250);
  lastFrame = now;

  const bits = input.read(); // also polls the gamepad's Start button
  const t0 = performance.now();
  // The single call into Go for this frame: simulate, draw, report.
  engine.frame(scene === 'watch' ? dt * watchSpeed : dt, scene === 'play' ? bits : 0);
  perf.goMs += performance.now() - t0;
  perf.frames++;

  ctx2d.putImageData(engine.image, 0, 0);

  const live = scene === 'play' || scene === 'watch';
  if (live) engine.forEachEvent((kind, a, x) => sound.play(kind, a, x, engine.width));
  sound.siren(live && !paused && !engine.over && engine.ufoActive);

  if (live && engine.over) {
    overSince ||= now;
    if (scene === 'play' && now - overSince > OVER_DELAY_MS) gameOver();
    else if (scene === 'watch' && now - overSince > WATCH_END_MS) toMenu();
  }
  if (now - perf.since > 500) updateStats(now);
}

function size(bytes) {
  return bytes >= 1 << 20 ? `${(bytes / (1 << 20)).toFixed(1)} МБ` : `${Math.round(bytes / 1024)} КБ`;
}

function updateStats(now) {
  $('st-fps').textContent = Math.round((perf.frames * 1000) / (now - perf.since));
  $('st-go').textContent = `${(perf.goMs / perf.frames).toFixed(2)} мс`;
  perf.frames = 0;
  perf.goMs = 0;
  perf.since = now;

  const entry = performance.getEntriesByName(new URL(engine.url, location.href).href)[0];
  if (entry && entry.decodedBodySize) {
    const sent = entry.encodedBodySize || entry.transferSize;
    $('st-wasm').textContent = sent && sent < entry.decodedBodySize
      ? `${size(sent)} / ${size(entry.decodedBodySize)}`
      : size(entry.decodedBodySize);
    $('st-wasm').title = 'по сети (gzip) / после распаковки';
  }
  $('st-tick').textContent = engine.tick;
  $('st-replay').textContent = scene === 'play' ? `${engine.replay().length} Б` : '–';

  const claims = api.claims;
  const pre = $('st-claims');
  pre.hidden = !claims;
  if (claims) {
    const left = Math.max(0, Math.round(claims.exp - Date.now() / 1000));
    $('st-jwt').textContent = `ещё ${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}`;
    pre.textContent = JSON.stringify(claims, null, 1);
  } else {
    $('st-jwt').textContent = 'нет';
  }
}

// ---------- start ----------

async function boot() {
  layout();
  addEventListener('resize', layout);
  document.addEventListener('fullscreenchange', layout);

  engine = await loadEngine('main.wasm', (stage) => {
    $('load-status').textContent = stage === 'download' ? 'Загрузка и компиляция main.wasm…' : 'Запуск Go…';
  });
  input = new Input(engine.inputs, onAction);
  sound = new Sound(engine.events);

  for (const el of document.querySelectorAll('[data-input]')) input.bindTouch(el, el.dataset.input);
  $('touch-pause').addEventListener('click', togglePause);
  $('btn-play').addEventListener('click', startGame);
  $('btn-again').addEventListener('click', startGame);
  $('btn-menu').addEventListener('click', toMenu);
  $('btn-resume').addEventListener('click', togglePause);
  $('btn-quit').addEventListener('click', toMenu);
  $('btn-sound').addEventListener('click', () => setMuted(!sound.muted));
  $('btn-crt').addEventListener('click', () => setCrt(!crt));
  $('btn-full').addEventListener('click', toggleFullscreen);
  $('btn-login').addEventListener('click', openAuth);
  $('btn-logout').addEventListener('click', () => api.logout().catch(() => {}).then(loadBoard));
  $('tab-login').addEventListener('click', () => setAuthMode(false));
  $('tab-register').addEventListener('click', () => setAuthMode(true));
  $('auth-cancel').addEventListener('click', () => $('auth').close());
  $('auth-form').addEventListener('submit', submitAuth);
  $('btn-sound').setAttribute('aria-pressed', String(!sound.muted));
  $('btn-crt').setAttribute('aria-pressed', String(crt));

  // Do not let the invaders win while the player is in another tab.
  const autoPause = () => { if (scene === 'play' && !paused) togglePause(); };
  document.addEventListener('visibilitychange', () => { if (document.hidden) autoPause(); });
  addEventListener('blur', autoPause);

  api.addEventListener('change', updateAccount);
  toMenu();
  requestAnimationFrame(frame);
  api.restore().then(loadBoard);
}

boot().catch(fail);
