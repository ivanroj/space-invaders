// Package replay records the inputs of a game and plays them back.
//
// Because the simulation is deterministic, a seed plus one input byte per
// tick describes a whole game. The browser sends that recording with a score
// and the server re-runs it to check the score is real.
//
// Wire format: the bytes "SIR", one byte with sim.Version, then run-length
// pairs: one input byte followed by a uvarint repeat count. A player holds a
// key for many ticks, so ten minutes of play fit in a few kilobytes.
package replay

import (
	"encoding/binary"
	"errors"

	"spaceinvaders/internal/sim"
)

const magic = "SIR"

// MaxTicks bounds a replay to two hours of play, which also bounds the work
// a client can make the server do.
const MaxTicks = 2 * 60 * 60 * sim.TickRate

var (
	ErrFormat  = errors.New("replay: malformed data")
	ErrVersion = errors.New("replay: recorded with a different engine version")
	ErrTooLong = errors.New("replay: too long")
	ErrPastEnd = errors.New("replay: inputs continue after game over")
)

// Recorder builds a recording one tick at a time.
type Recorder struct {
	buf   []byte
	cur   sim.Input
	run   uint64
	ticks int
}

// Reset starts a new recording, keeping the buffer.
func (r *Recorder) Reset() {
	r.buf = append(r.buf[:0], magic...)
	r.buf = append(r.buf, sim.Version)
	r.cur, r.run, r.ticks = 0, 0, 0
}

// Add records the input of the next tick.
func (r *Recorder) Add(in sim.Input) {
	if len(r.buf) == 0 {
		r.Reset()
	}
	if r.run > 0 && in != r.cur {
		r.flush()
	}
	r.cur = in
	r.run++
	r.ticks++
}

func (r *Recorder) flush() {
	r.buf = append(r.buf, byte(r.cur))
	r.buf = binary.AppendUvarint(r.buf, r.run)
	r.run = 0
}

// Ticks is the number of ticks recorded so far.
func (r *Recorder) Ticks() int { return r.ticks }

// Bytes returns the encoded recording. The recorder can keep recording.
func (r *Recorder) Bytes() []byte {
	if len(r.buf) == 0 {
		r.Reset()
	}
	out := append([]byte(nil), r.buf...)
	if r.run > 0 {
		out = append(out, byte(r.cur))
		out = binary.AppendUvarint(out, r.run)
	}
	return out
}

// Reader decodes a recording one tick at a time.
type Reader struct {
	data  []byte
	cur   sim.Input
	run   uint64
	ticks int
	err   error
}

// NewReader checks the header of a recording and returns a reader for it.
func NewReader(data []byte) (*Reader, error) {
	if len(data) < len(magic)+1 || string(data[:len(magic)]) != magic {
		return nil, ErrFormat
	}
	if data[len(magic)] != sim.Version {
		return nil, ErrVersion
	}
	return &Reader{data: data[len(magic)+1:]}, nil
}

// Next returns the input of the next tick. It returns false at the end of
// the recording or on malformed data; Err tells the two apart.
func (r *Reader) Next() (sim.Input, bool) {
	if r.run == 0 {
		if len(r.data) == 0 || r.err != nil {
			return 0, false
		}
		in := sim.Input(r.data[0])
		run, n := binary.Uvarint(r.data[1:])
		switch {
		case in&^sim.InputMask != 0, n <= 0, run == 0:
			r.err = ErrFormat
			return 0, false
		case run > uint64(MaxTicks-r.ticks):
			r.err = ErrTooLong
			return 0, false
		}
		r.data = r.data[1+n:]
		r.cur, r.run = in, run
	}
	r.run--
	r.ticks++
	return r.cur, true
}

// Err is the error that stopped Next, if any.
func (r *Reader) Err() error { return r.err }

// Result is the outcome of playing a recording back.
type Result struct {
	Score int
	Wave  int
	Ticks int
	Over  bool
	Stats sim.Stats
}

// Run plays a recording back on a fresh game with the given seed.
func Run(seed uint64, data []byte) (Result, error) {
	r, err := NewReader(data)
	if err != nil {
		return Result{}, err
	}
	g := sim.New(seed)
	for {
		in, ok := r.Next()
		if !ok {
			break
		}
		if g.Over() {
			return Result{}, ErrPastEnd
		}
		g.Step(in)
	}
	if r.Err() != nil {
		return Result{}, r.Err()
	}
	return Result{Score: g.Score, Wave: g.Wave, Ticks: r.ticks, Over: g.Over(), Stats: g.Stats}, nil
}
