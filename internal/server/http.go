// Package server is the HTTP side of the game: static files, accounts,
// JWT sessions and a leaderboard that only accepts scores it can reproduce.
package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"spaceinvaders/internal/sim"
)

// Config holds what New needs. Secret and Store are required.
type Config struct {
	Secret     []byte // HMAC key for JWTs, at least 32 bytes
	Store      *Store
	WebDir     string           // directory with index.html and main.wasm; empty serves the API only
	BcryptCost int              // 0 means bcrypt.DefaultCost
	Now        func() time.Time // nil means time.Now
	Logger     *slog.Logger     // nil discards logs
}

type Server struct {
	secret     []byte
	store      *Store
	bcryptCost int
	dummyHash  []byte
	now        func() time.Time
	log        *slog.Logger
	verify     chan struct{} // limits concurrent replay checks
	authLimit  *limiter
	handler    http.Handler
}

func New(cfg Config) (*Server, error) {
	if len(cfg.Secret) < 32 {
		return nil, errors.New("server: the JWT secret must be at least 32 bytes")
	}
	if cfg.Store == nil {
		return nil, errors.New("server: no store")
	}
	s := &Server{
		secret:     cfg.Secret,
		store:      cfg.Store,
		bcryptCost: cfg.BcryptCost,
		now:        cfg.Now,
		log:        cfg.Logger,
		verify:     make(chan struct{}, 4),
	}
	if s.bcryptCost == 0 {
		s.bcryptCost = bcrypt.DefaultCost
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	var err error
	if s.dummyHash, err = bcrypt.GenerateFromPassword([]byte(randomHex(16)), s.bcryptCost); err != nil {
		return nil, err
	}
	// Ten attempts at once, then one every six seconds per address: enough
	// for a person, too slow to guess passwords.
	s.authLimit = newLimiter(10, 6*time.Second, s.now)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, map[string]any{"ok": true, "engine": sim.Version})
	})
	mux.HandleFunc("POST /api/auth/register", s.limited(s.handleRegister))
	mux.HandleFunc("POST /api/auth/login", s.limited(s.handleLogin))
	mux.HandleFunc("POST /api/auth/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.withUser(s.handleMe))
	mux.HandleFunc("POST /api/games", s.withUser(s.handleNewGame))
	mux.HandleFunc("POST /api/scores", s.withUser(s.handleSubmit))
	mux.HandleFunc("GET /api/leaderboard", s.handleLeaderboard)
	mux.HandleFunc("GET /api/replays/{id}", s.handleReplay)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		fail(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	if cfg.WebDir != "" {
		mux.Handle("/", newStatic(cfg.WebDir))
	}
	s.handler = s.middleware(mux)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// contentSecurityPolicy lets the page load only its own files.
// 'wasm-unsafe-eval' is what permits compiling WebAssembly; it does not
// allow JavaScript eval.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; " +
	"style-src 'self'; img-src 'self' data:; connect-src 'self'; " +
	"object-src 'none'; base-uri 'none'; form-action 'self'"

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "path", r.URL.Path, "value", v)
				if sw.status == http.StatusOK {
					fail(sw, http.StatusInternalServerError, "internal", "internal error")
				}
			}
			s.log.Info("request", "method", r.Method, "path", r.URL.Path,
				"status", sw.status, "ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(sw, r)
	})
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	reply(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("internal error", "path", r.URL.Path, "err", err)
	fail(w, http.StatusInternalServerError, "internal", "internal error")
}

// decode reads a JSON body of at most limit bytes into v. On failure it has
// already written the response.
func decode(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "bad_content_type", "send application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "too_large", "the request body is too large")
		} else {
			fail(w, http.StatusBadRequest, "bad_json", "the request body is not valid JSON for this endpoint")
		}
		return false
	}
	return true
}

// limiter is a token bucket per client address.
type limiter struct {
	mu      sync.Mutex
	burst   float64
	every   time.Duration
	now     func() time.Time
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	seen   time.Time
}

func newLimiter(burst int, every time.Duration, now func() time.Time) *limiter {
	return &limiter{burst: float64(burst), every: every, now: now, buckets: map[string]*bucket{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= 10000 {
			full := time.Duration(l.burst) * l.every
			for k, old := range l.buckets {
				if now.Sub(old.seen) > full {
					delete(l.buckets, k) // refilled by now, same as a new one
				}
			}
		}
		b = &bucket{tokens: l.burst}
		l.buckets[key] = b
	} else {
		b.tokens = min(l.burst, b.tokens+float64(now.Sub(b.seen))/float64(l.every))
	}
	b.seen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (s *Server) limited(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !s.authLimit.allow(host) {
			w.Header().Set("Retry-After", "6")
			fail(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; wait a few seconds")
			return
		}
		next(w, r)
	}
}

// static serves the web directory. Text files and main.wasm compress to
// about a third of their size, so they are gzipped once and kept in memory.
type static struct {
	dir   string
	files http.Handler
	mu    sync.Mutex
	cache map[string]*packed
}

type packed struct {
	mod  time.Time
	size int64
	data []byte
}

func newStatic(dir string) *static {
	return &static{dir: dir, files: http.FileServer(http.Dir(dir)), cache: map[string]*packed{}}
}

var compressible = map[string]bool{".wasm": true, ".js": true, ".css": true, ".html": true, ".svg": true, ".json": true}

func (s *static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Always revalidate: a stale wasm.js next to a new main.wasm is the
	// classic way to break a Wasm app after a rebuild.
	w.Header().Set("Cache-Control", "no-cache")

	name := path.Clean("/" + r.URL.Path)
	if name == "/" {
		name = "/index.html"
	}
	ext := path.Ext(name)
	if !compressible[ext] || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		s.files.ServeHTTP(w, r)
		return
	}
	p, err := s.pack(name)
	if err != nil {
		s.files.ServeHTTP(w, r) // let the file server produce the 404
		return
	}
	h := w.Header()
	h.Set("Content-Type", mime.TypeByExtension(ext))
	h.Set("Content-Encoding", "gzip")
	h.Set("Vary", "Accept-Encoding")
	http.ServeContent(w, r, "", p.mod, bytes.NewReader(p.data))
}

func (s *static) pack(name string) (*packed, error) {
	full := filepath.Join(s.dir, filepath.FromSlash(name))
	st, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, os.ErrNotExist
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.cache[name]; ok && p.mod.Equal(st.ModTime()) && p.size == st.Size() {
		return p, nil
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(raw)
	zw.Close()
	p := &packed{mod: st.ModTime(), size: st.Size(), data: buf.Bytes()}
	s.cache[name] = p
	return p, nil
}
