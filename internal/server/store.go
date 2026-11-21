package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNameTaken  = errors.New("name is taken")
	ErrTicketUsed = errors.New("game ticket was already used")
	ErrNoUser     = errors.New("no such user")
)

// Run is a verified game. Every field comes from the server's own playback
// of the recording, never from the client.
type Run struct {
	Score    int       `json:"score"`
	Wave     int       `json:"wave"`
	Ticks    int       `json:"ticks"`
	Shots    int       `json:"shots"`
	Hits     int       `json:"hits"`
	Kills    int       `json:"kills"`
	MaxCombo int       `json:"maxCombo"`
	Seed     string    `json:"seed"`
	Replay   []byte    `json:"replay"`
	At       time.Time `json:"at"`
}

// User is an account. Hash is a bcrypt hash; the password is never stored.
type User struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Hash     []byte    `json:"hash"`
	TokenVer int       `json:"tokenVer"`
	Created  time.Time `json:"created"`
	Games    int       `json:"games"`
	Best     *Run      `json:"best,omitempty"`
}

// Store keeps accounts and best runs in memory and mirrors them to a JSON
// file. It is the only code that would change to move to a real database.
type Store struct {
	mu     sync.RWMutex
	path   string
	users  map[string]*User     // by ID
	byName map[string]*User     // by lower-case name
	used   map[string]time.Time // spent game tickets: jti -> expiry
}

type storeFile struct {
	Users []*User              `json:"users"`
	Used  map[string]time.Time `json:"usedTickets"`
}

// OpenStore loads the store from path. An empty path keeps everything in
// memory only.
func OpenStore(path string) (*Store, error) {
	s := &Store{
		path:   path,
		users:  map[string]*User{},
		byName: map[string]*User{},
		used:   map[string]time.Time{},
	}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	for _, u := range f.Users {
		s.users[u.ID] = u
		s.byName[nameKey(u.Name)] = u
	}
	if f.Used != nil {
		s.used = f.Used
	}
	return s, nil
}

func nameKey(name string) string { return strings.ToLower(name) }

// save writes the file atomically: a crash leaves the old file or the new
// one, never half of each. The caller holds the write lock.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	f := storeFile{Used: s.used}
	for _, u := range s.users {
		f.Users = append(f.Users, u)
	}
	sort.Slice(f.Users, func(i, j int) bool { return f.Users[i].ID < f.Users[j].ID })
	data, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".store-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

func (u *User) clone() User {
	c := *u
	if u.Best != nil {
		b := *u.Best
		c.Best = &b
	}
	return c
}

// CreateUser adds an account. Names are unique ignoring case.
func (s *Store) CreateUser(id, name string, hash []byte, now time.Time) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := nameKey(name)
	if _, ok := s.byName[key]; ok {
		return User{}, ErrNameTaken
	}
	u := &User{ID: id, Name: name, Hash: hash, Created: now}
	s.users[id], s.byName[key] = u, u
	if err := s.save(); err != nil {
		delete(s.users, id)
		delete(s.byName, key)
		return User{}, err
	}
	return u.clone(), nil
}

func (s *Store) UserByName(name string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byName[nameKey(name)]
	if !ok {
		return User{}, false
	}
	return u.clone(), true
}

func (s *Store) UserByID(id string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, false
	}
	return u.clone(), true
}

// RevokeTokens makes every token issued to the user so far invalid.
func (s *Store) RevokeTokens(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return ErrNoUser
	}
	u.TokenVer++
	return s.save()
}

// RecordRun spends the game ticket jti and stores the run if it beats the
// user's best. It reports whether it did.
func (s *Store) RecordRun(userID, jti string, ticketExp time.Time, run Run) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return false, ErrNoUser
	}
	if _, spent := s.used[jti]; spent {
		return false, ErrTicketUsed
	}
	for id, exp := range s.used {
		if exp.Before(run.At) {
			delete(s.used, id) // an expired ticket is rejected by its signature check
		}
	}
	s.used[jti] = ticketExp
	u.Games++
	best := u.Best == nil || run.Score > u.Best.Score
	if best {
		u.Best = &run
	}
	return best, s.save()
}

// Entry is one row of the leaderboard.
type Entry struct {
	Rank     int       `json:"rank"`
	UserID   string    `json:"userId"`
	Name     string    `json:"name"`
	Score    int       `json:"score"`
	Wave     int       `json:"wave"`
	Accuracy int       `json:"accuracy"` // percent
	MaxCombo int       `json:"maxCombo"`
	Seconds  int       `json:"seconds"`
	At       time.Time `json:"at"`
}

// Top returns the best run of each user, highest score first. An earlier run
// wins a tie.
func (s *Store) Top(n int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, 0, len(s.users))
	for _, u := range s.users {
		if u.Best == nil {
			continue
		}
		b := u.Best
		e := Entry{UserID: u.ID, Name: u.Name, Score: b.Score, Wave: b.Wave,
			MaxCombo: b.MaxCombo, Seconds: b.Ticks / 60, At: b.At}
		if b.Shots > 0 {
			e.Accuracy = min(100, b.Hits*100/b.Shots)
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if !a.At.Equal(b.At) {
			return a.At.Before(b.At)
		}
		return a.UserID < b.UserID
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	if n < len(out) {
		out = out[:n]
	}
	return out
}

// Rank is the leaderboard position of a user, or 0 without a run.
func (s *Store) Rank(userID string) int {
	for _, e := range s.Top(1 << 30) {
		if e.UserID == userID {
			return e.Rank
		}
	}
	return 0
}
