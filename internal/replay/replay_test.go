package replay

import (
	"errors"
	"testing"

	"spaceinvaders/internal/sim"
)

// record plays a bot game and returns the recording and the final game.
func record(seed uint64, maxTicks int) ([]byte, *sim.Game) {
	g := sim.New(seed)
	var rec Recorder
	rec.Reset()
	for i := 0; i < maxTicks && !g.Over(); i++ {
		in := sim.Bot(g)
		rec.Add(in)
		g.Step(in)
	}
	return rec.Bytes(), g
}

func TestRoundTrip(t *testing.T) {
	for seed := uint64(1); seed <= 5; seed++ {
		data, g := record(seed, 40000)
		res, err := Run(seed, data)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if res.Score != g.Score || res.Wave != g.Wave || res.Ticks != int(g.Tick) || res.Over != g.Over() {
			t.Fatalf("seed %d: replay %+v, live score=%d wave=%d tick=%d", seed, res, g.Score, g.Wave, g.Tick)
		}
		t.Logf("seed %d: %d ticks in %d bytes, score %d", seed, res.Ticks, len(data), res.Score)
	}
}

func TestWrongSeedGivesDifferentScore(t *testing.T) {
	data, g := record(7, 20000)
	res, err := Run(8, data)
	if err == nil && res.Score == g.Score {
		t.Fatal("a recording replayed with another seed reproduced the score")
	}
}

func TestRejectsBadInput(t *testing.T) {
	good, _ := record(1, 500)
	cases := map[string]struct {
		data []byte
		want error
	}{
		"empty":       {nil, ErrFormat},
		"bad magic":   {[]byte("XXX\x01\x00\x05"), ErrFormat},
		"bad version": {[]byte{'S', 'I', 'R', sim.Version + 1, 0, 5}, ErrVersion},
		"bad input":   {[]byte{'S', 'I', 'R', sim.Version, 0x80, 5}, ErrFormat},
		"zero run":    {[]byte{'S', 'I', 'R', sim.Version, 0, 0}, ErrFormat},
		"truncated":   {good[:len(good)-1], nil},
		"too long":    {[]byte{'S', 'I', 'R', sim.Version, 0, 0xff, 0xff, 0xff, 0x7f}, ErrTooLong},
	}
	for name, c := range cases {
		_, err := Run(1, c.data)
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}

func TestRejectsInputAfterGameOver(t *testing.T) {
	// Standing still gets the player killed or invaded eventually.
	g := sim.New(1)
	var rec Recorder
	rec.Reset()
	for !g.Over() {
		rec.Add(0)
		g.Step(0)
	}
	rec.Add(sim.InFire)
	if _, err := Run(1, rec.Bytes()); !errors.Is(err, ErrPastEnd) {
		t.Fatalf("got %v, want ErrPastEnd", err)
	}
}

func FuzzRun(f *testing.F) {
	good, _ := record(1, 300)
	f.Add(good)
	f.Add([]byte("SIR"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must never panic or run away, whatever the bytes are. Cap the
		// work so the fuzzer stays fast.
		if len(data) > 64 {
			return
		}
		Run(1, data)
	})
}
