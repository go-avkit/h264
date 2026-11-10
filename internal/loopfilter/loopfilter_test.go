package loopfilter

import (
	"bytes"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
)

func TestBlkIdxAt(t *testing.T) {
	want := map[[2]int]int{
		{0, 0}: 0, {4, 0}: 1, {8, 0}: 4, {12, 0}: 5,
		{0, 4}: 2, {4, 4}: 3, {8, 4}: 6, {12, 4}: 7,
		{0, 8}: 8, {4, 8}: 9, {8, 8}: 12, {12, 8}: 13,
		{0, 12}: 10, {4, 12}: 11, {8, 12}: 14, {12, 12}: 15,
	}
	seen := make(map[int]bool)
	for pos, idx := range want {
		got := BlkIdxAt(pos[0], pos[1])
		if got != idx {
			t.Errorf("BlkIdxAt(%d,%d) = %d, want %d", pos[0], pos[1], got, idx)
		}
		seen[got] = true
	}
	if len(seen) != 16 {
		t.Fatalf("BlkIdxAt is not a bijection onto 0..15: got %d distinct values", len(seen))
	}
	for i := 0; i < 16; i++ {
		if !seen[i] {
			t.Errorf("BlkIdxAt never produces %d", i)
		}
	}
}

type mbConfig struct {
	intra          bool
	ipcm           bool
	qpy            int
	sliceID        int
	nz             uint8
	mv             [2]int16
	ref            *frame.Picture
	disableDeblock uint32
	alphaOffset    int
	betaOffset     int
	chromaOff      [2]int
}

func makeMB(c mbConfig) *MB {
	m := &MB{
		Decoded:        true,
		Intra:          c.intra,
		IPCM:           c.ipcm,
		QPY:            c.qpy,
		SliceID:        c.sliceID,
		DisableDeblock: c.disableDeblock,
		AlphaOffset:    c.alphaOffset,
		BetaOffset:     c.betaOffset,
		ChromaQPOffset: c.chromaOff,
	}
	for i := range m.NzY {
		m.NzY[i] = c.nz
	}
	for i := range m.MvL0 {
		m.MvL0[i] = c.mv
	}
	for i := range m.RefPicL0 {
		m.RefPicL0[i] = c.ref
	}
	return m
}

var refA = &frame.Picture{}
var refB = &frame.Picture{}

func gridAt(grid map[[2]int]*MB, w, h int) func(x, y int) *MB {
	return func(x, y int) *MB {
		if x < 0 || y < 0 || x >= w || y >= h {
			return nil
		}
		return grid[[2]int{x, y}]
	}
}

func alternating(low, high byte, period int) func(x int) byte {
	return func(x int) byte {
		if (x/period)%2 == 0 {
			return low
		}
		return high
	}
}

func fillColumns(pic *frame.Picture, lumaCol, chromaCol func(x int) byte) {
	w, h := pic.Width, pic.Height
	buf := make([]byte, pic.Size())
	n := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			buf[n] = lumaCol(x)
			n++
		}
	}
	cw, ch := w/2, h/2
	for plane := 0; plane < 2; plane++ {
		for y := 0; y < ch; y++ {
			for x := 0; x < cw; x++ {
				buf[n] = chromaCol(x)
				n++
			}
		}
	}
	pic.CopyIn(buf)
}

func snapshot(pic *frame.Picture) ([]byte, []byte, []byte) {
	y := make([]byte, len(pic.Y))
	copy(y, pic.Y)
	cb := make([]byte, len(pic.Cb))
	copy(cb, pic.Cb)
	cr := make([]byte, len(pic.Cr))
	copy(cr, pic.Cr)
	return y, cb, cr
}

func picUnchanged(pic *frame.Picture, y, cb, cr []byte) bool {
	return bytes.Equal(pic.Y, y) && bytes.Equal(pic.Cb, cb) && bytes.Equal(pic.Cr, cr)
}

const flatLow = 60
const flatHigh = 80

func buildEdgePicture(mbW, mbH int) *frame.Picture {
	pic := frame.NewPicture(mbW, mbH)
	fillColumns(pic, alternating(flatLow, flatHigh, 4), alternating(flatLow, flatHigh, 4))
	return pic
}

func buildMBEdgePicture(mbW, mbH int, low, high byte) *frame.Picture {
	pic := frame.NewPicture(mbW, mbH)
	col := alternating(low, high, 16)
	fillColumns(pic, col, col)
	return pic
}

