package render

import "spaceinvaders/internal/sim"

// Palette. Colours are 0xRRGGBB.
const (
	colWhite  = 0xf4fbff
	colPlayer = 0x5ff2ff
	colGlass  = 0xffffff
	colFlame  = 0xffa23a
	colBunker = 0x39e27a
	colGround = 0x1e8f58
	colUFO    = 0xff3b5c
	colBoss   = 0xb46bff
	colEnemyB = 0xffc94a
	colFastB  = 0xff5a4a
	colHUD    = 0xcfe6ff
	colDimHUD = 0x6f86b8
	colGold   = 0xffd84a
	colAlert  = 0xff3b5c
)

// rowColour gives every invader row its own neon tint.
var rowColour = [sim.Rows]uint32{0xff5ad1, 0xb884ff, 0x5ac8ff, 0x4dffc3, 0xb4ff5a}

var powerColour = [sim.NumPowerups]uint32{0xffb03a, 0xff5ad1, 0x5ac8ff, 0xb884ff, 0x5dff8f}
var powerGlyph = [sim.NumPowerups]string{"R", "T", "S", "Z", "L"}
var powerName = [sim.NumPowerups]string{"RAPID FIRE", "TRIPLE SHOT", "SHIELD", "SLOW MOTION", "EXTRA LIFE"}

// Each invader has two animation frames; the formation flips between them on
// every step.
var invaderSprites = [sim.Rows][2]*Sprite{}

var (
	squid = [2]*Sprite{
		parse(
			"...##...",
			"..####..",
			".######.",
			"##o##o##",
			"########",
			"..#..#..",
			".#.##.#.",
			"#.#..#.#",
		),
		parse(
			"...##...",
			"..####..",
			".######.",
			"##o##o##",
			"########",
			".#.##.#.",
			"#......#",
			".#....#.",
		),
	}
	crab = [2]*Sprite{
		parse(
			"..#.....#..",
			"...#...#...",
			"..#######..",
			".##o###o##.",
			"###########",
			"#.#######.#",
			"#.#.....#.#",
			"...##.##...",
		),
		parse(
			"..#.....#..",
			"#..#...#..#",
			"#.#######.#",
			"###o###o###",
			"###########",
			".#########.",
			"..#.....#..",
			".#.......#.",
		),
	}
	octopus = [2]*Sprite{
		parse(
			"....####....",
			".##########.",
			"############",
			"###oo##oo###",
			"############",
			"...##..##...",
			"..##.##.##..",
			"##........##",
		),
		parse(
			"....####....",
			".##########.",
			"############",
			"###oo##oo###",
			"############",
			"..###..###..",
			".##..##..##.",
			"..##....##..",
		),
	}

	playerSprite = parse(
		"......o......",
		".....#o#.....",
		".....#o#.....",
		"..#.#####.#..",
		".###########.",
		"#############",
		"####.###.####",
		"##...#.#...##",
	)

	ufoSprite = parse(
		".....######.....",
		"...##########...",
		"..############..",
		".##o##o##o##o##.",
		"################",
		"..###..##..###..",
		"...#........#...",
	)

	bossSprite = mirror(
		".................#######",
		"..............##########",
		"............############",
		"..........#####ooooooooo",
		".........#####ooo++ooooo",
		"........######ooo++ooooo",
		".......########ooooooooo",
		"......##################",
		"....####################",
		"..######################",
		"########################",
		"###++###++###++###++####",
		"########################",
		".#######################",
		"...####..#####...#######",
		"....##....###.....######",
		"....##....###......####.",
		"...##......##.......##..",
		"..##.......#........##..",
		"..#.................#...",
	)

	// The classic "pop" shown for a few frames where an invader died.
	popSprite = parse(
		"..#..#..#..",
		"#..#...#..#",
		".#.......#.",
		"...........",
		"##.......##",
		".#.......#.",
		"#..#...#..#",
		"..#..#..#..",
	)

	capsule = parse(
		"..#####..",
		".#.....#.",
		"#.......#",
		"#.......#",
		"#.......#",
		"#.......#",
		"#.......#",
		".#.....#.",
		"..#####..",
	)
)

func init() {
	invaderSprites = [sim.Rows][2]*Sprite{squid, crab, crab, octopus, octopus}
	for r, pair := range invaderSprites {
		for _, s := range pair {
			if s.W != sim.InvaderWidth[r] || s.H != sim.InvaderH {
				panic("render: invader sprite does not match its hit box")
			}
		}
	}
	if bossSprite.W != sim.BossW || bossSprite.H != sim.BossH ||
		playerSprite.W != sim.PlayerW || playerSprite.H != sim.PlayerH ||
		ufoSprite.W != sim.UFOW || ufoSprite.H != sim.UFOH {
		panic("render: sprite does not match its hit box")
	}
}
