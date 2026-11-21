package render

import (
	"math"

	"spaceinvaders/internal/sim"
)

// Draw renders one frame of g into r.Pix.
func (r *Renderer) Draw(g *sim.Game, v View) {
	copy(r.Pix, r.bg)
	r.drawStars()

	r.rect(0, sim.GroundY+r.oy, W, 1, colGround)
	r.drawBunkers(g)
	if g.Boss.Active {
		r.drawBoss(g)
	} else {
		r.drawFormation(g)
	}
	r.drawUFO(g)
	r.drawPowerups(g)
	r.drawBullets(g)
	r.drawPlayer(g)

	for _, p := range r.pops {
		r.sprite(popSprite, p.x+r.ox, p.y+r.oy, [3]uint32{p.col})
	}
	r.drawParticles()
	r.drawPopups()

	if g.Slow > 0 {
		// Slow motion tints the whole screen cold.
		for i := 2; i < len(r.Pix); i += 4 {
			r.Pix[i] = sat(int(r.Pix[i]) + 22)
		}
	}
	if r.flash > 0.03 {
		a := int(r.flash * 200)
		for i := 0; i < len(r.Pix); i += 4 {
			r.Pix[i] = sat(int(r.Pix[i]) + a)
			r.Pix[i+1] = sat(int(r.Pix[i+1]) + a)
			r.Pix[i+2] = sat(int(r.Pix[i+2]) + a)
		}
	}

	r.drawHUD(g, v)
	if !v.Attract {
		r.drawBanner(g)
	}
	if v.Paused {
		r.dim()
		r.textC("PAUSED", W/2, 104, colWhite, 2)
	}
}

func (r *Renderer) drawBunkers(g *sim.Game) {
	for i := range g.Bunkers {
		b := &g.Bunkers[i]
		for y := 0; y < sim.BunkerH; y++ {
			row := b.Rows[y]
			if row == 0 {
				continue
			}
			// Lighter at the top, as if lit from above.
			col := scale(colBunker, 256-y*7)
			for x := 0; x < sim.BunkerW; x++ {
				if row&(1<<x) != 0 {
					r.set(sim.BunkerX[i]+x+r.ox, sim.BunkerY+y+r.oy, col)
				}
			}
		}
	}
}

func (r *Renderer) drawFormation(g *sim.Game) {
	f := &g.Inv
	frame := f.Steps & 1
	for row := 0; row < sim.Rows; row++ {
		for c := 0; c < sim.Cols; c++ {
			if !f.IsAlive(row, c) {
				continue
			}
			x, y, _, _ := f.Rect(row, c)
			if g.Phase == sim.PhaseIntro {
				// Invaders drop in one after another, bottom row first.
				t := 120 - g.PhaseTimer - (sim.Rows-1-row)*10 - c*2
				if t < 0 {
					continue
				}
				if t < 20 {
					y -= (20 - t) * (20 - t) / 3
				}
			}
			r.sprite(invaderSprites[row][frame], x+r.ox, y+r.oy, [3]uint32{rowColour[row], 0x10081c})
		}
	}
}

func (r *Renderer) drawBoss(g *sim.Game) {
	b := &g.Boss
	x, y := int(b.X>>8)+r.ox, int(b.Y>>8)+r.oy
	pal := [3]uint32{colBoss, 0xff3b6b, 0x4a2a6a}
	if r.frame/8&1 == 0 {
		pal[2] = colGold
	}
	if b.Enraged() {
		pal[0] = 0xff5a7a
		x += r.frame & 1
	}
	if b.Flash > 0 {
		pal = [3]uint32{colWhite, colWhite, colWhite}
	}
	r.sprite(bossSprite, x, y, pal)

	if g.Phase == sim.PhaseIntro {
		return
	}
	const barW = 120
	bx := (W - barW) / 2
	r.rect(bx-1, 13, barW+2, 4, 0x3a2148)
	fill := barW * b.HP / b.MaxHP
	col := uint32(colAlert)
	if !b.Enraged() {
		col = colBoss
	}
	r.rect(bx, 14, fill, 2, col)
}

func (r *Renderer) drawUFO(g *sim.Game) {
	if !g.UFO.Active {
		return
	}
	lights := uint32(colGold)
	if r.frame/5&1 == 0 {
		lights = 0x7a1020
	}
	r.sprite(ufoSprite, int(g.UFO.X>>8)+r.ox, sim.UFOY+r.oy, [3]uint32{colUFO, lights})
}

func (r *Renderer) drawPowerups(g *sim.Game) {
	for i := range g.Powerups {
		p := &g.Powerups[i]
		if !p.Active {
			continue
		}
		x, y := int(p.X>>8)+r.ox, int(p.Y>>8)+r.oy
		col := powerColour[p.Kind]
		pulse := 200 + int(56*math.Sin(float64(r.frame)*0.25))
		r.sprite(capsule, x, y, [3]uint32{scale(col, pulse)})
		r.small(powerGlyph[p.Kind], x+4, y+2, colWhite)
	}
}

var zigzag = [4]int{0, 1, 2, 1}

