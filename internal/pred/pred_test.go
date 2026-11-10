package pred

import (
	"math/rand"
	"testing"
)

const planeSize = 64
const planeStride = 64

type neighbours struct {
	top      [8]int
	left     [4]int
	tl       int
	haveTop  bool
	haveLeft bool
	haveTL   bool
	haveTR   bool
}

func clip1Ref(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func newPlane(seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	plane := make([]byte, planeSize*planeStride)
	r.Read(plane)
	return plane
}

func blockOffset() int {
	return 20*planeStride + 20
}

func snapshotOutside(plane []byte, offset, w, h int) []byte {
	cp := make([]byte, len(plane))
	copy(cp, plane)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cp[offset+y*planeStride+x] = 0
		}
	}
	return cp
}

func checkOutsideUnchanged(t *testing.T, before, after []byte, offset, w, h int) {
	t.Helper()
	afterMasked := make([]byte, len(after))
	copy(afterMasked, after)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			afterMasked[offset+y*planeStride+x] = 0
		}
	}
	for i := range before {
		if before[i] != afterMasked[i] {
			t.Fatalf("pixel outside block changed at index %d: before=%d after=%d", i, before[i], afterMasked[i])
		}
	}
}

func extractNeighbours4x4(plane []byte, offset int, avail Availability) neighbours {
	var n neighbours
	n.haveTop = avail.Has(AvailTop)
	n.haveLeft = avail.Has(AvailLeft)
	n.haveTL = avail.Has(AvailTopLeft)
	n.haveTR = avail.Has(AvailTopRight)
	if n.haveTop {
		for i := 0; i < 4; i++ {
			n.top[i] = int(plane[offset-planeStride+i])
		}
		if n.haveTR {
			for i := 4; i < 8; i++ {
				n.top[i] = int(plane[offset-planeStride+i])
			}
		} else {
			for i := 4; i < 8; i++ {
				n.top[i] = n.top[3]
			}
		}
	}
	if n.haveLeft {
		for i := 0; i < 4; i++ {
			n.left[i] = int(plane[offset+i*planeStride-1])
		}
	}
	if n.haveTL {
		n.tl = int(plane[offset-planeStride-1])
	}
	return n
}

func refTopAt(n neighbours, i int) int {
	if i == -1 {
		return n.tl
	}
	return n.top[i]
}

func refLeftAt(n neighbours, j int) int {
	if j == -1 {
		return n.tl
	}
	return n.left[j]
}

