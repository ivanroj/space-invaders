package server

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"spaceinvaders/internal/replay"
	"spaceinvaders/internal/sim"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

// clock is a settable time source, so tests can expire tokens instantly.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	t     *testing.T
	url   string
	clock *clock
	store *Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)}
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte(strings.Repeat("<p>space invaders</p>", 50)), 0o644)
	srv, err := New(Config{Secret: testSecret, Store: store, WebDir: web, BcryptCost: bcrypt.MinCost, Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return &env{t: t, url: ts.URL, clock: c, store: store}
}

// client is one browser: it keeps cookies and the access token.
type client struct {
	e     *env
	http  *http.Client
	token string
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}}
}

// do sends a request and decodes the JSON answer into out (if not nil). It
// returns the status and the error code of a failed call.
func (c *client) do(method, path string, body, out any) (int, string) {
	c.e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.url+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		var e struct{ Error apiError }
		json.Unmarshal(data, &e)
		return res.StatusCode, e.Error.Code
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			c.e.t.Fatalf("%s %s: bad JSON %q: %v", method, path, data, err)
		}
	}
	return res.StatusCode, ""
}

func (c *client) want(status int, code string, method, path string, body, out any) {
	c.e.t.Helper()
	gotStatus, gotCode := c.do(method, path, body, out)
	if gotStatus != status || gotCode != code {
		c.e.t.Fatalf("%s %s: got %d %q, want %d %q", method, path, gotStatus, gotCode, status, code)
	}
}

func (c *client) register(name string) session {
	c.e.t.Helper()
	var s session
	c.want(201, "", "POST", "/api/auth/register", credentials{name, "correct horse"}, &s)
	c.token = s.Token
	return s
}

// playOut plays a game to its end with a fixed input pattern.
func playOut(seedHex string) ([]byte, *sim.Game) { return playFor(seedHex, 1<<30) }

// playFor is playOut that stops after maxTicks at the latest.
func playFor(seedHex string, maxTicks uint32) ([]byte, *sim.Game) {
	seed, _ := strconv.ParseUint(seedHex, 16, 64)
	g := sim.New(seed)
	var rec replay.Recorder
	rec.Reset()
	for !g.Over() && g.Tick < maxTicks {
		in := sim.InFire
		if g.Tick/90%2 == 0 {
			in |= sim.InLeft
		} else {
			in |= sim.InRight
		}
		rec.Add(in)
		g.Step(in)
	}
	return rec.Bytes(), g
}

func gameTime(g *sim.Game) time.Duration {
	return time.Duration(g.Tick)*time.Second/sim.TickRate + time.Second
}

func TestAccounts(t *testing.T) {
	e := newEnv(t)
	a := e.client()

	a.want(400, "bad_name", "POST", "/api/auth/register", credentials{"x", "correct horse"}, nil)
	a.want(400, "bad_name", "POST", "/api/auth/register", credentials{"<script>", "correct horse"}, nil)
	a.want(400, "bad_password", "POST", "/api/auth/register", credentials{"alice", "short"}, nil)
	a.want(400, "bad_password", "POST", "/api/auth/register", credentials{"alice", strings.Repeat("x", 73)}, nil)

	s := a.register("Алиса_1")
	if s.User.Name != "Алиса_1" || s.Token == "" || s.ExpiresIn != 900 {
		t.Fatalf("unexpected session %+v", s)
	}
	a.want(409, "name_taken", "POST", "/api/auth/register", credentials{"алиса_1", "correct horse"}, nil)

	b := e.client()
	b.want(401, "bad_credentials", "POST", "/api/auth/login", credentials{"Алиса_1", "wrong password"}, nil)
	b.want(401, "bad_credentials", "POST", "/api/auth/login", credentials{"nobody", "correct horse"}, nil)
	b.want(401, "no_token", "GET", "/api/me", nil, nil)
	var login session
	b.want(200, "", "POST", "/api/auth/login", credentials{"алиса_1", "correct horse"}, &login)
	b.token = login.Token
	var me userView
	b.want(200, "", "GET", "/api/me", nil, &me)
	if me.ID != s.User.ID {
		t.Fatalf("me = %+v, want id %s", me, s.User.ID)
	}

	if u, _ := e.store.UserByName("Алиса_1"); bytes.Contains(u.Hash, []byte("correct horse")) {
		t.Fatal("the password is stored in clear text")
	}
}

