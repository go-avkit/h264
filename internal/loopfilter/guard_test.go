package loopfilter

import (
	"testing"

	"github.com/go-avkit/h264/internal/frame"
)

// failOnPanic turns a panic in f into a failure that names the guard, so a
// removed guard fails an assertion instead of crashing the suite. The calls
// below stay on the test goroutine (a 1x1 picture runs inline in Wavefront).
func failOnPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
		}
	}()
	f()
}

func TestApplyGuardsNilArguments(t *testing.T) {
	pic := buildEdgePicture(1, 1)
	at := func(x, y int) *MB { return nil }
	for _, c := range []struct {
		name string
		pic  *frame.Picture
		at   func(x, y int) *MB
	}{
		{"nil picture", nil, at},
		{"nil lookup", pic, nil},
		{"both nil", nil, nil},
		{"neither nil", pic, at},
	} {
		t.Run(c.name, func(t *testing.T) {
			failOnPanic(t, "Apply", func() { Apply(c.pic, 1, 1, c.at) })
		})
	}
}

func TestNeighbourForGuards(t *testing.T) {
	cur := makeMB(mbConfig{qpy: 30})
	decoded := makeMB(mbConfig{qpy: 30})
	notDecoded := makeMB(mbConfig{qpy: 30})
	notDecoded.Decoded = false
	for _, c := range []struct {
		name string
		n    *MB
		want *MB
	}{
		{"no macroblock", nil, nil},
		{"not decoded", notDecoded, nil},
		{"decoded", decoded, decoded},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := edgeContext{at: func(x, y int) *MB { return c.n }}
			failOnPanic(t, "neighbourFor", func() {
				if got := ctx.neighbourFor(cur, 1, 1, -1, 0); got != c.want {
					t.Errorf("neighbourFor = %p, want %p", got, c.want)
				}
			})
		})
	}
}

func TestEdgesWithoutNeighbourReturnWithoutTouchingThePicture(t *testing.T) {
	pic := buildEdgePicture(1, 1)
	y, cb, cr := snapshot(pic)
	ctx := newEdgeContext(pic, func(x, y int) *MB { return nil })
	cur := makeMB(mbConfig{intra: true, qpy: 51})
	failOnPanic(t, "verticalEdge without a left neighbour", func() {
		ctx.verticalEdge(cur, nil, 0, 0, 0, 0xffff, 0, 0)
	})
	failOnPanic(t, "horizontalEdge without a top neighbour", func() {
		ctx.horizontalEdge(cur, nil, 0, 0, 0, 0xffff, 0, 0)
	})
	if !picUnchanged(pic, y, cb, cr) {
		t.Fatal("an edge without a neighbour changed the picture")
	}
}

// Luma edges 1 and 3 have no chroma edge of their own in 4:2:0. With a
// strength above zero on every edge, a chroma row pair that moves at row 2 or
// 6 means the horizontal pass filtered chroma for an odd luma edge.
func TestHorizontalChromaOnlyAtLumaEdges0And2(t *testing.T) {
	pic := frame.NewPicture(1, 1)
	rows := func(r int) byte {
		if (r/2)%2 == 0 {
			return flatLow
		}
		return flatHigh
	}
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			pic.Cb[pic.ChromaOffset(c, r)] = rows(r)
			pic.Cr[pic.ChromaOffset(c, r)] = rows(r)
		}
	}
	mb := makeMB(mbConfig{qpy: 51, ref: refA, nz: 1})
	Apply(pic, 1, 1, func(x, y int) *MB {
		if x == 0 && y == 0 {
			return mb
		}
		return nil
	})
	moved := func(r int) bool {
		return pic.Cb[pic.ChromaOffset(3, r)] != rows(r) || pic.Cr[pic.ChromaOffset(3, r)] != rows(r)
	}
	if !moved(3) || !moved(4) {
		t.Error("chroma did not filter at the edge paired with luma edge 2")
	}
	for _, r := range []int{1, 2, 5, 6} {
		if moved(r) {
			t.Errorf("chroma row %d moved: filtered at an edge paired with luma edge 1 or 3", r)
		}
	}
}

// Bipredictive macroblocks whose lists are exchanged: each of the two
// reference comparisons alone must make the strength differ.
func TestMotionDiffersExchangedListsCompareBothReferences(t *testing.T) {
	a := frame.NewPicture(1, 1)
	b := frame.NewPicture(1, 1)
	c := frame.NewPicture(1, 1)
	still := [2]int16{}
	far := [2]int16{8, 0}
	for _, k := range []struct {
		name string
		p, q *MB
		want bool
	}{
		// p.L0 != q.L1 holds, p.L1 != q.L0 does not.
		{"first reference only", biMB(still, still, a, b), biMB(still, still, b, c), true},
		// p.L0 != q.L1 does not hold, p.L1 != q.L0 does.
		{"second reference only", biMB(still, still, a, b), biMB(still, still, c, a), true},
		{"neither, vectors equal", biMB(still, far, a, b), biMB(far, still, b, a), false},
		{"neither, first vectors apart", biMB(still, far, a, b), biMB(still, still, b, a), true},
		{"neither, second vectors apart", biMB(still, still, a, b), biMB(far, still, b, a), true},
	} {
		t.Run(k.name, func(t *testing.T) {
			if got := motionDiffers(k.p, k.q, 0, 0); got != k.want {
				t.Fatalf("motionDiffers = %v, want %v", got, k.want)
			}
		})
	}
}

// The early return on a zero strength (verticalEdge and horizontalEdge, "!any")
// saves work and nothing else: a mutant without it filters with strength 0 and
// the picture is the same. This pins the reason, on every edge method, so the
// two survivors are documented equivalents and not untested guards.
func TestZeroStrengthFiltersNothing(t *testing.T) {
	pic := buildEdgePicture(1, 1)
	y, cb, cr := snapshot(pic)
	ctx := newEdgeContext(pic, nil)
	var zero [4]uint8
	// strong is never set with a zero strength: it needs an intra macroblock,
	// whose strengths are 3 or 4.
	const strong = false
	ctx.planes.LumaVertical(pic.LumaOffset(4, 0), zero, strong, 51, 51)
	ctx.planes.LumaHorizontal(pic.LumaOffset(0, 4), zero, strong, 51, 51)
	ctx.planes.ChromaVertical(pic.ChromaOffset(4, 0), zero, strong, 51, 51, 51, 51)
	ctx.planes.ChromaHorizontal(pic.ChromaOffset(0, 4), zero, strong, 51, 51, 51, 51)
	if !picUnchanged(pic, y, cb, cr) {
		t.Fatal("a zero boundary strength changed the picture")
	}
}
