//go:build js && wasm

// Command wasm is the browser build of the game. It exposes a small API on
// the global object SpaceInvaders; web/wasm.js is the only code that calls
// it. Each animation frame crosses the JS/Wasm boundary once: JS calls
// frame(), Go advances the simulation, draws the picture and copies two
// buffers back (pixels and a block of numbers for the HUD and sound).
package main

import (
	"encoding/binary"
	"math"
	"strconv"
	"syscall/js"

	"spaceinvaders/internal/render"
	"spaceinvaders/internal/replay"
	"spaceinvaders/internal/sim"
)

const (
	modeDemo  = 1 // the bot plays behind the menu
	modePlay  = 2 // the player plays and the inputs are recorded
	modeWatch = 3 // a recording is played back

	tickMs = 1000.0 / sim.TickRate
	// A display frame this close to a tick counts as exactly one tick.
	// Without it a 60 Hz screen, whose frames are never exactly 16.67 ms,
	// would now and then run zero or two ticks in a frame, which shows as
	// stutter.
	snapMs       = 0.7
	maxCatchUp   = 5 // ticks simulated in one frame at most
	maxEvents    = 16
	eventFields  = 3
	stateEvents  = 16 // index of the first event in the state block
	stateLen     = stateEvents + maxEvents*eventFields
	demoRestartS = 3
)

// Indices into the state block. They are published to JS as
// SpaceInvaders.state so the two sides cannot drift apart.
const (
	stMode = iota
	stPhase
	stScore
	stLives
	stWave
	stTick
	stMult
	stUFO
	stBoss
	stShots
	stHits
	stKills
	stMaxCombo
	stTicksRun
	stEventCount
)

type app struct {
	game   *sim.Game
	view   *render.Renderer
	rec    replay.Recorder
	tape   *replay.Reader // the recording being watched
	mode   int
	paused bool
	hi     int
	acc    float64
	idle   int // ticks since the demo game ended

	pixels js.Value
	state  js.Value
	buf    [stateLen * 4]byte
	events []sim.Event
}

func main() {
	a := &app{view: render.New()}
	a.startDemo()

	api := js.Global().Get("Object").New()
	api.Set("width", render.W)
	api.Set("height", render.H)
	api.Set("version", sim.Version)
	api.Set("tickRate", sim.TickRate)
	api.Set("stateLength", stateLen)
	api.Set("state", map[string]any{
		"mode": stMode, "phase": stPhase, "score": stScore, "lives": stLives,
		"wave": stWave, "tick": stTick, "mult": stMult, "ufo": stUFO,
		"boss": stBoss, "shots": stShots, "hits": stHits, "kills": stKills,
		"maxCombo": stMaxCombo, "ticksRun": stTicksRun,
		"eventCount": stEventCount, "events": stateEvents, "eventFields": eventFields,
	})
	api.Set("events", map[string]any{
		"shoot": int(sim.EvShoot), "invaderKilled": int(sim.EvInvaderKilled),
		"playerHit": int(sim.EvPlayerHit), "ufoKilled": int(sim.EvUFOKilled),
		"powerup": int(sim.EvPowerupTaken), "bunkerHit": int(sim.EvBunkerHit),
		"bulletClash": int(sim.EvBulletClash), "step": int(sim.EvStep),
		"enemyShoot": int(sim.EvEnemyShoot), "waveStart": int(sim.EvWaveStart),
		"waveClear": int(sim.EvWaveClear), "gameOver": int(sim.EvGameOver),
		"bossHit": int(sim.EvBossHit), "bossKilled": int(sim.EvBossKilled),
		"bossShoot": int(sim.EvBossShoot), "extraLife": int(sim.EvExtraLife),
		"shieldBreak": int(sim.EvShieldBreak),
	})
	api.Set("phases", map[string]any{
		"intro": int(sim.PhaseIntro), "playing": int(sim.PhasePlaying), "dying": int(sim.PhaseDying),
		"clear": int(sim.PhaseClear), "gameOver": int(sim.PhaseGameOver),
	})
	api.Set("modes", map[string]any{"demo": modeDemo, "play": modePlay, "watch": modeWatch})
	api.Set("inputs", map[string]any{"left": int(sim.InLeft), "right": int(sim.InRight), "fire": int(sim.InFire)})
	api.Set("bind", js.FuncOf(a.bind))
	api.Set("start", js.FuncOf(a.start))
	api.Set("watch", js.FuncOf(a.watch))
	api.Set("demo", js.FuncOf(a.demo))
	api.Set("setPaused", js.FuncOf(a.setPaused))
	api.Set("setHiScore", js.FuncOf(a.setHiScore))
	api.Set("frame", js.FuncOf(a.frame))
	api.Set("replay", js.FuncOf(a.replay))
	js.Global().Set("SpaceInvaders", api)

	select {} // keep the Go runtime alive for the callbacks
}

// demoSeed varies the attract mode without touching the clock inside the
// simulation.
var demoSeed uint64 = 0xC0FFEE