func TestScoreIsTakenFromServerPlayback(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	me := c.register("alice")

	var game gameView
	c.want(201, "", "POST", "/api/games", nil, &game)
	rec, g := playOut(game.Seed)
	if g.Score == 0 {
		t.Fatal("the test game scored nothing; the test proves little")
	}
	submit := submitRequest{Ticket: game.Ticket, Replay: rec}

	// Sent at once, the game would have been played faster than real time.
	c.want(422, "too_fast", "POST", "/api/scores", submit, nil)

	e.clock.advance(gameTime(g))
	var res submitView
	c.want(200, "", "POST", "/api/scores", submit, &res)
	if res.Score != g.Score || res.Wave != g.Wave || !res.PersonalBest || res.Rank != 1 {
		t.Fatalf("got %+v, want score %d wave %d", res, g.Score, g.Wave)
	}

	// One ticket, one score.
	c.want(409, "ticket_used", "POST", "/api/scores", submit, nil)

	var board struct{ Entries []Entry }
	e.client().want(200, "", "GET", "/api/leaderboard", nil, &board)
	if len(board.Entries) != 1 || board.Entries[0].Name != "alice" || board.Entries[0].Score != g.Score {
		t.Fatalf("leaderboard = %+v", board.Entries)
	}

	var rv replayView
	e.client().want(200, "", "GET", "/api/replays/"+me.User.ID, nil, &rv)
	if rv.Seed != game.Seed || !bytes.Equal(rv.Replay, rec) {
		t.Fatal("the stored replay differs from the submitted one")
	}
	e.client().want(404, "no_replay", "GET", "/api/replays/ffff", nil, nil)
}

