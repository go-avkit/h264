package bits

import (
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/testutil"
)

func TestReadBitsAcrossByteBoundaries(t *testing.T) {
	data := []byte{0b10110010, 0b01011101, 0b11110000, 0b00001111, 0b10101010}
	tests := []struct {
		skip int
		n    int
		want uint32
	}{
		{0, 1, 0b1},
		{0, 8, 0b10110010},
		{0, 9, 0b101100100},
		{3, 8, 0b10010010},
		{7, 2, 0b00},
		{4, 16, 0b0010010111011111},
		{0, 32, 0b10110010010111011111000000001111},
		{8, 32, 0b01011101111100000000111110101010},
		{1, 32, 0b01100100101110111110000000011111},
		{39, 1, 0b0},
	}
	for _, tc := range tests {
		r := NewReader(data)
		if err := r.Skip(tc.skip); err != nil {
			t.Fatalf("skip %d: %v", tc.skip, err)
		}
		got, err := r.ReadBits(tc.n)
		if err != nil {
			t.Fatalf("skip %d read %d: %v", tc.skip, tc.n, err)
		}
		if got != tc.want {
			t.Errorf("skip %d read %d = %#b, want %#b", tc.skip, tc.n, got, tc.want)
		}
		if r.BitPos() != tc.skip+tc.n {
			t.Errorf("skip %d read %d: pos = %d, want %d", tc.skip, tc.n, r.BitPos(), tc.skip+tc.n)
		}
	}
}

func TestReadBitsZeroAndInvalidCounts(t *testing.T) {
	r := NewReader([]byte{0xFF})
	v, err := r.ReadBits(0)
	if v != 0 || err != nil {
		t.Fatalf("ReadBits(0) = %d, %v", v, err)
	}
	if r.BitPos() != 0 {
		t.Fatalf("ReadBits(0) advanced position to %d", r.BitPos())
	}
	for _, n := range []int{-1, 33, 64} {
		if _, err := r.ReadBits(n); !errors.Is(err, ErrBitCount) {
			t.Errorf("ReadBits(%d) error = %v, want ErrBitCount", n, err)
		}
	}
}

func TestReadOverrun(t *testing.T) {
	r := NewReader([]byte{0xFF})
	if _, err := r.ReadBits(9); !errors.Is(err, ErrOverrun) {
		t.Fatalf("ReadBits(9) on 1 byte: %v", err)
	}
	if err := r.Skip(8); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadBit(); !errors.Is(err, ErrOverrun) {
		t.Fatalf("ReadBit at end: %v", err)
	}
	if err := r.Skip(1); !errors.Is(err, ErrOverrun) {
		t.Fatalf("Skip past end: %v", err)
	}
	if err := r.Skip(-1); !errors.Is(err, ErrOverrun) {
		t.Fatalf("Skip negative: %v", err)
	}
	empty := NewReader(nil)
	if _, err := empty.ReadBit(); !errors.Is(err, ErrOverrun) {
		t.Fatalf("ReadBit on empty: %v", err)
	}
}

func TestPeekDoesNotAdvance(t *testing.T) {
	r := NewReader([]byte{0xAB, 0xCD})
	p, err := r.PeekBits(12)
	if err != nil {
		t.Fatal(err)
	}
	if p != 0xABC {
		t.Fatalf("PeekBits(12) = %#x, want 0xABC", p)
	}
	if r.BitPos() != 0 {
		t.Fatalf("peek advanced to %d", r.BitPos())
	}
	if _, err := r.PeekBits(17); !errors.Is(err, ErrOverrun) {
		t.Fatalf("PeekBits past end: %v", err)
	}
	if r.BitPos() != 0 {
		t.Fatalf("failed peek advanced to %d", r.BitPos())
	}
}

