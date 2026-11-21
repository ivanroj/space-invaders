package render

import (
	"math"

	"spaceinvaders/internal/sim"
)

const maxParticles = 640

type particle struct {
	x, y, vx, vy float32
	life, total  float32
	gravity      float32
	col          uint32
	big          bool
}

type popup struct {
	text string
	x, y float32
	life int
	col  uint32
	big  bool
}

type pop struct {
	x, y, life int
	col        uint32
}

type star struct {
	x, y, speed, phase float32
	level              int
}

// View is the presentation state that lives outside the simulation.
type View struct {
	HiScore int
	Paused  bool
	Attract bool   // demo mode behind the menu: no banners
	Label   string // blinks under the score line, e.g. "DEMO"
}

// Renderer owns the frame buffer and every cosmetic effect.
type Renderer struct {
	Pix []byte // W*H*4 bytes, RGBA

	bg     []byte
	parts  [maxParticles]particle
	next   int
	popups []popup
	pops   []pop
	stars  []star
	shake  float32
	flash  float32
	ox, oy int
	frame  int
	seed   uint32
}

func New() *Renderer {
	r := &Renderer{
		Pix:  make([]byte, W*H*4),
		bg:   make([]byte, W*H*4),
		seed: 0x2545F491,
	}
	// Background: a faint vertical gradient, darkest at the top.
	for y := 0; y < H; y++ {
		t := y * 256 / H
		for x := 0; x < W; x++ {
			i := (y*W + x) * 4
			r.bg[i] = byte(4 + 7*t>>8)
			r.bg[i+1] = byte(5 + 2*t>>8)
			r.bg[i+2] = byte(14 + 16*t>>8)
			r.bg[i+3] = 255
		}
	}
	for i := 0; i < 90; i++ {
		layer := i % 3
		r.stars = append(r.stars, star{
			x:     r.rnd() * W,
			y:     r.rnd() * H,
			speed: 0.05 + float32(layer)*0.09,
			phase: r.rnd() * 6.28,
			level: 70 + layer*55,
		})
	}
	return r
}

// rnd returns a cosmetic random number in [0, 1). It is separate from the
// simulation's generator so that effects can never change a replay.
func (r *Renderer) rnd() float32 {
	r.seed ^= r.seed << 13
	r.seed ^= r.seed >> 17
	r.seed ^= r.seed << 5
	return float32(r.seed>>8) / (1 << 24)
}

func (r *Renderer) spark(x, y, vx, vy, life, gravity float32, col uint32, big bool) {
	r.parts[r.next] = particle{x: x, y: y, vx: vx, vy: vy, life: life, total: life, gravity: gravity, col: col, big: big}
	r.next = (r.next + 1) % maxParticles
}

func (r *Renderer) burst(x, y, n int, col uint32, speed float32, big bool) {
	for i := 0; i < n; i++ {
		a := float64(r.rnd()) * 2 * math.Pi
		v := speed * (0.25 + r.rnd())
		c := col
		if r.rnd() < 0.25 {
			c = colWhite
		}
		r.spark(float32(x), float32(y), float32(math.Cos(a))*v, float32(math.Sin(a))*v,
			18+r.rnd()*26, 0.02, c, big && r.rnd() < 0.5)
	}
}

func (r *Renderer) ring(x, y, n int, col uint32, speed float32) {
	for i := 0; i < n; i++ {
		a := float64(i) / float64(n) * 2 * math.Pi
		r.spark(float32(x), float32(y), float32(math.Cos(a))*speed, float32(math.Sin(a))*speed, 22, 0, col, false)
	}
}

func (r *Renderer) say(text string, x, y int, col uint32, big bool) {
	r.popups = append(r.popups, popup{text: text, x: float32(x), y: float32(y), life: 50, col: col, big: big})
}

