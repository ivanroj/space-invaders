// Command snapshot plays the game with the built-in bot and saves frames as
// PNG files. It runs the same simulation and renderer as the browser build,
// which makes it handy for checking the picture without opening a browser.
//
//	go run ./cmd/snapshot -seed 7 -ticks 600,2400 -out shots
//
// With -stream it writes raw RGBA frames to standard output instead, which
// ffmpeg turns into a video or a GIF:
//
//	go run ./cmd/snapshot -seed 2 -stream 240:840 |
//	  ffmpeg -f rawvideo -pixel_format rgba -video_size 320x240 -framerate 60 -i - demo.mp4
package main

import (
	"bufio"
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
	stream := flag.String("stream", "", "FROM:TO ticks to write to stdout as raw RGBA frames")
	flag.Parse()

	if *stream != "" {
		if err := streamFrames(*seed, *stream); err != nil {
			log.Fatal(err)
		}
		return
	}

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

func streamFrames(seed uint64, span string) error {
	var from, to uint32
	if _, err := fmt.Sscanf(span, "%d:%d", &from, &to); err != nil || from >= to {
		return fmt.Errorf("bad -stream %q, want FROM:TO", span)
	}
	out := bufio.NewWriter(os.Stdout)
	g := sim.New(seed)
	r := render.New()
	for g.Tick < to && !g.Over() {
		g.Step(sim.Bot(g))
		r.Handle(g.Events)
		r.Tick()
		if g.Tick > from {
			r.Draw(g, render.View{})
			if _, err := out.Write(r.Pix); err != nil {
				return err
			}
		}
	}
	return out.Flush()
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