func TestSeekAndBitsLeft(t *testing.T) {
	r := NewReader([]byte{0x00, 0x00})
	if r.BitsLeft() != 16 {
		t.Fatalf("BitsLeft = %d", r.BitsLeft())
	}
	if err := r.Seek(5); err != nil {
		t.Fatal(err)
	}
	if r.BitPos() != 5 || r.BitsLeft() != 11 {
		t.Fatalf("after seek: pos %d left %d", r.BitPos(), r.BitsLeft())
	}
	if err := r.Seek(16); err != nil {
		t.Fatalf("seek to end: %v", err)
	}
	for _, bad := range []int{-1, 17} {
		if err := r.Seek(bad); !errors.Is(err, ErrOverrun) {
			t.Errorf("Seek(%d) = %v", bad, err)
		}
	}
}

func TestByteAligned(t *testing.T) {
	r := NewReader([]byte{0xFF, 0xFF})
	for i := 0; i <= 16; i++ {
		if err := r.Seek(i); err != nil {
			t.Fatal(err)
		}
		if got, want := r.ByteAligned(), i%8 == 0; got != want {
			t.Errorf("pos %d: ByteAligned = %v", i, got)
		}
	}
}

func TestReadFlag(t *testing.T) {
	r := NewReader([]byte{0b10000000})
	f, err := r.ReadFlag()
	if err != nil || !f {
		t.Fatalf("first flag = %v, %v", f, err)
	}
	f, err = r.ReadFlag()
	if err != nil || f {
		t.Fatalf("second flag = %v, %v", f, err)
	}
}

var ueVectors = []struct {
	code string
	val  uint32
}{
	{"1", 0},
	{"010", 1},
	{"011", 2},
	{"00100", 3},
	{"00101", 4},
	{"00110", 5},
	{"00111", 6},
	{"0001000", 7},
	{"0001111", 14},
	{"000010000", 15},
	{"000011111", 30},
}

func bitsToBytes(t *testing.T, s string) []byte {
	t.Helper()
	w := NewWriter()
	for _, c := range s {
		switch c {
		case '0':
			w.WriteBit(0)
		case '1':
			w.WriteBit(1)
		default:
			t.Fatalf("bad bit %q", c)
		}
	}
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	return w.Bytes()
}

func TestReadUEVectors(t *testing.T) {
	for _, tc := range ueVectors {
		r := NewReader(bitsToBytes(t, tc.code))
		got, err := r.ReadUE()
		if err != nil {
			t.Fatalf("%s: %v", tc.code, err)
		}
		if got != tc.val {
			t.Errorf("ReadUE(%s) = %d, want %d", tc.code, got, tc.val)
		}
		if r.BitPos() != len(tc.code) {
			t.Errorf("ReadUE(%s) consumed %d bits, want %d", tc.code, r.BitPos(), len(tc.code))
		}
	}
}

func TestWriteUEVectors(t *testing.T) {
	for _, tc := range ueVectors {
		w := NewWriter()
		w.WriteUE(tc.val)
		if err := w.Err(); err != nil {
			t.Fatal(err)
		}
		if w.BitsWritten() != len(tc.code) {
			t.Errorf("WriteUE(%d) wrote %d bits, want %d", tc.val, w.BitsWritten(), len(tc.code))
		}
		r := NewReader(w.Bytes())
		for i, c := range tc.code {
			b, err := r.ReadBit()
			if err != nil {
				t.Fatal(err)
			}
			if b != uint32(c-'0') {
				t.Fatalf("WriteUE(%d) bit %d = %d, want %c", tc.val, i, b, c)
			}
		}
	}
}

var seVectors = []struct {
	codeNum uint32
	val     int32
}{
	{0, 0}, {1, 1}, {2, -1}, {3, 2}, {4, -2}, {5, 3}, {6, -3},
	{7, 4}, {8, -4}, {9, 5}, {10, -5},
}

func TestSEMapping(t *testing.T) {
	for _, tc := range seVectors {
		w := NewWriter()
		w.WriteUE(tc.codeNum)
		if err := w.Err(); err != nil {
			t.Fatal(err)
		}
		r := NewReader(w.Bytes())
		got, err := r.ReadSE()
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.val {
			t.Errorf("codeNum %d decoded as %d, want %d", tc.codeNum, got, tc.val)
		}

		w2 := NewWriter()
		w2.WriteSE(tc.val)
		if err := w2.Err(); err != nil {
			t.Fatal(err)
		}
		r2 := NewReader(w2.Bytes())
		gotNum, err := r2.ReadUE()
		if err != nil {
			t.Fatal(err)
		}
		if gotNum != tc.codeNum {
			t.Errorf("WriteSE(%d) produced codeNum %d, want %d", tc.val, gotNum, tc.codeNum)
		}
	}
}

