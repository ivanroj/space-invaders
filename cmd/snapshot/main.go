// Command snapshot plays the game with the built-in bot and saves frames as
// PNG files. It runs the same simulation and renderer as the browser build,
// which makes it handy for checking the picture without opening a browser.
//
//	go run ./cmd/snapshot -seed 7 -ticks 600,2400 -out shots
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"spaceinvaders/internal/render"
	"spaceinvaders/internal/sim"
)

func main() {
	seed := flag.Uint64("seed", 1, "game seed")
	ticks := flag.String("ticks", "300,1200,3000", "comma-separated ticks to capture")
	out := flag.String("out", "shots", "output directory")
	zoom := flag.Int("zoom", 3, "integer upscale factor")
	flag.Parse()

	want := map[uint32]bool{}
	var last uint32
	for _, s := range strings.Split(*ticks, ",") {
		n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32)
		if err != nil {
			log.Fatalf("bad tick %q", s)
		}
		want[uint32(n)] = true
		last = max(last, uint32(n))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}

	g := sim.New(*seed)
	r := render.New()
	for g.Tick < last && !g.Over() {
		g.Step(sim.Bot(g))
		r.Handle(g.Events)
		r.Tick()
		if want[g.Tick] {
			r.Draw(g, render.View{})
			name := filepath.Join(*out, fmt.Sprintf("tick-%06d.png", g.Tick))
			if err := save(name, r.Pix, *zoom); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("%s  score=%d wave=%d lives=%d\n", name, g.Score, g.Wave, g.Lives)
		}
	}
}

func save(name string, pix []byte, zoom int) error {
	img := image.NewRGBA(image.Rect(0, 0, render.W*zoom, render.H*zoom))
	for y := 0; y < render.H*zoom; y++ {
		for x := 0; x < render.W*zoom; x++ {
			copy(img.Pix[img.PixOffset(x, y):], pix[(y/zoom*render.W+x/zoom)*4:][:4])
		}
	}
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