func ref4x4(mode int, n neighbours) [4][4]int {
	var out [4][4]int
	switch mode {
	case I4x4Vertical:
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				out[y][x] = n.top[x]
			}
		}
	case I4x4Horizontal:
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				out[y][x] = n.left[y]
			}
		}
	case I4x4DC:
		var dc int
		switch {
		case n.haveLeft && n.haveTop:
			sum := n.top[0] + n.top[1] + n.top[2] + n.top[3] + n.left[0] + n.left[1] + n.left[2] + n.left[3]
			dc = (sum + 4) >> 3
		case n.haveLeft:
			sum := n.left[0] + n.left[1] + n.left[2] + n.left[3]
			dc = (sum + 2) >> 2
		case n.haveTop:
			sum := n.top[0] + n.top[1] + n.top[2] + n.top[3]
			dc = (sum + 2) >> 2
		default:
			dc = 128
		}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				out[y][x] = dc
			}
		}
	case I4x4DiagonalDownLeft:
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				if x == 3 && y == 3 {
					out[y][x] = (n.top[6] + 3*n.top[7] + 2) >> 2
				} else {
					s := x + y
					out[y][x] = (n.top[s] + 2*n.top[s+1] + n.top[s+2] + 2) >> 2
				}
			}
		}
	case I4x4DiagonalDownRight:
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				switch {
				case x > y:
					d := x - y
					out[y][x] = (refTopAt(n, d-2) + 2*refTopAt(n, d-1) + refTopAt(n, d) + 2) >> 2
				case x < y:
					d := y - x
					out[y][x] = (refLeftAt(n, d-2) + 2*refLeftAt(n, d-1) + refLeftAt(n, d) + 2) >> 2
				default:
					out[y][x] = (n.top[0] + 2*n.tl + n.left[0] + 2) >> 2
				}
			}
		}
	case I4x4VerticalRight:
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				zVR := 2*x - y
				switch {
				case zVR == 0 || zVR == 2 || zVR == 4 || zVR == 6:
					idx := x - (y >> 1) - 1
					out[y][x] = (refTopAt(n, idx) + refTopAt(n, idx+1) + 1) >> 1
				case zVR == 1 || zVR == 3 || zVR == 5:
					idx := x - (y >> 1) - 2
					out[y][x] = (refTopAt(n, idx) + 2*refTopAt(n, idx+1) + refTopAt(n, idx+2) + 2) >> 2
				case zVR == -1:
					out[y][x] = (refLeftAt(n, 0) + 2*n.tl + n.top[0] + 2) >> 2
				default:
					out[y][x] = (refLeftAt(n, y-1) + 2*refLeftAt(n, y-2) + refLeftAt(n, y-3) + 2) >> 2
				}
			}
		}
	case I4x4HorizontalDown:
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				zHD := 2*y - x
				switch {
				case zHD == 0 || zHD == 2 || zHD == 4 || zHD == 6:
					idx := y - (x >> 1) - 1
					out[y][x] = (refLeftAt(n, idx) + refLeftAt(n, idx+1) + 1) >> 1
				case zHD == 1 || zHD == 3 || zHD == 5:
					idx := y - (x >> 1) - 2
					out[y][x] = (refLeftAt(n, idx) + 2*refLeftAt(n, idx+1) + refLeftAt(n, idx+2) + 2) >> 2
				case zHD == -1:
					out[y][x] = (refLeftAt(n, 0) + 2*n.tl + n.top[0] + 2) >> 2
				default:
					out[y][x] = (refTopAt(n, x-1) + 2*refTopAt(n, x-2) + refTopAt(n, x-3) + 2) >> 2
				}
			}
		}
	case I4x4VerticalLeft:
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				if y == 0 || y == 2 {
					idx := x + (y >> 1)
					out[y][x] = (n.top[idx] + n.top[idx+1] + 1) >> 1
				} else {
					idx := x + (y >> 1)
					out[y][x] = (n.top[idx] + 2*n.top[idx+1] + n.top[idx+2] + 2) >> 2
				}
			}
		}
	case I4x4HorizontalUp:
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				zHU := x + 2*y
				switch {
				case zHU == 0 || zHU == 2 || zHU == 4:
					idx := y + (x >> 1)
					out[y][x] = (n.left[idx] + n.left[idx+1] + 1) >> 1
				case zHU == 1 || zHU == 3:
					idx := y + (x >> 1)
					out[y][x] = (n.left[idx] + 2*n.left[idx+1] + n.left[idx+2] + 2) >> 2
				case zHU == 5:
					out[y][x] = (n.left[2] + 3*n.left[3] + 2) >> 2
				default:
					out[y][x] = n.left[3]
				}
			}
		}
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			out[y][x] = clip1Ref(out[y][x])
		}
	}
	return out
}

func allAvailCombos() []Availability {
	var out []Availability
	for i := 0; i < 16; i++ {
		out = append(out, Availability(i))
	}
	return out
}

func TestIntra4x4AllModesAllAvail(t *testing.T) {
	modes := []int{
		I4x4Vertical, I4x4Horizontal, I4x4DC, I4x4DiagonalDownLeft,
		I4x4DiagonalDownRight, I4x4VerticalRight, I4x4HorizontalDown,
		I4x4VerticalLeft, I4x4HorizontalUp,
	}
	offset := blockOffset()
	for _, mode := range modes {
		for _, avail := range allAvailCombos() {
			if !Intra4x4ModeAvailable(mode, avail) {
				continue
			}
			for iter := 0; iter < 50; iter++ {
				seed := int64(mode)*10000 + int64(avail)*100 + int64(iter)
				plane := newPlane(seed)
				n := extractNeighbours4x4(plane, offset, avail)
				before := snapshotOutside(plane, offset, 4, 4)
				Intra4x4(plane, planeStride, offset, mode, avail)
				want := ref4x4(mode, n)
				for y := 0; y < 4; y++ {
					for x := 0; x < 4; x++ {
						got := int(plane[offset+y*planeStride+x])
						if got != want[y][x] {
							t.Fatalf("mode=%d avail=%d iter=%d x=%d y=%d got=%d want=%d", mode, avail, iter, x, y, got, want[y][x])
						}
					}
				}
				checkOutsideUnchanged(t, before, plane, offset, 4, 4)
			}
		}
	}
}