func TestUEMalformed(t *testing.T) {
	allZero := make([]byte, 8)
	r := NewReader(allZero)
	if _, err := r.ReadUE(); !errors.Is(err, ErrInvalidCode) && !errors.Is(err, ErrOverrun) {
		t.Fatalf("ReadUE on all zeros: %v", err)
	}
	long := make([]byte, 16)
	r2 := NewReader(long)
	if _, err := r2.ReadUE(); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("ReadUE on 128 zero bits: %v", err)
	}
	truncated := NewReader([]byte{0b00000001})
	if _, err := truncated.ReadUE(); !errors.Is(err, ErrOverrun) {
		t.Fatalf("ReadUE truncated suffix: %v", err)
	}
	if _, err := NewReader(allZero).ReadSE(); err == nil {
		t.Fatal("ReadSE on all zeros should fail")
	}
}

func TestUEMaxValue(t *testing.T) {
	w := NewWriter()
	w.WriteUE(math.MaxUint32)
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	if w.BitsWritten() != 65 {
		t.Fatalf("WriteUE(MaxUint32) wrote %d bits, want 65", w.BitsWritten())
	}
	r := NewReader(w.Bytes())
	got, err := r.ReadUE()
	if err != nil {
		t.Fatal(err)
	}
	if got != math.MaxUint32 {
		t.Fatalf("round trip of MaxUint32 = %d", got)
	}
}

func TestUERangeOverflow(t *testing.T) {
	w := NewWriter()
	w.WriteBits(0, 32)
	w.WriteBit(1)
	w.WriteBits(1, 32)
	w.AlignZero()
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	r := NewReader(w.Bytes())
	if _, err := r.ReadUE(); !errors.Is(err, ErrRange) {
		t.Fatalf("ReadUE beyond uint32: %v", err)
	}
}

func TestSERangeOverflow(t *testing.T) {
	w0 := NewWriter()
	w0.WriteSE(math.MinInt32)
	if !errors.Is(w0.Err(), ErrRange) {
		t.Fatalf("WriteSE(MinInt32) err = %v, want ErrRange", w0.Err())
	}
	w := NewWriter()
	w.WriteUE(math.MaxUint32 - 2)
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	r := NewReader(w.Bytes())
	got, err := r.ReadSE()
	if err != nil {
		t.Fatalf("ReadSE(largest odd codeNum): %v", err)
	}
	if got != math.MaxInt32 {
		t.Fatalf("ReadSE(largest odd codeNum) = %d, want %d", got, math.MaxInt32)
	}

	w2 := NewWriter()
	w2.WriteUE(math.MaxUint32)
	if err := w2.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReader(w2.Bytes()).ReadSE(); !errors.Is(err, ErrRange) {
		t.Fatalf("ReadSE(MaxUint32 codeNum) = %v, want ErrRange", err)
	}
}

func TestReadSERejectsMinInt32(t *testing.T) {
	w := NewWriter()
	w.WriteBits(0, 32)
	w.WriteBit(1)
	w.WriteBits(1, 32)
	w.AlignZero()
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReader(w.Bytes()).ReadSE(); !errors.Is(err, ErrRange) {
		t.Fatalf("ReadSE(codeNum 2^32) = %v, want ErrRange", err)
	}
}

func TestSEExtremes(t *testing.T) {
	for _, v := range []int32{math.MaxInt32, -math.MaxInt32, 1 << 20, -(1 << 20)} {
		w := NewWriter()
		w.WriteSE(v)
		if err := w.Err(); err != nil {
			t.Fatalf("WriteSE(%d): %v", v, err)
		}
		r := NewReader(w.Bytes())
		got, err := r.ReadSE()
		if err != nil {
			t.Fatalf("ReadSE(%d): %v", v, err)
		}
		if got != v {
			t.Errorf("se round trip %d -> %d", v, got)
		}
	}
}

