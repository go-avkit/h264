package transform

import (
	"math/rand"
	"testing"
)

func TestDefaultScalingListsInRangeAndSymmetric(t *testing.T) {
	checkRange := func(name string, vals []uint8) {
		t.Helper()
		for i, v := range vals {
			if v < 1 {
				t.Fatalf("%s[%d] = %d out of range [1,255]", name, i, v)
			}
		}
	}
	checkRange("DefaultScalingList4x4Intra", DefaultScalingList4x4Intra[:])
	checkRange("DefaultScalingList4x4Inter", DefaultScalingList4x4Inter[:])
	checkRange("DefaultScalingList8x8Intra", DefaultScalingList8x8Intra[:])
	checkRange("DefaultScalingList8x8Inter", DefaultScalingList8x8Inter[:])

	checkSymmetric := func(name string, m [64]uint8) {
		t.Helper()
		for row := 0; row < 8; row++ {
			for col := 0; col < 8; col++ {
				if m[row*8+col] != m[col*8+row] {
					t.Fatalf("%s not symmetric at (%d,%d)", name, row, col)
				}
			}
		}
	}
	checkSymmetric("DefaultScalingList8x8Intra", DefaultScalingList8x8Intra)
	checkSymmetric("DefaultScalingList8x8Inter", DefaultScalingList8x8Inter)
}

func TestNormAdjust8x8AndQuant8ScaleShapes(t *testing.T) {
	for m := 0; m < 6; m++ {
		for c := 0; c < 6; c++ {
			if normAdjust8x8[m][c] <= 0 {
				t.Fatalf("normAdjust8x8[%d][%d] = %d must be positive", m, c, normAdjust8x8[m][c])
			}
			if quant8Scale[m][c] <= 0 {
				t.Fatalf("quant8Scale[%d][%d] = %d must be positive", m, c, quant8Scale[m][c])
			}
		}
	}
	for m := 0; m < 5; m++ {
		for c := 0; c < 6; c++ {
			if normAdjust8x8[m][c] >= normAdjust8x8[m+1][c] {
				t.Fatalf("normAdjust8x8[%d][%d]=%d should be < normAdjust8x8[%d][%d]=%d", m, c, normAdjust8x8[m][c], m+1, c, normAdjust8x8[m+1][c])
			}
			if quant8Scale[m][c] <= quant8Scale[m+1][c] {
				t.Fatalf("quant8Scale[%d][%d]=%d should be > quant8Scale[%d][%d]=%d", m, c, quant8Scale[m][c], m+1, c, quant8Scale[m+1][c])
			}
		}
	}
}

func TestBuildLevelScale8x8FlatMatchesNormAdjustTimes16(t *testing.T) {
	ls := BuildLevelScale8x8(FlatWeightScale8x8)
	for m := 0; m < 6; m++ {
		for pos := 0; pos < 64; pos++ {
			want := normAdjust8x8[m][class8x8Pos[pos]] * 16
			if ls[m][pos] != want {
				t.Fatalf("LevelScale8x8[%d][%d] = %d, want %d", m, pos, ls[m][pos], want)
			}
		}
	}
}

func TestBuildQuantScale8x8FlatMatchesQuant8Scale(t *testing.T) {
	qs := BuildQuantScale8x8(FlatWeightScale8x8)
	for m := 0; m < 6; m++ {
		for pos := 0; pos < 64; pos++ {
			want := quant8Scale[m][class8x8Pos[pos]]
			if qs[m][pos] != want {
				t.Fatalf("QuantScale8x8[%d][%d] = %d, want %d", m, pos, qs[m][pos], want)
			}
		}
	}
}

func TestQuantDequant8x8QP0NearLossless(t *testing.T) {
	rng := newRNG8x8(3000)
	ls := BuildLevelScale8x8(FlatWeightScale8x8)
	qs := BuildQuantScale8x8(FlatWeightScale8x8)
	var maxErr int32
	for n := 0; n < 4000; n++ {
		orig := randBlock8x8(rng)
		b := orig
		Forward8x8(&b)
		Quant8x8(&b, 0, &qs, true)
		Dequant8x8(&b, 0, &ls)
		Inverse8x8(&b)
		for i := 0; i < 64; i++ {
			e := abs32(b[i] - orig[i])
			if e > maxErr {
				maxErr = e
			}
		}
	}
	if maxErr > 2 {
		t.Fatalf("qp=0: max abs error = %d, want <= 2", maxErr)
	}
}

var maxErr8x8Intra = [52]int32{
	17, 17, 17, 17, 19, 19, 19, 19, 19, 21, 19, 21, 23, 23, 23, 25,
	25, 27, 29, 31, 33, 35, 35, 43, 41, 45, 49, 53, 55, 61, 69, 73,
	83, 87, 97, 113, 117, 135, 155, 169, 195, 205, 225, 245, 287, 305, 361, 429,
	423, 467, 527, 601,
}

var maxErr8x8Inter = [52]int32{
	17, 17, 19, 19, 19, 19, 19, 19, 21, 21, 21, 23, 25, 25, 27, 27,
	29, 31, 33, 33, 39, 41, 43, 45, 53, 57, 57, 67, 67, 73, 83, 99,
	103, 109, 119, 133, 145, 161, 203, 211, 219, 249, 271, 299, 365, 387, 409, 451,
	539, 591, 661, 675,
}