func TestIntra4x4TopRightSubstitution(t *testing.T) {
	offset := blockOffset()
	avail := AvailTop | AvailLeft | AvailTopLeft
	for iter := 0; iter < 20; iter++ {
		plane := newPlane(int64(9000 + iter))
		plane[offset-planeStride+3] = byte(50 + iter)
		for i := 4; i < 8; i++ {
			plane[offset-planeStride+i] = byte(200 + i)
		}
		n := extractNeighbours4x4(plane, offset, avail)
		if n.top[4] != n.top[3] || n.top[5] != n.top[3] || n.top[6] != n.top[3] || n.top[7] != n.top[3] {
			t.Fatalf("extractNeighbours4x4 did not substitute top-right correctly: %+v", n.top)
		}
		for _, mode := range []int{I4x4DiagonalDownLeft, I4x4VerticalLeft} {
			plane2 := make([]byte, len(plane))
			copy(plane2, plane)
			Intra4x4(plane2, planeStride, offset, mode, avail)
			want := ref4x4(mode, n)
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					got := int(plane2[offset+y*planeStride+x])
					if got != want[y][x] {
						t.Fatalf("mode=%d iter=%d x=%d y=%d got=%d want=%d (substitution mismatch)", mode, iter, x, y, got, want[y][x])
					}
				}
			}
		}
	}
}

type neighbours16 struct {
	top      [16]int
	left     [16]int
	tl       int
	haveTop  bool
	haveLeft bool
	haveTL   bool
}

func extractNeighbours16(plane []byte, offset int, avail Availability) neighbours16 {
	var n neighbours16
	n.haveTop = avail.Has(AvailTop)
	n.haveLeft = avail.Has(AvailLeft)
	n.haveTL = avail.Has(AvailTopLeft)
	if n.haveTop {
		for i := 0; i < 16; i++ {
			n.top[i] = int(plane[offset-planeStride+i])
		}
	}
	if n.haveLeft {
		for i := 0; i < 16; i++ {
			n.left[i] = int(plane[offset+i*planeStride-1])
		}
	}
	if n.haveTL {
		n.tl = int(plane[offset-planeStride-1])
	}
	return n
}

func refTopAt16(n neighbours16, i int) int {
	if i == -1 {
		return n.tl
	}
	return n.top[i]
}

func refLeftAt16(n neighbours16, j int) int {
	if j == -1 {
		return n.tl
	}
	return n.left[j]
}

func ref16x16(mode int, n neighbours16) [16][16]int {
	var out [16][16]int
	switch mode {
	case I16x16Vertical:
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				out[y][x] = n.top[x]
			}
		}
	case I16x16Horizontal:
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				out[y][x] = n.left[y]
			}
		}
	case I16x16DC:
		var dc int
		switch {
		case n.haveLeft && n.haveTop:
			sum := 0
			for i := 0; i < 16; i++ {
				sum += n.top[i] + n.left[i]
			}
			dc = (sum + 16) >> 5
		case n.haveTop:
			sum := 0
			for i := 0; i < 16; i++ {
				sum += n.top[i]
			}
			dc = (sum + 8) >> 4
		case n.haveLeft:
			sum := 0
			for i := 0; i < 16; i++ {
				sum += n.left[i]
			}
			dc = (sum + 8) >> 4
		default:
			dc = 128
		}
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				out[y][x] = dc
			}
		}
	case I16x16Plane:
		H := 0
		for i := 0; i < 8; i++ {
			H += (i + 1) * (refTopAt16(n, 8+i) - refTopAt16(n, 6-i))
		}
		V := 0
		for j := 0; j < 8; j++ {
			V += (j + 1) * (refLeftAt16(n, 8+j) - refLeftAt16(n, 6-j))
		}
		a := 16 * (refLeftAt16(n, 15) + refTopAt16(n, 15))
		b := (5*H + 32) >> 6
		c := (5*V + 32) >> 6
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				out[y][x] = (a + b*(x-7) + c*(y-7) + 16) >> 5
			}
		}
	}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			out[y][x] = clip1Ref(out[y][x])
		}
	}
	return out
}