func TestTE(t *testing.T) {
	w := NewWriter()
	w.WriteTE(0, 1)
	w.WriteTE(1, 1)
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	if w.BitsWritten() != 2 {
		t.Fatalf("two te(v) with max 1 wrote %d bits", w.BitsWritten())
	}
	r := NewReader(w.Bytes())
	for _, want := range []uint32{0, 1} {
		got, err := r.ReadTE(1)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("ReadTE(1) = %d, want %d", got, want)
		}
	}

	w2 := NewWriter()
	w2.WriteTE(7, 5)
	if err := w2.Err(); err != nil {
		t.Fatal(err)
	}
	r2 := NewReader(w2.Bytes())
	got, err := r2.ReadTE(5)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Fatalf("ReadTE(5) = %d, want 7", got)
	}

	w3 := NewWriter()
	w3.WriteTE(0, 0)
	if err := w3.Err(); err != nil {
		t.Fatal(err)
	}
	if w3.BitsWritten() != 0 {
		t.Fatalf("te(v) with max 0 wrote %d bits", w3.BitsWritten())
	}
	r3 := NewReader(nil)
	if v, err := r3.ReadTE(0); v != 0 || err != nil {
		t.Fatalf("ReadTE(0) = %d, %v", v, err)
	}
}

func TestMoreRBSPData(t *testing.T) {
	w := NewWriter()
	w.WriteUE(3)
	w.WriteSE(-4)
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	payload := w.Bytes()

	r := NewReader(payload)
	if !r.MoreRBSPData() {
		t.Fatal("expected data at start")
	}
	if _, err := r.ReadUE(); err != nil {
		t.Fatal(err)
	}
	if !r.MoreRBSPData() {
		t.Fatal("expected data after first symbol")
	}
	if _, err := r.ReadSE(); err != nil {
		t.Fatal(err)
	}
	if r.MoreRBSPData() {
		t.Fatal("expected no data before trailing bits")
	}
	if err := r.ReadRBSPTrailingBits(); err != nil {
		t.Fatal(err)
	}
	if r.MoreRBSPData() {
		t.Fatal("expected no data at end")
	}
}

func TestMoreRBSPDataEdgeCases(t *testing.T) {
	if NewReader(nil).MoreRBSPData() {
		t.Fatal("empty buffer reports data")
	}
	if NewReader([]byte{0x00, 0x00}).MoreRBSPData() {
		t.Fatal("all-zero buffer reports data")
	}
	r := NewReader([]byte{0x80})
	if r.MoreRBSPData() {
		t.Fatal("stop bit only should report no data")
	}
	r2 := NewReader([]byte{0x01})
	if !r2.MoreRBSPData() {
		t.Fatal("stop bit in last position should report data before it")
	}
}

func TestReadRBSPTrailingBitsErrors(t *testing.T) {
	if err := NewReader([]byte{0x00}).ReadRBSPTrailingBits(); !errors.Is(err, ErrTrailing) {
		t.Fatal("zero stop bit accepted")
	}
	if err := NewReader([]byte{0xC0}).ReadRBSPTrailingBits(); !errors.Is(err, ErrAlignment) {
		t.Fatal("non-zero alignment accepted")
	}
	if err := NewReader(nil).ReadRBSPTrailingBits(); !errors.Is(err, ErrOverrun) {
		t.Fatal("empty buffer accepted")
	}
	r := NewReader([]byte{0x80})
	if err := r.Seek(8); err != nil {
		t.Fatal(err)
	}
	if err := r.ReadRBSPTrailingBits(); !errors.Is(err, ErrOverrun) {
		t.Fatal("exhausted buffer accepted")
	}
	r2 := NewReader([]byte{0b10100000})
	if err := r2.Skip(1); err != nil {
		t.Fatal(err)
	}
	if err := r2.ReadRBSPTrailingBits(); !errors.Is(err, ErrTrailing) {
		t.Fatalf("zero stop bit mid-byte: %v", err)
	}
}

