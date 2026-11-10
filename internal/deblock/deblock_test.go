package deblock

import (
	"math/rand"
	"testing"
)

func TestTableAlphaBetaProperties(t *testing.T) {
	for i := 0; i < 16; i++ {
		if alphaTable[i] != 0 {
			t.Fatalf("alphaTable[%d] = %d, want 0", i, alphaTable[i])
		}
		if betaTable[i] != 0 {
			t.Fatalf("betaTable[%d] = %d, want 0", i, betaTable[i])
		}
	}
	for i := 0; i < 51; i++ {
		if alphaTable[i] > alphaTable[i+1] {
			t.Fatalf("alphaTable not monotonic at %d: %d > %d", i, alphaTable[i], alphaTable[i+1])
		}
		if betaTable[i] > betaTable[i+1] {
			t.Fatalf("betaTable not monotonic at %d: %d > %d", i, betaTable[i], betaTable[i+1])
		}
	}
	if alphaTable[51] != 255 {
		t.Fatalf("alphaTable[51] = %d, want 255", alphaTable[51])
	}
	if betaTable[51] != 18 {
		t.Fatalf("betaTable[51] = %d, want 18", betaTable[51])
	}
	if alphaTable[16] != 4 {
		t.Fatalf("alphaTable[16] = %d, want 4", alphaTable[16])
	}
	if betaTable[16] != 2 {
		t.Fatalf("betaTable[16] = %d, want 2", betaTable[16])
	}
}

func TestTableTc0Properties(t *testing.T) {
	for row := 0; row < 3; row++ {
		for i := 0; i < 16; i++ {
			if tc0Table[row][i] != 0 {
				t.Fatalf("tc0Table[%d][%d] = %d, want 0", row, i, tc0Table[row][i])
			}
		}
		for i := 0; i < 51; i++ {
			if tc0Table[row][i] > tc0Table[row][i+1] {
				t.Fatalf("tc0Table[%d] not monotonic at %d: %d > %d", row, i, tc0Table[row][i], tc0Table[row][i+1])
			}
		}
	}
	for i := 0; i < 52; i++ {
		if tc0Table[0][i] > tc0Table[1][i] {
			t.Fatalf("tc0Table row bS=1 > bS=2 at indexA=%d", i)
		}
		if tc0Table[1][i] > tc0Table[2][i] {
			t.Fatalf("tc0Table row bS=2 > bS=3 at indexA=%d", i)
		}
	}
	if tc0Table[0][51] != 13 || tc0Table[1][51] != 17 || tc0Table[2][51] != 25 {
		t.Fatalf("tc0Table[.][51] = %v, want [13 17 25]", [3]uint8{tc0Table[0][51], tc0Table[1][51], tc0Table[2][51]})
	}
}