func TestIntra16x16AllModesAllAvail(t *testing.T) {
	modes := []int{I16x16Vertical, I16x16Horizontal, I16x16DC, I16x16Plane}
	offset := blockOffset()
	for _, mode := range modes {
		for _, avail := range allAvailCombos() {
			if !Intra16x16ModeAvailable(mode, avail) {
				continue
			}
			for iter := 0; iter < 30; iter++ {
				seed := int64(mode)*10000 + int64(avail)*100 + int64(iter) + 500000
				plane := newPlane(seed)
				n := extractNeighbours16(plane, offset, avail)
				before := snapshotOutside(plane, offset, 16, 16)
				Intra16x16(plane, planeStride, offset, mode, avail)
				want := ref16x16(mode, n)
				for y := 0; y < 16; y++ {
					for x := 0; x < 16; x++ {
						got := int(plane[offset+y*planeStride+x])
						if got != want[y][x] {
							t.Fatalf("mode=%d avail=%d iter=%d x=%d y=%d got=%d want=%d", mode, avail, iter, x, y, got, want[y][x])
						}
					}
				}
				checkOutsideUnchanged(t, before, plane, offset, 16, 16)
			}
		}
	}
}

func TestIntra16x16PlaneExtremes(t *testing.T) {
	offset := blockOffset()
	avail := AvailTop | AvailLeft | AvailTopLeft
	sizes := planeSize * planeStride
	cases := [][]byte{}

	allZero := make([]byte, sizes)
	cases = append(cases, allZero)

	allMax := make([]byte, sizes)
	for i := range allMax {
		allMax[i] = 255
	}
	cases = append(cases, allMax)

	gradient := make([]byte, sizes)
	for y := 0; y < planeSize; y++ {
		for x := 0; x < planeSize; x++ {
			gradient[y*planeStride+x] = byte((x * 8) % 256)
		}
	}
	cases = append(cases, gradient)

	gradient2 := make([]byte, sizes)
	for y := 0; y < planeSize; y++ {
		for x := 0; x < planeSize; x++ {
			v := 255 - ((x + y) * 4 % 256)
			gradient2[y*planeStride+x] = byte(v)
		}
	}
	cases = append(cases, gradient2)

	clipped := 0
	for ci, plane := range cases {
		n := extractNeighbours16(plane, offset, avail)
		Intra16x16(plane, planeStride, offset, I16x16Plane, avail)
		want := ref16x16(I16x16Plane, n)
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				got := int(plane[offset+y*planeStride+x])
				if got != want[y][x] {
					t.Fatalf("case %d at %d,%d: got %d, want %d", ci, x, y, got, want[y][x])
				}
				if raw := plane16x16Unclipped(n, x, y); raw < 0 || raw > 255 {
					clipped++
				}
			}
		}
	}
	if clipped == 0 {
		t.Fatal("no sample needed clipping, the test does not exercise Clip1")
	}
	t.Logf("%d samples were clipped", clipped)
}

func plane16x16Unclipped(n neighbours16, x, y int) int {
	h, v := 0, 0
	for i := 0; i < 8; i++ {
		h += (i + 1) * (n.top[8+i] - refTopAt16(n, 6-i))
		v += (i + 1) * (n.left[8+i] - refLeftAt16(n, 6-i))
	}
	a := 16 * (n.left[15] + n.top[15])
	b := (5*h + 32) >> 6
	c := (5*v + 32) >> 6
	return (a + b*(x-7) + c*(y-7) + 16) >> 5
}