func TestWriterAlignment(t *testing.T) {
	w := NewWriter()
	w.WriteBits(0b101, 3)
	w.AlignZero()
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	if got := w.Bytes(); len(got) != 1 || got[0] != 0b10100000 {
		t.Fatalf("AlignZero produced %#v", got)
	}

	w2 := NewWriter()
	w2.WriteBits(0b101, 3)
	w2.AlignOne()
	if err := w2.Err(); err != nil {
		t.Fatal(err)
	}
	if got := w2.Bytes(); len(got) != 1 || got[0] != 0b10111111 {
		t.Fatalf("AlignOne produced %#v", got)
	}

	w3 := NewWriter()
	w3.WriteBits(0xFF, 8)
	w3.AlignZero()
	if err := w3.Err(); err != nil {
		t.Fatal(err)
	}
	if w3.BitsWritten() != 8 {
		t.Fatalf("AlignZero on aligned writer added %d bits", w3.BitsWritten()-8)
	}
}

func TestWriterInvalidBitCount(t *testing.T) {
	for _, n := range []int{-1, 33} {
		w := NewWriter()
		w.WriteBits(0, n)
		if !errors.Is(w.Err(), ErrBitCount) {
			t.Errorf("WriteBits(0, %d) err = %v, want ErrBitCount", n, w.Err())
		}
	}
	w := NewWriter()
	w.WriteBits(0xFFFF, 0)
	if w.BitsWritten() != 0 {
		t.Fatal("zero-width write advanced the writer")
	}
	if w.Err() != nil {
		t.Fatalf("Err() = %v, want nil", w.Err())
	}
}

func TestWriterMasksExcessBits(t *testing.T) {
	w := NewWriter()
	w.WriteBits(0xFF, 4)
	w.WriteBits(0x00, 4)
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	if got := w.Bytes(); len(got) != 1 || got[0] != 0xF0 {
		t.Fatalf("masking failed: %#v", got)
	}
}

func TestWriterReset(t *testing.T) {
	w := NewWriterSize(16)
	w.WriteBits(0x3, 5)
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	w.Reset()
	if w.BitsWritten() != 0 || len(w.Bytes()) != 0 {
		t.Fatalf("Reset left %d bits and %d bytes", w.BitsWritten(), len(w.Bytes()))
	}
	w.WriteBits(0xAA, 8)
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	if got := w.Bytes(); len(got) != 1 || got[0] != 0xAA {
		t.Fatalf("after reset: %#v", got)
	}
}

func TestReaderReset(t *testing.T) {
	r := NewReader([]byte{0xFF})
	if _, err := r.ReadBits(8); err != nil {
		t.Fatal(err)
	}
	r.Reset([]byte{0x0F, 0x00})
	if r.BitPos() != 0 || r.BitsLeft() != 16 {
		t.Fatalf("after reset: pos %d left %d", r.BitPos(), r.BitsLeft())
	}
	if !r.MoreRBSPData() {
		t.Fatal("reset did not recompute the trailing bit position")
	}
}

func TestWriteBits32(t *testing.T) {
	w := NewWriter()
	w.WriteBits(0xDEADBEEF, 32)
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	r := NewReader(w.Bytes())
	got, err := r.ReadBits(32)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0xDEADBEEF {
		t.Fatalf("32 bit round trip = %#x", got)
	}
}

type op struct {
	kind byte
	n    int
	u    uint32
	s    int32
}

func randomOps(rng *rand.Rand, count int) []op {
	ops := make([]op, count)
	for i := range ops {
		switch rng.Intn(4) {
		case 0:
			n := rng.Intn(33)
			v := rng.Uint32()
			if n < 32 {
				v &= 1<<uint(n) - 1
			}
			ops[i] = op{kind: 'b', n: n, u: v}
		case 1:
			ops[i] = op{kind: 'u', u: uint32(rng.Intn(1 << 20))}
		case 2:
			ops[i] = op{kind: 's', s: int32(rng.Intn(1<<20) - (1 << 19))}
		default:
			ops[i] = op{kind: 't', n: rng.Intn(8) + 1, u: uint32(rng.Intn(2))}
		}
	}
	return ops
}

func TestRandomRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(20260830))
	for iter := 0; iter < 500; iter++ {
		ops := randomOps(rng, 1+rng.Intn(60))
		w := NewWriter()
		for _, o := range ops {
			switch o.kind {
			case 'b':
				w.WriteBits(o.u, o.n)
			case 'u':
				w.WriteUE(o.u)
			case 's':
				w.WriteSE(o.s)
			case 't':
				max := uint32(o.n)
				v := o.u
				if max > 1 {
					v = o.u % (max + 1)
				}
				w.WriteTE(v, max)
			}
		}
		w.WriteRBSPTrailingBits()
		if err := w.Err(); err != nil {
			t.Fatalf("iter %d: write: %v", iter, err)
		}
		r := NewReader(w.Bytes())
		for j, o := range ops {
			switch o.kind {
			case 'b':
				got, err := r.ReadBits(o.n)
				if err != nil {
					t.Fatalf("iter %d op %d: %v", iter, j, err)
				}
				if got != o.u {
					t.Fatalf("iter %d op %d: bits %d = %#x, want %#x", iter, j, o.n, got, o.u)
				}
			case 'u':
				got, err := r.ReadUE()
				if err != nil {
					t.Fatalf("iter %d op %d: %v", iter, j, err)
				}
				if got != o.u {
					t.Fatalf("iter %d op %d: ue = %d, want %d", iter, j, got, o.u)
				}
			case 's':
				got, err := r.ReadSE()
				if err != nil {
					t.Fatalf("iter %d op %d: %v", iter, j, err)
				}
				if got != o.s {
					t.Fatalf("iter %d op %d: se = %d, want %d", iter, j, got, o.s)
				}
			case 't':
				max := uint32(o.n)
				want := o.u
				if max > 1 {
					want = o.u % (max + 1)
				}
				got, err := r.ReadTE(max)
				if err != nil {
					t.Fatalf("iter %d op %d: %v", iter, j, err)
				}
				if got != want {
					t.Fatalf("iter %d op %d: te = %d, want %d", iter, j, got, want)
				}
			}
		}
		if err := r.ReadRBSPTrailingBits(); err != nil {
			t.Fatalf("iter %d: trailing bits: %v", iter, err)
		}
	}
}

func TestReadBitMatchesReadBits(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(rng.Intn(256))
	}
	a := NewReader(data)
	b := NewReader(data)
	for i := 0; i < len(data)*8; i++ {
		x, err := a.ReadBit()
		if err != nil {
			t.Fatal(err)
		}
		y, err := b.ReadBits(1)
		if err != nil {
			t.Fatal(err)
		}
		if x != y {
			t.Fatalf("bit %d: ReadBit %d, ReadBits %d", i, x, y)
		}
	}
}

func FuzzReaderNeverPanics(f *testing.F) {
	for _, rbsp := range testutil.RBSPOfType(
		testutil.NALTypeSPS, testutil.NALTypePPS, testutil.NALTypeSEI,
		testutil.NALTypeSliceIDR, testutil.NALTypeSliceNonIDR,
	) {
		f.Add(rbsp)
	}
	f.Add([]byte{0x00})
	f.Add([]byte{0xFF, 0x80})
	f.Add([]byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42})
	f.Fuzz(func(t *testing.T, data []byte) {
		r := NewReader(data)
		for i := 0; i < 256 && r.BitsLeft() > 0; i++ {
			switch i % 5 {
			case 0:
				if _, err := r.ReadUE(); err != nil {
					return
				}
			case 1:
				if _, err := r.ReadSE(); err != nil {
					return
				}
			case 2:
				if _, err := r.ReadBits(i%33 + 1); err != nil {
					return
				}
			case 3:
				if _, err := r.ReadTE(uint32(i % 4)); err != nil {
					return
				}
			default:
				if _, err := r.ReadBit(); err != nil {
					return
				}
			}
			r.MoreRBSPData()
			r.ByteAligned()
		}
	})
}

func FuzzWriteReadUE(f *testing.F) {
	f.Add(uint32(0))
	f.Add(uint32(1))
	f.Add(uint32(math.MaxUint32))
	f.Fuzz(func(t *testing.T, v uint32) {
		w := NewWriter()
		w.WriteUE(v)
		w.AlignZero()
		if err := w.Err(); err != nil {
			t.Fatal(err)
		}
		got, err := NewReader(w.Bytes()).ReadUE()
		if err != nil {
			t.Fatalf("ReadUE after WriteUE(%d): %v", v, err)
		}
		if got != v {
			t.Fatalf("ue round trip %d -> %d", v, got)
		}
	})
}