func buildInternalEdgePicture(low, high byte, stepCol int) *frame.Picture {
	pic := frame.NewPicture(1, 1)
	col := func(x int) byte {
		if x < stepCol {
			return low
		}
		return high
	}
	fillColumns(pic, col, col)
	return pic
}

func TestBoundaryStrengthBothIntra(t *testing.T) {
	t.Run("mb_edge_bs4_stronger_than_internal_bs3", func(t *testing.T) {
		mbEdgePic := buildMBEdgePicture(2, 1, flatLow, flatHigh)
		mb0 := makeMB(mbConfig{intra: true, qpy: 51})
		mb1 := makeMB(mbConfig{intra: true, qpy: 51})
		grid := map[[2]int]*MB{{0, 0}: mb0, {1, 0}: mb1}
		Apply(mbEdgePic, 2, 1, gridAt(grid, 2, 1))

		p3 := mbEdgePic.Y[mbEdgePic.LumaOffset(12, 0)]
		p2 := mbEdgePic.Y[mbEdgePic.LumaOffset(13, 0)]
		p1 := mbEdgePic.Y[mbEdgePic.LumaOffset(14, 0)]
		p0 := mbEdgePic.Y[mbEdgePic.LumaOffset(15, 0)]
		q0 := mbEdgePic.Y[mbEdgePic.LumaOffset(16, 0)]
		q1 := mbEdgePic.Y[mbEdgePic.LumaOffset(17, 0)]
		q2 := mbEdgePic.Y[mbEdgePic.LumaOffset(18, 0)]
		_ = p3
		if p0 == flatLow || q0 == flatHigh {
			t.Fatalf("mb edge (bS4): p0/q0 not modified: p0=%d q0=%d", p0, q0)
		}
		if p2 == flatLow || q2 == flatHigh {
			t.Fatalf("mb edge (bS4) should modify p2/q2 (strong filter), got p2=%d q2=%d (orig %d/%d)", p2, q2, flatLow, flatHigh)
		}
		if p1 == flatLow || q1 == flatHigh {
			t.Fatalf("mb edge (bS4) should modify p1/q1, got p1=%d q1=%d", p1, q1)
		}

		internalPic := buildInternalEdgePicture(flatLow, flatHigh, 4)
		mbInt := makeMB(mbConfig{intra: true, qpy: 51})
		gridInt := map[[2]int]*MB{{0, 0}: mbInt}
		Apply(internalPic, 1, 1, gridAt(gridInt, 1, 1))

		ip0 := internalPic.Y[internalPic.LumaOffset(3, 0)]
		iq0 := internalPic.Y[internalPic.LumaOffset(4, 0)]
		ip1 := internalPic.Y[internalPic.LumaOffset(2, 0)]
		iq1 := internalPic.Y[internalPic.LumaOffset(5, 0)]
		ip2 := internalPic.Y[internalPic.LumaOffset(1, 0)]

		if ip0 == flatLow || iq0 == flatHigh {
			t.Fatalf("internal edge (bS3): p0/q0 not modified: p0=%d q0=%d", ip0, iq0)
		}
		if ip1 == flatLow || iq1 == flatHigh {
			t.Fatalf("internal edge (bS3): p1/q1 should be modified too, got p1=%d q1=%d", ip1, iq1)
		}
		if ip2 != flatLow {
			t.Fatalf("internal edge (bS3) must NOT touch p2 (weak filter), got p2=%d (orig %d)", ip2, flatLow)
		}

		if ip0 == p0 && iq0 == q0 {
			t.Errorf("bS4 and bS3 filtering produced identical p0/q0 results, expected them to differ")
		}
	})
}

func applyPairAndSampleEdge(mb0, mb1 *MB) (p0, q0 byte) {
	pic := buildMBEdgePicture(2, 1, flatLow, flatHigh)
	grid := map[[2]int]*MB{{0, 0}: mb0, {1, 0}: mb1}
	Apply(pic, 2, 1, gridAt(grid, 2, 1))
	p0 = pic.Y[pic.LumaOffset(15, 0)]
	q0 = pic.Y[pic.LumaOffset(16, 0)]
	return
}

