package transform

import (
	"math/rand"
	"testing"
)

func absI32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func absI64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func newRNG(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

func randSampleValue(rng *rand.Rand) int32 {
	return int32(rng.Intn(511) - 255)
}

func randDCValue(rng *rand.Rand) int32 {
	return int32(rng.Intn(4096) - 2048)
}

func randACBlock(rng *rand.Rand) Block {
	var b Block
	for i := range b {
		b[i] = randSampleValue(rng)
	}
	return b
}

func randDCBlock(rng *rand.Rand) Block {
	var b Block
	for i := range b {
		b[i] = randDCValue(rng)
	}
	return b
}

func randChromaDC(rng *rand.Rand) ChromaDC {
	var b ChromaDC
	for i := range b {
		b[i] = randDCValue(rng)
	}
	return b
}

func matMulCf(x [4][4]int64) [4][4]int64 {
	cf := [4][4]int64{
		{1, 1, 1, 1},
		{2, 1, -1, -2},
		{1, -1, -1, 1},
		{1, -2, 2, -1},
	}
	var temp [4][4]int64
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			var s int64
			for k := 0; k < 4; k++ {
				s += cf[i][k] * x[k][j]
			}
			temp[i][j] = s
		}
	}
	var result [4][4]int64
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			var s int64
			for k := 0; k < 4; k++ {
				s += temp[i][k] * cf[j][k]
			}
			result[i][j] = s
		}
	}
	return result
}

func blockToMat64(b Block) [4][4]int64 {
	var m [4][4]int64
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			m[r][c] = int64(b[r*4+c])
		}
	}
	return m
}

func blockToMat32(b Block) [4][4]int32 {
	var m [4][4]int32
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			m[r][c] = b[r*4+c]
		}
	}
	return m
}

func mat32ToBlock(m [4][4]int32) Block {
	var b Block
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			b[r*4+c] = m[r][c]
		}
	}
	return b
}

func referenceInverseCore(x [4][4]int32) [4][4]int32 {
	var temp [4][4]int32
	for row := 0; row < 4; row++ {
		s0, s1, s2, s3 := x[row][0], x[row][1], x[row][2], x[row][3]
		t0 := s0 + s2
		t1 := s0 - s2
		t2 := s1>>1 - s3
		t3 := s1 + s3>>1
		temp[row][0] = t0 + t3
		temp[row][1] = t1 + t2
		temp[row][2] = t1 - t2
		temp[row][3] = t0 - t3
	}
	var out [4][4]int32
	for col := 0; col < 4; col++ {
		s0, s1, s2, s3 := temp[0][col], temp[1][col], temp[2][col], temp[3][col]
		t0 := s0 + s2
		t1 := s0 - s2
		t2 := s1>>1 - s3
		t3 := s1 + s3>>1
		out[0][col] = (t0 + t3 + 32) >> 6
		out[1][col] = (t1 + t2 + 32) >> 6
		out[2][col] = (t1 - t2 + 32) >> 6
		out[3][col] = (t0 - t3 + 32) >> 6
	}
	return out
}

func TestForward4x4DCConstantBlock(t *testing.T) {
	for k := int32(-255); k <= 255; k++ {
		var b Block
		for i := range b {
			b[i] = k
		}
		Forward4x4(&b)
		if b[0] != 16*k {
			t.Fatalf("k=%d: DC = %d, want %d", k, b[0], 16*k)
		}
		for i := 1; i < 16; i++ {
			if b[i] != 0 {
				t.Fatalf("k=%d: AC[%d] = %d, want 0", k, i, b[i])
			}
		}
	}
}

func TestInverse4x4DCOnlyIsFlat(t *testing.T) {
	for d := int32(-1024); d <= 1024; d++ {
		var b Block
		b[0] = d
		Inverse4x4(&b)
		want := (d + 32) >> 6
		for i := 0; i < 16; i++ {
			if b[i] != want {
				t.Fatalf("D=%d: pos[%d] = %d, want %d", d, i, b[i], want)
			}
		}
	}
}

