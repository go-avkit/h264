package cavlc

import (
	"math"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/testutil"
)

func roundTrip(t *testing.T, coeffs []int32, nC int) {
	t.Helper()
	w := bits.NewWriter()
	if err := WriteBlock(w, coeffs, nC); err != nil {
		t.Fatalf("WriteBlock(nC=%d, %v): %v", nC, coeffs, err)
	}
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatalf("writer error: %v", err)
	}
	got := make([]int32, len(coeffs))
	r := bits.NewReader(w.Bytes())
	n, err := ReadBlock(r, got, nC)
	if err != nil {
		t.Fatalf("ReadBlock(nC=%d, %v): %v", nC, coeffs, err)
	}
	want := 0
	for _, v := range coeffs {
		if v != 0 {
			want++
		}
	}
	if n != want {
		t.Fatalf("nC=%d %v: totalCoeff = %d, want %d", nC, coeffs, n, want)
	}
	for i := range coeffs {
		if got[i] != coeffs[i] {
			t.Fatalf("nC=%d: round trip of %v gave %v", nC, coeffs, got)
		}
	}
}

var nCLumaValues = []int{0, 1, 2, 3, 4, 5, 7, 8, 12, 16, 32}

var nCValues = []int{-1, 0, 1, 2, 3, 4, 5, 7, 8, 12, 16, 32}