// Handle turns the events of one simulation tick into effects.
func (r *Renderer) Handle(events []sim.Event) {
	for _, e := range events {
		x, y := int(e.X), int(e.Y)
		switch e.Kind {
		case sim.EvShoot:
			for i := 0; i < 3; i++ {
				r.spark(float32(x), float32(y-2), (r.rnd()-0.5)*0.8, -0.4-r.rnd()*0.6, 8, 0, colPlayer, false)
			}
		case sim.EvInvaderKilled:
			col := rowColour[e.A]
			r.burst(x, y, 16, col, 1.5, false)
			r.pops = append(r.pops, pop{x: x - popSprite.W/2, y: y - popSprite.H/2, life: 9, col: col})
			c := uint32(colWhite)
			if int(e.B) > sim.InvaderPoints[e.A] {
				c = colGold
			}
			r.say("+"+itoa(int(e.B), 1), x, y-4, c, false)
		case sim.EvPlayerHit:
			r.burst(x, y, 70, colPlayer, 2.6, true)
			r.burst(x, y, 30, colFlame, 1.6, true)
			r.shake, r.flash = 7, 0.5
		case sim.EvUFOKilled:
			r.burst(x, y, 40, colUFO, 2, true)
			r.say("+"+itoa(int(e.A), 1), x, y-4, colGold, false)
			r.shake = max(r.shake, 2.5)
		case sim.EvPowerupTaken:
			r.ring(x, y, 20, powerColour[e.A], 1.6)
			r.say(powerName[e.A], x, y-14, powerColour[e.A], true)
		case sim.EvBunkerHit:
			r.burst(x, y, 5, colBunker, 0.9, false)
		case sim.EvBulletClash:
			r.burst(x, y, 8, colWhite, 1.2, false)
		case sim.EvBossHit:
			r.burst(x, y, 5, colWhite, 1.3, false)
			r.shake = max(r.shake, 1)
		case sim.EvBossKilled:
			for i := 0; i < 6; i++ {
				dx, dy := int((r.rnd()-0.5)*40), int((r.rnd()-0.5)*16)
				r.burst(x+dx, y+dy, 36, rowColour[i%sim.Rows], 3, true)
			}
			r.say("+"+itoa(int(e.A), 1), x, y-4, colGold, true)
			r.shake, r.flash = 10, 1
		case sim.EvShieldBreak:
			r.ring(x, y, 28, powerColour[sim.PowShield], 2.2)
			r.shake = max(r.shake, 2)
		case sim.EvExtraLife:
			r.say("1UP", x, y-16, powerColour[sim.PowLife], true)
		}
	}
}

// Tick advances the effects by one simulation tick.
func (r *Renderer) Tick() {
	r.frame++
	for i := range r.parts {
		p := &r.parts[i]
		if p.life <= 0 {
			continue
		}
		p.x += p.vx
		p.y += p.vy
		p.vy += p.gravity
		p.vx *= 0.97
		p.vy *= 0.97
		p.life--
	}
	for i := range r.stars {
		s := &r.stars[i]
		s.y += s.speed
		if s.y >= H {
			s.y -= H
			s.x = r.rnd() * W
		}
	}
	keep := r.popups[:0]
	for _, p := range r.popups {
		p.y -= 0.35
		p.life--
		if p.life > 0 {
			keep = append(keep, p)
		}
	}
	r.popups = keep
	alive := r.pops[:0]
	for _, p := range r.pops {
		p.life--
		if p.life > 0 {
			alive = append(alive, p)
		}
	}
	r.pops = alive

	r.shake *= 0.88
	r.flash *= 0.86
	r.ox, r.oy = 0, 0
	if r.shake > 0.4 {
		r.ox = int((r.rnd() - 0.5) * 2 * r.shake)
		r.oy = int((r.rnd() - 0.5) * 2 * r.shake)
	}
}

func (r *Renderer) drawStars() {
	for i := range r.stars {
		s := &r.stars[i]
		tw := 0.65 + 0.35*float32(math.Sin(float64(s.phase)+float64(r.frame)*0.06))
		r.add(int(s.x), int(s.y), 0xbfd4ff, int(float32(s.level)*tw))
	}
}

func (r *Renderer) drawParticles() {
	for i := range r.parts {
		p := &r.parts[i]
		if p.life <= 0 {
			continue
		}
		a := int(256 * p.life / p.total)
		x, y := int(p.x)+r.ox, int(p.y)+r.oy
		r.add(x, y, p.col, a)
		if p.big {
			r.add(x+1, y, p.col, a/2)
			r.add(x, y+1, p.col, a/2)
			r.add(x-1, y, p.col, a/3)
			r.add(x, y-1, p.col, a/3)
		}
	}
}

func (r *Renderer) drawPopups() {
	for _, p := range r.popups {
		if p.life < 12 && p.life&1 == 0 {
			continue // flicker out
		}
		x, y := int(p.x)+r.ox, int(p.y)+r.oy
		if p.big {
			x = min(max(x, textW(p.text, 1)/2+2), W-textW(p.text, 1)/2-2)
			r.textC(p.text, x, y, p.col, 1)
		} else {
			r.small(p.text, x, y, p.col)
		}
	}
}