func (a *app) startDemo() {
	demoSeed = demoSeed*6364136223846793005 + 1442695040888963407
	a.game = sim.New(demoSeed)
	a.mode, a.paused, a.idle, a.acc = modeDemo, false, 0, 0
}

// bind(pixels Uint8ClampedArray, state Uint8Array) stores the two buffers
// frame() fills.
func (a *app) bind(_ js.Value, args []js.Value) any {
	a.pixels, a.state = args[0], args[1]
	return nil
}

// start(seedHex string) begins a recorded game. It returns false if the seed
// is not a hexadecimal 64-bit number.
func (a *app) start(_ js.Value, args []js.Value) any {
	seed, err := strconv.ParseUint(args[0].String(), 16, 64)
	if err != nil {
		return false
	}
	a.game = sim.New(seed)
	a.rec.Reset()
	a.mode, a.paused, a.acc = modePlay, false, 0
	return true
}

// watch(seedHex string, recording Uint8Array) plays a recording back. It
// returns false if the seed or the recording header is invalid.
func (a *app) watch(_ js.Value, args []js.Value) any {
	seed, err := strconv.ParseUint(args[0].String(), 16, 64)
	if err != nil {
		return false
	}
	data := make([]byte, args[1].Length())
	js.CopyBytesToGo(data, args[1])
	tape, err := replay.NewReader(data)
	if err != nil {
		return false
	}
	a.game, a.tape = sim.New(seed), tape
	a.mode, a.paused, a.acc = modeWatch, false, 0
	return true
}

func (a *app) demo(js.Value, []js.Value) any {
	a.startDemo()
	return nil
}

func (a *app) setPaused(_ js.Value, args []js.Value) any {
	a.paused = args[0].Bool() && a.mode != modeDemo && !a.game.Over()
	return a.paused
}

func (a *app) setHiScore(_ js.Value, args []js.Value) any {
	a.hi = args[0].Int()
	return nil
}

// replay() returns the recording of the current game as a Uint8Array.
func (a *app) replay(js.Value, []js.Value) any {
	data := a.rec.Bytes()
	out := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(out, data)
	return out
}

// frame(dtMs number, input number) advances the game by the elapsed time,
// draws it and fills the bound buffers.
func (a *app) frame(_ js.Value, args []js.Value) any {
	dt, in := args[0].Float(), sim.Input(args[1].Int())&sim.InputMask
	a.events = a.events[:0]
	ticks := 0

	if !a.paused {
		if math.Abs(dt-tickMs) < snapMs {
			dt = tickMs
		}
		a.acc += dt
		for a.acc >= tickMs && ticks < maxCatchUp {
			a.acc -= tickMs
			a.tick(in)
			ticks++
		}
		if a.acc > tickMs {
			a.acc = 0 // the tab was in the background: do not fast-forward
		}
	}

	view := render.View{HiScore: a.hi, Paused: a.paused}
	if a.mode == modeDemo {
		view.Attract, view.Label = true, "DEMO"
	}
	a.view.Draw(a.game, view)
	js.CopyBytesToJS(a.pixels, a.view.Pix)
	a.writeState(ticks)
	js.CopyBytesToJS(a.state, a.buf[:])
	return nil
}

func (a *app) tick(in sim.Input) {
	g := a.game
	switch a.mode {
	case modeDemo:
		if g.Over() {
			a.idle++
			if a.idle > demoRestartS*sim.TickRate {
				a.startDemo()
				return
			}
		} else {
			in = sim.Bot(g)
		}
	case modeWatch:
		var ok bool
		if in, ok = a.tape.Next(); !ok {
			a.view.Tick()
			return
		}
	}
	if !g.Over() {
		if a.mode == modePlay {
			a.rec.Add(in)
		}
		g.Step(in)
		a.view.Handle(g.Events)
		a.events = append(a.events, g.Events...)
	}
	a.view.Tick()
}

func (a *app) writeState(ticks int) {
	g := a.game
	put := func(i, v int) { binary.LittleEndian.PutUint32(a.buf[i*4:], uint32(int32(v))) }
	flag := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	put(stMode, a.mode)
	put(stPhase, int(g.Phase))
	put(stScore, g.Score)
	put(stLives, g.Lives)
	put(stWave, g.Wave)
	put(stTick, int(g.Tick))
	put(stMult, g.Mult())
	put(stUFO, flag(g.UFO.Active))
	put(stBoss, flag(g.Boss.Active))
	put(stShots, g.Stats.Shots)
	put(stHits, g.Stats.Hits)
	put(stKills, g.Stats.Kills)
	put(stMaxCombo, g.Stats.MaxCombo)
	put(stTicksRun, ticks)

	n := min(len(a.events), maxEvents)
	put(stEventCount, n)
	for i, e := range a.events[:n] {
		base := stateEvents + i*eventFields
		put(base, int(e.Kind))
		put(base+1, int(e.A))
		put(base+2, int(e.X))
	}
}
