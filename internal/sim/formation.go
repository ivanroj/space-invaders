package sim

// Formation is the grid of invaders. It moves in discrete steps like the
// arcade original: the fewer invaders remain, the shorter the pause between
// steps.
type Formation struct {
	Alive     [Rows]uint16 // bit c of row r is set while that invader lives
	Count     int
	X, Y      int // top-left of the grid, pixels
	Dir       int
	StepTimer int
	Steps     int // steps taken; the low bit selects the animation frame
	FireTimer int
}

func (f *Formation) spawn(wave int, rng *RNG) {
	for r := range f.Alive {
		f.Alive[r] = 1<<Cols - 1
	}
	f.Count = Rows * Cols
	f.X = (ScreenW - Cols*CellW) / 2
	f.Y = 34 + min(wave-1, 5)*8
	f.Dir = 1
	f.StepTimer = 30
	f.FireTimer = 60 + rng.Intn(30)
}

// IsAlive reports whether the invader at row r, column c is alive.
func (f *Formation) IsAlive(r, c int) bool { return f.Alive[r]&(1<<c) != 0 }

// Rect returns the hit box of the invader at row r, column c.
func (f *Formation) Rect(r, c int) (x, y, w, h int) {
	w = InvaderWidth[r]
	return f.X + c*CellW + (CellW-w)/2, f.Y + r*CellH, w, InvaderH
}

func (f *Formation) hit(bx, by, bw, bh int) (r, c int, ok bool) {
	for r = Rows - 1; r >= 0; r-- {
		if f.Alive[r] == 0 {
			continue
		}
		for c = 0; c < Cols; c++ {
			if !f.IsAlive(r, c) {
				continue
			}
			x, y, w, h := f.Rect(r, c)
			if overlap(bx, by, bw, bh, x, y, w, h) {
				return r, c, true
			}
		}
	}
	return 0, 0, false
}

// columns returns the bit set of columns that still have an invader.
func (f *Formation) columns() uint16 {
	var m uint16
	for _, row := range f.Alive {
		m |= row
	}
	return m
}

// lowestRow returns the bottom-most live row in column c, or -1.
func (f *Formation) lowestRow(c int) int {
	for r := Rows - 1; r >= 0; r-- {
		if f.IsAlive(r, c) {
			return r
		}
	}
	return -1
}

func (g *Game) stepInterval() int {
	return max(2+g.Inv.Count*26/(Rows*Cols)-min(g.Wave-1, 10), 2)
}

func (g *Game) stepFormation() {
	f := &g.Inv
	if f.Count == 0 {
		return
	}
	f.StepTimer--
	if f.StepTimer > 0 {
		return
	}
	f.StepTimer = g.stepInterval()

	cols := f.columns()
	minC, maxC := 0, Cols-1
	for cols&(1<<minC) == 0 {
		minC++
	}
	for cols&(1<<maxC) == 0 {
		maxC--
	}
	left := f.X + minC*CellW + 2
	right := f.X + maxC*CellW + CellW - 2

	if (f.Dir > 0 && right+StepPx > ScreenW-EdgePad) || (f.Dir < 0 && left-StepPx < EdgePad) {
		f.Y += DropPx
		f.Dir = -f.Dir
	} else {
		f.X += f.Dir * StepPx
	}
	f.Steps++
	g.emit(EvStep, 0, 0, f.Steps&3, f.Count)

	bottom := 0
	for r := Rows - 1; r >= 0; r-- {
		if f.Alive[r] != 0 {
			bottom = f.Y + r*CellH + InvaderH
			break
		}
	}
	if bottom > BunkerY {
		g.crushBunkers()
	}
	if bottom >= PlayerY {
		// The invaders have landed: the game ends whatever lives remain.
		g.Lives = 0
		g.killPlayer(int(g.Player.X>>8) + PlayerW/2)
	}
}

func (g *Game) crushBunkers() {
	f := &g.Inv
	for r := 0; r < Rows; r++ {
		for c := 0; c < Cols; c++ {
			if !f.IsAlive(r, c) {
				continue
			}
			x, y, w, h := f.Rect(r, c)
			for i := range g.Bunkers {
				g.Bunkers[i].eraseRect(x-BunkerX[i], y-BunkerY, w, h)
			}
		}
	}
}

func (g *Game) fireInterval() int {
	return max(52-4*min(g.Wave, 8), 14) + g.rng.Intn(24)
}

func (g *Game) maxFormationBullets() int {
	return min(2+(g.Wave+1)/2, 7)
}

func (g *Game) invaderFire() {
	f := &g.Inv
	f.FireTimer--
	if f.FireTimer > 0 {
		return
	}
	f.FireTimer = g.fireInterval()
	if f.Count == 0 || g.activeEnemyBullets() >= g.maxFormationBullets() {
		return
	}

	cols := f.columns()
	var live [Cols]int
	n := 0
	for c := 0; c < Cols; c++ {
		if cols&(1<<c) != 0 {
			live[n] = c
			n++
		}
	}
	col := live[g.rng.Intn(n)]
	if g.rng.Intn(3) == 0 {
		// One shot in three comes from the column right above the player.
		pc := int(g.Player.X>>8) + PlayerW/2
		best := 1 << 30
		for _, c := range live[:n] {
			d := f.X + c*CellW + CellW/2 - pc
			if d < 0 {
				d = -d
			}
			if d < best {
				best, col = d, c
			}
		}
	}
	row := f.lowestRow(col)
	kind := uint8(g.rng.Intn(3))
	vy := int32(300 + 20*min(g.Wave-1, 10))
	if kind == 2 {
		vy = vy * 5 / 4
	}
	x := f.X + col*CellW + CellW/2 - EBulletW/2
	y := f.Y + row*CellH + InvaderH
	if g.spawnEnemyBullet(int32(x)*FP, int32(y)*FP, 0, vy, kind) {
		g.emit(EvEnemyShoot, x, y, int(kind), 0)
	}
}