func TestForward4x4Linearity(t *testing.T) {
	rng := newRNG(42)
	for trial := 0; trial < 500; trial++ {
		a := randACBlock(rng)
		bBlock := randACBlock(rng)
		s := int32(rng.Intn(7) - 3)
		u := int32(rng.Intn(7) - 3)

		var combined Block
		for i := range combined {
			combined[i] = s*a[i] + u*bBlock[i]
		}

		fa := a
		Forward4x4(&fa)
		fb := bBlock
		Forward4x4(&fb)
		fc := combined
		Forward4x4(&fc)

		for i := 0; i < 16; i++ {
			expected := s*fa[i] + u*fb[i]
			if fc[i] != expected {
				t.Fatalf("trial %d pos %d: Forward4x4(s*A+t*B) = %d, want %d (s=%d,t=%d)", trial, i, fc[i], expected, s, u)
			}
		}
	}
}

func TestForward4x4MatrixCrossCheck(t *testing.T) {
	rng := newRNG(2024)
	for trial := 0; trial < 5000; trial++ {
		orig := randACBlock(rng)
		b := orig
		Forward4x4(&b)
		ref := matMulCf(blockToMat64(orig))
		for r := 0; r < 4; r++ {
			for c := 0; c < 4; c++ {
				got := int64(b[r*4+c])
				if got != ref[r][c] {
					t.Fatalf("trial %d (%d,%d): Forward4x4 = %d, matrix reference = %d", trial, r, c, got, ref[r][c])
				}
			}
		}
	}
}

func TestInverse4x4SpecCrossCheck(t *testing.T) {
	rng := newRNG(2025)
	for trial := 0; trial < 5000; trial++ {
		orig := randDCBlock(rng)
		b := orig
		Inverse4x4(&b)
		ref := referenceInverseCore(blockToMat32(orig))
		refBlock := mat32ToBlock(ref)
		for i := 0; i < 16; i++ {
			if b[i] != refBlock[i] {
				t.Fatalf("trial %d pos %d: Inverse4x4 = %d, spec reference = %d", trial, i, b[i], refBlock[i])
			}
		}
	}
}

func TestHadamard4x4Involution(t *testing.T) {
	rng := newRNG(7)
	for trial := 0; trial < 2000; trial++ {
		var orig Block
		for i := range orig {
			orig[i] = int32(rng.Intn(2001) - 1000)
		}
		b := orig
		Hadamard4x4(&b)
		Hadamard4x4(&b)
		for i := 0; i < 16; i++ {
			want := 16 * orig[i]
			if b[i] != want {
				t.Fatalf("trial %d pos %d: Hadamard4x4 twice = %d, want %d", trial, i, b[i], want)
			}
		}
	}
}

func TestHadamard2x2Involution(t *testing.T) {
	rng := newRNG(8)
	for trial := 0; trial < 5000; trial++ {
		var orig ChromaDC
		for i := range orig {
			orig[i] = int32(rng.Intn(100000001) - 50000000)
		}
		b := orig
		Hadamard2x2(&b)
		Hadamard2x2(&b)
		for i := 0; i < 4; i++ {
			want := 4 * orig[i]
			if b[i] != want {
				t.Fatalf("trial %d pos %d: Hadamard2x2 twice = %d, want %d", trial, i, b[i], want)
			}
		}
	}
}

func refHadamardForwardDC4x4Butterfly(x [4][4]int32) [4][4]int32 {
	var temp [4][4]int32
	for row := 0; row < 4; row++ {
		s0, s1, s2, s3 := x[row][0], x[row][1], x[row][2], x[row][3]
		t0 := s0 + s3
		t1 := s1 + s2
		t2 := s1 - s2
		t3 := s0 - s3
		temp[row][0] = t0 + t1
		temp[row][1] = t3 + t2
		temp[row][2] = t0 - t1
		temp[row][3] = t3 - t2
	}
	var out [4][4]int32
	for col := 0; col < 4; col++ {
		s0, s1, s2, s3 := temp[0][col], temp[1][col], temp[2][col], temp[3][col]
		t0 := s0 + s3
		t1 := s1 + s2
		t2 := s1 - s2
		t3 := s0 - s3
		out[0][col] = (t0 + t1 + 1) >> 1
		out[1][col] = (t3 + t2 + 1) >> 1
		out[2][col] = (t0 - t1 + 1) >> 1
		out[3][col] = (t3 - t2 + 1) >> 1
	}
	return out
}