func TestQuantDequant8x8RoundTripAC(t *testing.T) {
	ls := BuildLevelScale8x8(FlatWeightScale8x8)
	qs := BuildQuantScale8x8(FlatWeightScale8x8)
	for _, mode := range []struct {
		intra bool
		seed  int64
		table [52]int32
	}{
		{true, 4001, maxErr8x8Intra},
		{false, 4002, maxErr8x8Inter},
	} {
		rng := newRNG8x8(mode.seed)
		for qp := 0; qp < 52; qp++ {
			var maxErr int32
			for n := 0; n < 300; n++ {
				orig := randBlock8x8(rng)
				b := orig
				Forward8x8(&b)
				Quant8x8(&b, qp, &qs, mode.intra)
				Dequant8x8(&b, qp, &ls)
				Inverse8x8(&b)
				for i := 0; i < 64; i++ {
					e := abs32(b[i] - orig[i])
					if e > maxErr {
						maxErr = e
					}
				}
			}
			if maxErr > mode.table[qp] {
				t.Errorf("intra=%v qp=%d: max abs error = %d, want <= %d", mode.intra, qp, maxErr, mode.table[qp])
			}
		}
	}
}

func nonFlatAsymmetricWeightScale8x8() [64]uint8 {
	var w [64]uint8
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			w[row*8+col] = uint8(1 + (row*7+col*3)%40)
		}
	}
	return w
}

func TestQuantDequant8x8CustomAsymmetricScalingList(t *testing.T) {
	weight := nonFlatAsymmetricWeightScale8x8()
	ls := BuildLevelScale8x8(weight)
	qs := BuildQuantScale8x8(weight)
	rng := newRNG8x8(5000)
	for _, qp := range []int{0, 10, 26, 40, 51} {
		var maxErr int32
		for n := 0; n < 500; n++ {
			orig := randBlock8x8(rng)
			b := orig
			Forward8x8(&b)
			Quant8x8(&b, qp, &qs, true)
			Dequant8x8(&b, qp, &ls)
			Inverse8x8(&b)
			for i := 0; i < 64; i++ {
				e := abs32(b[i] - orig[i])
				if e > maxErr {
					maxErr = e
				}
			}
		}
		bound := (maxErr8x8Intra[qp] + 20) * 3
		if maxErr > bound {
			t.Errorf("custom scaling list qp=%d: max abs error = %d, want <= %d", qp, maxErr, bound)
		}
	}
}

func TestQuantDequant8x8NonzeroCountNonIncreasingWithQP(t *testing.T) {
	rng := newRNG8x8(6000)
	qs := BuildQuantScale8x8(FlatWeightScale8x8)
	countNonzero := func(b Block8x8) int {
		n := 0
		for _, v := range b {
			if v != 0 {
				n++
			}
		}
		return n
	}
	for trial := 0; trial < 100; trial++ {
		orig := randBlock8x8(rng)
		prevCount := 65
		for qp := 0; qp < 52; qp++ {
			b := orig
			Forward8x8(&b)
			Quant8x8(&b, qp, &qs, true)
			c := countNonzero(b)
			if c > prevCount {
				t.Fatalf("trial %d qp=%d: nonzero count %d exceeds previous QP's %d", trial, qp, c, prevCount)
			}
			prevCount = c
		}
	}
}

func TestFlatWeightScalesAreSixteen(t *testing.T) {
	for i, v := range FlatWeightScale4x4 {
		if v != 16 {
			t.Fatalf("FlatWeightScale4x4[%d] = %d, want 16", i, v)
		}
	}
	for i, v := range FlatWeightScale8x8 {
		if v != 16 {
			t.Fatalf("FlatWeightScale8x8[%d] = %d, want 16", i, v)
		}
	}
}

func TestClass8x8PosStructural(t *testing.T) {
	seen := map[uint8]int{}
	for _, c := range class8x8Pos {
		seen[c]++
	}
	if len(seen) != 6 {
		t.Fatalf("class8x8Pos uses %d distinct classes, want 6", len(seen))
	}
}

func TestBuildLevelScale8x8RandomWeightIsMonotoneInWeight(t *testing.T) {
	rng := rand.New(rand.NewSource(7000))
	var w1, w2 [64]uint8
	for i := 0; i < 64; i++ {
		w1[i] = uint8(1 + rng.Intn(100))
		w2[i] = w1[i] + uint8(1+rng.Intn(50))
	}
	ls1 := BuildLevelScale8x8(w1)
	ls2 := BuildLevelScale8x8(w2)
	for m := 0; m < 6; m++ {
		for pos := 0; pos < 64; pos++ {
			if ls2[m][pos] <= ls1[m][pos] {
				t.Fatalf("LevelScale8x8 not monotone in weight at m=%d pos=%d: %d vs %d", m, pos, ls1[m][pos], ls2[m][pos])
			}
		}
	}
}

func TestFlatBuilders4x4MatchTheUnweightedTables(t *testing.T) {
	ls := BuildLevelScale4x4(FlatWeightScale4x4)
	qs := BuildQuantScale4x4(FlatWeightScale4x4)
	for m := 0; m < 6; m++ {
		for i := 0; i < 16; i++ {
			if ls[m][i] != levelScale[m][i] {
				t.Fatalf("level scale at m %d position %d is %d, want %d",
					m, i, ls[m][i], levelScale[m][i])
			}
			if qs[m][i] != levelQuant[m][i] {
				t.Fatalf("quant scale at m %d position %d is %d, want %d",
					m, i, qs[m][i], levelQuant[m][i])
			}
		}
	}
}
