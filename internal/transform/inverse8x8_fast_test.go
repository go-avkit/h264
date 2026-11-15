package transform

import (
	"math/rand"
	"testing"
)

// slowInverse8x8 is the transform as it was before the shortcuts: eight rows and
// eight columns through the plain one-dimensional transform, no skipping.
func slowInverse8x8(b *Block8x8) {
	b[0] += 32
	var tmp Block8x8
	for r := 0; r < 8; r++ {
		var o [8]int32
		o[0], o[1], o[2], o[3], o[4], o[5], o[6], o[7] = idct8(b[r*8], b[r*8+1], b[r*8+2], b[r*8+3], b[r*8+4], b[r*8+5], b[r*8+6], b[r*8+7])
		copy(tmp[r*8:], o[:])
	}
	for c := 0; c < 8; c++ {
		o0, o1, o2, o3, o4, o5, o6, o7 := idct8(tmp[c], tmp[8+c], tmp[16+c], tmp[24+c], tmp[32+c], tmp[40+c], tmp[48+c], tmp[56+c])
		for i, v := range [8]int32{o0, o1, o2, o3, o4, o5, o6, o7} {
			b[i*8+c] = v >> 6
		}
	}
}

func checkSame(t *testing.T, name string, in Block8x8) {
	t.Helper()
	got, want := in, in
	Inverse8x8(&got)
	slowInverse8x8(&want)
	if got != want {
		t.Fatalf("%s: input %v: fast %v, plain %v", name, in, got, want)
	}
}

// The shortcuts against the plain transform (itself tied to the independent
// restatement of the spec by TestInverse8x8IndependentRestatement): every
// single-coefficient block at every position and a spread of values, every DC
// value over the legal range, random blocks with zero rows and zero columns,
// and dense blocks.
func TestInverse8x8ShortcutsAreExact(t *testing.T) {
	values := []int32{1, -1, 2, 3, -5, 31, 32, 33, 63, 64, 65, 255, -256, 1023, -1024, 32767, -32768, 1 << 20, -(1 << 20)}
	for pos := 0; pos < 64; pos++ {
		for _, v := range values {
			var b Block8x8
			b[pos] = v
			checkSame(t, "one coefficient", b)
		}
	}
	for d := int32(-40000); d <= 40000; d++ { // every DC value of a legal block, and beyond
		var b Block8x8
		b[0] = d
		checkSame(t, "DC only", b)
	}
	rng := rand.New(rand.NewSource(81))
	for i := 0; i < 200000; i++ {
		var b Block8x8
		for r := 0; r < 8; r++ {
			if rng.Intn(3) == 0 {
				continue // a zero row
			}
			for c := 0; c < 8; c++ {
				if rng.Intn(4) == 0 {
					b[r*8+c] = int32(rng.Intn(1<<17) - 1<<16)
				}
			}
		}
		if rng.Intn(5) == 0 {
			b[0] = int32(rng.Intn(1<<17) - 1<<16)
		}
		checkSame(t, "sparse", b)
	}
	for i := 0; i < 50000; i++ {
		var b Block8x8
		for j := range b {
			b[j] = int32(rng.Intn(1<<17) - 1<<16)
		}
		checkSame(t, "dense", b)
	}
}

func BenchmarkInverse8x8(b *testing.B) {
	cases := map[string]func() Block8x8{
		"dc":     func() Block8x8 { var x Block8x8; x[0] = 700; return x },
		"sparse": func() Block8x8 { var x Block8x8; x[0], x[1], x[8], x[9] = 700, -90, 60, 12; return x },
		"dense": func() Block8x8 {
			var x Block8x8
			for i := range x {
				x[i] = int32(i*37%200 - 100)
			}
			return x
		},
	}
	for name, mk := range cases {
		b.Run(name, func(b *testing.B) {
			src := mk()
			for i := 0; i < b.N; i++ {
				x := src
				Inverse8x8(&x)
			}
		})
	}
}
