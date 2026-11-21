// The client of the server API: accounts, JWT sessions, scores.
//
// Two tokens are involved. The access token is a JWT kept only in this
// object's memory and sent in the Authorization header; it lives 15 minutes.
// The refresh token is an HttpOnly cookie that JavaScript cannot read; the
// browser sends it to /api/auth/refresh to get a new access token. A script
// injected into the page could therefore borrow the session while the page
// is open, but could not carry a long-lived credential away.

const MESSAGES = {
  bad_credentials: 'Неверное имя или пароль.',
  bad_name: 'Имя: 3–16 букв, цифр, «_» или «-».',
  bad_password: 'Пароль: от 8 до 72 символов.',
  name_taken: 'Это имя уже занято.',
  rate_limited: 'Слишком много попыток. Подождите несколько секунд.',
  no_session: 'Сессия истекла. Войдите снова.',
  bad_token: 'Сессия истекла. Войдите снова.',
  bad_ticket: 'Билет игры недействителен или просрочен.',
  ticket_used: 'Этот результат уже отправлен.',
  ticket_owner: 'Игра была начата под другим именем.',
  bad_replay: 'Сервер не смог воспроизвести запись игры.',
  not_finished: 'Запись обрывается до конца игры.',
  too_fast: 'Запись длиннее, чем прошло времени с начала игры.',
  engine_version: 'Игра обновилась. Перезагрузите страницу.',
  too_large: 'Запись игры слишком большая.',
  network: 'Нет связи с сервером.',
  no_api: 'Сервер рекордов недоступен: запустите go run ./cmd/server.',
};

export class ApiError extends Error {
  constructor(status, code, message) {
    super(MESSAGES[code] || message || `Ошибка сервера (${status}).`);
    this.status = status;
    this.code = code;
  }
}

/** Decodes the payload of a JWT. It is base64url JSON, signed but not
 *  encrypted: anyone holding a token can read it. */
export function decodeJwt(token) {
  const part = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
  const bytes = Uint8Array.from(atob(part), (c) => c.charCodeAt(0));
  return JSON.parse(new TextDecoder().decode(bytes));
}

function toBase64(bytes) {
  let s = '';
  for (let i = 0; i < bytes.length; i += 0x8000) {
    s += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
  }
  return btoa(s);
}

function fromBase64(text) {
  return Uint8Array.from(atob(text), (c) => c.charCodeAt(0));
}

const REFRESH_MARGIN_S = 30;

/** Dispatches a "change" event whenever the signed-in user changes. */
export class Api extends EventTarget {
  #token = null;
  #refreshing = null;

  /** @type {{id: string, name: string, games: number, best: number, rank: number} | null} */
  user = null;

  /** The claims of the current access token, or null. */
  get claims() {
    return this.#token ? decodeJwt(this.#token) : null;
  }

  async #send(method, path, body, withToken) {
    const headers = {};
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (withToken && this.#token) headers.Authorization = `Bearer ${this.#token}`;
    let res;
    try {
      res = await fetch(path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        credentials: 'same-origin',
      });
    } catch {
      throw new ApiError(0, 'network');
    }
    if (res.status === 204) return null;
    let data;
    try {
      data = await res.json();
    } catch {
      throw new ApiError(res.status, 'no_api'); // a plain file server answered
    }
    if (!res.ok) {
      throw new ApiError(res.status, data.error?.code, data.error?.message);
    }
    return data;
  }

  // A call that needs the access token. It renews a token that is about to
  // expire first, and renews once more if the server still says 401.
  async #authed(method, path, body) {
    const claims = this.claims;
    if (claims && claims.exp - Date.now() / 1000 < REFRESH_MARGIN_S) {
      await this.#refresh().catch(() => {});
    }
    try {
      return await this.#send(method, path, body, true);
    } catch (err) {
      if (!(err instanceof ApiError) || err.status !== 401) throw err;
      await this.#refresh();
      return this.#send(method, path, body, true);
    }
  }

  // Several calls may find the token expired at once; they share one refresh.
  #refresh() {
    this.#refreshing ??= this.#send('POST', '/api/auth/refresh')
      .then((session) => this.#open(session))
      .catch((err) => {
        if (err.status === 401) this.#close();
        throw err;
      })
      .finally(() => { this.#refreshing = null; });
    return this.#refreshing;
  }

  #open(session) {
    this.#token = session.token;
    this.user = session.user;
    this.dispatchEvent(new Event('change'));
  }

  #close() {
    if (!this.#token && !this.user) return;
    this.#token = null;
    this.user = null;
    this.dispatchEvent(new Event('change'));
  }

  /** Picks up a session left by an earlier visit, if the cookie is there. */
  async restore() {
    try {
      await this.#refresh();
    } catch {
      // No cookie or no server: stay a guest.
    }
  }

  async login(name, password) {
    this.#open(await this.#send('POST', '/api/auth/login', { name, password }));
  }

  async register(name, password) {
    this.#open(await this.#send('POST', '/api/auth/register', { name, password }));
  }

  async logout() {
    try {
      await this.#send('POST', '/api/auth/logout');
    } finally {
      this.#close();
    }
  }

  async me() {
    this.user = await this.#authed('GET', '/api/me');
    this.dispatchEvent(new Event('change'));
  }

  /** Asks the server for a seed and the signed ticket that goes with it. */
  newGame() {
    return this.#authed('POST', '/api/games');
  }

  /** Sends the recording of a finished game. The score is not sent: the
   *  server works it out by replaying the inputs. */
  submit(ticket, recording) {
    return this.#authed('POST', '/api/scores', { ticket, replay: toBase64(recording) });
  }

  async leaderboard(limit = 10) {
    return (await this.#send('GET', `/api/leaderboard?limit=${limit}`)).entries;
  }

  async replay(userId) {
    const r = await this.#send('GET', `/api/replays/${encodeURIComponent(userId)}`);
    return { ...r, replay: fromBase64(r.replay) };
  }
}
