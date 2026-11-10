package deblock

import (
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/simd"
)

func referenceLumaEdgeHorizontal(plane []byte, stride, offset int, bS [4]uint8, indexA, indexB int) {
	filterEdge(plane, offset, 1, stride, 16, 4, bS, indexA, indexB, false)
}

func referenceLumaEdgeVertical(plane []byte, stride, offset int, bS [4]uint8, indexA, indexB int) {
	filterEdge(plane, offset, stride, 1, 16, 4, bS, indexA, indexB, false)
}

func randomPlane(r *rand.Rand, stride, rows, spread int) []byte {
	p := make([]byte, stride*rows)
	base := r.Intn(256)
	for i := range p {
		v := base + r.Intn(2*spread+1) - spread
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		p[i] = byte(v)
	}
	return p
}

func TestAcceleratedVerticalLumaEdgeMatchesTheScalarFilter(t *testing.T) {
	if !simd.Accelerated() {
		t.Skip("no accelerated kernels on this machine")
	}
	const stride, rows = 64, 40
	r := rand.New(rand.NewSource(7))
	tried := 0
	for round := 0; round < 40000; round++ {
		spread := []int{2, 6, 16, 64, 255}[r.Intn(5)]
		plane := randomPlane(r, stride, rows, spread)
		offset := 8*stride + 8 + r.Intn(stride-24)
		indexA := r.Intn(52)
		indexB := r.Intn(52)
		var bS [4]uint8
		for i := range bS {
			bS[i] = uint8(r.Intn(5))
		}

		want := append([]byte(nil), plane...)
		referenceLumaEdgeVertical(want, stride, offset, bS, indexA, indexB)

		got := append([]byte(nil), plane...)
		if !acceleratedLumaEdgeVertical(got, offset, stride, bS, indexA, indexB) {
			continue
		}
		tried++
		for i := range want {
			if want[i] != got[i] {
				t.Fatalf("round %d: bS %v indexA %d indexB %d spread %d: byte %d (row %d, column %d) is %d, the scalar filter gives %d",
					round, bS, indexA, indexB, spread, i, i/stride, i%stride, got[i], want[i])
			}
		}
	}
	if tried < 5000 {
		t.Fatalf("only %d rounds reached an accelerated kernel", tried)
	}
	t.Logf("%d vertical rounds matched the scalar filter byte for byte", tried)
}

func TestAcceleratedLumaEdgeMatchesTheScalarFilter(t *testing.T) {
	if !simd.Accelerated() {
		t.Skip("no accelerated kernels on this machine")
	}
	const stride, rows = 64, 24
	r := rand.New(rand.NewSource(1))
	tried := 0
	for round := 0; round < 40000; round++ {
		spread := []int{2, 6, 16, 64, 255}[r.Intn(5)]
		plane := randomPlane(r, stride, rows, spread)
		offset := 8*stride + r.Intn(stride-16)
		indexA := r.Intn(52)
		indexB := r.Intn(52)
		var bS [4]uint8
		switch r.Intn(3) {
		case 0:
			for i := range bS {
				bS[i] = 4
			}
		case 1:
			for i := range bS {
				bS[i] = uint8(r.Intn(4))
			}
		default:
			for i := range bS {
				bS[i] = uint8(r.Intn(5))
			}
		}

		want := append([]byte(nil), plane...)
		referenceLumaEdgeHorizontal(want, stride, offset, bS, indexA, indexB)

		got := append([]byte(nil), plane...)
		if !acceleratedLumaEdge(got, offset, stride, bS, indexA, indexB) {
			continue
		}
		tried++
		for i := range want {
			if want[i] != got[i] {
				line := (i - offset) % stride
				t.Fatalf("round %d: bS %v indexA %d indexB %d spread %d: byte %d (line %d, row %d) is %d, the scalar filter gives %d",
					round, bS, indexA, indexB, spread, i, line, (i-offset)/stride, got[i], want[i])
			}
		}
	}
	if tried < 10000 {
		t.Fatalf("only %d rounds reached an accelerated kernel", tried)
	}
	t.Logf("%d rounds matched the scalar filter byte for byte", tried)
}
