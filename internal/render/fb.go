// Package render is a software renderer: it draws a sim.Game into an RGBA
// frame buffer, pixel by pixel, and owns the purely cosmetic effects
// (particles, starfield, screen shake). Nothing in here feeds back into the
// simulation, so floating point is fine.
package render

import "spaceinvaders/internal/sim"

const (
	W = sim.ScreenW
	H = sim.ScreenH
)

// Sprite is a small indexed image: 0 is transparent, 1..3 select a colour
// from the palette passed to the draw call.
type Sprite struct {
	W, H int
	Px   []uint8
}

// parse builds a sprite from ASCII art. '.' is transparent, '#', 'o' and '+'
// are palette slots 1, 2 and 3.
func parse(rows ...string) *Sprite {
	s := &Sprite{W: len(rows[0]), H: len(rows)}
	s.Px = make([]uint8, s.W*s.H)
	for y, row := range rows {
		if len(row) != s.W {
			panic("render: ragged sprite: " + row)
		}
		for x, ch := range row {
			switch ch {
			case '#':
				s.Px[y*s.W+x] = 1
			case 'o':
				s.Px[y*s.W+x] = 2
			case '+':
				s.Px[y*s.W+x] = 3
			}
		}
	}
	return s
}

// mirror builds a symmetric sprite from its left half.
func mirror(half ...string) *Sprite {
	rows := make([]string, len(half))
	for i, h := range half {
		b := []byte(h)
		for j := len(h) - 1; j >= 0; j-- {
			b = append(b, h[j])
		}
		rows[i] = string(b)
	}
	return parse(rows...)
}

func (r *Renderer) set(x, y int, c uint32) {
	if uint(x) >= W || uint(y) >= H {
		return
	}
	i := (y*W + x) * 4
	r.Pix[i] = byte(c >> 16)
	r.Pix[i+1] = byte(c >> 8)
	r.Pix[i+2] = byte(c)
}

// add blends c over the pixel additively with strength a (0..256), which is
// what makes overlapping sparks look hot rather than flat.
func (r *Renderer) add(x, y int, c uint32, a int) {
	if uint(x) >= W || uint(y) >= H || a <= 0 {
		return
	}
	i := (y*W + x) * 4
	r.Pix[i] = sat(int(r.Pix[i]) + int(c>>16&0xff)*a>>8)
	r.Pix[i+1] = sat(int(r.Pix[i+1]) + int(c>>8&0xff)*a>>8)
	r.Pix[i+2] = sat(int(r.Pix[i+2]) + int(c&0xff)*a>>8)
}

func sat(v int) byte {
	if v > 255 {
		return 255
	}
	return byte(v)
}

func (r *Renderer) rect(x, y, w, h int, c uint32) {
	for yy := max(y, 0); yy < min(y+h, H); yy++ {
		for xx := max(x, 0); xx < min(x+w, W); xx++ {
			r.set(xx, yy, c)
		}
	}
}

func (r *Renderer) sprite(s *Sprite, x, y int, pal [3]uint32) {
	for sy := 0; sy < s.H; sy++ {
		for sx := 0; sx < s.W; sx++ {
			if p := s.Px[sy*s.W+sx]; p != 0 {
				r.set(x+sx, y+sy, pal[p-1])
			}
		}
	}
}

// dim darkens the whole frame; used behind the pause banner.
func (r *Renderer) dim() {
	for i := 0; i < len(r.Pix); i += 4 {
		r.Pix[i] >>= 1
		r.Pix[i+1] >>= 1
		r.Pix[i+2] >>= 1
	}
}

func scale(c uint32, a int) uint32 {
	return uint32(int(c>>16&0xff)*a>>8)<<16 | uint32(int(c>>8&0xff)*a>>8)<<8 | uint32(int(c&0xff)*a>>8)
}
