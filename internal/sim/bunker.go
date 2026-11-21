package sim

// Bunker is a destructible shield stored as a bitmap: bit x of Rows[y] is set
// while that pixel is solid.
type Bunker struct {
	Rows [BunkerH]uint32
}

// Reset rebuilds the bunker: an arch with chamfered corners.
func (b *Bunker) Reset() {
	for y := 0; y < BunkerH; y++ {
		var row uint32
		for x := 0; x < BunkerW; x++ {
			mx := min(x, BunkerW-1-x) // distance to the nearest side
			solid := mx+y >= 4
			if y >= 10 && mx >= 6+max(0, 12-y) {
				solid = false // the notch under the arch
			}
			if solid {
				row |= 1 << x
			}
		}
		b.Rows[y] = row
	}
}

// Solid reports whether the pixel at (x, y), relative to the bunker, is intact.
func (b *Bunker) Solid(x, y int) bool {
	return x >= 0 && x < BunkerW && y >= 0 && y < BunkerH && b.Rows[y]&(1<<x) != 0
}

func (b *Bunker) eraseRect(x, y, w, h int) {
	x0, x1 := max(x, 0), min(x+w, BunkerW)
	if x0 >= x1 {
		return
	}
	mask := uint32(1<<x1-1) &^ uint32(1<<x0-1)
	for yy := max(y, 0); yy < min(y+h, BunkerH); yy++ {
		b.Rows[yy] &^= mask
	}
}

// blast carves a ragged crater around (cx, cy).
func (b *Bunker) blast(cx, cy int, rng *RNG) {
	for dy := -3; dy <= 3; dy++ {
		for dx := -3; dx <= 3; dx++ {
			d := dx*dx + dy*dy
			if d > 10 || (d > 3 && rng.Intn(3) != 0) {
				continue
			}
			x, y := cx+dx, cy+dy
			if x >= 0 && x < BunkerW && y >= 0 && y < BunkerH {
				b.Rows[y] &^= 1 << x
			}
		}
	}
}

// hitBunkers tests the vertical pixel column x, y..y+h-1 against every
// bunker. dir is the direction the bullet travels (-1 up, 1 down) and decides
// which end of the column meets the bunker first.
func (g *Game) hitBunkers(x, y, h, dir int) bool {
	if y+h <= BunkerY || y >= BunkerY+BunkerH {
		return false
	}
	for i := range g.Bunkers {
		lx := x - BunkerX[i]
		if lx < 0 || lx >= BunkerW {
			continue
		}
		b := &g.Bunkers[i]
		for k := 0; k < h; k++ {
			ly := y - BunkerY + k
			if dir < 0 {
				ly = y - BunkerY + h - 1 - k
			}
			if b.Solid(lx, ly) {
				b.blast(lx, ly, &g.rng)
				g.emit(EvBunkerHit, x, ly+BunkerY, 0, 0)
				return true
			}
		}
		return false
	}
	return false
}
