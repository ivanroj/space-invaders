package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"spaceinvaders/internal/replay"
	"spaceinvaders/internal/sim"
)

// The server signs three kinds of JWT with the same key. The audience claim
// tells them apart, so a token of one kind is never accepted as another.
const (
	issuer = "space-invaders"

	audAccess  = "api"     // sent in the Authorization header, short-lived
	audRefresh = "refresh" // kept in an HttpOnly cookie, buys new access tokens
	audGame    = "game"    // a ticket for one game; carries the seed

	accessTTL  = 15 * time.Minute
	refreshTTL = 7 * 24 * time.Hour
	ticketTTL  = replay.MaxTicks/sim.TickRate*time.Second + 10*time.Minute

	refreshCookie = "si_refresh"
	clockLeeway   = 5 * time.Second
)

type claims struct {
	Name string `json:"name,omitempty"`
	Ver  int    `json:"ver"`
	Seed string `json:"seed,omitempty"`
	jwt.RegisteredClaims
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the OS random source is gone; nothing is safe to sign
	}
	return hex.EncodeToString(b)
}

func (s *Server) sign(aud string, ttl time.Duration, c claims) (string, error) {
	now := s.now()
	c.RegisteredClaims.Issuer = issuer
	c.RegisteredClaims.Audience = jwt.ClaimStrings{aud}
	c.RegisteredClaims.IssuedAt = jwt.NewNumericDate(now)
	c.RegisteredClaims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	c.RegisteredClaims.ID = randomHex(12)
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
}

// parse verifies the signature and the registered claims. Pinning the
// algorithm matters: without it a token with "alg":"none" has no signature
// to check.
func (s *Server) parse(aud, token string) (*claims, error) {
	var c claims
	_, err := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(aud),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockLeeway),
		jwt.WithTimeFunc(s.now),
	).ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return s.secret, nil })
	if err != nil {
		return nil, err
	}
	if c.Subject == "" || c.IssuedAt == nil {
		return nil, errors.New("token has no subject or issue time")
	}
	return &c, nil
}

// userFor returns the account a token belongs to, if the token has not been
// revoked.
func (s *Server) userFor(c *claims) (User, bool) {
	u, ok := s.store.UserByID(c.Subject)
	return u, ok && u.TokenVer == c.Ver
}

type handlerWithUser func(http.ResponseWriter, *http.Request, User)

// withUser guards a handler with an access token.
func (s *Server) withUser(next handlerWithUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			fail(w, http.StatusUnauthorized, "no_token", "an access token is required")
			return
		}
		c, err := s.parse(audAccess, token)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			fail(w, http.StatusUnauthorized, "bad_token", "the access token is invalid or expired")
			return
		}
		u, ok := s.userFor(c)
		if !ok {
			fail(w, http.StatusUnauthorized, "bad_token", "the access token was revoked")
			return
		}
		next(w, r, u)
	}
}

const (
	minName, maxName = 3, 16
	// bcrypt ignores everything after 72 bytes, so longer passwords are
	// refused instead of silently truncated.
	minPassword, maxPassword = 8, 72
)

func validName(name string) bool {
	if n := utf8.RuneCountInString(name); n < minName || n > maxName {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

type credentials struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type session struct {
	Token     string   `json:"token"`
	ExpiresIn int      `json:"expiresIn"` // seconds
	User      userView `json:"user"`
}

type userView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Games int    `json:"games"`
	Best  int    `json:"best"`
	Rank  int    `json:"rank"`
}

func (s *Server) viewOf(u User) userView {
	v := userView{ID: u.ID, Name: u.Name, Games: u.Games}
	if u.Best != nil {
		v.Best = u.Best.Score
		v.Rank = s.store.Rank(u.ID)
	}
	return v
}

// openSession answers with an access token and sets the refresh cookie.
func (s *Server) openSession(w http.ResponseWriter, r *http.Request, status int, u User) {
	access, err := s.sign(audAccess, accessTTL, claims{
		Name: u.Name, Ver: u.TokenVer,
		RegisteredClaims: jwt.RegisteredClaims{Subject: u.ID},
	})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	refresh, err := s.sign(audRefresh, refreshTTL, claims{
		Ver:              u.TokenVer,
		RegisteredClaims: jwt.RegisteredClaims{Subject: u.ID},
	})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.setRefreshCookie(w, r, refresh, int(refreshTTL/time.Second))
	reply(w, status, session{Token: access, ExpiresIn: int(accessTTL / time.Second), User: s.viewOf(u)})
}

// The refresh token lives in a cookie JavaScript cannot read (HttpOnly), is
// sent only to the auth endpoints (Path) and never on cross-site requests
// (SameSite), so neither an XSS bug nor a foreign page can use it directly.
func (s *Server) setRefreshCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookie,
		Value:    value,
		Path:     "/api/auth",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !decode(w, r, 4<<10, &req) {
		return
	}
	if !validName(req.Name) {
		fail(w, http.StatusBadRequest, "bad_name", "name must be 3-16 letters, digits, '_' or '-'")
		return
	}
	if len(req.Password) < minPassword || len(req.Password) > maxPassword {
		fail(w, http.StatusBadRequest, "bad_password", "password must be 8-72 bytes long")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.bcryptCost)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	u, err := s.store.CreateUser(randomHex(8), req.Name, hash, s.now())
	if errors.Is(err, ErrNameTaken) {
		fail(w, http.StatusConflict, "name_taken", "this name is already taken")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.openSession(w, r, http.StatusCreated, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !decode(w, r, 4<<10, &req) {
		return
	}
	u, found := s.store.UserByName(req.Name)
	hash := u.Hash
	if !found {
		// Check a password anyway, so the response time does not reveal
		// whether the name exists.
		hash = s.dummyHash
	}
	err := bcrypt.CompareHashAndPassword(hash, []byte(req.Password))
	if !found || err != nil {
		fail(w, http.StatusUnauthorized, "bad_credentials", "wrong name or password")
		return
	}
	s.openSession(w, r, http.StatusOK, u)
}

// refreshUser returns the account named by a valid refresh cookie.
func (s *Server) refreshUser(r *http.Request) (User, bool) {
	ck, err := r.Cookie(refreshCookie)
	if err != nil {
		return User{}, false
	}
	c, err := s.parse(audRefresh, ck.Value)
	if err != nil {
		return User{}, false
	}
	return s.userFor(c)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	u, ok := s.refreshUser(r)
	if !ok {
		fail(w, http.StatusUnauthorized, "no_session", "sign in again")
		return
	}
	s.openSession(w, r, http.StatusOK, u)
}

// handleLogout ends every session of the user: a JWT cannot be deleted, so
// the account's token version is raised and older tokens stop matching it.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.refreshUser(r); ok {
		if err := s.store.RevokeTokens(u.ID); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	s.setRefreshCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, u User) {
	reply(w, http.StatusOK, s.viewOf(u))
}
