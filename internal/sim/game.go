package sim

// Bullet is a projectile. X and Y are the top-left corner in sub-pixels.
type Bullet struct {
	Active bool
	X, Y   int32
	VX, VY int32
	Kind   uint8
}

// Player is the ship. X is the left edge in sub-pixels.
type Player struct {
	X        int32
	Cooldown int
	Invuln   int
	Rapid    int
	Triple   int
	Shield   int
}

// Powerup is a falling bonus capsule.
type Powerup struct {
	Active bool
	X, Y   int32
	Kind   PowerupKind
}

// UFO is the mystery ship that crosses the top of the screen.
type UFO struct {
	Active bool
	X      int32
	Dir    int32
	Timer  int
}

// Stats are shown on the game over screen.
type Stats struct {
	Shots    int
	Hits     int
	Kills    int
	MaxCombo int
}

// Game is the whole simulation state. Fixed-size arrays are used for objects
// so that a tick allocates nothing.
type Game struct {
	rng RNG

	Tick       uint32
	Phase      Phase
	PhaseTimer int
	Score      int
	Lives      int
	Wave       int
	NextLifeAt int
	Combo      int
	ComboTimer int
	Slow       int

	Player   Player
	Inv      Formation
	Boss     Boss
	UFO      UFO
	PBullets [MaxPlayerBullets]Bullet
	EBullets [MaxEnemyBullets]Bullet
	Bunkers  [NumBunkers]Bunker
	Powerups [MaxPowerups]Powerup
	Stats    Stats

	// Events lists what happened during the last Step. It is reused.
	Events []Event
}

// New starts a game. Two games created with the same seed and fed the same
// inputs are identical on every platform.
func New(seed uint64) *Game {
	g := &Game{
		rng:        RNG{s: seed},
		Lives:      StartLives,
		NextLifeAt: LifeEvery,
		Events:     make([]Event, 0, 64),
	}
	g.Player.X = (ScreenW - PlayerW) / 2 * FP
	g.startWave(1)
	return g
}

// Over reports whether the game has ended.
func (g *Game) Over() bool { return g.Phase == PhaseGameOver }

// IsBossWave reports whether the current wave is a boss fight.
func (g *Game) IsBossWave() bool { return g.Wave%BossEvery == 0 }

// Mult is the current score multiplier, 1..MaxMult.
func (g *Game) Mult() int {
	if g.Combo <= 1 {
		return 1
	}
	return min(1+(g.Combo-1)/4, MaxMult)
}

func (g *Game) emit(k EventKind, x, y int, a, b int) {
	g.Events = append(g.Events, Event{Kind: k, X: int16(x), Y: int16(y), A: int32(a), B: int32(b)})
}

func (g *Game) startWave(n int) {
	g.Wave = n
	g.PBullets = [MaxPlayerBullets]Bullet{}
	g.EBullets = [MaxEnemyBullets]Bullet{}
	g.Powerups = [MaxPowerups]Powerup{}
	g.UFO = UFO{Timer: 900 + g.rng.Intn(900)}
	g.Boss = Boss{}
	g.Inv = Formation{}
	for i := range g.Bunkers {
		g.Bunkers[i].Reset()
	}
	g.Phase = PhaseIntro
	if g.IsBossWave() {
		g.Boss.spawn(n / BossEvery)
		g.PhaseTimer = 150
	} else {
		g.Inv.spawn(n, &g.rng)
		g.PhaseTimer = 120
	}
	g.emit(EvWaveStart, ScreenW/2, ScreenH/2, n, 0)
}

