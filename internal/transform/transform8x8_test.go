package transform

import (
	"math/rand"
	"testing"
)

func newRNG8x8(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

func randSample8x8Value(rng *rand.Rand) int32 {
	return int32(rng.Intn(511) - 255)
}

func randBlock8x8(rng *rand.Rand) Block8x8 {
	var b Block8x8
	for i := range b {
		b[i] = randSample8x8Value(rng)
	}
	return b
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestZigZagScan8x8IsPermutation(t *testing.T) {
	var seen [64]bool
	for i, v := range ZigZagScan8x8 {
		if seen[v] {
			t.Fatalf("ZigZagScan8x8[%d] = %d repeats an earlier value", i, v)
		}
		seen[v] = true
	}
	for v, ok := range seen {
		if !ok {
			t.Fatalf("ZigZagScan8x8 never produces %d", v)
		}
	}
}

func TestClass8x8PosPeriodicAndSymmetric(t *testing.T) {
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			pos := row*8 + col
			modPos := (row%4)*8 + col%4
			if class8x8Pos[pos] != class8x8Pos[modPos] {
				t.Fatalf("class8x8Pos not periodic mod 4 at (%d,%d): %d vs %d", row, col, class8x8Pos[pos], class8x8Pos[modPos])
			}
			transposed := col*8 + row
			if class8x8Pos[pos] != class8x8Pos[transposed] {
				t.Fatalf("class8x8Pos not symmetric at (%d,%d): %d vs %d", row, col, class8x8Pos[pos], class8x8Pos[transposed])
			}
		}
	}
}

var forwardCore8x8Matrix = [8][8]int64{
	{8, 8, 8, 8, 8, 8, 8, 8},
	{12, 10, 6, 3, -3, -6, -10, -12},
	{8, 4, -4, -8, -8, -4, 4, 8},
	{10, -3, -12, -6, 6, 12, 3, -10},
	{8, -8, -8, 8, 8, -8, -8, 8},
	{6, -12, 3, 10, -10, -3, 12, -6},
	{4, -8, 8, -4, -4, 8, -8, 4},
	{3, -6, 10, -12, 12, -10, 6, -3},
}

func TestForwardCore8x8MatrixIsOrthogonal(t *testing.T) {
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			var dot int64
			for k := 0; k < 8; k++ {
				dot += forwardCore8x8Matrix[r][k] * forwardCore8x8Matrix[c][k]
			}
			if r == c {
				if dot == 0 {
					t.Fatalf("row %d has zero norm", r)
				}
				continue
			}
			if dot != 0 {
				t.Fatalf("rows %d and %d are not orthogonal: dot = %d", r, c, dot)
			}
		}
	}
}

func matVec8(mat [8][8]int64, v [8]float64) [8]float64 {
	var out [8]float64
	for r := 0; r < 8; r++ {
		var s float64
		for c := 0; c < 8; c++ {
			s += float64(mat[r][c]) * v[c]
		}
		out[r] = s
	}
	return out
}

func referenceForward8x8Matrix(b Block8x8) [64]float64 {
	var tmp [8][8]float64
	for row := 0; row < 8; row++ {
		var src [8]float64
		for col := 0; col < 8; col++ {
			src[col] = float64(b[row*8+col])
		}
		out := matVec8(forwardCore8x8Matrix, src)
		for col := 0; col < 8; col++ {
			tmp[row][col] = out[col] / 8
		}
	}
	var out [64]float64
	for col := 0; col < 8; col++ {
		var src [8]float64
		for row := 0; row < 8; row++ {
			src[row] = tmp[row][col]
		}
		res := matVec8(forwardCore8x8Matrix, src)
		for row := 0; row < 8; row++ {
			out[row*8+col] = res[row] / 8
		}
	}
	return out
}

const forward8x8MatrixTolerance = 16.0