func TestCheatsAreRejected(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.client(), e.client()
	alice.register("alice")
	bob.register("bob")

	var game gameView
	alice.want(201, "", "POST", "/api/games", nil, &game)
	rec, g := playOut(game.Seed)
	e.clock.advance(gameTime(g))

	// There is no score field to lie in.
	body := map[string]any{"ticket": game.Ticket, "replay": rec, "score": 999999}
	alice.want(400, "bad_json", "POST", "/api/scores", body, nil)

	// Someone else's ticket.
	bob.want(403, "ticket_owner", "POST", "/api/scores", submitRequest{game.Ticket, rec}, nil)

	// A recording that stops early, or is not a recording.
	half, _ := playFor(game.Seed, g.Tick/2)
	alice.want(422, "not_finished", "POST", "/api/scores", submitRequest{game.Ticket, half}, nil)
	alice.want(422, "bad_replay", "POST", "/api/scores", submitRequest{game.Ticket, []byte("garbage")}, nil)

	// A ticket with a seed of the player's choosing has a broken signature.
	parts := strings.Split(game.Ticket, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	payload = bytes.Replace(payload, []byte(game.Seed), []byte("0000000000000001"), 1)
	parts[1] = base64.RawURLEncoding.EncodeToString(payload)
	forged := strings.Join(parts, ".")
	alice.want(400, "bad_ticket", "POST", "/api/scores", submitRequest{forged, rec}, nil)

	// An access token is not a ticket.
	alice.want(400, "bad_ticket", "POST", "/api/scores", submitRequest{alice.token, rec}, nil)

	var board struct{ Entries []Entry }
	alice.want(200, "", "GET", "/api/leaderboard", nil, &board)
	if len(board.Entries) != 0 {
		t.Fatalf("a rejected score reached the leaderboard: %+v", board.Entries)
	}

	// After all that the honest submission still works.
	alice.want(200, "", "POST", "/api/scores", submitRequest{game.Ticket, rec}, nil)
}

func TestBestScoreIsKept(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	c.register("alice")
	best := 0
	for i := 0; i < 3; i++ {
		var game gameView
		c.want(201, "", "POST", "/api/games", nil, &game)
		rec, g := playOut(game.Seed)
		e.clock.advance(gameTime(g))
		var res submitView
		c.want(200, "", "POST", "/api/scores", submitRequest{game.Ticket, rec}, &res)
		if res.PersonalBest != (g.Score > best) {
			t.Fatalf("game %d: personalBest = %v with score %d over %d", i, res.PersonalBest, g.Score, best)
		}
		best = max(best, g.Score)
		if res.Best != best {
			t.Fatalf("game %d: best = %d, want %d", i, res.Best, best)
		}
	}
	var me userView
	c.want(200, "", "GET", "/api/me", nil, &me)
	if me.Games != 3 || me.Best != best {
		t.Fatalf("me = %+v", me)
	}
}

func signWith(t *testing.T, method jwt.SigningMethod, key any, c claims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, c).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestForgedTokensAreRejected(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	me := c.register("alice")
	good := c.token

	valid := func() claims {
		now := e.clock.now()
		return claims{Name: "alice", RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: me.User.ID, Audience: jwt.ClaimStrings{audAccess},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}}
	}
	// The baseline: these claims with the right key are accepted.
	c.token = signWith(t, jwt.SigningMethodHS256, testSecret, valid())
	c.want(200, "", "GET", "/api/me", nil, nil)

	forged := map[string]string{
		"unsigned (alg none)": signWith(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, valid()),
		"wrong key":           signWith(t, jwt.SigningMethodHS256, []byte("another key, also 32 bytes long!"), valid()),
		"other algorithm":     signWith(t, jwt.SigningMethodHS512, testSecret, valid()),
		"tampered signature":  good[:len(good)-2] + "AA",
		"not a token":         "hello",
	}
	wrongAud := valid()
	wrongAud.Audience = jwt.ClaimStrings{audRefresh}
	forged["refresh token as access"] = signWith(t, jwt.SigningMethodHS256, testSecret, wrongAud)
	wrongIss := valid()
	wrongIss.Issuer = "someone-else"
	forged["wrong issuer"] = signWith(t, jwt.SigningMethodHS256, testSecret, wrongIss)
	noExp := valid()
	noExp.ExpiresAt = nil
	forged["no expiry"] = signWith(t, jwt.SigningMethodHS256, testSecret, noExp)
	expired := valid()
	expired.ExpiresAt = jwt.NewNumericDate(e.clock.now().Add(-time.Minute))
	forged["expired"] = signWith(t, jwt.SigningMethodHS256, testSecret, expired)
	stranger := valid()
	stranger.Subject = "deadbeef"
	forged["unknown user"] = signWith(t, jwt.SigningMethodHS256, testSecret, stranger)

	for name, token := range forged {
		c.token = token
		if status, _ := c.do("GET", "/api/me", nil, nil); status != 401 {
			t.Errorf("%s: got %d, want 401", name, status)
		}
	}
}

func TestSessionLifecycle(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	c.register("alice")

	// The access token dies after 15 minutes.
	e.clock.advance(accessTTL + time.Minute)
	c.want(401, "bad_token", "GET", "/api/me", nil, nil)

	// The refresh cookie buys a new one, without the password.
	var s session
	c.want(200, "", "POST", "/api/auth/refresh", nil, &s)
	c.token = s.Token
	c.want(200, "", "GET", "/api/me", nil, nil)

	// A browser without the cookie gets nothing.
	e.client().want(401, "no_session", "POST", "/api/auth/refresh", nil, nil)

	// Logging out revokes the access token too, although it has not expired.
	c.want(204, "", "POST", "/api/auth/logout", nil, nil)
	c.want(401, "bad_token", "GET", "/api/me", nil, nil)
	c.want(401, "no_session", "POST", "/api/auth/refresh", nil, nil)

	// The refresh token dies after a week.
	c.want(200, "", "POST", "/api/auth/login", credentials{"alice", "correct horse"}, &s)
	e.clock.advance(refreshTTL + time.Minute)
	c.want(401, "no_session", "POST", "/api/auth/refresh", nil, nil)
}

