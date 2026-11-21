package sim

import "testing"

// play runs a bot-driven game for up to maxTicks and returns it.
func play(seed uint64, maxTicks int) *Game {
	g := New(seed)
	for i := 0; i < maxTicks && !g.Over(); i++ {
		g.Step(Bot(g))
	}
	return g
}

func TestDeterministic(t *testing.T) {
	a, b := play(42, 20000), play(42, 20000)
	if a.Hash() != b.Hash() || a.Score != b.Score {
		t.Fatalf("same seed, same inputs, different result: %x/%d vs %x/%d", a.Hash(), a.Score, b.Hash(), b.Score)
	}
	if c := play(43, 20000); c.Hash() == a.Hash() {
		t.Fatal("different seeds gave the same game")
	}
}

func TestBotMakesProgress(t *testing.T) {
	best := 0
	for seed := uint64(1); seed <= 8; seed++ {
		g := play(seed, 60*60*10)
		t.Logf("seed %d: score %d, wave %d, ticks %d, over=%v", seed, g.Score, g.Wave, g.Tick, g.Over())
		best = max(best, g.Wave)
	}
	if best < 2 {
		t.Fatalf("bot never cleared a wave (best wave %d)", best)
	}
}
