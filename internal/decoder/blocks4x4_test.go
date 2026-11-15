package decoder

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/transform"
)

// addBlocks4x4 equals the three scalar stages applied block by block, for
// every qp of the range, both region sizes, random masks and both DC modes.
func TestAddBlocks4x4MatchesScalarStages(t *testing.T) {
	r := rand.New(rand.NewSource(13))
	var scale levelScale4x4
	for m := range scale {
		for i := range scale[m] {
			scale[m][i] = int32(10 + r.Intn(1200))
		}
	}
	for qp := 0; qp <= 51; qp++ {
		for _, n := range []int{4, 16} {
			size := 16
			if n == 4 {
				size = 8
			}
			for rep := 0; rep < 6; rep++ {
				var blocks, ref [16]transform.Block
				for b := range blocks {
					switch rep % 3 {
					case 0:
						blocks[b][0] = int32(r.Intn(401) - 200)
					case 1:
						for k := 0; k < 3; k++ {
							blocks[b][r.Intn(16)] = int32(r.Intn(61) - 30)
						}
					default:
						for i := range blocks[b] {
							blocks[b][i] = int32(r.Intn(41) - 20)
						}
					}
				}
				ref = blocks
				mask := uint16(r.Uint32())
				skipDC := rep%2 == 1
				stride := size + r.Intn(24)
				got := make([]byte, size*stride+8)
				for i := range got {
					got[i] = byte(r.Intn(256))
				}
				want := append([]byte(nil), got...)
				addBlocks4x4(got, stride, 3, n, &blocks, mask, qp, &scale, skipDC)
				for b := 0; b < n; b++ {
					if mask>>uint(b)&1 == 0 {
						continue
					}
					dequant4x4(&ref[b], qp, &scale, skipDC)
					transform.Inverse4x4(&ref[b])
					transform.AddResidual4x4(want, stride, 3+blockY[b]*stride+blockX[b], &ref[b])
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("qp %d n %d rep %d mask %#x: planes differ", qp, n, rep, mask)
				}
			}
		}
	}
}

// dequantLumaDC equals the Hadamard transform followed by the scaling of
// Equations 8-326 and 8-327.
func TestDequantLumaDCMatchesScalar(t *testing.T) {
	r := rand.New(rand.NewSource(14))
	var scale levelScale4x4
	for m := range scale {
		scale[m][0] = int32(10 + r.Intn(500))
	}
	for qp := 0; qp <= 51; qp++ {
		var got, want transform.Block
		for i := range got {
			got[i] = int32(r.Intn(4001) - 2000)
		}
		want = got
		dequantLumaDC(&got, qp, &scale)
		transform.Hadamard4x4(&want)
		v, shift := scale[qp%6][0], qp/6
		for i := range want {
			if shift >= 6 {
				want[i] = want[i] * v << uint(shift-6)
			} else {
				want[i] = (want[i]*v + 1<<uint(5-shift)) >> uint(6-shift)
			}
		}
		if got != want {
			t.Fatalf("qp %d: %v, want %v", qp, got, want)
		}
	}
}