// Step advances the simulation by one tick (1/60 s).
func (g *Game) Step(in Input) {
	g.Events = g.Events[:0]
	if g.Phase == PhaseGameOver {
		return
	}
	g.Tick++

	switch g.Phase {
	case PhaseIntro:
		g.movePlayer(in)
		if g.Boss.Active && g.Boss.Y < BossRestY*FP {
			g.Boss.Y += 100
		}
		g.PhaseTimer--
		if g.PhaseTimer <= 0 {
			if g.Boss.Active {
				g.Boss.Y = BossRestY * FP
			}
			g.Phase = PhasePlaying
		}

	case PhasePlaying:
		g.tickTimers()
		g.movePlayer(in)
		g.fire(in)
		g.updatePlayerBullets()
		if g.Phase == PhasePlaying && (g.Slow == 0 || g.Tick&1 == 0) {
			g.updateEnemies()
		}
		g.updatePowerups()

	case PhaseDying:
		g.PhaseTimer--
		if g.PhaseTimer <= 0 {
			if g.Lives <= 0 {
				g.Phase = PhaseGameOver
				g.emit(EvGameOver, ScreenW/2, ScreenH/2, g.Score, 0)
				return
			}
			g.EBullets = [MaxEnemyBullets]Bullet{}
			g.Player = Player{X: (ScreenW - PlayerW) / 2 * FP, Invuln: 120}
			g.Phase = PhasePlaying
		}

	case PhaseClear:
		g.tickTimers()
		g.movePlayer(in)
		g.updatePlayerBullets()
		g.updatePowerups()
		g.PhaseTimer--
		if g.PhaseTimer <= 0 {
			g.startWave(g.Wave + 1)
		}
	}
}

func (g *Game) tickTimers() {
	p := &g.Player
	dec := func(v *int) {
		if *v > 0 {
			*v--
		}
	}
	dec(&p.Cooldown)
	dec(&p.Invuln)
	dec(&p.Rapid)
	dec(&p.Triple)
	dec(&p.Shield)
	dec(&g.Slow)
	if g.ComboTimer > 0 {
		g.ComboTimer--
		if g.ComboTimer == 0 {
			g.Combo = 0
		}
	}
}

func (g *Game) movePlayer(in Input) {
	p := &g.Player
	if in&InLeft != 0 {
		p.X -= PlayerSpeed
	}
	if in&InRight != 0 {
		p.X += PlayerSpeed
	}
	p.X = min(max(p.X, EdgePad*FP), (ScreenW-EdgePad-PlayerW)*FP)
}

func (g *Game) activePlayerBullets() int {
	n := 0
	for i := range g.PBullets {
		if g.PBullets[i].Active {
			n++
		}
	}
	return n
}

func (g *Game) fire(in Input) {
	p := &g.Player
	if in&InFire == 0 || p.Cooldown > 0 {
		return
	}
	volleys, cooldown, perVolley := 2, 16, 1
	if p.Rapid > 0 {
		volleys, cooldown = 6, 7
	}
	if p.Triple > 0 {
		perVolley = 3
	}
	if g.activePlayerBullets() > (volleys-1)*perVolley {
		return
	}
	x := p.X + (PlayerW/2)*FP
	y := int32((PlayerY - PBulletH) * FP)
	g.spawnPlayerBullet(x, y, 0)
	if p.Triple > 0 {
		g.spawnPlayerBullet(x-3*FP, y+2*FP, -150)
		g.spawnPlayerBullet(x+3*FP, y+2*FP, 150)
	}
	p.Cooldown = cooldown
	g.Stats.Shots++
	g.emit(EvShoot, int(x>>8), PlayerY, perVolley, 0)
}

func (g *Game) spawnPlayerBullet(x, y, vx int32) {
	for i := range g.PBullets {
		if !g.PBullets[i].Active {
			g.PBullets[i] = Bullet{Active: true, X: x, Y: y, VX: vx, VY: -PBulletSpeed}
			return
		}
	}
}

func (g *Game) spawnEnemyBullet(x, y, vx, vy int32, kind uint8) bool {
	for i := range g.EBullets {
		if !g.EBullets[i].Active {
			g.EBullets[i] = Bullet{Active: true, X: x, Y: y, VX: vx, VY: vy, Kind: kind}
			return true
		}
	}
	return false
}

func (g *Game) activeEnemyBullets() int {
	n := 0
	for i := range g.EBullets {
		if g.EBullets[i].Active {
			n++
		}
	}
	return n
}

func overlap(ax, ay, aw, ah, bx, by, bw, bh int) bool {
	return ax < bx+bw && bx < ax+aw && ay < by+bh && by < ay+ah
}