func TestBoundaryStrengthInter(t *testing.T) {
	base := func() mbConfig {
		return mbConfig{intra: false, qpy: 51, ref: refA, mv: [2]int16{0, 0}, nz: 0}
	}

	t.Run("no_coeffs_same_ref_same_mv_not_filtered", func(t *testing.T) {
		mb0 := makeMB(base())
		mb1 := makeMB(base())
		p0, q0 := applyPairAndSampleEdge(mb0, mb1)
		if p0 != flatLow || q0 != flatHigh {
			t.Fatalf("expected no filtering: p0=%d (want %d) q0=%d (want %d)", p0, flatLow, q0, flatHigh)
		}
	})

	t.Run("nonzero_luma_coeffs_filtered", func(t *testing.T) {
		c0 := base()
		c1 := base()
		c1.nz = 1
		mb0 := makeMB(c0)
		mb1 := makeMB(c1)
		p0, q0 := applyPairAndSampleEdge(mb0, mb1)
		if p0 == flatLow || q0 == flatHigh {
			t.Fatalf("expected filtering when q side has nonzero coeffs: p0=%d q0=%d", p0, q0)
		}
	})

	t.Run("different_ref_pics_filtered", func(t *testing.T) {
		c0 := base()
		c1 := base()
		c1.ref = refB
		mb0 := makeMB(c0)
		mb1 := makeMB(c1)
		p0, q0 := applyPairAndSampleEdge(mb0, mb1)
		if p0 == flatLow || q0 == flatHigh {
			t.Fatalf("expected filtering with different reference pictures: p0=%d q0=%d", p0, q0)
		}
	})

	t.Run("ipcm_side_counts_as_nonzero_coeffs_even_with_nz_zero", func(t *testing.T) {
		const low, high byte = 70, 80
		pic := buildMBEdgePicture(2, 1, low, high)
		c0 := base()
		c1 := base()
		c1.ipcm = true
		mb0 := makeMB(c0)
		mb1 := makeMB(c1)
		grid := map[[2]int]*MB{{0, 0}: mb0, {1, 0}: mb1}
		Apply(pic, 2, 1, gridAt(grid, 2, 1))
		p0 := pic.Y[pic.LumaOffset(15, 0)]
		q0 := pic.Y[pic.LumaOffset(16, 0)]
		if p0 == low || q0 == high {
			t.Fatalf("expected filtering when q side is IPCM even though NzY is zero: p0=%d q0=%d", p0, q0)
		}
	})

	mvCases := []struct {
		name     string
		mv       [2]int16
		filtered bool
	}{
		{"dx_+4_filtered", [2]int16{4, 0}, true},
		{"dx_-4_filtered", [2]int16{-4, 0}, true},
		{"dx_+3_not_filtered", [2]int16{3, 0}, false},
		{"dx_-3_not_filtered", [2]int16{-3, 0}, false},
		{"dy_+4_filtered", [2]int16{0, 4}, true},
		{"dy_-4_filtered", [2]int16{0, -4}, true},
		{"dy_+3_not_filtered", [2]int16{0, 3}, false},
		{"dy_-3_not_filtered", [2]int16{0, -3}, false},
	}
	for _, mc := range mvCases {
		t.Run(mc.name, func(t *testing.T) {
			c0 := base()
			c1 := base()
			c1.mv = mc.mv
			mb0 := makeMB(c0)
			mb1 := makeMB(c1)
			p0, q0 := applyPairAndSampleEdge(mb0, mb1)
			changed := p0 != flatLow || q0 != flatHigh
			if changed != mc.filtered {
				t.Fatalf("mv diff %v: filtered=%v (p0=%d q0=%d), want filtered=%v", mc.mv, changed, p0, q0, mc.filtered)
			}
		})
	}
}

func TestDisableDeblockValue1(t *testing.T) {
	picOff := buildEdgePicture(1, 1)
	origY, origCb, origCr := snapshot(picOff)
	mbOff := makeMB(mbConfig{intra: true, qpy: 51, disableDeblock: 1})
	gridOff := map[[2]int]*MB{{0, 0}: mbOff}
	Apply(picOff, 1, 1, gridAt(gridOff, 1, 1))
	if !picUnchanged(picOff, origY, origCb, origCr) {
		t.Fatalf("DisableDeblock=1 must leave the macroblock completely untouched")
	}

	picOn := buildEdgePicture(1, 1)
	before := make([]byte, len(picOn.Y))
	copy(before, picOn.Y)
	mbOn := makeMB(mbConfig{intra: true, qpy: 51, disableDeblock: 0})
	gridOn := map[[2]int]*MB{{0, 0}: mbOn}
	Apply(picOn, 1, 1, gridAt(gridOn, 1, 1))
	if bytes.Equal(picOn.Y, before) {
		t.Fatalf("DisableDeblock=0 baseline should have filtered internal edges but nothing changed")
	}
}