func TestChromaPlaneExtremes(t *testing.T) {
	offset := blockOffset()
	avail := AvailTop | AvailLeft | AvailTopLeft
	sizes := planeSize * planeStride
	cases := [][]byte{}

	allZero := make([]byte, sizes)
	cases = append(cases, allZero)

	allMax := make([]byte, sizes)
	for i := range allMax {
		allMax[i] = 255
	}
	cases = append(cases, allMax)

	gradient := make([]byte, sizes)
	for y := 0; y < planeSize; y++ {
		for x := 0; x < planeSize; x++ {
			gradient[y*planeStride+x] = byte((x * 16) % 256)
		}
	}
	cases = append(cases, gradient)

	gradient2 := make([]byte, sizes)
	for y := 0; y < planeSize; y++ {
		for x := 0; x < planeSize; x++ {
			v := 255 - ((x + y) * 8 % 256)
			gradient2[y*planeStride+x] = byte(v)
		}
	}
	cases = append(cases, gradient2)

	steep := make([]byte, sizes)
	for i := range steep {
		steep[i] = 128
	}
	for i := 0; i < 8; i++ {
		steep[offset-planeStride+i] = byte(i * 36)
		steep[offset+i*planeStride-1] = byte(255 - i*36)
	}
	steep[offset-planeStride-1] = 128
	cases = append(cases, steep)

	clipped := 0
	for ci, plane := range cases {
		n := extractNeighboursChroma(plane, offset, avail)
		IntraChroma8x8(plane, planeStride, offset, ChromaPlane, avail)
		want := refChroma(ChromaPlane, n)
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				got := int(plane[offset+y*planeStride+x])
				if got != want[y][x] {
					t.Fatalf("case %d at %d,%d: got %d, want %d", ci, x, y, got, want[y][x])
				}
				if raw := planeChromaUnclipped(n, x, y); raw < 0 || raw > 255 {
					clipped++
				}
			}
		}
	}
	if clipped == 0 {
		t.Fatal("no sample needed clipping, the test does not exercise Clip1")
	}
	t.Logf("%d samples were clipped", clipped)
}

func planeChromaUnclipped(n neighboursChroma, x, y int) int {
	h, v := 0, 0
	for i := 0; i < 4; i++ {
		h += (i + 1) * (n.top[4+i] - refTopAtC(n, 2-i))
		v += (i + 1) * (n.left[4+i] - refLeftAtC(n, 2-i))
	}
	a := 16 * (n.left[7] + n.top[7])
	b := (34*h + 32) >> 6
	c := (34*v + 32) >> 6
	return (a + b*(x-3) + c*(y-3) + 16) >> 5
}

type neighboursChroma struct {
	top      [8]int
	left     [8]int
	tl       int
	haveTop  bool
	haveLeft bool
	haveTL   bool
}

func extractNeighboursChroma(plane []byte, offset int, avail Availability) neighboursChroma {
	var n neighboursChroma
	n.haveTop = avail.Has(AvailTop)
	n.haveLeft = avail.Has(AvailLeft)
	n.haveTL = avail.Has(AvailTopLeft)
	if n.haveTop {
		for i := 0; i < 8; i++ {
			n.top[i] = int(plane[offset-planeStride+i])
		}
	}
	if n.haveLeft {
		for i := 0; i < 8; i++ {
			n.left[i] = int(plane[offset+i*planeStride-1])
		}
	}
	if n.haveTL {
		n.tl = int(plane[offset-planeStride-1])
	}
	return n
}

func refTopAtC(n neighboursChroma, i int) int {
	if i == -1 {
		return n.tl
	}
	return n.top[i]
}

func refLeftAtC(n neighboursChroma, j int) int {
	if j == -1 {
		return n.tl
	}
	return n.left[j]
}

func refChromaDC(n neighboursChroma) [8][8]int {
	var out [8][8]int
	positions := [4][2]int{{0, 0}, {4, 0}, {0, 4}, {4, 4}}
	for _, pos := range positions {
		xO, yO := pos[0], pos[1]
		sumTop := 0
		for i := 0; i < 4; i++ {
			sumTop += n.top[xO+i]
		}
		sumLeft := 0
		for j := 0; j < 4; j++ {
			sumLeft += n.left[yO+j]
		}
		var dc int
		switch {
		case (xO == 0 && yO == 0) || (xO > 0 && yO > 0):
			switch {
			case n.haveTop && n.haveLeft:
				dc = (sumTop + sumLeft + 4) >> 3
			case n.haveTop:
				dc = (sumTop + 2) >> 2
			case n.haveLeft:
				dc = (sumLeft + 2) >> 2
			default:
				dc = 128
			}
		case xO > 0 && yO == 0:
			switch {
			case n.haveTop:
				dc = (sumTop + 2) >> 2
			case n.haveLeft:
				dc = (sumLeft + 2) >> 2
			default:
				dc = 128
			}
		default:
			switch {
			case n.haveLeft:
				dc = (sumLeft + 2) >> 2
			case n.haveTop:
				dc = (sumTop + 2) >> 2
			default:
				dc = 128
			}
		}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				out[yO+y][xO+x] = dc
			}
		}
	}
	return out
}

