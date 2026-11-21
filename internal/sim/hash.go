package sim

// Hash folds the whole simulation state into 64 bits (FNV-1a). Tests compare
// it between runs and between platforms to prove the simulation is
// deterministic.
func (g *Game) Hash() uint64 {
	h := uint64(14695981039346656037)
	add := func(v int64) {
		for i := 0; i < 8; i++ {
			h ^= uint64(v>>(8*i)) & 0xff
			h *= 1099511628211
		}
	}
	bullets := func(bs []Bullet) {
		for i := range bs {
			b := &bs[i]
			if b.Active {
				add(int64(i))
				add(int64(b.X))
				add(int64(b.Y))
				add(int64(b.VX))
				add(int64(b.VY))
				add(int64(b.Kind))
			}
		}
	}

	add(int64(g.rng.s))
	add(int64(g.Tick))
	add(int64(g.Phase))
	add(int64(g.PhaseTimer))
	add(int64(g.Score))
	add(int64(g.Lives))
	add(int64(g.Wave))
	add(int64(g.NextLifeAt))
	add(int64(g.Combo))
	add(int64(g.ComboTimer))
	add(int64(g.Slow))

	p := &g.Player
	for _, v := range []int{int(p.X), p.Cooldown, p.Invuln, p.Rapid, p.Triple, p.Shield} {
		add(int64(v))
	}

	f := &g.Inv
	for _, row := range f.Alive {
		add(int64(row))
	}
	for _, v := range []int{f.Count, f.X, f.Y, f.Dir, f.StepTimer, f.Steps, f.FireTimer} {
		add(int64(v))
	}

	b := &g.Boss
	if b.Active {
		for _, v := range []int{int(b.X), int(b.Y), b.HP, int(b.Dir), b.Cooldown, int(b.Pattern), b.PatLeft, b.PatTimer} {
			add(int64(v))
		}
	}
	if g.UFO.Active {
		add(int64(g.UFO.X))
		add(int64(g.UFO.Dir))
	}
	add(int64(g.UFO.Timer))

	bullets(g.PBullets[:])
	bullets(g.EBullets[:])
	for i := range g.Bunkers {
		for _, row := range g.Bunkers[i].Rows {
			add(int64(row))
		}
	}
	for i := range g.Powerups {
		if pw := &g.Powerups[i]; pw.Active {
			add(int64(i))
			add(int64(pw.X))
			add(int64(pw.Y))
			add(int64(pw.Kind))
		}
	}
	return h
}
