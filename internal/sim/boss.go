package sim

// Boss is the mothership fought on every fifth wave.
type Boss struct {
	Active   bool
	X, Y     int32
	HP       int
	MaxHP    int
	Level    int
	Dir      int32
	Cooldown int
	Pattern  uint8
	PatLeft  int
	PatTimer int
	Flash    int
}

const (
	bossFan uint8 = iota
	bossAimed
	bossRain
	numBossPatterns
)

func (b *Boss) spawn(level int) {
	*b = Boss{
		Active:   true,
		X:        (ScreenW - BossW) / 2 * FP,
		Y:        -BossH * FP,
		HP:       24 + 16*level,
		Level:    level,
		Dir:      1,
		Cooldown: 50,
	}
	b.MaxHP = b.HP
}

// Enraged reports whether the boss is in its faster final third.
func (b *Boss) Enraged() bool { return b.HP*3 <= b.MaxHP }

// speed is the horizontal speed in sub-pixels per tick.
func (b *Boss) speed() int32 {
	v := int32(130 + 25*min(b.Level, 6))
	if b.Enraged() {
		v = v * 8 / 5
	}
	return v
}

func (b *Boss) hitBy(x, y, w, h int) bool {
	bx, by := int(b.X>>8), int(b.Y>>8)
	// The hull is narrower at the top; two boxes approximate the sprite.
	return overlap(x, y, w, h, bx+8, by, BossW-16, BossH/2) ||
		overlap(x, y, w, h, bx, by+BossH/2, BossW, BossH/2-2)
}

func (g *Game) damageBoss(px, py int) {
	b := &g.Boss
	b.HP--
	b.Flash = 4
	g.addScore(10 * g.Mult())
	if g.ComboTimer > 0 {
		g.ComboTimer = ComboWindow
	}
	if b.HP > 0 {
		g.emit(EvBossHit, px, py, b.HP, b.MaxHP)
		return
	}
	cx, cy := int(b.X>>8)+BossW/2, int(b.Y>>8)+BossH/2
	b.Active = false
	pts := 2000 * b.Level * g.registerKill()
	g.addScore(pts)
	g.emit(EvBossKilled, cx, cy, pts, b.Level)
	g.dropPowerup(cx-12, cy, g.rollPowerup())
	g.dropPowerup(cx+12, cy, g.rollPowerup())
	g.waveCleared()
}

func (g *Game) updateBoss() {
	b := &g.Boss
	if b.Flash > 0 {
		b.Flash--
	}
	b.X += b.Dir * b.speed()
	if b.X < 8*FP {
		b.X, b.Dir = 8*FP, 1
	} else if b.X > (ScreenW-8-BossW)*FP {
		b.X, b.Dir = (ScreenW-8-BossW)*FP, -1
	}

	if b.PatLeft > 0 {
		b.PatTimer--
		if b.PatTimer <= 0 {
			g.bossShot()
		}
		return
	}
	b.Cooldown--
	if b.Cooldown > 0 {
		return
	}
	b.Cooldown = max(80-8*b.Level, 30)
	if b.Enraged() {
		b.Cooldown = b.Cooldown * 2 / 3
	}
	b.Pattern = uint8(g.rng.Intn(int(numBossPatterns)))
	switch b.Pattern {
	case bossFan:
		b.PatLeft = 1
	case bossAimed:
		b.PatLeft = 3
		if b.Enraged() {
			b.PatLeft = 5
		}
	case bossRain:
		b.PatLeft = 8
		if b.Enraged() {
			b.PatLeft = 12
		}
	}
	b.PatTimer = 1
}

func (g *Game) bossShot() {
	b := &g.Boss
	b.PatLeft--
	ox := b.X + (BossW/2-EBulletW/2)*FP
	oy := b.Y + BossH*FP

	switch b.Pattern {
	case bossFan:
		n := int32(5)
		if b.Enraged() {
			n = 7
		}
		for i := int32(0); i < n; i++ {
			g.spawnEnemyBullet(ox, oy, (i-n/2)*110, 400, 1)
		}
	case bossAimed:
		// Aim at the player with integer maths: scale (dx, dy) to the
		// bullet speed using an integer square root.
		const speed = 520
		dx := int64(g.Player.X+(PlayerW/2)*FP) - int64(ox)
		dy := int64(PlayerY*FP) - int64(oy)
		l := max(isqrt(dx*dx+dy*dy), 1)
		g.spawnEnemyBullet(ox, oy, int32(dx*speed/l), int32(dy*speed/l), 2)
		b.PatTimer = 9
	case bossRain:
		x := b.X + int32(g.rng.Intn(BossW-EBulletW))*FP
		g.spawnEnemyBullet(x, oy, 0, 480, 0)
		b.PatTimer = 6
	}
	g.emit(EvBossShoot, int(ox>>8), int(oy>>8), int(b.Pattern), 0)
}