func (r *Renderer) drawBullets(g *sim.Game) {
	if g.Phase == sim.PhaseDying {
		return
	}
	for i := range g.PBullets {
		b := &g.PBullets[i]
		if !b.Active {
			continue
		}
		x, y := int(b.X>>8)+r.ox, int(b.Y>>8)+r.oy
		r.rect(x, y, 1, 2, colWhite)
		r.rect(x, y+2, 1, sim.PBulletH-2, colPlayer)
		for k := 0; k < 6; k++ {
			r.add(x, y+sim.PBulletH+k, colPlayer, 150-k*25)
		}
	}
	for i := range g.EBullets {
		b := &g.EBullets[i]
		if !b.Active {
			continue
		}
		x, y := int(b.X>>8)+r.ox, int(b.Y>>8)+r.oy
		phase := r.frame / 3
		switch b.Kind {
		case 0: // plasma bolt with a rolling cross-bar
			r.rect(x+1, y, 1, sim.EBulletH, colEnemyB)
			bar := y + phase%sim.EBulletH
			r.set(x, bar, colEnemyB)
			r.set(x+2, bar, colEnemyB)
		case 1: // squiggle
			for k := 0; k < sim.EBulletH; k++ {
				r.set(x+zigzag[(k+phase)&3], y+k, colWhite)
			}
		default: // fast dart
			r.rect(x+1, y, 1, sim.EBulletH, colFastB)
			r.rect(x, y+4, 3, 1, colFastB)
			r.set(x+1, y+sim.EBulletH-1, colWhite)
		}
	}
}

func (r *Renderer) drawPlayer(g *sim.Game) {
	if g.Phase == sim.PhaseDying || g.Phase == sim.PhaseGameOver {
		return
	}
	p := &g.Player
	x, y := int(p.X>>8)+r.ox, sim.PlayerY+r.oy
	if p.Invuln > 0 && p.Invuln/4&1 == 1 {
		return // blink while invulnerable
	}
	hull := uint32(colPlayer)
	if p.Rapid > 0 && r.frame/4&1 == 0 {
		hull = powerColour[sim.PowRapid]
	}
	r.sprite(playerSprite, x, y, [3]uint32{hull, colGlass})

	// Engine flames flicker under the two nozzles.
	for _, nx := range [2]int{2, 10} {
		n := 1 + int(r.rnd()*3)
		for k := 0; k < n; k++ {
			r.add(x+nx, y+sim.PlayerH+k, colFlame, 255-k*70)
		}
	}

	if p.Shield > 0 && (p.Shield > 120 || r.frame/6&1 == 0) {
		cx, cy := float64(x)+sim.PlayerW/2, float64(y)+4
		col := powerColour[sim.PowShield]
		for k := 0; k < 40; k++ {
			a := float64(k) / 40 * 2 * math.Pi
			glow := 150 + int(100*math.Sin(a*3+float64(r.frame)*0.2))
			r.add(int(cx+11*math.Cos(a)+0.5), int(cy+8*math.Sin(a)+0.5), col, glow)
		}
	}
}

func (r *Renderer) drawHUD(g *sim.Game, v View) {
	r.text("SCORE", 6, 4, colDimHUD, 1)
	r.text(itoa(g.Score, 6), 42, 4, colHUD, 1)

	hi := max(v.HiScore, g.Score)
	r.text("HI", 133, 4, colDimHUD, 1)
	hiCol := uint32(colHUD)
	if g.Score > 0 && g.Score >= v.HiScore {
		hiCol = colGold
	}
	r.text(itoa(hi, 6), 151, 4, hiCol, 1)

	r.text("WAVE", 267, 4, colDimHUD, 1)
	r.text(itoa(g.Wave, 2), 297, 4, colHUD, 1)

	for i := 0; i < g.Lives; i++ {
		r.sprite(playerSprite, 6+i*16, 228, [3]uint32{scale(colPlayer, 190), colGlass})
	}

	if m := g.Mult(); m > 1 {
		r.textC("x"+itoa(m, 1), W/2, 227, colGold, 1)
		r.rect(W/2-10, 236, 20*g.ComboTimer/sim.ComboWindow, 1, colGold)
	}

	// Active bonuses, right to left, each with a timer bar.
	x := W - 6
	bonus := func(kind sim.PowerupKind, left, total int) {
		if left <= 0 {
			return
		}
		x -= 16
		col := powerColour[kind]
		r.small(powerGlyph[kind], x+7, 227, col)
		r.rect(x, 234, 14, 1, scale(col, 70))
		r.rect(x, 234, 1+13*left/total, 1, col)
		x -= 4
	}
	bonus(sim.PowRapid, g.Player.Rapid, sim.RapidTicks)
	bonus(sim.PowTriple, g.Player.Triple, sim.TripleTicks)
	bonus(sim.PowShield, g.Player.Shield, sim.ShieldTicks)
	bonus(sim.PowSlow, g.Slow, sim.SlowTicks)

	if v.Label != "" && r.frame/40&1 == 0 {
		r.textC(v.Label, W/2, 16, colDimHUD, 1)
	}
}

func (r *Renderer) drawBanner(g *sim.Game) {
	switch g.Phase {
	case sim.PhaseIntro:
		if g.PhaseTimer < 25 {
			return
		}
		if g.IsBossWave() {
			if r.frame/10&1 == 0 {
				r.textC("WARNING", W/2, 96, colAlert, 3)
			}
			r.textC("MOTHERSHIP APPROACHING", W/2, 126, colHUD, 1)
			return
		}
		r.textC("WAVE "+itoa(g.Wave, 1), W/2, 146, colWhite, 2)
		if r.frame/12&1 == 0 {
			r.textC("GET READY", W/2, 165, colDimHUD, 1)
		}
	case sim.PhaseClear:
		if g.IsBossWave() {
			r.textC("MOTHERSHIP DESTROYED", W/2, 100, colGold, 2)
		} else {
			r.textC("WAVE CLEAR", W/2, 100, colGold, 2)
		}
	case sim.PhaseGameOver:
		r.textC("GAME OVER", W/2, 88, colAlert, 3)
	}
}
