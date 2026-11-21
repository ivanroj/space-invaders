// Package sim is the deterministic core of the game.
//
// It contains no floating point maths, no clocks and no I/O: the state after N
// ticks depends only on the seed and the N input bytes. The same package is
// compiled to WebAssembly for the browser and to native code for the server,
// which replays the recorded inputs to verify a submitted score.
package sim

// Version changes whenever the simulation rules change. A replay recorded with
// one version cannot be verified with another.
const Version = 1

const (
	TickRate = 60

	ScreenW = 320
	ScreenH = 240

	// FP is the number of sub-pixels in one pixel. Moving objects store
	// positions in sub-pixels so that speeds below 1 px/tick stay exact.
	FP = 256

	PlayTop = 16
	GroundY = 222

	PlayerW     = 13
	PlayerH     = 8
	PlayerY     = 208
	PlayerSpeed = 2 * FP
	StartLives  = 3
	MaxLives    = 5
	LifeEvery   = 5000

	PBulletW     = 1
	PBulletH     = 6
	PBulletSpeed = 4 * FP
	EBulletW     = 3
	EBulletH     = 7

	MaxPlayerBullets = 24
	MaxEnemyBullets  = 48
	MaxPowerups      = 6

	Rows     = 5
	Cols     = 11
	CellW    = 16
	CellH    = 14
	InvaderH = 8
	StepPx   = 3
	DropPx   = 8
	EdgePad  = 4

	NumBunkers = 4
	BunkerW    = 22
	BunkerH    = 16
	BunkerY    = 176

	UFOW = 16
	UFOH = 7
	UFOY = 20

	BossW     = 48
	BossH     = 20
	BossRestY = 30
	BossEvery = 5

	PowerupW = 9
	PowerupH = 9

	ComboWindow = 90
	MaxMult     = 5
)

// Phase is the coarse state of a game.
type Phase uint8

const (
	PhaseIntro Phase = iota
	PhasePlaying
	PhaseDying
	PhaseClear
	PhaseGameOver
)

// Input is a bit set of the buttons held during one tick.
type Input uint8

const (
	InLeft Input = 1 << iota
	InRight
	InFire

	InputMask = InLeft | InRight | InFire
)

// PowerupKind identifies a bonus capsule.
type PowerupKind uint8

const (
	PowRapid PowerupKind = iota
	PowTriple
	PowShield
	PowSlow
	PowLife
	NumPowerups
)

const (
	RapidTicks  = 600
	TripleTicks = 600
	ShieldTicks = 900
	SlowTicks   = 360
)

// EventKind tells the presentation layer what happened during a tick.
type EventKind uint8

const (
	EvShoot EventKind = iota + 1
	EvInvaderKilled
	EvPlayerHit
	EvUFOSpawn
	EvUFOKilled
	EvUFOLeft
	EvPowerupTaken
	EvBunkerHit
	EvBulletClash
	EvStep
	EvEnemyShoot
	EvWaveStart
	EvWaveClear
	EvGameOver
	EvBossHit
	EvBossKilled
	EvBossShoot
	EvExtraLife
	EvShieldBreak
)

// Event is a one-tick notification. X and Y are in pixels; the meaning of A
// and B depends on Kind.
type Event struct {
	Kind EventKind
	X, Y int16
	A, B int32
}

// InvaderWidth is the sprite width of the invader type used in each row.
var InvaderWidth = [Rows]int{8, 11, 11, 12, 12}

// InvaderPoints is the base score of an invader in each row.
var InvaderPoints = [Rows]int{30, 20, 20, 10, 10}

// ufoScores is indexed by the number of shots fired, as in the 1978 original:
// counting shots lets a careful player always get the 300.
var ufoScores = [15]int{100, 50, 50, 100, 150, 100, 100, 50, 300, 100, 100, 100, 50, 150, 100}

// BunkerX is the left edge of each bunker.
var BunkerX = [NumBunkers]int{46, 114, 182, 250}