func TestDisableDeblockValue2SliceBoundary(t *testing.T) {
	pic := frame.NewPicture(3, 1)
	col := func(x int) byte {
		switch {
		case x < 16:
			return 80
		case x < 20:
			return 60
		case x < 32:
			return 80
		default:
			return 60
		}
	}
	fillColumns(pic, col, col)

	mb0 := makeMB(mbConfig{intra: false, qpy: 51, ref: refA, mv: [2]int16{0, 0}, nz: 1, sliceID: 0})
	mb1 := makeMB(mbConfig{intra: false, qpy: 51, ref: refA, mv: [2]int16{0, 0}, nz: 1, sliceID: 1, disableDeblock: 2})
	mb2 := makeMB(mbConfig{intra: false, qpy: 51, ref: refA, mv: [2]int16{0, 0}, nz: 1, sliceID: 1, disableDeblock: 2})
	grid := map[[2]int]*MB{{0, 0}: mb0, {1, 0}: mb1, {2, 0}: mb2}
	Apply(pic, 3, 1, gridAt(grid, 3, 1))

	p0 := pic.Y[pic.LumaOffset(15, 0)]
	q0 := pic.Y[pic.LumaOffset(16, 0)]
	if p0 != col(15) || q0 != col(16) {
		t.Errorf("edge between different SliceIDs (0 vs 1) with DisableDeblock=2 must be suppressed: p0=%d (want %d) q0=%d (want %d)", p0, col(15), q0, col(16))
	}

	p0b := pic.Y[pic.LumaOffset(31, 0)]
	q0b := pic.Y[pic.LumaOffset(32, 0)]
	if p0b == col(31) && q0b == col(32) {
		t.Errorf("edge between same-SliceID neighbours with DisableDeblock=2 must still be filtered: p0=%d q0=%d unchanged", p0b, q0b)
	}

	ip0 := pic.Y[pic.LumaOffset(19, 0)]
	iq0 := pic.Y[pic.LumaOffset(20, 0)]
	if ip0 == col(19) && iq0 == col(20) {
		t.Errorf("internal edge inside a DisableDeblock=2 macroblock must still be filtered: p0=%d q0=%d unchanged", ip0, iq0)
	}
}

func TestIPCMFiltersAsQP0(t *testing.T) {
	picNonPCM := buildEdgePicture(2, 1)
	origBefore := make([]byte, len(picNonPCM.Y))
	copy(origBefore, picNonPCM.Y)
	mb0 := makeMB(mbConfig{intra: true, qpy: 51, ipcm: false})
	mb1 := makeMB(mbConfig{intra: true, qpy: 51, ipcm: false})
	grid := map[[2]int]*MB{{0, 0}: mb0, {1, 0}: mb1}
	Apply(picNonPCM, 2, 1, gridAt(grid, 2, 1))
	if bytes.Equal(picNonPCM.Y, origBefore) {
		t.Fatalf("non-IPCM high-QP macroblocks should filter, but nothing changed")
	}

	picPCM := buildEdgePicture(2, 1)
	y, cb, cr := snapshot(picPCM)
	mb0p := makeMB(mbConfig{intra: true, qpy: 51, ipcm: true})
	mb1p := makeMB(mbConfig{intra: true, qpy: 51, ipcm: true})
	gridP := map[[2]int]*MB{{0, 0}: mb0p, {1, 0}: mb1p}
	Apply(picPCM, 2, 1, gridAt(gridP, 2, 1))
	if !picUnchanged(picPCM, y, cb, cr) {
		t.Fatalf("IPCM macroblocks filter at QP 0 (alpha=beta=0), expected no filtering at all")
	}
}