func refChroma(mode int, n neighboursChroma) [8][8]int {
	var out [8][8]int
	switch mode {
	case ChromaDC:
		out = refChromaDC(n)
	case ChromaHorizontal:
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				out[y][x] = n.left[y]
			}
		}
	case ChromaVertical:
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				out[y][x] = n.top[x]
			}
		}
	case ChromaPlane:
		H := 0
		for i := 0; i < 4; i++ {
			H += (i + 1) * (refTopAtC(n, 4+i) - refTopAtC(n, 2-i))
		}
		V := 0
		for j := 0; j < 4; j++ {
			V += (j + 1) * (refLeftAtC(n, 4+j) - refLeftAtC(n, 2-j))
		}
		a := 16 * (refLeftAtC(n, 7) + refTopAtC(n, 7))
		b := (34*H + 32) >> 6
		c := (34*V + 32) >> 6
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				out[y][x] = (a + b*(x-3) + c*(y-3) + 16) >> 5
			}
		}
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			out[y][x] = clip1Ref(out[y][x])
		}
	}
	return out
}

func TestChromaAllModesAllAvail(t *testing.T) {
	modes := []int{ChromaDC, ChromaHorizontal, ChromaVertical, ChromaPlane}
	offset := blockOffset()
	for _, mode := range modes {
		for _, avail := range allAvailCombos() {
			if !ChromaModeAvailable(mode, avail) {
				continue
			}
			for iter := 0; iter < 30; iter++ {
				seed := int64(mode)*10000 + int64(avail)*100 + int64(iter) + 900000
				plane := newPlane(seed)
				n := extractNeighboursChroma(plane, offset, avail)
				before := snapshotOutside(plane, offset, 8, 8)
				IntraChroma8x8(plane, planeStride, offset, mode, avail)
				want := refChroma(mode, n)
				for y := 0; y < 8; y++ {
					for x := 0; x < 8; x++ {
						got := int(plane[offset+y*planeStride+x])
						if got != want[y][x] {
							t.Fatalf("mode=%d avail=%d iter=%d x=%d y=%d got=%d want=%d", mode, avail, iter, x, y, got, want[y][x])
						}
					}
				}
				checkOutsideUnchanged(t, before, plane, offset, 8, 8)
			}
		}
	}
}

func TestChromaDCSubBlockPositions(t *testing.T) {
	offset := blockOffset()
	for _, avail := range allAvailCombos() {
		for iter := 0; iter < 20; iter++ {
			seed := int64(avail)*1000 + int64(iter) + 700000
			plane := newPlane(seed)
			n := extractNeighboursChroma(plane, offset, avail)
			IntraChroma8x8(plane, planeStride, offset, ChromaDC, avail)
			want := refChromaDC(n)
			positions := [4][2]int{{0, 0}, {4, 0}, {0, 4}, {4, 4}}
			for _, pos := range positions {
				xO, yO := pos[0], pos[1]
				for y := 0; y < 4; y++ {
					for x := 0; x < 4; x++ {
						got := int(plane[offset+(yO+y)*planeStride+(xO+x)])
						if got != want[yO+y][xO+x] {
							t.Fatalf("avail=%d quadrant=%v x=%d y=%d got=%d want=%d", avail, pos, x, y, got, want[yO+y][xO+x])
						}
					}
				}
			}
		}
	}
}

func TestDCFallbackTo128(t *testing.T) {
	offset := blockOffset()
	plane := newPlane(1)
	Intra4x4(plane, planeStride, offset, I4x4DC, 0)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if plane[offset+y*planeStride+x] != 128 {
				t.Fatalf("intra4x4 dc fallback: got %d want 128", plane[offset+y*planeStride+x])
			}
		}
	}

	plane2 := newPlane(2)
	Intra16x16(plane2, planeStride, offset, I16x16DC, 0)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if plane2[offset+y*planeStride+x] != 128 {
				t.Fatalf("intra16x16 dc fallback: got %d want 128", plane2[offset+y*planeStride+x])
			}
		}
	}

	plane3 := newPlane(3)
	IntraChroma8x8(plane3, planeStride, offset, ChromaDC, 0)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if plane3[offset+y*planeStride+x] != 128 {
				t.Fatalf("chroma dc fallback: got %d want 128", plane3[offset+y*planeStride+x])
			}
		}
	}
}