func TestRoundTripHandPickedBlocks(t *testing.T) {
	cases := [][]int32{
		make([]int32, 16),
		{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{-1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{-1, -1, -1, -1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{3, -2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{100, -100, 50, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		{-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1},
		{2047, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{-2047, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 5, 0, 0, 0, -3, 0, 0, 0, 2, 0, 0, 0, -1},
	}
	for _, nC := range nCValues {
		if nC < 0 {
			continue
		}
		for _, c := range cases {
			roundTrip(t, append([]int32(nil), c...), nC)
		}
	}
}

func TestRoundTripChromaDC(t *testing.T) {
	cases := [][]int32{
		{0, 0, 0, 0},
		{1, 0, 0, 0},
		{-1, 0, 0, 0},
		{1, 1, 1, 1},
		{-1, -1, -1, -1},
		{7, -6, 5, -4},
		{0, 0, 0, 9},
		{0, 3, 0, 0},
	}
	for _, c := range cases {
		roundTrip(t, append([]int32(nil), c...), -1)
	}
}

func TestRoundTripAC15(t *testing.T) {
	rng := rand.New(rand.NewSource(4242))
	for _, nC := range nCValues {
		if nC < 0 {
			continue
		}
		for iter := 0; iter < 400; iter++ {
			c := make([]int32, 15)
			for i := range c {
				if rng.Intn(3) != 0 {
					continue
				}
				c[i] = int32(rng.Intn(41) - 20)
			}
			roundTrip(t, c, nC)
		}
	}
}

func randomBlock(rng *rand.Rand, n int, magnitude int) []int32 {
	c := make([]int32, n)
	density := rng.Intn(n) + 1
	for i := 0; i < density; i++ {
		pos := rng.Intn(n)
		v := int32(rng.Intn(2*magnitude+1) - magnitude)
		if v == 0 {
			v = 1
		}
		c[pos] = v
	}
	return c
}

func TestRoundTripRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(20260831))
	for _, magnitude := range []int{1, 2, 5, 40, 500, 2000} {
		for _, nC := range nCValues {
			n := 16
			if nC < 0 {
				n = 4
			}
			for iter := 0; iter < 300; iter++ {
				roundTrip(t, randomBlock(rng, n, magnitude), nC)
			}
		}
	}
}

func TestCoeffTokenTableSelection(t *testing.T) {
	cases := []struct {
		nC   int
		want int
	}{
		{-2, chromaDCTable}, {-1, chromaDCTable},
		{0, 0}, {1, 0},
		{2, 1}, {3, 1},
		{4, 2}, {7, 2},
		{8, 3}, {16, 3}, {100, 3},
	}
	for _, c := range cases {
		if got := coeffTokenTable(c.nC); got != c.want {
			t.Errorf("coeffTokenTable(%d) = %d, want %d", c.nC, got, c.want)
		}
	}
}

func TestFixedLengthCoeffToken(t *testing.T) {
	for tc := 0; tc <= 16; tc++ {
		for t1 := 0; t1 <= 3 && t1 <= tc; t1++ {
			if tc == 0 && t1 != 0 {
				continue
			}
			w := bits.NewWriter()
			if err := writeCoeffToken(w, 8, t1, tc); err != nil {
				t.Fatalf("writeCoeffToken(nC=8, %d, %d): %v", t1, tc, err)
			}
			if w.BitsWritten() != 6 {
				t.Fatalf("nC>=8 coeff_token for (%d,%d) used %d bits", t1, tc, w.BitsWritten())
			}
			w.AlignZero()
			r := bits.NewReader(w.Bytes())
			gotT1, gotTC, err := readCoeffToken(r, 8)
			if err != nil {
				t.Fatalf("readCoeffToken: %v", err)
			}
			if gotT1 != t1 || gotTC != tc {
				t.Errorf("nC>=8 round trip (%d,%d) gave (%d,%d)", t1, tc, gotT1, gotTC)
			}
		}
	}
}

func TestReadBlockRejectsTruncatedInput(t *testing.T) {
	w := bits.NewWriter()
	src := []int32{5, -4, 3, -2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if err := WriteBlock(w, src, 0); err != nil {
		t.Fatal(err)
	}
	full := w.Bytes()
	for n := 0; n < len(full); n++ {
		r := bits.NewReader(full[:n])
		out := make([]int32, 16)
		if _, err := ReadBlock(r, out, 0); err == nil {
			t.Errorf("truncating to %d bytes still decoded", n)
		}
	}
}

func TestReadBlockRejectsGarbage(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	decoded := 0
	for i := 0; i < 20000; i++ {
		data := make([]byte, 1+rng.Intn(8))
		for j := range data {
			data[j] = byte(rng.Intn(256))
		}
		out := make([]int32, 16)
		if _, err := ReadBlock(bits.NewReader(data), out, rng.Intn(20)); err == nil {
			decoded++
		}
	}
	if decoded == 0 {
		t.Error("no random input decoded, the test is not exercising the decoder")
	}
}

func TestZeroBlockCostsOneToken(t *testing.T) {
	for _, nC := range []int{0, 2, 4, 8} {
		w := bits.NewWriter()
		if err := WriteBlock(w, make([]int32, 16), nC); err != nil {
			t.Fatal(err)
		}
		if w.BitsWritten() == 0 {
			t.Errorf("nC=%d: empty block wrote no bits", nC)
		}
	}
}

func FuzzReadBlock(f *testing.F) {
	slices := testutil.RBSPOfType(testutil.NALTypeSliceIDR, testutil.NALTypeSliceNonIDR)
	for i, rbsp := range slices {
		if i >= 24 {
			break
		}
		for _, nC := range []int{-1, 0, 2, 4, 8, 17} {
			f.Add(rbsp, nC)
		}
	}
	f.Add([]byte{0x80}, 0)
	f.Add([]byte{0x00, 0x00, 0x01}, 4)
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 8)
	f.Fuzz(func(t *testing.T, data []byte, nC int) {
		if nC < -1 {
			nC = -1
		}
		if nC > 64 {
			nC = 64
		}
		n := 16
		if nC < 0 {
			n = 4
		}
		out := make([]int32, n)
		count, err := ReadBlock(bits.NewReader(data), out, nC)
		if err != nil {
			return
		}
		if count < 0 || count > n {
			t.Fatalf("totalCoeff %d out of range for %d coefficients", count, n)
		}
		nonzero := 0
		for _, v := range out {
			if v != 0 {
				nonzero++
			}
		}
		if nonzero != count {
			t.Fatalf("reported %d coefficients but %d are non-zero", count, nonzero)
		}
	})
}

func FuzzWriteReadBlock(f *testing.F) {
	f.Add(int64(1), 0)
	f.Add(int64(77), 8)
	f.Fuzz(func(t *testing.T, seed int64, nC int) {
		if nC < 0 {
			nC = 0
		}
		if nC > 64 {
			nC = 64
		}
		rng := rand.New(rand.NewSource(seed))
		c := randomBlock(rng, 16, 300)
		w := bits.NewWriter()
		if err := WriteBlock(w, c, nC); err != nil {
			return
		}
		w.WriteRBSPTrailingBits()
		out := make([]int32, 16)
		if _, err := ReadBlock(bits.NewReader(w.Bytes()), out, nC); err != nil {
			t.Fatalf("failed to read back %v: %v", c, err)
		}
		for i := range c {
			if c[i] != out[i] {
				t.Fatalf("round trip of %v gave %v", c, out)
			}
		}
	})
}

func BenchmarkReadBlock(b *testing.B) {
	c := []int32{3, -2, 1, 1, -1, 0, 4, 0, 0, -1, 0, 0, 2, 0, 0, 0}
	w := bits.NewWriter()
	if err := WriteBlock(w, c, 4); err != nil {
		b.Fatal(err)
	}
	w.WriteRBSPTrailingBits()
	data := w.Bytes()
	out := make([]int32, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ReadBlock(bits.NewReader(data), out, 4); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteBlock(b *testing.B) {
	c := []int32{3, -2, 1, 1, -1, 0, 4, 0, 0, -1, 0, 0, 2, 0, 0, 0}
	w := bits.NewWriter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Reset()
		if err := WriteBlock(w, c, 4); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRoundTripLargeLevelsInEveryPosition(t *testing.T) {
	levels := []int32{16, 17, 31, 32, 63, 64, 100, 255, 256, 1000, 2062, 2063, 2064,
		4096, 8191, 8192, 20000, 32767}
	for _, nC := range nCLumaValues {
		for _, mag := range levels {
			for _, sign := range []int32{1, -1} {
				for pos := 0; pos < 16; pos++ {
					coeffs := make([]int32, 16)
					coeffs[pos] = sign * mag
					roundTrip(t, coeffs, nC)
				}
			}
		}
	}
}

func TestRoundTripLargeLevelsBesideSmallOnes(t *testing.T) {
	for _, nC := range nCLumaValues {
		for _, mag := range []int32{18, 300, 5000, 32767} {
			coeffs := []int32{mag, 1, -1, 2, 0, -mag, 3, 0, 1, 0, 0, mag / 2, 0, 0, -1, 1}
			roundTrip(t, coeffs, nC)
		}
	}
}

func TestRoundTripChromaDCLargeLevels(t *testing.T) {
	for _, mag := range []int32{16, 64, 2063, 32767} {
		for pos := 0; pos < 4; pos++ {
			coeffs := make([]int32, 4)
			coeffs[pos] = mag
			roundTrip(t, coeffs, -1)
			coeffs[pos] = -mag
			roundTrip(t, coeffs, -1)
		}
	}
}

func TestWriteBlockRefusesLevelsBeyondTheSyntax(t *testing.T) {
	for _, mag := range []int32{1 << 28, 1 << 30, math.MaxInt32} {
		coeffs := make([]int32, 16)
		coeffs[0] = mag
		w := bits.NewWriter()
		if err := WriteBlock(w, coeffs, 0); err == nil {
			t.Fatalf("level %d was accepted, but it cannot be represented", mag)
		}
	}
}
