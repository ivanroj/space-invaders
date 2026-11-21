package sim

// Bot chooses an input for the current tick. It plays the attract mode behind
// the menu and generates long, realistic input streams for tests. It only
// reads the state, so it is as deterministic as the simulation.
func Bot(g *Game) Input {
	pc := int(g.Player.X>>8) + PlayerW/2
	target := botTarget(g, pc)

	want := Input(0)
	if target < pc-1 {
		want = InLeft
	} else if target > pc+1 {
		want = InRight
	}

	// Try the wanted move first, then the alternatives, and take the one
	// that stays alive longest.
	choices := [3]Input{want, 0, InLeft}
	switch want {
	case 0:
		choices = [3]Input{0, InLeft, InRight}
	case InLeft:
		choices = [3]Input{InLeft, 0, InRight}
	case InRight:
		choices = [3]Input{InRight, 0, InLeft}
	}
	best, bestT := want, -1
	for _, c := range choices {
		if t := botSurvival(g, c); t > bestT {
			best, bestT = c, t
		}
	}
	return best | InFire
}

const botLookahead = 48

// botSurvival predicts for how many ticks the ship survives if it holds move
// for a while and then stops. It returns botLookahead when nothing hits.
func botSurvival(g *Game, move Input) int {
	const hold = 14
	x := int(g.Player.X)
	for t := 1; t <= botLookahead; t++ {
		if t <= hold {
			if move == InLeft {
				x -= PlayerSpeed
			} else if move == InRight {
				x += PlayerSpeed
			}
			x = min(max(x, EdgePad*FP), (ScreenW-EdgePad-PlayerW)*FP)
		}
		px := x >> 8
		for i := range g.EBullets {
			b := &g.EBullets[i]
			if !b.Active {
				continue
			}
			bx := (int(b.X) + int(b.VX)*t) >> 8
			by := (int(b.Y) + int(b.VY)*t) >> 8
			// Two pixels of safety margin around the real hit box.
			if overlap(bx, by, EBulletW, EBulletH+2, px-2, PlayerY-1, PlayerW+4, PlayerH+2) {
				return t
			}
		}
	}
	return botLookahead
}

func botTarget(g *Game, pc int) int {
	for i := range g.Powerups {
		if p := &g.Powerups[i]; p.Active && int(p.Y>>8) > 110 {
			return int(p.X>>8) + PowerupW/2
		}
	}
	if b := &g.Boss; b.Active {
		// Lead the target by the time a bullet needs to climb to the boss.
		const climb = (PlayerY - BossRestY - BossH) * FP / PBulletSpeed
		x := int(b.X+b.Dir*b.speed()*climb)>>8 + BossW/2
		return min(max(x, 8+BossW/2), ScreenW-8-BossW/2)
	}
	if g.UFO.Active {
		// Lead the target: the bullet needs time to climb to the UFO.
		return int(g.UFO.X>>8) + UFOW/2 + int(g.UFO.Dir)*40
	}
	f := &g.Inv
	target, best := pc, 1<<30
	for c := 0; c < Cols; c++ {
		if f.lowestRow(c) < 0 {
			continue
		}
		x := f.X + c*CellW + CellW/2
		d := x - pc
		if d < 0 {
			d = -d
		}
		if d < best {
			best, target = d, x
		}
	}
	return target
}
