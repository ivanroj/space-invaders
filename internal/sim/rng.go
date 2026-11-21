package sim

// RNG is SplitMix64. The standard library generator is avoided on purpose:
// its algorithm may change between Go releases, and a replay must give the
// same result on every build.
type RNG struct{ s uint64 }

func (r *RNG) Next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Intn returns a number in [0, n).
func (r *RNG) Intn(n int) int {
	return int(((r.Next() >> 32) * uint64(n)) >> 32)
}

// isqrt returns floor(sqrt(n)) using integers only.
func isqrt(n int64) int64 {
	if n <= 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}