func TestRefreshCookieIsHardened(t *testing.T) {
	e := newEnv(t)
	body, _ := json.Marshal(credentials{"alice", "correct horse"})
	res, err := http.Post(e.url+"/api/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	ck := cookies[0]
	if !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode || ck.Path != "/api/auth" {
		t.Fatalf("weak refresh cookie: %+v", ck)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	limited := 0
	for i := 0; i < 15; i++ {
		if status, _ := c.do("POST", "/api/auth/login", credentials{"alice", "guess " + strconv.Itoa(i)}, nil); status == 429 {
			limited++
		}
	}
	if limited != 5 {
		t.Fatalf("%d of 15 attempts were limited, want 5", limited)
	}
	e.clock.advance(time.Minute)
	c.want(401, "bad_credentials", "POST", "/api/auth/login", credentials{"alice", "one more"}, nil)
}

func TestRequestLimits(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	c.register("alice")
	c.want(413, "too_large", "POST", "/api/scores", submitRequest{"t", make([]byte, 2<<20)}, nil)

	req, _ := http.NewRequest("POST", e.url+"/api/auth/login", strings.NewReader(`{"name":"alice","password":"correct horse"}`))
	req.Header.Set("Content-Type", "text/plain") // what a cross-site form can send
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 415 {
		t.Fatalf("text/plain login: got %d, want 415", res.StatusCode)
	}
	c.want(400, "bad_limit", "GET", "/api/leaderboard?limit=0", nil, nil)
	c.want(404, "not_found", "GET", "/api/nope", nil, nil)
}

func TestStaticFiles(t *testing.T) {
	e := newEnv(t)
	req, _ := http.NewRequest("GET", e.url+"/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := http.DefaultTransport.RoundTrip(req) // no transparent gunzip
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Encoding") != "gzip" || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("headers: %v", res.Header)
	}
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "'wasm-unsafe-eval'") || strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("CSP = %q", csp)
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(zr)
	if !strings.Contains(string(page), "space invaders") {
		t.Fatalf("page = %q", page)
	}

	plain, err := http.Get(e.url + "/missing.js")
	if err != nil {
		t.Fatal(err)
	}
	plain.Body.Close()
	if plain.StatusCode != 404 {
		t.Fatalf("missing file: got %d", plain.StatusCode)
	}
}

func TestStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.CreateUser("id1", "alice", []byte("hash"), now); err != nil {
		t.Fatal(err)
	}
	exp := now.Add(time.Hour)
	if _, err := s.RecordRun("id1", "ticket1", exp, Run{Score: 500, Wave: 2, At: now}); err != nil {
		t.Fatal(err)
	}

	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := s.UserByName("ALICE")
	if !ok || u.Best == nil || u.Best.Score != 500 || u.Games != 1 {
		t.Fatalf("after reopen: %+v", u)
	}
	if _, err := s.RecordRun("id1", "ticket1", exp, Run{Score: 900, At: now}); err != ErrTicketUsed {
		t.Fatalf("a spent ticket was accepted after a restart: %v", err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("store file mode is %v, want 0600", st.Mode().Perm())
	}
}

func TestSecretMustBeLong(t *testing.T) {
	store, _ := OpenStore("")
	if _, err := New(Config{Secret: []byte("short"), Store: store}); err == nil {
		t.Fatal("a 5-byte secret was accepted")
	}
}