func FuzzWriteReadSE(f *testing.F) {
	f.Add(int32(0))
	f.Add(int32(-1))
	f.Add(int32(math.MaxInt32))
	f.Fuzz(func(t *testing.T, v int32) {
		w := NewWriter()
		w.WriteSE(v)
		if errors.Is(w.Err(), ErrRange) {
			return
		}
		if err := w.Err(); err != nil {
			t.Fatal(err)
		}
		w.AlignZero()
		if err := w.Err(); err != nil {
			t.Fatal(err)
		}
		got, err := NewReader(w.Bytes()).ReadSE()
		if err != nil {
			t.Fatalf("ReadSE after WriteSE(%d): %v", v, err)
		}
		if got != v {
			t.Fatalf("se round trip %d -> %d", v, got)
		}
	})
}

func BenchmarkReadUE(b *testing.B) {
	w := NewWriter()
	for i := 0; i < 4096; i++ {
		w.WriteUE(uint32(i))
	}
	if err := w.Err(); err != nil {
		b.Fatal(err)
	}
	data := w.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReader(data)
		for j := 0; j < 4096; j++ {
			if _, err := r.ReadUE(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkReadBits8(b *testing.B) {
	data := make([]byte, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReader(data)
		for j := 0; j < 4096; j++ {
			if _, err := r.ReadBits(8); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func TestWriteFlagRoundTrip(t *testing.T) {
	w := NewWriter()
	w.WriteFlag(true)
	w.WriteFlag(false)
	w.WriteFlag(true)
	w.WriteFlag(true)
	w.AlignZero()
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	r := NewReader(w.Bytes())
	want := []bool{true, false, true, true}
	for i, wantFlag := range want {
		got, err := r.ReadFlag()
		if err != nil {
			t.Fatalf("flag %d: %v", i, err)
		}
		if got != wantFlag {
			t.Errorf("flag %d = %v, want %v", i, got, wantFlag)
		}
	}
}

func TestWriterStickyError(t *testing.T) {
	w := NewWriter()
	w.WriteBits(0, 99)
	if !errors.Is(w.Err(), ErrBitCount) {
		t.Fatalf("Err() = %v, want ErrBitCount", w.Err())
	}
	w.WriteUE(5)
	w.WriteBit(1)
	w.WriteFlag(true)
	w.AlignZero()
	w.AlignOne()
	w.WriteRBSPTrailingBits()
	w.WriteSE(3)
	w.WriteTE(1, 5)
	if w.BitsWritten() != 0 {
		t.Fatalf("BitsWritten() = %d, want 0", w.BitsWritten())
	}
	if len(w.Bytes()) != 0 {
		t.Fatalf("Bytes() = %#v, want empty", w.Bytes())
	}
	if !errors.Is(w.Err(), ErrBitCount) {
		t.Fatalf("Err() = %v, want ErrBitCount still", w.Err())
	}
}

func TestReadTEOverrunPropagates(t *testing.T) {
	if _, err := NewReader(nil).ReadTE(1); !errors.Is(err, ErrOverrun) {
		t.Fatalf("ReadTE(1) on empty reader = %v, want ErrOverrun", err)
	}
	data := make([]byte, 16)
	if _, err := NewReader(data).ReadTE(5); err == nil {
		t.Fatal("ReadTE(5) on all-zero 16 byte buffer should fail")
	}
}

func TestReadRBSPTrailingBitsAlignmentLoop(t *testing.T) {
	w := NewWriter()
	w.WriteBits(0b101, 3)
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	got := w.Bytes()
	if len(got) != 1 || got[0] != 0b10110000 {
		t.Fatalf("Bytes() = %#v, want [0b10110000]", got)
	}
	r := NewReader(got)
	if _, err := r.ReadBits(3); err != nil {
		t.Fatal(err)
	}
	if err := r.ReadRBSPTrailingBits(); err != nil {
		t.Fatal(err)
	}
}