func fillPlaneConst(v byte) []byte {
	plane := make([]byte, planeSize*planeStride)
	for i := range plane {
		plane[i] = v
	}
	return plane
}

func TestGoldenAllSameValue(t *testing.T) {
	offset := blockOffset()
	avail := AvailTop | AvailLeft | AvailTopLeft | AvailTopRight

	for _, mode := range []int{I4x4Vertical, I4x4Horizontal, I4x4DC, I4x4DiagonalDownLeft, I4x4DiagonalDownRight, I4x4VerticalLeft, I4x4HorizontalUp} {
		plane := fillPlaneConst(100)
		Intra4x4(plane, planeStride, offset, mode, avail)
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				if plane[offset+y*planeStride+x] != 100 {
					t.Fatalf("mode=%d all-100 golden failed at x=%d y=%d got=%d", mode, x, y, plane[offset+y*planeStride+x])
				}
			}
		}
	}

	for _, mode := range []int{I16x16Vertical, I16x16Horizontal, I16x16DC, I16x16Plane} {
		plane := fillPlaneConst(100)
		Intra16x16(plane, planeStride, offset, mode, avail)
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				if plane[offset+y*planeStride+x] != 100 {
					t.Fatalf("mode=%d all-100 golden failed at x=%d y=%d got=%d", mode, x, y, plane[offset+y*planeStride+x])
				}
			}
		}
	}

	for _, mode := range []int{ChromaDC, ChromaHorizontal, ChromaVertical, ChromaPlane} {
		plane := fillPlaneConst(100)
		IntraChroma8x8(plane, planeStride, offset, mode, avail)
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				if plane[offset+y*planeStride+x] != 100 {
					t.Fatalf("mode=%d all-100 golden failed at x=%d y=%d got=%d", mode, x, y, plane[offset+y*planeStride+x])
				}
			}
		}
	}
}

func TestGoldenHorizontalRamp4x4(t *testing.T) {
	offset := blockOffset()
	plane := fillPlaneConst(0)
	for x := 0; x < 8; x++ {
		plane[offset-planeStride+x] = byte(10 * (x + 1))
	}
	for y := 0; y < 4; y++ {
		plane[offset+y*planeStride-1] = 50
	}
	plane[offset-planeStride-1] = 5

	avail := AvailTop | AvailLeft | AvailTopLeft | AvailTopRight
	Intra4x4(plane, planeStride, offset, I4x4Vertical, avail)
	expected := [4]int{10, 20, 30, 40}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if int(plane[offset+y*planeStride+x]) != expected[x] {
				t.Fatalf("vertical ramp mismatch at x=%d y=%d got=%d want=%d", x, y, plane[offset+y*planeStride+x], expected[x])
			}
		}
	}
}

func TestGoldenVerticalRamp4x4(t *testing.T) {
	offset := blockOffset()
	plane := fillPlaneConst(0)
	for y := 0; y < 4; y++ {
		plane[offset+y*planeStride-1] = byte(10 * (y + 1))
	}
	for x := 0; x < 8; x++ {
		plane[offset-planeStride+x] = 50
	}
	plane[offset-planeStride-1] = 5

	avail := AvailTop | AvailLeft | AvailTopLeft | AvailTopRight
	Intra4x4(plane, planeStride, offset, I4x4Horizontal, avail)
	expected := [4]int{10, 20, 30, 40}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if int(plane[offset+y*planeStride+x]) != expected[y] {
				t.Fatalf("horizontal ramp mismatch at x=%d y=%d got=%d want=%d", x, y, plane[offset+y*planeStride+x], expected[y])
			}
		}
	}
}

