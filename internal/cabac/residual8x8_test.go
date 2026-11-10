package cabac

import (
	"math/rand"
	"testing"
)

func surjectiveOnto(t *testing.T, name string, vals []uint8, n int) {
	t.Helper()
	seen := make([]bool, n)
	for i, v := range vals {
		if int(v) >= n {
			t.Fatalf("%s[%d] = %d, out of range [0,%d)", name, i, v, n)
		}
		seen[v] = true
	}
	for i, ok := range seen {
		if !ok {
			t.Fatalf("%s never selects context %d", name, i)
		}
	}
}

func TestSignificanceOffsets8x8CoverTheirContexts(t *testing.T) {
	surjectiveOnto(t, "significantCoeffFlagOffset8x8", significantCoeffFlagOffset8x8[:], 15)
	surjectiveOnto(t, "lastCoeffFlagOffset8x8", lastCoeffFlagOffset8x8[:], 9)
}

func TestLastCoeffFlagOffset8x8IsMonotonic(t *testing.T) {
	for i := 1; i < len(lastCoeffFlagOffset8x8); i++ {
		if lastCoeffFlagOffset8x8[i] < lastCoeffFlagOffset8x8[i-1] {
			t.Fatalf("lastCoeffFlagOffset8x8 decreases at %d: %d then %d",
				i, lastCoeffFlagOffset8x8[i-1], lastCoeffFlagOffset8x8[i])
		}
	}
}

func TestResidual8x8ContextIndicesInRange(t *testing.T) {
	for i, v := range significantCoeffFlagOffset8x8 {
		if idx := offSignificant8x8 + int(v); idx >= NumContexts {
			t.Fatalf("significance context %d for scan position %d out of range", idx, i)
		}
	}
	for i, v := range lastCoeffFlagOffset8x8 {
		if idx := offLastSignificant8x8 + int(v); idx >= NumContexts {
			t.Fatalf("last-significance context %d for scan position %d out of range", idx, i)
		}
	}
	for numGt1 := 0; numGt1 <= 8; numGt1++ {
		for numEq1 := 0; numEq1 <= 8; numEq1++ {
			for binIdx := 0; binIdx < 14; binIdx++ {
				idx := absLevelIncAt(offAbsLevel8x8, 4, binIdx, numGt1, numEq1)
				if idx < offAbsLevel8x8 || idx >= offAbsLevel8x8+10 {
					t.Fatalf("level context %d outside the ten contexts of category 5", idx)
				}
			}
		}
	}
}

func TestResidual8x8DoesNotOverlapSmallerCategories(t *testing.T) {
	last4x4 := offAbsLevel + catOffsetAbsLevel[CatChromaAC] + 10
	if offSignificant8x8 < last4x4 {
		t.Fatalf("category 5 contexts start at %d, inside the 4x4 range ending at %d", offSignificant8x8, last4x4)
	}
	if offLastSignificant8x8 < offSignificant8x8+15 {
		t.Fatalf("last-significance contexts overlap the significance contexts")
	}
	if offAbsLevel8x8 < offLastSignificant8x8+9 {
		t.Fatalf("level contexts overlap the last-significance contexts")
	}
}

func checkResidual8x8RoundTrip(t *testing.T, qp int, intra bool, initIDC uint32, coeffs *[64]int32) {
	t.Helper()
	want := *coeffs
	encodeAndDecode(t, qp, intra, initIDC, func(e *Encoder) {
		e.ResidualBlock8x8(coeffs)
	}, func(d *Decoder) {
		var got [64]int32
		n := d.ResidualBlock8x8(&got)
		nonZero := 0
		for i := 0; i < 64; i++ {
			if got[i] != want[i] {
				t.Fatalf("ResidualBlock8x8 coeff[%d] = %d, want %d", i, got[i], want[i])
			}
			if want[i] != 0 {
				nonZero++
			}
		}
		if n != nonZero {
			t.Fatalf("ResidualBlock8x8 reported %d coefficients, want %d", n, nonZero)
		}
	})
}

func TestResidualBlock8x8EdgeCases(t *testing.T) {
	for _, pos := range []int{0, 1, 31, 62, 63} {
		var b [64]int32
		b[pos] = 1
		checkResidual8x8RoundTrip(t, 26, true, 0, &b)
		b[pos] = -1
		checkResidual8x8RoundTrip(t, 26, false, 1, &b)
	}

	var full [64]int32
	for i := range full {
		full[i] = int32(i - 32)
		if full[i] == 0 {
			full[i] = 1
		}
	}
	checkResidual8x8RoundTrip(t, 30, false, 2, &full)

	var big [64]int32
	big[0] = 32000
	big[63] = -32000
	checkResidual8x8RoundTrip(t, 10, true, 0, &big)
}

func TestResidualBlock8x8RandomRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(20260901))
	for i := 0; i < 300; i++ {
		var b [64]int32
		allZero := true
		for j := range b {
			switch rng.Intn(4) {
			case 0, 1:
			case 2:
				v := int32(1 + rng.Intn(3))
				if rng.Intn(2) == 0 {
					v = -v
				}
				b[j] = v
			default:
				v := int32(1 + rng.Intn(20000))
				if rng.Intn(2) == 0 {
					v = -v
				}
				b[j] = v
			}
			if b[j] != 0 {
				allZero = false
			}
		}
		if allZero {
			b[rng.Intn(64)] = 1
		}
		checkResidual8x8RoundTrip(t, rng.Intn(52), rng.Intn(2) == 0, uint32(rng.Intn(3)), &b)
	}
}

func TestTransformSize8x8FlagRoundTrip(t *testing.T) {
	for inc := 0; inc < 3; inc++ {
		for _, flag := range []bool{false, true} {
			inc, flag := inc, flag
			encodeAndDecode(t, 26, true, 0, func(e *Encoder) {
				e.TransformSize8x8Flag(inc, flag)
			}, func(d *Decoder) {
				if got := d.TransformSize8x8Flag(inc); got != flag {
					t.Fatalf("TransformSize8x8Flag(%d) = %v, want %v", inc, got, flag)
				}
			})
		}
	}
}