func TestNilNeighbourHandling(t *testing.T) {
	t.Run("at_nil_for_everything_is_noop", func(t *testing.T) {
		pic := buildEdgePicture(3, 3)
		y, cb, cr := snapshot(pic)
		at := func(x, y int) *MB { return nil }
		Apply(pic, 3, 3, at)
		if !picUnchanged(pic, y, cb, cr) {
			t.Fatalf("Apply with at()==nil everywhere must not modify the picture")
		}
	})

	t.Run("out_of_range_neighbours_no_panic_no_phantom_edge", func(t *testing.T) {
		pic := buildEdgePicture(2, 2)
		mb00 := makeMB(mbConfig{intra: true, qpy: 51})
		mb10 := makeMB(mbConfig{intra: true, qpy: 51})
		mb01 := makeMB(mbConfig{intra: true, qpy: 51})
		mb11 := makeMB(mbConfig{intra: true, qpy: 51})
		grid := map[[2]int]*MB{
			{0, 0}: mb00, {1, 0}: mb10,
			{0, 1}: mb01, {1, 1}: mb11,
		}
		at := gridAt(grid, 2, 2)

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Apply panicked with boundary MBs having nil neighbours: %v", r)
			}
		}()
		Apply(pic, 2, 2, at)

		lp0 := pic.Y[pic.LumaOffset(0, 0)]
		if lp0 != flatLow {
			t.Errorf("top-left corner sample must not be filtered as if a left/top neighbour existed, got %d want %d", lp0, flatLow)
		}
	})

	t.Run("nil_and_not_decoded_mb_skipped_without_panic", func(t *testing.T) {
		pic := buildEdgePicture(2, 1)
		mb0 := makeMB(mbConfig{intra: true, qpy: 51})
		mb1 := makeMB(mbConfig{intra: true, qpy: 51})
		mb1.Decoded = false
		grid := map[[2]int]*MB{{0, 0}: mb0, {1, 0}: mb1}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Apply panicked on a not-Decoded macroblock: %v", r)
			}
		}()
		Apply(pic, 2, 1, gridAt(grid, 2, 1))
	})
}

func TestAlphaOffsetIncreasesFilteringStrength(t *testing.T) {
	build := func(alphaOffset int) byte {
		pic := frame.NewPicture(1, 1)
		lumaCol := func(x int) byte {
			if x < 4 {
				return 100
			}
			return 131
		}
		fillColumns(pic, lumaCol, func(x int) byte { return 128 })
		mb := makeMB(mbConfig{intra: false, qpy: 40, ref: refA, mv: [2]int16{0, 0}, nz: 1, alphaOffset: alphaOffset})
		grid := map[[2]int]*MB{{0, 0}: mb}
		Apply(pic, 1, 1, gridAt(grid, 1, 1))
		return pic.Y[pic.LumaOffset(3, 0)]
	}

	low := build(-8)
	base := build(0)
	high := build(8)

	deltaLow := int(100) - int(low)
	deltaBase := int(100) - int(base)
	deltaHigh := int(100) - int(high)
	if deltaLow < 0 {
		deltaLow = -deltaLow
	}
	if deltaBase < 0 {
		deltaBase = -deltaBase
	}
	if deltaHigh < 0 {
		deltaHigh = -deltaHigh
	}

	if !(deltaLow < deltaBase && deltaBase < deltaHigh) {
		t.Fatalf("expected strictly increasing filtering strength with AlphaOffset: low(-8)=%d base(0)=%d high(+8)=%d (p0 values %d,%d,%d)", deltaLow, deltaBase, deltaHigh, low, base, high)
	}
}

func TestBetaOffsetChangesFilteringStrength(t *testing.T) {
	buildP1 := func(betaOffset int, ap byte) byte {
		pic := frame.NewPicture(1, 1)
		lumaCol := func(x int) byte {
			switch x {
			case 0, 2, 3:
				return 100
			case 1:
				return 100 + ap
			default:
				return 90
			}
		}
		fillColumns(pic, lumaCol, func(x int) byte { return 128 })
		mb := makeMB(mbConfig{intra: false, qpy: 40, ref: refA, mv: [2]int16{0, 0}, nz: 1, betaOffset: betaOffset})
		grid := map[[2]int]*MB{{0, 0}: mb}
		Apply(pic, 1, 1, gridAt(grid, 1, 1))
		return pic.Y[pic.LumaOffset(2, 0)]
	}

	t.Run("raising_beta_offset_increases_filtering", func(t *testing.T) {
		base := buildP1(0, 15)
		high := buildP1(8, 15)
		if base != 100 {
			t.Fatalf("expected baseline p1 unmodified (100), got %d", base)
		}
		if high == 100 {
			t.Fatalf("expected raised BetaOffset to enable p1 filtering, still 100")
		}
	})

	t.Run("lowering_beta_offset_decreases_filtering", func(t *testing.T) {
		base := buildP1(0, 11)
		low := buildP1(-8, 11)
		if base == 100 {
			t.Fatalf("expected baseline p1 to be filtered (changed from 100), stayed at 100")
		}
		if low != 100 {
			t.Fatalf("expected lowered BetaOffset to disable p1 filtering, got %d want 100", low)
		}
	})
}