func TestIntra4x4ModeAvailableExhaustive(t *testing.T) {
	modes := []int{
		I4x4Vertical, I4x4Horizontal, I4x4DC, I4x4DiagonalDownLeft,
		I4x4DiagonalDownRight, I4x4VerticalRight, I4x4HorizontalDown,
		I4x4VerticalLeft, I4x4HorizontalUp,
	}
	for _, mode := range modes {
		for _, avail := range allAvailCombos() {
			got := Intra4x4ModeAvailable(mode, avail)
			var want bool
			switch mode {
			case I4x4Vertical, I4x4DiagonalDownLeft, I4x4VerticalLeft:
				want = avail.Has(AvailTop)
			case I4x4Horizontal, I4x4HorizontalUp:
				want = avail.Has(AvailLeft)
			case I4x4DC:
				want = true
			case I4x4DiagonalDownRight, I4x4VerticalRight, I4x4HorizontalDown:
				want = avail.Has(AvailLeft) && avail.Has(AvailTop) && avail.Has(AvailTopLeft)
			}
			if got != want {
				t.Fatalf("Intra4x4ModeAvailable(%d,%d)=%v want %v", mode, avail, got, want)
			}
		}
	}
	if Intra4x4ModeAvailable(99, AvailTop|AvailLeft|AvailTopLeft|AvailTopRight) {
		t.Fatalf("unknown mode should not be available")
	}
}

func TestIntra16x16ModeAvailableExhaustive(t *testing.T) {
	modes := []int{I16x16Vertical, I16x16Horizontal, I16x16DC, I16x16Plane}
	for _, mode := range modes {
		for _, avail := range allAvailCombos() {
			got := Intra16x16ModeAvailable(mode, avail)
			var want bool
			switch mode {
			case I16x16Vertical:
				want = avail.Has(AvailTop)
			case I16x16Horizontal:
				want = avail.Has(AvailLeft)
			case I16x16DC:
				want = true
			case I16x16Plane:
				want = avail.Has(AvailLeft) && avail.Has(AvailTop) && avail.Has(AvailTopLeft)
			}
			if got != want {
				t.Fatalf("Intra16x16ModeAvailable(%d,%d)=%v want %v", mode, avail, got, want)
			}
		}
	}
	if Intra16x16ModeAvailable(99, AvailTop|AvailLeft|AvailTopLeft|AvailTopRight) {
		t.Fatalf("unknown mode should not be available")
	}
}

func TestChromaModeAvailableExhaustive(t *testing.T) {
	modes := []int{ChromaDC, ChromaHorizontal, ChromaVertical, ChromaPlane}
	for _, mode := range modes {
		for _, avail := range allAvailCombos() {
			got := ChromaModeAvailable(mode, avail)
			var want bool
			switch mode {
			case ChromaDC:
				want = true
			case ChromaHorizontal:
				want = avail.Has(AvailLeft)
			case ChromaVertical:
				want = avail.Has(AvailTop)
			case ChromaPlane:
				want = avail.Has(AvailLeft) && avail.Has(AvailTop) && avail.Has(AvailTopLeft)
			}
			if got != want {
				t.Fatalf("ChromaModeAvailable(%d,%d)=%v want %v", mode, avail, got, want)
			}
		}
	}
	if ChromaModeAvailable(99, AvailTop|AvailLeft|AvailTopLeft|AvailTopRight) {
		t.Fatalf("unknown mode should not be available")
	}
}

func TestAvailabilityHas(t *testing.T) {
	a := AvailLeft | AvailTopRight
	if !a.Has(AvailLeft) {
		t.Fatalf("expected AvailLeft set")
	}
	if !a.Has(AvailTopRight) {
		t.Fatalf("expected AvailTopRight set")
	}
	if a.Has(AvailTop) {
		t.Fatalf("did not expect AvailTop set")
	}
	if a.Has(AvailTopLeft) {
		t.Fatalf("did not expect AvailTopLeft set")
	}
	if !a.Has(AvailLeft | AvailTopRight) {
		t.Fatalf("expected combined flags set")
	}
}

func BenchmarkIntra4x4DC(b *testing.B) {
	offset := blockOffset()
	plane := newPlane(42)
	avail := AvailTop | AvailLeft | AvailTopLeft | AvailTopRight
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Intra4x4(plane, planeStride, offset, I4x4DC, avail)
	}
}

func BenchmarkIntra16x16Plane(b *testing.B) {
	offset := blockOffset()
	plane := newPlane(43)
	avail := AvailTop | AvailLeft | AvailTopLeft
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Intra16x16(plane, planeStride, offset, I16x16Plane, avail)
	}
}

func BenchmarkIntraChroma8x8Plane(b *testing.B) {
	offset := blockOffset()
	plane := newPlane(44)
	avail := AvailTop | AvailLeft | AvailTopLeft
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IntraChroma8x8(plane, planeStride, offset, ChromaPlane, avail)
	}
}