func (g *Game) addScore(n int) {
	g.Score += n
	for g.Score >= g.NextLifeAt {
		g.NextLifeAt += LifeEvery
		if g.Lives < MaxLives {
			g.Lives++
			g.emit(EvExtraLife, int(g.Player.X>>8)+PlayerW/2, PlayerY, g.Lives, 0)
		}
	}
}

// registerKill extends the combo chain and returns the multiplier to apply.
func (g *Game) registerKill() int {
	g.Combo++
	g.ComboTimer = ComboWindow
	g.Stats.Kills++
	g.Stats.MaxCombo = max(g.Stats.MaxCombo, g.Combo)
	return g.Mult()
}

func (g *Game) updatePlayerBullets() {
	phase := g.Phase
	for i := range g.PBullets {
		b := &g.PBullets[i]
		if !b.Active {
			continue
		}
		b.X += b.VX
		b.Y += b.VY
		px, py := int(b.X>>8), int(b.Y>>8)
		if py+PBulletH <= PlayTop || px < 0 || px >= ScreenW {
			b.Active = false
			continue
		}
		if g.playerBulletHit(px, py) {
			b.Active = false
			g.Stats.Hits++
			if g.Phase != phase {
				return // the wave was cleared; the remaining bullets are gone
			}
		}
	}
}

// playerBulletHit resolves one player bullet against everything it can hit.
func (g *Game) playerBulletHit(px, py int) bool {
	if g.hitBunkers(px, py, PBulletH, -1) {
		return true
	}
	// A one-pixel bullet is unforgiving; test a three-pixel column instead.
	bx, bw := px-1, PBulletW+2

	for i := range g.EBullets {
		e := &g.EBullets[i]
		if e.Active && overlap(bx, py, bw, PBulletH, int(e.X>>8), int(e.Y>>8), EBulletW, EBulletH) {
			e.Active = false
			g.emit(EvBulletClash, px, py, 0, 0)
			return true
		}
	}

	if r, c, ok := g.Inv.hit(bx, py, bw, PBulletH); ok {
		g.killInvader(r, c)
		return true
	}

	if u := &g.UFO; u.Active && overlap(bx, py, bw, PBulletH, int(u.X>>8), UFOY, UFOW, UFOH) {
		u.Active = false
		u.Timer = 1200 + g.rng.Intn(900)
		pts := ufoScores[g.Stats.Shots%len(ufoScores)] * g.registerKill()
		g.addScore(pts)
		cx := int(u.X>>8) + UFOW/2
		g.emit(EvUFOKilled, cx, UFOY+UFOH/2, pts, 0)
		g.dropPowerup(cx, UFOY+UFOH, g.rollPowerup())
		return true
	}

	if g.Boss.Active && g.Boss.hitBy(bx, py, bw, PBulletH) {
		g.damageBoss(px, py)
		return true
	}
	return false
}

func (g *Game) killInvader(r, c int) {
	f := &g.Inv
	f.Alive[r] &^= 1 << c
	f.Count--
	mult := g.registerKill()
	pts := InvaderPoints[r] * mult
	g.addScore(pts)
	cx, cy := f.X+c*CellW+CellW/2, f.Y+r*CellH+InvaderH/2
	g.emit(EvInvaderKilled, cx, cy, r, pts)
	if g.rng.Intn(100) < 7 {
		g.dropPowerup(cx, cy, g.rollPowerup())
	}
	if f.Count == 0 {
		g.waveCleared()
	}
}

func (g *Game) waveCleared() {
	g.EBullets = [MaxEnemyBullets]Bullet{}
	if g.UFO.Active {
		g.UFO.Active = false
		g.emit(EvUFOLeft, 0, 0, 0, 0)
	}
	g.Phase = PhaseClear
	g.PhaseTimer = 110
	g.emit(EvWaveClear, ScreenW/2, ScreenH/2, g.Wave, 0)
}

func (g *Game) rollPowerup() PowerupKind {
	switch r := g.rng.Intn(100); {
	case r < 30:
		return PowRapid
	case r < 60:
		return PowTriple
	case r < 78:
		return PowShield
	case r < 93:
		return PowSlow
	default:
		return PowLife
	}
}