func TestChromaEdgesPairWithLumaEdges0And2Only(t *testing.T) {
	pic := frame.NewPicture(2, 1)
	lumaCol := alternating(flatLow, flatHigh, 4)
	chromaCol := alternating(flatLow, flatHigh, 2)
	fillColumns(pic, lumaCol, chromaCol)

	cfg := mbConfig{intra: false, qpy: 51, ref: refA, mv: [2]int16{0, 0}, nz: 1}
	mb0 := makeMB(cfg)
	mb1 := makeMB(cfg)
	grid := map[[2]int]*MB{{0, 0}: mb0, {1, 0}: mb1}
	Apply(pic, 2, 1, gridAt(grid, 2, 1))

	changedAt := func(col int) bool {
		p0 := pic.Cb[pic.ChromaOffset(col-1, 0)]
		q0 := pic.Cb[pic.ChromaOffset(col, 0)]
		wantP0 := chromaCol(col - 1)
		wantQ0 := chromaCol(col)
		return p0 != wantP0 || q0 != wantQ0
	}

	if !changedAt(8) {
		t.Errorf("expected chroma to change at the macroblock edge (e=0), global chroma column 8")
	}
	if !changedAt(4) {
		t.Errorf("expected chroma to change at the halfway edge (e=2) inside mb0, global chroma column 4")
	}
	if !changedAt(12) {
		t.Errorf("expected chroma to change at the halfway edge (e=2) inside mb1, global chroma column 12")
	}

	if changedAt(2) {
		t.Errorf("chroma must not change at the position paired with luma edge 1 (global chroma column 2)")
	}
	if changedAt(6) {
		t.Errorf("chroma must not change at the position paired with luma edge 3 (global chroma column 6)")
	}
	if changedAt(10) {
		t.Errorf("chroma must not change at the position paired with luma edge 1 in mb1 (global chroma column 10)")
	}
	if changedAt(14) {
		t.Errorf("chroma must not change at the position paired with luma edge 3 in mb1 (global chroma column 14)")
	}
}

func TestApplyNilArguments(t *testing.T) {
	Apply(nil, 1, 1, func(x, y int) *MB { return nil })

	pic := buildEdgePicture(1, 1)
	y, cb, cr := snapshot(pic)
	Apply(pic, 1, 1, nil)
	if !picUnchanged(pic, y, cb, cr) {
		t.Fatalf("Apply with nil at() must not modify the picture")
	}
}

func biMB(mv0, mv1 [2]int16, ref0, ref1 *frame.Picture) *MB {
	m := &MB{Decoded: true, Bipredictive: true}
	for i := range m.MvL0 {
		m.MvL0[i] = mv0
		m.MvL1[i] = mv1
		m.RefPicL0[i] = ref0
		m.RefPicL1[i] = ref1
	}
	return m
}

func TestMotionDiffersAcrossTwoLists(t *testing.T) {
	a := frame.NewPicture(1, 1)
	b := frame.NewPicture(1, 1)
	still := [2]int16{}
	far := [2]int16{8, 0}

	cases := []struct {
		name string
		p, q *MB
		want bool
	}{
		{"same vectors and pictures", biMB(still, far, a, b), biMB(still, far, a, b), false},
		{"lists exchanged", biMB(still, far, a, b), biMB(far, still, b, a), false},
		{"lists exchanged but vectors apart", biMB(still, far, a, b), biMB(still, far, b, a), true},
		{"second list points elsewhere", biMB(still, far, a, b), biMB(still, far, a, a), true},
		{"second list vector apart", biMB(still, still, a, b), biMB(still, far, a, b), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := motionDiffers(c.p, c.q, 0, 0); got != c.want {
				t.Fatalf("motionDiffers = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMotionDiffersIgnoresTheSecondListWhenNeitherIsBipredictive(t *testing.T) {
	a := frame.NewPicture(1, 1)
	b := frame.NewPicture(1, 1)
	p := biMB([2]int16{}, [2]int16{40, 0}, a, b)
	q := biMB([2]int16{}, [2]int16{}, a, nil)
	p.Bipredictive = false
	q.Bipredictive = false
	if motionDiffers(p, q, 0, 0) {
		t.Fatal("the second list was consulted for predictive macroblocks")
	}
}