func TestForward8x8MatrixCrossCheck(t *testing.T) {
	rng := newRNG8x8(2002)
	check := func(name string, b Block8x8) {
		got := b
		Forward8x8(&got)
		ref := referenceForward8x8Matrix(b)
		for i := 0; i < 64; i++ {
			d := absF(float64(got[i]) - ref[i])
			if d > forward8x8MatrixTolerance {
				t.Fatalf("%s pos %d: Forward8x8 = %d, Cf8-matrix/8 reference = %.2f, diff %.2f exceeds tolerance %.2f",
					name, i, got[i], ref[i], d, forward8x8MatrixTolerance)
			}
		}
	}
	for trial := 0; trial < 3000; trial++ {
		check("random", randBlock8x8(rng))
	}
	var allMax, allMin, checker, ramp Block8x8
	for i := range allMax {
		allMax[i] = 255
		allMin[i] = -255
	}
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			if (row+col)%2 == 0 {
				checker[row*8+col] = 255
			} else {
				checker[row*8+col] = -255
			}
			ramp[row*8+col] = int32((row*8+col)%511 - 255)
		}
	}
	check("allMax", allMax)
	check("allMin", allMin)
	check("checkerboard", checker)
	check("ramp", ramp)
}

func TestInverse8x8DCOnlyIsFlat(t *testing.T) {
	rng := newRNG8x8(2003)
	for trial := 0; trial < 500; trial++ {
		var b Block8x8
		b[0] = int32(rng.Intn(200001) - 100000)
		want := (b[0] + 32) >> 6
		Inverse8x8(&b)
		for i := 0; i < 64; i++ {
			if b[i] != want {
				t.Fatalf("trial %d pos %d: Inverse8x8 of DC-only block = %d, want flat %d", trial, i, b[i], want)
			}
		}
	}
}

func idct8x1DIndependent(src [8]int32) [8]int32 {
	var out [8]int32
	a0 := src[0] + src[4]
	a2 := src[0] - src[4]
	a4 := (src[2] >> 1) - src[6]
	a6 := (src[6] >> 1) + src[2]
	out[0] = a0 + a6
	out[2] = a2 + a4
	out[4] = a2 - a4
	out[6] = a0 - a6
	a1 := -src[3] + src[5] - src[7] - (src[7] >> 1)
	a3 := src[1] + src[7] - src[3] - (src[3] >> 1)
	a5 := -src[1] + src[7] + src[5] + (src[5] >> 1)
	a7 := src[3] + src[5] + src[1] + (src[1] >> 1)
	out[1] = (a7 >> 2) + a1
	out[3] = a3 + (a5 >> 2)
	out[5] = (a3 >> 2) - a5
	out[7] = a7 - (a1 >> 2)
	return [8]int32{out[0] + out[7], out[2] + out[5], out[4] + out[3], out[6] + out[1], out[6] - out[1], out[4] - out[3], out[2] - out[5], out[0] - out[7]}
}

func referenceInverse8x8(m [8][8]int32) [8][8]int32 {
	m[0][0] += 32
	var tmp [8][8]int32
	for row := 0; row < 8; row++ {
		tmp[row] = idct8x1DIndependent(m[row])
	}
	var out [8][8]int32
	for col := 0; col < 8; col++ {
		var src [8]int32
		for row := 0; row < 8; row++ {
			src[row] = tmp[row][col]
		}
		res := idct8x1DIndependent(src)
		for row := 0; row < 8; row++ {
			out[row][col] = res[row] >> 6
		}
	}
	return out
}

func TestInverse8x8IndependentRestatement(t *testing.T) {
	rng := newRNG8x8(2004)
	for trial := 0; trial < 3000; trial++ {
		b := randBlock8x8(rng)
		got := b
		Inverse8x8(&got)

		var m [8][8]int32
		for row := 0; row < 8; row++ {
			for col := 0; col < 8; col++ {
				m[row][col] = b[row*8+col]
			}
		}
		ref := referenceInverse8x8(m)
		for row := 0; row < 8; row++ {
			for col := 0; col < 8; col++ {
				if got[row*8+col] != ref[row][col] {
					t.Fatalf("trial %d (%d,%d): Inverse8x8 = %d, independent restatement = %d", trial, row, col, got[row*8+col], ref[row][col])
				}
			}
		}
	}
}

func TestAddResidual8x8Clipping(t *testing.T) {
	plane := make([]byte, 8*8)
	for i := range plane {
		plane[i] = 250
	}
	var b Block8x8
	for i := range b {
		b[i] = 100
	}
	b[0] = -1000
	AddResidual8x8(plane, 8, 0, &b)
	if plane[0] != 0 {
		t.Fatalf("clipping to 0 failed: got %d", plane[0])
	}
	for i := 1; i < 64; i++ {
		if plane[i] != 255 {
			t.Fatalf("clipping to 255 failed at %d: got %d", i, plane[i])
		}
	}
}
