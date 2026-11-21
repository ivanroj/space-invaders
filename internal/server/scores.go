package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"spaceinvaders/internal/replay"
	"spaceinvaders/internal/sim"
)

// The browser is not trusted with the score. The flow is:
//
//  1. POST /api/games: the server picks the seed and returns it inside a
//     signed ticket bound to the user.
//  2. The player plays; the Wasm engine records one input byte per tick.
//  3. POST /api/scores: the browser sends the ticket and the recording. The
//     server replays the inputs with the same engine and takes the score
//     from its own run.
//
// What this does not stop is a program that plays well. It proves a score
// was reached by legal inputs, not that a human pressed the keys.

type gameView struct {
	Ticket string `json:"ticket"`
	Seed   string `json:"seed"` // hex: a uint64 does not fit a JS number
	Engine int    `json:"engine"`
}

func (s *Server) handleNewGame(w http.ResponseWriter, r *http.Request, u User) {
	seed := randomHex(8)
	ticket, err := s.sign(audGame, ticketTTL, claims{
		Seed: seed, Ver: u.TokenVer,
		RegisteredClaims: jwt.RegisteredClaims{Subject: u.ID},
	})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	reply(w, http.StatusCreated, gameView{Ticket: ticket, Seed: seed, Engine: sim.Version})
}

type submitRequest struct {
	Ticket string `json:"ticket"`
	Replay []byte `json:"replay"` // base64 in JSON
}

type submitView struct {
	Score        int  `json:"score"`
	Wave         int  `json:"wave"`
	PersonalBest bool `json:"personalBest"`
	Best         int  `json:"best"`
	Rank         int  `json:"rank"`
}

// A game cannot contain more ticks than the time since its ticket was
// issued allows. The engine treats a display frame as one tick on screens
// that are close to 60 Hz but not exactly, so a real game can run a few
// percent fast; the slack covers that and clock rounding.
const (
	realTimeSlack   = 3 * time.Second
	realTimePercent = 5
)

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request, u User) {
	var req submitRequest
	if !decode(w, r, 1<<20, &req) {
		return
	}
	t, err := s.parse(audGame, req.Ticket)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_ticket", "the game ticket is invalid or expired")
		return
	}
	if t.Subject != u.ID {
		fail(w, http.StatusForbidden, "ticket_owner", "the game ticket belongs to another player")
		return
	}
	seed, err := strconv.ParseUint(t.Seed, 16, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_ticket", "the game ticket has no seed")
		return
	}

	// Playback costs CPU, so only a few run at once.
	select {
	case s.verify <- struct{}{}:
		defer func() { <-s.verify }()
	case <-r.Context().Done():
		return
	}
	res, err := replay.Run(seed, req.Replay)
	switch {
	case errors.Is(err, replay.ErrVersion):
		fail(w, http.StatusConflict, "engine_version", "the game was updated; reload the page")
		return
	case err != nil:
		fail(w, http.StatusUnprocessableEntity, "bad_replay", err.Error())
		return
	case !res.Over:
		fail(w, http.StatusUnprocessableEntity, "not_finished", "the recording stops before the game is over")
		return
	}
	now := s.now()
	played := time.Duration(res.Ticks) * time.Second / sim.TickRate
	elapsed := now.Sub(t.IssuedAt.Time)
	if played > elapsed+elapsed*realTimePercent/100+realTimeSlack {
		fail(w, http.StatusUnprocessableEntity, "too_fast", "the recording is longer than the time since the game started")
		return
	}

	run := Run{
		Score: res.Score, Wave: res.Wave, Ticks: res.Ticks,
		Shots: res.Stats.Shots, Hits: res.Stats.Hits, Kills: res.Stats.Kills,
		MaxCombo: res.Stats.MaxCombo, Seed: t.Seed, Replay: req.Replay, At: now,
	}
	best, err := s.store.RecordRun(u.ID, t.ID, t.ExpiresAt.Time, run)
	if errors.Is(err, ErrTicketUsed) {
		fail(w, http.StatusConflict, "ticket_used", "this game was already submitted")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	u, _ = s.store.UserByID(u.ID)
	reply(w, http.StatusOK, submitView{
		Score: res.Score, Wave: res.Wave, PersonalBest: best,
		Best: u.Best.Score, Rank: s.store.Rank(u.ID),
	})
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			fail(w, http.StatusBadRequest, "bad_limit", "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	reply(w, http.StatusOK, map[string]any{"entries": s.store.Top(limit)})
}

type replayView struct {
	Name   string `json:"name"`
	Score  int    `json:"score"`
	Seed   string `json:"seed"`
	Replay []byte `json:"replay"`
}

// handleReplay returns the recording of a user's best run so anyone can
// watch it: the same inputs give the same game in every browser.
func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	u, ok := s.store.UserByID(r.PathValue("id"))
	if !ok || u.Best == nil {
		fail(w, http.StatusNotFound, "no_replay", "this player has no recorded game")
		return
	}
	reply(w, http.StatusOK, replayView{Name: u.Name, Score: u.Best.Score, Seed: u.Best.Seed, Replay: u.Best.Replay})
}