func refClip3(lo, hi, x int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func refClip1(x int) int {
	return refClip3(0, 255, x)
}

func refAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func refLuma(samples [8]int, bS uint8, indexA, indexB int) [8]int {
	out := samples
	if bS == 0 {
		return out
	}
	p3, p2, p1, p0 := samples[0], samples[1], samples[2], samples[3]
	q0, q1, q2, q3 := samples[4], samples[5], samples[6], samples[7]
	alpha := int(alphaTable[indexA])
	beta := int(betaTable[indexB])
	if refAbs(p0-q0) >= alpha || refAbs(p1-p0) >= beta || refAbs(q1-q0) >= beta {
		return out
	}
	if bS < 4 {
		tc0 := int(tc0Table[bS-1][indexA])
		ap := refAbs(p2 - p0)
		aq := refAbs(q2 - q0)
		tc := tc0
		if ap < beta {
			tc++
		}
		if aq < beta {
			tc++
		}
		delta := refClip3(-tc, tc, (((q0-p0)<<2)+(p1-q1)+4)>>3)
		out[3] = refClip1(p0 + delta)
		out[4] = refClip1(q0 - delta)
		if ap < beta {
			out[2] = p1 + refClip3(-tc0, tc0, (p2+((p0+q0+1)>>1)-(p1<<1))>>1)
		}
		if aq < beta {
			out[5] = q1 + refClip3(-tc0, tc0, (q2+((p0+q0+1)>>1)-(q1<<1))>>1)
		}
		return out
	}
	ap := refAbs(p2 - p0)
	aq := refAbs(q2 - q0)
	small := refAbs(p0-q0) < ((alpha >> 2) + 2)
	if ap < beta && small {
		out[3] = (p2 + 2*p1 + 2*p0 + 2*q0 + q1 + 4) >> 3
		out[2] = (p2 + p1 + p0 + q0 + 2) >> 2
		out[1] = (2*p3 + 3*p2 + p1 + p0 + q0 + 4) >> 3
	} else {
		out[3] = (2*p1 + p0 + q1 + 2) >> 2
	}
	if aq < beta && small {
		out[4] = (q2 + 2*q1 + 2*q0 + 2*p0 + p1 + 4) >> 3
		out[5] = (q2 + q1 + q0 + p0 + 2) >> 2
		out[6] = (2*q3 + 3*q2 + q1 + q0 + p0 + 4) >> 3
	} else {
		out[4] = (2*q1 + q0 + p1 + 2) >> 2
	}
	return out
}

func refChroma(samples [4]int, bS uint8, indexA, indexB int) [4]int {
	out := samples
	if bS == 0 {
		return out
	}
	p1, p0, q0, q1 := samples[0], samples[1], samples[2], samples[3]
	alpha := int(alphaTable[indexA])
	beta := int(betaTable[indexB])
	if refAbs(p0-q0) >= alpha || refAbs(p1-p0) >= beta || refAbs(q1-q0) >= beta {
		return out
	}
	if bS < 4 {
		tc := int(tc0Table[bS-1][indexA]) + 1
		delta := refClip3(-tc, tc, (((q0-p0)<<2)+(p1-q1)+4)>>3)
		out[1] = refClip1(p0 + delta)
		out[2] = refClip1(q0 - delta)
		return out
	}
	out[1] = (2*p1 + p0 + q1 + 2) >> 2
	out[2] = (2*q1 + q0 + p1 + 2) >> 2
	return out
}

func randSamplesMixed(rng *rand.Rand, n int) []int {
	out := make([]int, n)
	switch rng.Intn(4) {
	case 0:
		v := rng.Intn(256)
		for i := range out {
			out[i] = v
		}
	case 1:
		base := rng.Intn(256)
		slope := rng.Intn(11) - 5
		for i := range out {
			out[i] = refClip3(0, 255, base+i*slope)
		}
	case 2:
		pv := rng.Intn(256)
		qv := rng.Intn(256)
		half := n / 2
		for i := range out {
			if i < half {
				out[i] = pv
			} else {
				out[i] = qv
			}
		}
	default:
		for i := range out {
			out[i] = rng.Intn(256)
		}
	}
	return out
}

const testStride = 64
const testBufSize = testStride * testStride
const testOffset = 32*testStride + 32

type lumaEdgeFunc func(plane []byte, stride, offset int, bS [4]uint8, indexA, indexB int)
type chromaEdgeFunc func(plane []byte, stride, offset int, bS [4]uint8, indexA, indexB int)

func lumaSampleIndices(base, sampleStep int) [8]int {
	var idx [8]int
	for k := 0; k < 4; k++ {
		idx[k] = base - (4-k)*sampleStep
	}
	for k := 4; k < 8; k++ {
		idx[k] = base + (k-4)*sampleStep
	}
	return idx
}

func chromaSampleIndices(base, sampleStep int) [4]int {
	var idx [4]int
	for k := 0; k < 2; k++ {
		idx[k] = base - (2-k)*sampleStep
	}
	for k := 2; k < 4; k++ {
		idx[k] = base + (k-2)*sampleStep
	}
	return idx
}

func runLumaCrossCheck(t *testing.T, rng *rand.Rand, iters, lineStep, sampleStep, numLines, groupSize int, call lumaEdgeFunc) {
	plane := make([]byte, testBufSize)
	expected := make([]byte, testBufSize)
	sawFiltered := false
	sawUnfiltered := false
	for iter := 0; iter < iters; iter++ {
		for i := range plane {
			plane[i] = byte(rng.Intn(256))
		}
		copy(expected, plane)

		var bS [4]uint8
		for i := range bS {
			bS[i] = uint8(rng.Intn(5))
		}
		indexA := rng.Intn(52)
		indexB := rng.Intn(52)

		for line := 0; line < numLines; line++ {
			base := testOffset + line*lineStep
			idx := lumaSampleIndices(base, sampleStep)
			raw := randSamplesMixed(rng, 8)
			var samples [8]int
			for k := 0; k < 8; k++ {
				samples[k] = raw[k]
				plane[idx[k]] = byte(samples[k])
			}
			b := bS[line/groupSize]
			out := refLuma(samples, b, indexA, indexB)
			if out != samples {
				sawFiltered = true
			} else {
				sawUnfiltered = true
			}
			for k := 0; k < 8; k++ {
				expected[idx[k]] = byte(out[k])
			}
		}

		call(plane, testStride, testOffset, bS, indexA, indexB)

		for i := 0; i < testBufSize; i++ {
			if plane[i] != expected[i] {
				t.Fatalf("iter %d: mismatch at byte %d: got %d want %d", iter, i, plane[i], expected[i])
			}
		}
	}
	if !sawFiltered {
		t.Fatal("no filtered case occurred across the randomised sweep")
	}
	if !sawUnfiltered {
		t.Fatal("no unfiltered case occurred across the randomised sweep")
	}
}

func runChromaCrossCheck(t *testing.T, rng *rand.Rand, iters, lineStep, sampleStep, numLines, groupSize int, call chromaEdgeFunc) {
	plane := make([]byte, testBufSize)
	expected := make([]byte, testBufSize)
	sawFiltered := false
	sawUnfiltered := false
	for iter := 0; iter < iters; iter++ {
		for i := range plane {
			plane[i] = byte(rng.Intn(256))
		}
		copy(expected, plane)

		var bS [4]uint8
		for i := range bS {
			bS[i] = uint8(rng.Intn(5))
		}
		indexA := rng.Intn(52)
		indexB := rng.Intn(52)

		for line := 0; line < numLines; line++ {
			base := testOffset + line*lineStep
			idx := chromaSampleIndices(base, sampleStep)
			raw := randSamplesMixed(rng, 4)
			var samples [4]int
			for k := 0; k < 4; k++ {
				samples[k] = raw[k]
				plane[idx[k]] = byte(samples[k])
			}
			b := bS[line/groupSize]
			out := refChroma(samples, b, indexA, indexB)
			if out != samples {
				sawFiltered = true
			} else {
				sawUnfiltered = true
			}
			for k := 0; k < 4; k++ {
				expected[idx[k]] = byte(out[k])
			}
		}

		call(plane, testStride, testOffset, bS, indexA, indexB)

		for i := 0; i < testBufSize; i++ {
			if plane[i] != expected[i] {
				t.Fatalf("iter %d: mismatch at byte %d: got %d want %d", iter, i, plane[i], expected[i])
			}
		}
	}
	if !sawFiltered {
		t.Fatal("no filtered case occurred across the randomised sweep")
	}
	if !sawUnfiltered {
		t.Fatal("no unfiltered case occurred across the randomised sweep")
	}
}

func TestCrossCheckLumaVertical(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	runLumaCrossCheck(t, rng, 20000, testStride, 1, 16, 4, FilterLumaEdgeVertical)
}

func TestCrossCheckLumaHorizontal(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	runLumaCrossCheck(t, rng, 20000, 1, testStride, 16, 4, FilterLumaEdgeHorizontal)
}

func TestCrossCheckChromaVertical(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	runChromaCrossCheck(t, rng, 20000, testStride, 1, 8, 2, FilterChromaEdgeVertical)
}

func TestCrossCheckChromaHorizontal(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	runChromaCrossCheck(t, rng, 20000, 1, testStride, 8, 2, FilterChromaEdgeHorizontal)
}

func TestBSZeroIsNoOp(t *testing.T) {
	rng := rand.New(rand.NewSource(10))
	bS := [4]uint8{0, 0, 0, 0}
	for indexA := 0; indexA <= 51; indexA++ {
		for indexB := 0; indexB <= 51; indexB += 7 {
			plane := make([]byte, testBufSize)
			for i := range plane {
				plane[i] = byte(rng.Intn(256))
			}
			original := make([]byte, testBufSize)
			copy(original, plane)

			FilterLumaEdgeVertical(plane, testStride, testOffset, bS, indexA, indexB)
			FilterLumaEdgeHorizontal(plane, testStride, testOffset, bS, indexA, indexB)
			FilterChromaEdgeVertical(plane, testStride, testOffset, bS, indexA, indexB)
			FilterChromaEdgeHorizontal(plane, testStride, testOffset, bS, indexA, indexB)

			for i := range plane {
				if plane[i] != original[i] {
					t.Fatalf("indexA=%d indexB=%d: byte %d changed with bS all zero", indexA, indexB, i)
				}
			}
		}
	}
}

func TestLowIndexIsNoOp(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	bS := [4]uint8{1, 2, 3, 4}
	for indexA := 0; indexA <= 15; indexA++ {
		for trial := 0; trial < 5; trial++ {
			indexB := rng.Intn(52)
			plane := make([]byte, testBufSize)
			for i := range plane {
				plane[i] = byte(rng.Intn(256))
			}
			original := make([]byte, testBufSize)
			copy(original, plane)

			FilterLumaEdgeVertical(plane, testStride, testOffset, bS, indexA, indexB)
			FilterChromaEdgeVertical(plane, testStride, testOffset, bS, indexA, indexB)

			for i := range plane {
				if plane[i] != original[i] {
					t.Fatalf("indexA=%d (alpha=0) indexB=%d: byte %d changed", indexA, indexB, i)
				}
			}
		}
	}
	for indexB := 0; indexB <= 15; indexB++ {
		for trial := 0; trial < 5; trial++ {
			indexA := rng.Intn(52)
			plane := make([]byte, testBufSize)
			for i := range plane {
				plane[i] = byte(rng.Intn(256))
			}
			original := make([]byte, testBufSize)
			copy(original, plane)

			FilterLumaEdgeHorizontal(plane, testStride, testOffset, bS, indexA, indexB)
			FilterChromaEdgeHorizontal(plane, testStride, testOffset, bS, indexA, indexB)

			for i := range plane {
				if plane[i] != original[i] {
					t.Fatalf("indexA=%d indexB=%d (beta=0): byte %d changed", indexA, indexB, i)
				}
			}
		}
	}
}

func TestBoundaryIsolationLuma(t *testing.T) {
	bS := [4]uint8{2, 2, 4, 4}
	indexA, indexB := 51, 51
	for _, orientation := range []string{"vertical", "horizontal"} {
		var lineStep, sampleStep int
		var call lumaEdgeFunc
		if orientation == "vertical" {
			lineStep, sampleStep = testStride, 1
			call = FilterLumaEdgeVertical
		} else {
			lineStep, sampleStep = 1, testStride
			call = FilterLumaEdgeHorizontal
		}

		plane := make([]byte, testBufSize)
		for i := range plane {
			plane[i] = 128
		}
		for line := 0; line < 16; line++ {
			base := testOffset + line*lineStep
			idx := lumaSampleIndices(base, sampleStep)
			ramp := []int{40, 60, 90, 110, 150, 170, 200, 220}
			for k := 0; k < 8; k++ {
				plane[idx[k]] = byte(ramp[k])
			}
		}
		original := make([]byte, testBufSize)
		copy(original, plane)

		call(plane, testStride, testOffset, bS, indexA, indexB)

		for line := 0; line < 16; line++ {
			base := testOffset + line*lineStep
			idx := lumaSampleIndices(base, sampleStep)
			b := bS[line/4]
			var allowed map[int]bool
			if b < 4 {
				allowed = map[int]bool{idx[2]: true, idx[3]: true, idx[4]: true, idx[5]: true}
			} else {
				allowed = map[int]bool{idx[1]: true, idx[2]: true, idx[3]: true, idx[4]: true, idx[5]: true, idx[6]: true}
			}
			for k := 0; k < 8; k++ {
				changed := plane[idx[k]] != original[idx[k]]
				if changed && !allowed[idx[k]] {
					t.Fatalf("%s bS=%d line %d: disallowed sample index %d (k=%d) was modified", orientation, b, line, idx[k], k)
				}
			}
		}

		for i := range plane {
			isEdgeByte := false
			for line := 0; line < 16; line++ {
				base := testOffset + line*lineStep
				idx := lumaSampleIndices(base, sampleStep)
				for _, ix := range idx {
					if ix == i {
						isEdgeByte = true
					}
				}
			}
			if !isEdgeByte && plane[i] != original[i] {
				t.Fatalf("%s: byte %d outside the edge window was modified", orientation, i)
			}
		}
	}
}

func TestBoundaryIsolationChroma(t *testing.T) {
	bS := [4]uint8{2, 3, 4, 1}
	indexA, indexB := 51, 51
	for _, orientation := range []string{"vertical", "horizontal"} {
		var lineStep, sampleStep int
		var call chromaEdgeFunc
		if orientation == "vertical" {
			lineStep, sampleStep = testStride, 1
			call = FilterChromaEdgeVertical
		} else {
			lineStep, sampleStep = 1, testStride
			call = FilterChromaEdgeHorizontal
		}

		plane := make([]byte, testBufSize)
		for i := range plane {
			plane[i] = 128
		}
		for line := 0; line < 8; line++ {
			base := testOffset + line*lineStep
			idx := chromaSampleIndices(base, sampleStep)
			ramp := []int{60, 100, 150, 190}
			for k := 0; k < 4; k++ {
				plane[idx[k]] = byte(ramp[k])
			}
		}
		original := make([]byte, testBufSize)
		copy(original, plane)

		call(plane, testStride, testOffset, bS, indexA, indexB)

		for line := 0; line < 8; line++ {
			base := testOffset + line*lineStep
			idx := chromaSampleIndices(base, sampleStep)
			allowed := map[int]bool{idx[1]: true, idx[2]: true}
			for k := 0; k < 4; k++ {
				changed := plane[idx[k]] != original[idx[k]]
				if changed && !allowed[idx[k]] {
					t.Fatalf("%s line %d: disallowed sample index %d (k=%d) was modified", orientation, line, idx[k], k)
				}
			}
		}
	}
}

func TestOutputsStayInByteRange(t *testing.T) {
	rng := rand.New(rand.NewSource(20))
	for trial := 0; trial < 5000; trial++ {
		var samples [8]int
		for i := range samples {
			samples[i] = rng.Intn(256)
		}
		bS := uint8(rng.Intn(5))
		indexA := rng.Intn(52)
		indexB := rng.Intn(52)
		out := refLuma(samples, bS, indexA, indexB)
		for _, v := range out {
			if v < 0 || v > 255 {
				t.Fatalf("luma output out of range: %d (samples=%v bS=%d indexA=%d indexB=%d)", v, samples, bS, indexA, indexB)
			}
		}
		var csamples [4]int
		for i := range csamples {
			csamples[i] = rng.Intn(256)
		}
		cout := refChroma(csamples, bS, indexA, indexB)
		for _, v := range cout {
			if v < 0 || v > 255 {
				t.Fatalf("chroma output out of range: %d (samples=%v bS=%d indexA=%d indexB=%d)", v, csamples, bS, indexA, indexB)
			}
		}
	}
}

func TestExtremeInputs(t *testing.T) {
	patterns := map[string][8]int{
		"all_zero":       {0, 0, 0, 0, 0, 0, 0, 0},
		"all_max":        {255, 255, 255, 255, 255, 255, 255, 255},
		"alternating":    {0, 255, 0, 255, 0, 255, 0, 255},
		"steep_gradient": {0, 36, 73, 109, 146, 182, 219, 255},
	}
	indexA, indexB := 51, 51
	for name, pat := range patterns {
		for bS := uint8(1); bS <= 4; bS++ {
			plane := make([]byte, testBufSize)
			for i := range plane {
				plane[i] = 128
			}
			idx := lumaSampleIndices(testOffset, 1)
			for k := 0; k < 8; k++ {
				plane[idx[k]] = byte(pat[k])
			}
			expectedLine := refLuma(pat, bS, indexA, indexB)

			FilterLumaEdgeVertical(plane, testStride, testOffset, [4]uint8{bS, bS, bS, bS}, indexA, indexB)

			for k := 0; k < 8; k++ {
				got := int(plane[idx[k]])
				if got != expectedLine[k] {
					t.Fatalf("pattern=%s bS=%d: sample %d got %d want %d", name, bS, k, got, expectedLine[k])
				}
				if got < 0 || got > 255 {
					t.Fatalf("pattern=%s bS=%d: sample %d out of range: %d", name, bS, k, got)
				}
			}
		}
	}
}

func buildRandomPlane(rng *rand.Rand, w, h int) [][]byte {
	p := make([][]byte, h)
	for r := range p {
		p[r] = make([]byte, w)
		for c := range p[r] {
			p[r][c] = byte(rng.Intn(256))
		}
	}
	return p
}

func flatten(p [][]byte) []byte {
	h := len(p)
	w := len(p[0])
	out := make([]byte, h*w)
	for r := 0; r < h; r++ {
		copy(out[r*w:(r+1)*w], p[r])
	}
	return out
}

func unflatten(flat []byte, w, h int) [][]byte {
	p := make([][]byte, h)
	for r := 0; r < h; r++ {
		p[r] = append([]byte(nil), flat[r*w:(r+1)*w]...)
	}
	return p
}

func transpose(p [][]byte) [][]byte {
	h := len(p)
	w := len(p[0])
	out := make([][]byte, w)
	for c := 0; c < w; c++ {
		out[c] = make([]byte, h)
		for r := 0; r < h; r++ {
			out[c][r] = p[r][c]
		}
	}
	return out
}

func TestVerticalHorizontalTransposeAgreement(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	const size = 64
	for trial := 0; trial < 200; trial++ {
		row, col := 32, 32
		grid := buildRandomPlane(rng, size, size)
		flatV := flatten(grid)

		var bS [4]uint8
		for i := range bS {
			bS[i] = uint8(rng.Intn(5))
		}
		indexA := rng.Intn(52)
		indexB := rng.Intn(52)

		offsetV := row*size + col
		FilterLumaEdgeVertical(flatV, size, offsetV, bS, indexA, indexB)
		resultV := unflatten(flatV, size, size)

		transposedGrid := transpose(grid)
		flatH := flatten(transposedGrid)
		offsetH := col*size + row
		FilterLumaEdgeHorizontal(flatH, size, offsetH, bS, indexA, indexB)
		resultH := unflatten(flatH, size, size)

		expectedH := transpose(resultV)

		for r := 0; r < size; r++ {
			for c := 0; c < size; c++ {
				if resultH[r][c] != expectedH[r][c] {
					t.Fatalf("trial %d: mismatch at [%d][%d]: got %d want %d", trial, r, c, resultH[r][c], expectedH[r][c])
				}
			}
		}
	}
}

func TestIndexAIndexBAverageQP(t *testing.T) {
	cases := []struct {
		qp, offset, want int
	}{
		{26, 0, 26},
		{0, 0, 0},
		{51, 0, 51},
		{0, -12, 0},
		{51, 12, 51},
		{30, -12, 18},
		{30, 12, 42},
		{0, -1, 0},
		{51, 1, 51},
		{25, 6, 31},
	}
	for _, c := range cases {
		if got := IndexA(c.qp, c.offset); got != c.want {
			t.Errorf("IndexA(%d,%d) = %d, want %d", c.qp, c.offset, got, c.want)
		}
		if got := IndexB(c.qp, c.offset); got != c.want {
			t.Errorf("IndexB(%d,%d) = %d, want %d", c.qp, c.offset, got, c.want)
		}
	}

	avgCases := []struct {
		qpP, qpQ, want int
	}{
		{0, 0, 0},
		{26, 26, 26},
		{10, 11, 11},
		{11, 10, 11},
		{0, 51, 26},
		{51, 51, 51},
		{5, 6, 6},
	}
	for _, c := range avgCases {
		if got := AverageQP(c.qpP, c.qpQ); got != c.want {
			t.Errorf("AverageQP(%d,%d) = %d, want %d", c.qpP, c.qpQ, got, c.want)
		}
	}
}

func benchPlane() []byte {
	p := make([]byte, testBufSize)
	for i := range p {
		p[i] = byte(i * 37 % 256)
	}
	return p
}

func BenchmarkFilterLumaEdgeVertical(b *testing.B) {
	plane := benchPlane()
	bS := [4]uint8{2, 2, 3, 4}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FilterLumaEdgeVertical(plane, testStride, testOffset, bS, 35, 30)
	}
}

func BenchmarkFilterLumaEdgeHorizontal(b *testing.B) {
	plane := benchPlane()
	bS := [4]uint8{2, 2, 3, 4}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FilterLumaEdgeHorizontal(plane, testStride, testOffset, bS, 35, 30)
	}
}

func BenchmarkFilterChromaEdgeVertical(b *testing.B) {
	plane := benchPlane()
	bS := [4]uint8{2, 2, 3, 4}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FilterChromaEdgeVertical(plane, testStride, testOffset, bS, 35, 30)
	}
}

func BenchmarkFilterChromaEdgeHorizontal(b *testing.B) {
	plane := benchPlane()
	bS := [4]uint8{2, 2, 3, 4}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FilterChromaEdgeHorizontal(plane, testStride, testOffset, bS, 35, 30)
	}
}