func TestHadamardForwardDC4x4(t *testing.T) {
	rng := newRNG(19)
	differed := 0
	for trial := 0; trial < 2000; trial++ {
		var orig Block
		for i := range orig {
			orig[i] = int32(rng.Intn(4001) - 2000)
		}

		got := orig
		HadamardForwardDC4x4(&got)

		ref := mat32ToBlock(refHadamardForwardDC4x4Butterfly(blockToMat32(orig)))
		for i := 0; i < 16; i++ {
			if got[i] != ref[i] {
				t.Fatalf("trial %d pos %d: HadamardForwardDC4x4 = %d, independent reference = %d", trial, i, got[i], ref[i])
			}
		}

		plain := orig
		Hadamard4x4(&plain)
		for i := 0; i < 16; i++ {
			want := (plain[i] + 1) >> 1
			if got[i] != want {
				t.Fatalf("trial %d pos %d: HadamardForwardDC4x4 = %d, want (Hadamard4x4+1)>>1 = %d", trial, i, got[i], want)
			}
		}

		for i := 0; i < 16; i++ {
			if got[i] != plain[i] {
				differed++
				break
			}
		}
	}
	if differed == 0 {
		t.Fatalf("HadamardForwardDC4x4 never differed from Hadamard4x4 over %d trials; halving appears to be missing", 2000)
	}
}

func TestDCPathsAgreeWithACPath(t *testing.T) {
	const k = int32(2047)
	const maxPairDiff = 1664
	var observedMaxDiff int32

	for qp := 0; qp < 52; qp++ {
		var ac Block
		ac[0] = k
		Quant4x4(&ac, qp, true)
		Dequant4x4(&ac, qp, false)
		acOut := ac[0]

		var luma Block
		for i := range luma {
			luma[i] = k
		}
		QuantLumaDC(&luma, qp, true)
		DequantLumaDC(&luma, qp)
		lumaOut := luma[0]

		var chroma ChromaDC
		for i := range chroma {
			chroma[i] = k
		}
		QuantChromaDC(&chroma, qp, true)
		DequantChromaDC(&chroma, qp)
		chromaOut := chroma[0]

		diffs := []int32{
			absI32(acOut - lumaOut),
			absI32(acOut - chromaOut),
			absI32(lumaOut - chromaOut),
		}
		for _, d := range diffs {
			if d > observedMaxDiff {
				observedMaxDiff = d
			}
			if d > maxPairDiff {
				t.Errorf("qp=%d: path outputs diverge by %d (acOut=%d lumaOut=%d chromaOut=%d), want <= %d", qp, d, acOut, lumaOut, chromaOut, maxPairDiff)
			}
		}
	}
	if observedMaxDiff > maxPairDiff {
		t.Fatalf("observed max pairwise diff %d exceeds locked bound %d", observedMaxDiff, maxPairDiff)
	}
}

var acMaxErrIntra = [52]int32{
	1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4, 5,
	5, 7, 7, 8, 9, 10, 12, 13, 14, 17, 17, 19, 22, 26, 27, 31,
	35, 42, 44, 49, 56, 60, 79, 89, 88, 97, 120, 127, 153, 171, 171, 215,
	219, 236, 273, 331,
}

var acMaxErrInter = [52]int32{
	1, 1, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 5, 6,
	7, 8, 9, 10, 12, 13, 14, 16, 17, 21, 23, 26, 28, 32, 37, 38,
	43, 50, 56, 62, 71, 73, 99, 96, 108, 128, 140, 152, 170, 193, 220, 252,
	317, 294, 335, 347,
}