func (g *Game) dropPowerup(cx, cy int, kind PowerupKind) {
	for i := range g.Powerups {
		if !g.Powerups[i].Active {
			g.Powerups[i] = Powerup{
				Active: true,
				X:      int32(cx-PowerupW/2) * FP,
				Y:      int32(cy) * FP,
				Kind:   kind,
			}
			return
		}
	}
}

func (g *Game) updatePowerups() {
	px := int(g.Player.X >> 8)
	for i := range g.Powerups {
		p := &g.Powerups[i]
		if !p.Active {
			continue
		}
		p.Y += 200
		x, y := int(p.X>>8), int(p.Y>>8)
		if y > GroundY {
			p.Active = false
			continue
		}
		if !overlap(x, y, PowerupW, PowerupH, px, PlayerY, PlayerW, PlayerH) {
			continue
		}
		p.Active = false
		switch p.Kind {
		case PowRapid:
			g.Player.Rapid = RapidTicks
		case PowTriple:
			g.Player.Triple = TripleTicks
		case PowShield:
			g.Player.Shield = ShieldTicks
		case PowSlow:
			g.Slow = SlowTicks
		case PowLife:
			if g.Lives < MaxLives {
				g.Lives++
			} else {
				g.addScore(500)
			}
		}
		g.emit(EvPowerupTaken, x+PowerupW/2, y, int(p.Kind), 0)
	}
}

func (g *Game) updateEnemies() {
	if g.Boss.Active {
		g.updateBoss()
	} else {
		g.stepFormation()
		if g.Phase != PhasePlaying {
			return
		}
		g.invaderFire()
		g.updateUFO()
	}
	g.updateEnemyBullets()
}

func (g *Game) updateUFO() {
	u := &g.UFO
	if !u.Active {
		u.Timer--
		if u.Timer > 0 || g.Inv.Count < 8 {
			return
		}
		u.Active = true
		if g.rng.Intn(2) == 0 {
			u.Dir, u.X = 1, -UFOW*FP
		} else {
			u.Dir, u.X = -1, ScreenW*FP
		}
		g.emit(EvUFOSpawn, 0, UFOY, 0, 0)
		return
	}
	u.X += u.Dir * 230
	if x := int(u.X >> 8); x < -UFOW || x > ScreenW {
		u.Active = false
		u.Timer = 1200 + g.rng.Intn(900)
		g.emit(EvUFOLeft, 0, 0, 0, 0)
	}
}

func (g *Game) updateEnemyBullets() {
	px := int(g.Player.X >> 8)
	for i := range g.EBullets {
		b := &g.EBullets[i]
		if !b.Active {
			continue
		}
		b.X += b.VX
		b.Y += b.VY
		x, y := int(b.X>>8), int(b.Y>>8)
		if y+EBulletH >= GroundY || x < -EBulletW || x >= ScreenW {
			b.Active = false
			continue
		}
		if g.hitBunkers(x+EBulletW/2, y, EBulletH, 1) {
			b.Active = false
			continue
		}
		// The player's hit box is one pixel smaller on each side than the
		// sprite: near misses should feel like misses.
		if overlap(x, y, EBulletW, EBulletH, px+1, PlayerY+1, PlayerW-2, PlayerH-1) {
			b.Active = false
			g.hitPlayer()
			if g.Phase != PhasePlaying {
				return
			}
		}
	}
}

func (g *Game) hitPlayer() {
	p := &g.Player
	cx := int(p.X>>8) + PlayerW/2
	if p.Invuln > 0 {
		return
	}
	if p.Shield > 0 {
		p.Shield = 0
		p.Invuln = 45
		g.emit(EvShieldBreak, cx, PlayerY+PlayerH/2, 0, 0)
		return
	}
	g.Lives--
	g.killPlayer(cx)
}

func (g *Game) killPlayer(cx int) {
	g.Combo, g.ComboTimer = 0, 0
	g.Phase = PhaseDying
	g.PhaseTimer = 80
	g.emit(EvPlayerHit, cx, PlayerY+PlayerH/2, g.Lives, 0)
}