func TestQuantDequant4x4RoundTripAC(t *testing.T) {
	for _, mode := range []struct {
		intra bool
		seed  int64
		table [52]int32
	}{
		{true, 111, acMaxErrIntra},
		{false, 222, acMaxErrInter},
	} {
		rng := newRNG(mode.seed)
		for qp := 0; qp < 52; qp++ {
			var maxErr int32
			for n := 0; n < 8000; n++ {
				orig := randACBlock(rng)
				b := orig
				Forward4x4(&b)
				Quant4x4(&b, qp, mode.intra)
				Dequant4x4(&b, qp, false)
				Inverse4x4(&b)
				for i := 0; i < 16; i++ {
					e := absI32(b[i] - orig[i])
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

func TestQuantDequant4x4QP0IsNearLossless(t *testing.T) {
	rng := newRNG(666)
	var maxErr int32
	for n := 0; n < 20000; n++ {
		orig := randACBlock(rng)
		b := orig
		Forward4x4(&b)
		Quant4x4(&b, 0, true)
		Dequant4x4(&b, 0, false)
		Inverse4x4(&b)
		for i := 0; i < 16; i++ {
			e := absI32(b[i] - orig[i])
			if e > maxErr {
				maxErr = e
			}
		}
	}
	if maxErr > 1 {
		t.Fatalf("qp=0: max abs error = %d, want <= 1", maxErr)
	}
}

func TestQuant4x4NonzeroCountNonIncreasingWithQP(t *testing.T) {
	rng := newRNG(555)
	for trial := 0; trial < 200; trial++ {
		orig := randACBlock(rng)
		prevCount := 17
		for qp := 0; qp < 52; qp++ {
			b := orig
			Forward4x4(&b)
			Quant4x4(&b, qp, true)
			cnt := 0
			for i := 0; i < 16; i++ {
				if b[i] != 0 {
					cnt++
				}
			}
			if cnt > prevCount {
				t.Fatalf("trial %d: nonzero count increased at qp=%d: %d > %d", trial, qp, cnt, prevCount)
			}
			prevCount = cnt
		}
	}
}

var lumaDCMaxErr = [52]int64{
	19, 20, 28, 22, 24, 31, 41, 42, 43, 46, 52, 60, 66, 77, 83, 98,
	116, 128, 136, 152, 168, 196, 224, 256, 292, 304, 336, 364, 468, 480, 524, 580,
	708, 784, 840, 1036, 1068, 1288, 1428, 1548, 1932, 2148, 2204, 2420, 2688, 2968, 3596, 3652,
	4588, 4840, 5380, 6224,
}

func TestQuantDequantLumaDCRoundTrip(t *testing.T) {
	rng := newRNG(333)
	for qp := 0; qp < 52; qp++ {
		var maxErr int64
		for n := 0; n < 3000; n++ {
			orig := randDCBlock(rng)

			bi := orig
			QuantLumaDC(&bi, qp, true)
			DequantLumaDC(&bi, qp)
			for i := 0; i < 16; i++ {
				target := int64(orig[i]) * 4
				e := absI64(target - int64(bi[i]))
				if e > maxErr {
					maxErr = e
				}
			}

			be := orig
			QuantLumaDC(&be, qp, false)
			DequantLumaDC(&be, qp)
			for i := 0; i < 16; i++ {
				target := int64(orig[i]) * 4
				e := absI64(target - int64(be[i]))
				if e > maxErr {
					maxErr = e
				}
			}
		}
		if maxErr > lumaDCMaxErr[qp] {
			t.Errorf("qp=%d: max abs error vs 4x = %d, want <= %d", qp, maxErr, lumaDCMaxErr[qp])
		}
	}
}

var chromaDCMaxErr = [52]int64{
	16, 17, 20, 19, 24, 28, 30, 33, 36, 40, 48, 52, 56, 64, 72, 88,
	100, 108, 116, 132, 156, 168, 184, 204, 228, 256, 308, 340, 372, 404, 476, 544,
	604, 668, 784, 832, 920, 1044, 1216, 1384, 1436, 1720, 1804, 2116, 2372, 2628, 3040, 3480,
	3840, 3956, 4604, 5428,
}

func TestQuantDequantChromaDCRoundTrip(t *testing.T) {
	rng := newRNG(444)
	for qp := 0; qp < 52; qp++ {
		var maxErr int64
		for n := 0; n < 3000; n++ {
			orig := randChromaDC(rng)

			bi := orig
			QuantChromaDC(&bi, qp, true)
			DequantChromaDC(&bi, qp)
			for i := 0; i < 4; i++ {
				target := int64(orig[i]) * 4
				e := absI64(target - int64(bi[i]))
				if e > maxErr {
					maxErr = e
				}
			}

			be := orig
			QuantChromaDC(&be, qp, false)
			DequantChromaDC(&be, qp)
			for i := 0; i < 4; i++ {
				target := int64(orig[i]) * 4
				e := absI64(target - int64(be[i]))
				if e > maxErr {
					maxErr = e
				}
			}
		}
		if maxErr > chromaDCMaxErr[qp] {
			t.Errorf("qp=%d: max abs error vs 4x = %d, want <= %d", qp, maxErr, chromaDCMaxErr[qp])
		}
	}
}

func TestDequant4x4SkipDC(t *testing.T) {
	rng := newRNG(99)
	for qp := 0; qp < 52; qp++ {
		for trial := 0; trial < 100; trial++ {
			orig := randDCBlock(rng)

			full := orig
			Dequant4x4(&full, qp, false)

			skip := orig
			Dequant4x4(&skip, qp, true)

			if skip[0] != orig[0] {
				t.Fatalf("qp=%d trial=%d: skipDC modified b[0]: got %d, want untouched %d", qp, trial, skip[0], orig[0])
			}
			for i := 1; i < 16; i++ {
				if skip[i] != full[i] {
					t.Fatalf("qp=%d trial=%d pos=%d: skipDC result %d != full result %d", qp, trial, i, skip[i], full[i])
				}
			}
		}
	}
}

func TestClip1Exhaustive(t *testing.T) {
	for v := int32(-1000); v <= 1000; v++ {
		got := Clip1(v)
		var want byte
		switch {
		case v < 0:
			want = 0
		case v > 255:
			want = 255
		default:
			want = byte(v)
		}
		if got != want {
			t.Fatalf("Clip1(%d) = %d, want %d", v, got, want)
		}
	}
	cases := []struct {
		v    int32
		want byte
	}{
		{-1, 0},
		{0, 0},
		{255, 255},
		{256, 255},
	}
	for _, c := range cases {
		if got := Clip1(c.v); got != c.want {
			t.Fatalf("Clip1(%d) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestResidual4x4(t *testing.T) {
	srcStride, srcOff := 13, 27
	predStride, predOff := 9, 5

	src := make([]byte, srcOff+3*srcStride+4+7)
	for i := range src {
		src[i] = byte((i*37 + 11) % 256)
	}
	pred := make([]byte, predOff+3*predStride+4+7)
	for i := range pred {
		pred[i] = byte((i*53 + 7) % 256)
	}

	var dst Block
	Residual4x4(&dst, src, srcStride, srcOff, pred, predStride, predOff)

	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			s := int32(src[srcOff+y*srcStride+x])
			p := int32(pred[predOff+y*predStride+x])
			want := s - p
			got := dst[y*4+x]
			if got != want {
				t.Fatalf("y=%d x=%d: Residual4x4 = %d, want %d", y, x, got, want)
			}
		}
	}
}

func TestAddResidual4x4(t *testing.T) {
	stride, offset := 11, 17
	plane := make([]byte, offset+3*stride+4+10)
	for i := range plane {
		plane[i] = byte((i*19 + 3) % 256)
	}
	origPlane := make([]byte, len(plane))
	copy(origPlane, plane)

	var b Block
	deltas := []int32{
		-500, 500, 0, 10,
		-10, 300, -300, 1,
		-1, 255, -255, 128,
		-128, 5, -5, 42,
	}
	copy(b[:], deltas)

	AddResidual4x4(plane, stride, offset, &b)

	inWindow := make(map[int]bool)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			idx := offset + y*stride + x
			inWindow[idx] = true
			want := Clip1(int32(origPlane[idx]) + b[y*4+x])
			if plane[idx] != want {
				t.Fatalf("y=%d x=%d: AddResidual4x4 wrote %d, want %d", y, x, plane[idx], want)
			}
		}
	}
	for i := range plane {
		if inWindow[i] {
			continue
		}
		if plane[i] != origPlane[i] {
			t.Fatalf("byte %d outside 4x4 window was modified: got %d, want %d", i, plane[i], origPlane[i])
		}
	}

	sawZeroClip := false
	sawMaxClip := false
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			idx := offset + y*stride + x
			if plane[idx] == 0 {
				sawZeroClip = true
			}
			if plane[idx] == 255 {
				sawMaxClip = true
			}
		}
	}
	if !sawZeroClip {
		t.Fatalf("expected at least one pixel clipped to 0 in this test data")
	}
	if !sawMaxClip {
		t.Fatalf("expected at least one pixel clipped to 255 in this test data")
	}
}

func TestZigZagIndexOrder(t *testing.T) {
	expected := [16]int{
		0, 1, 4, 8,
		5, 2, 3, 6,
		9, 12, 13, 10,
		7, 11, 14, 15,
	}
	for i := 0; i < 16; i++ {
		if got := ZigZagIndex(i); got != expected[i] {
			t.Fatalf("ZigZagIndex(%d) = %d, want %d", i, got, expected[i])
		}
	}
	var seen [16]bool
	for i := 0; i < 16; i++ {
		v := ZigZagIndex(i)
		if v < 0 || v > 15 {
			t.Fatalf("ZigZagIndex(%d) = %d out of range", i, v)
		}
		if seen[v] {
			t.Fatalf("ZigZagIndex is not a permutation: value %d appears more than once", v)
		}
		seen[v] = true
	}
}

func TestScanBlockRoundTrip(t *testing.T) {
	rng := newRNG(13)
	for trial := 0; trial < 2000; trial++ {
		var scan [16]int32
		for i := range scan {
			scan[i] = int32(rng.Intn(20001) - 10000)
		}
		var dst Block
		ScanToBlock(&dst, &scan)
		var scan2 [16]int32
		BlockToScan(&scan2, &dst)
		for i := 0; i < 16; i++ {
			if scan2[i] != scan[i] {
				t.Fatalf("trial %d pos %d: BlockToScan(ScanToBlock(x)) = %d, want %d", trial, i, scan2[i], scan[i])
			}
		}

		var b Block
		for i := range b {
			b[i] = int32(rng.Intn(20001) - 10000)
		}
		var bscan [16]int32
		BlockToScan(&bscan, &b)
		var b2 Block
		ScanToBlock(&b2, &bscan)
		for i := 0; i < 16; i++ {
			if b2[i] != b[i] {
				t.Fatalf("trial %d pos %d: ScanToBlock(BlockToScan(x)) = %d, want %d", trial, i, b2[i], b[i])
			}
		}
	}
}

func BenchmarkForward4x4(bch *testing.B) {
	src := Block{-120, 45, 3, -7, 88, -1, 0, 33, -255, 255, 12, 12, -64, 64, 1, -1}
	var b Block
	for i := 0; i < bch.N; i++ {
		b = src
		Forward4x4(&b)
	}
}

func BenchmarkInverse4x4(bch *testing.B) {
	src := Block{512, -300, 40, 12, -8, 6, 0, 0, 3, -3, 1, 0, 0, 0, 0, 0}
	var b Block
	for i := 0; i < bch.N; i++ {
		b = src
		Inverse4x4(&b)
	}
}

func BenchmarkQuant4x4(bch *testing.B) {
	src := Block{-120, 45, 3, -7, 88, -1, 0, 33, -255, 255, 12, 12, -64, 64, 1, -1}
	var b Block
	for i := 0; i < bch.N; i++ {
		b = src
		Quant4x4(&b, 26, true)
	}
}

func BenchmarkDequant4x4(bch *testing.B) {
	src := Block{-12, 4, 0, -1, 8, -1, 0, 3, -25, 25, 1, 1, -6, 6, 0, -1}
	var b Block
	for i := 0; i < bch.N; i++ {
		b = src
		Dequant4x4(&b, 26, false)
	}
}

func BenchmarkAddResidual4x4(bch *testing.B) {
	stride, offset := 8, 9
	srcPlane := make([]byte, offset+3*stride+4)
	for i := range srcPlane {
		srcPlane[i] = byte((i * 29) % 256)
	}
	plane := make([]byte, len(srcPlane))
	b := Block{-120, 45, 3, -7, 88, -1, 0, 33, -60, 60, 12, 12, -64, 64, 1, -1}
	for i := 0; i < bch.N; i++ {
		copy(plane, srcPlane)
		AddResidual4x4(plane, stride, offset, &b)
	}
}
