package nal

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/testutil"
)

func TestHeaderRoundTrip(t *testing.T) {
	for refIDC := uint8(0); refIDC <= 3; refIDC++ {
		for nalType := 0; nalType <= 31; nalType++ {
			h := Header{RefIDC: refIDC, Type: Type(nalType)}
			b := h.Byte()
			if b&0x80 != 0 {
				t.Fatalf("Header{%d,%d}.Byte() = 0x%02x sets forbidden bit", refIDC, nalType, b)
			}
			got, err := ParseHeader(b)
			if err != nil {
				t.Fatalf("ParseHeader(0x%02x) unexpected error: %v", b, err)
			}
			if got.RefIDC != refIDC || got.Type != Type(nalType) {
				t.Fatalf("round trip mismatch: want {%d,%d} got {%d,%d}", refIDC, nalType, got.RefIDC, got.Type)
			}
		}
	}
}

func TestParseHeaderForbiddenBit(t *testing.T) {
	for b := 0; b <= 0xFF; b++ {
		h, err := ParseHeader(byte(b))
		if b&0x80 != 0 {
			if !errors.Is(err, ErrForbiddenBit) {
				t.Fatalf("byte 0x%02x: want ErrForbiddenBit, got %v", b, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("byte 0x%02x: unexpected error %v", b, err)
		}
		wantRefIDC := uint8(b >> 5 & 3)
		wantType := Type(b & 0x1F)
		if h.RefIDC != wantRefIDC || h.Type != wantType {
			t.Fatalf("byte 0x%02x: got {%d,%d} want {%d,%d}", b, h.RefIDC, h.Type, wantRefIDC, wantType)
		}
	}
}

func TestParse(t *testing.T) {
	if _, err := Parse(nil); !errors.Is(err, ErrEmptyUnit) {
		t.Fatalf("Parse(nil): want ErrEmptyUnit, got %v", err)
	}
	if _, err := Parse([]byte{}); !errors.Is(err, ErrEmptyUnit) {
		t.Fatalf("Parse(empty): want ErrEmptyUnit, got %v", err)
	}
	if _, err := Parse([]byte{0x80, 0x01}); !errors.Is(err, ErrForbiddenBit) {
		t.Fatalf("Parse(forbidden): want ErrForbiddenBit, got %v", err)
	}
	header := Header{RefIDC: 3, Type: TypeSPS}
	rbsp := []byte{0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0xFF}
	escaped := Escape(nil, rbsp)
	ebsp := append([]byte{header.Byte()}, escaped...)
	unit, err := Parse(ebsp)
	if err != nil {
		t.Fatalf("Parse(valid): unexpected error %v", err)
	}
	if unit.Header != header {
		t.Fatalf("Parse(valid): header = %+v, want %+v", unit.Header, header)
	}
	if !bytes.Equal(unit.RBSP, rbsp) {
		t.Fatalf("Parse(valid): RBSP = %v, want %v", unit.RBSP, rbsp)
	}
}

func TestTypeString(t *testing.T) {
	for tt := 0; tt <= 31; tt++ {
		typ := Type(tt)
		want, known := typeNames[typ]
		if !known {
			want = fmt.Sprintf("reserved(%d)", tt)
		}
		if got := typ.String(); got != want {
			t.Fatalf("Type(%d).String() = %q, want %q", tt, got, want)
		}
	}
	if got := Type(20).String(); got != "reserved(20)" {
		t.Fatalf("Type(20).String() = %q, want reserved(20)", got)
	}
	if got := Type(31).String(); got != "reserved(31)" {
		t.Fatalf("Type(31).String() = %q, want reserved(31)", got)
	}
}

func TestTypeIsSlice(t *testing.T) {
	for tt := 0; tt <= 31; tt++ {
		typ := Type(tt)
		want := tt == 1 || tt == 5 || tt == 19
		if got := typ.IsSlice(); got != want {
			t.Fatalf("Type(%d).IsSlice() = %v, want %v", tt, got, want)
		}
	}
}

func TestTypeIsVCL(t *testing.T) {
	for tt := 0; tt <= 31; tt++ {
		typ := Type(tt)
		want := tt >= 1 && tt <= 5
		if got := typ.IsVCL(); got != want {
			t.Fatalf("Type(%d).IsVCL() = %v, want %v", tt, got, want)
		}
	}
}

type escapeVector struct {
	name    string
	raw     []byte
	escaped []byte
}

var escapeVectors = []escapeVector{
	{"zero-zero-zero", []byte{0x00, 0x00, 0x00}, []byte{0x00, 0x00, 0x03, 0x00}},
	{"zero-zero-one", []byte{0x00, 0x00, 0x01}, []byte{0x00, 0x00, 0x03, 0x01}},
	{"zero-zero-two", []byte{0x00, 0x00, 0x02}, []byte{0x00, 0x00, 0x03, 0x02}},
	{"zero-zero-three", []byte{0x00, 0x00, 0x03}, []byte{0x00, 0x00, 0x03, 0x03}},
	{"zero-zero-four-unchanged", []byte{0x00, 0x00, 0x04}, []byte{0x00, 0x00, 0x04}},
	{"six-zeros", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, []byte{0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00, 0x00}},
	{"empty", []byte{}, []byte{}},
	{"single-byte", []byte{0x2A}, []byte{0x2A}},
	{"no-zero-run", []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0xFF}, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0xFF}},
}

func TestEscapeUnescapeVectors(t *testing.T) {
	for _, v := range escapeVectors {
		t.Run(v.name, func(t *testing.T) {
			got := Escape(nil, v.raw)
			if !bytes.Equal(got, v.escaped) {
				t.Fatalf("Escape(%v) = %v, want %v", v.raw, got, v.escaped)
			}
			back := Unescape(nil, v.escaped)
			if !bytes.Equal(back, v.raw) {
				t.Fatalf("Unescape(%v) = %v, want %v", v.escaped, back, v.raw)
			}
		})
	}
}

func TestEscapeUnescapeProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(1234567))
	alphabet := []byte{0x00, 0x01, 0x02, 0x03}
	for i := 0; i < 2000; i++ {
		n := rng.Intn(65)
		src := make([]byte, n)
		for j := range src {
			if rng.Intn(10) < 7 {
				src[j] = alphabet[rng.Intn(len(alphabet))]
			} else {
				src[j] = byte(rng.Intn(256))
			}
		}
		escaped := Escape(nil, src)
		back := Unescape(nil, escaped)
		if !bytes.Equal(back, src) {
			t.Fatalf("iteration %d: round trip mismatch: src=%v escaped=%v back=%v", i, src, escaped, back)
		}
		for k := 0; k+2 < len(escaped); k++ {
			if escaped[k] == 0x00 && escaped[k+1] == 0x00 && escaped[k+2] <= 0x02 {
				t.Fatalf("iteration %d: forbidden sequence at offset %d in %v (src=%v)", i, k, escaped, src)
			}
		}
	}
}

func TestEscapeUnescapeDstPrefix(t *testing.T) {
	prefix := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	src := []byte{0x00, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00}
	escapedOnly := Escape(nil, src)

	dst := append([]byte(nil), prefix...)
	out := Escape(dst, src)
	if !bytes.Equal(out[:len(prefix)], prefix) {
		t.Fatalf("Escape with dst prefix corrupted prefix: got %v, want %v", out[:len(prefix)], prefix)
	}
	if !bytes.Equal(out[len(prefix):], escapedOnly) {
		t.Fatalf("Escape with dst prefix: appended part = %v, want %v", out[len(prefix):], escapedOnly)
	}

	dst2 := append([]byte(nil), prefix...)
	out2 := Unescape(dst2, escapedOnly)
	if !bytes.Equal(out2[:len(prefix)], prefix) {
		t.Fatalf("Unescape with dst prefix corrupted prefix: got %v, want %v", out2[:len(prefix)], prefix)
	}
	if !bytes.Equal(out2[len(prefix):], src) {
		t.Fatalf("Unescape with dst prefix: appended part = %v, want %v", out2[len(prefix):], src)
	}
}

func TestTrimTrailingZeros(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"empty", []byte{}, []byte{}},
		{"all-zeros", []byte{0x00, 0x00, 0x00, 0x00}, []byte{}},
		{"no-trailing-zeros", []byte{0x01, 0x02, 0x03}, []byte{0x01, 0x02, 0x03}},
		{"some-trailing-zeros", []byte{0x01, 0x02, 0x00, 0x00, 0x00}, []byte{0x01, 0x02}},
		{"single-zero", []byte{0x00}, []byte{}},
		{"single-nonzero", []byte{0x07}, []byte{0x07}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := trimTrailingZeros(c.in)
			if !bytes.Equal(got, c.want) {
				t.Fatalf("trimTrailingZeros(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestIndexStartCode(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		from int
		want int
	}{
		{"finds-prefix", []byte{0xAA, 0x00, 0x00, 0x01, 0xBB}, 0, 1},
		{"honours-from-offset", []byte{0x00, 0x00, 0x01, 0xAA, 0x00, 0x00, 0x01, 0xBB}, 3, 4},
		{"absent", []byte{0x00, 0x01, 0x02, 0x03}, 0, -1},
		{"from-past-end", []byte{0x00, 0x00, 0x01}, 10, -1},
		{"negative-from-clamps", []byte{0x00, 0x00, 0x01, 0x02}, -5, 0},
		{"buffer-shorter-than-three", []byte{0x00, 0x00}, 0, -1},
		{"empty-data", []byte{}, 0, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := indexStartCode(c.data, c.from)
			if got != c.want {
				t.Fatalf("indexStartCode(%v, %d) = %d, want %d", c.data, c.from, got, c.want)
			}
		})
	}
}

func equalPayloads(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestSplitAnnexB(t *testing.T) {
	t.Run("no-start-code", func(t *testing.T) {
		got := SplitAnnexB([]byte{0x01, 0x02, 0x03, 0x04})
		if got != nil {
			t.Fatalf("SplitAnnexB(no start code) = %v, want nil", got)
		}
	})

	t.Run("leading-garbage-and-four-byte-start-code", func(t *testing.T) {
		data := []byte{0xDE, 0xAD, 0x00, 0x00, 0x01, 0x11, 0x22, 0x00, 0x00, 0x00, 0x01, 0x33, 0x44}
		want := [][]byte{{0x11, 0x22}, {0x33, 0x44}}
		got := SplitAnnexB(data)
		if !equalPayloads(got, want) {
			t.Fatalf("SplitAnnexB(%v) = %v, want %v", data, got, want)
		}
	})

	t.Run("empty-payloads-skipped", func(t *testing.T) {
		data := []byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x01, 0xAA, 0xBB}
		want := [][]byte{{0xAA, 0xBB}}
		got := SplitAnnexB(data)
		if !equalPayloads(got, want) {
			t.Fatalf("SplitAnnexB(%v) = %v, want %v", data, got, want)
		}
	})

	t.Run("trailing-zeros-stripped-at-end-of-stream", func(t *testing.T) {
		data := []byte{0x00, 0x00, 0x01, 0x11, 0x22, 0x00, 0x00}
		want := [][]byte{{0x11, 0x22}}
		got := SplitAnnexB(data)
		if !equalPayloads(got, want) {
			t.Fatalf("SplitAnnexB(%v) = %v, want %v", data, got, want)
		}
	})

	t.Run("mixed-three-and-four-byte-start-codes", func(t *testing.T) {
		data := []byte{
			0x00, 0x00, 0x01, 0x10, 0x20, 0x30,
			0x00, 0x00, 0x00, 0x01, 0x40, 0x50,
			0x00, 0x00, 0x01, 0x60, 0x70, 0x00, 0x00,
		}
		want := [][]byte{{0x10, 0x20, 0x30}, {0x40, 0x50}, {0x60, 0x70}}
		got := SplitAnnexB(data)
		if !equalPayloads(got, want) {
			t.Fatalf("SplitAnnexB(%v) = %v, want %v", data, got, want)
		}
	})
}

func annexBTestUnits() []Unit {
	return []Unit{
		{Header: Header{RefIDC: 3, Type: TypeSPS}, RBSP: []byte{0x64, 0x00, 0x1F, 0xAC, 0x2C}},
		{Header: Header{RefIDC: 0, Type: TypeSliceIDR}, RBSP: []byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x00, 0x00, 0x03, 0xFF}},
		{Header: Header{RefIDC: 2, Type: TypeSEI}, RBSP: []byte{}},
		{Header: Header{RefIDC: 1, Type: TypePPS}, RBSP: []byte{0xFF, 0x00, 0x00, 0x02, 0xAB, 0x00, 0x00}},
		{Header: Header{RefIDC: 3, Type: TypeAccessUnitDelim}, RBSP: []byte{0xF0}},
	}
}

func buildAnnexBStream(units []Unit, longStartCodes []bool) []byte {
	var buf []byte
	for i, u := range units {
		buf = AppendAnnexB(buf, u, longStartCodes[i%len(longStartCodes)])
	}
	return buf
}

func collectViaScanner(stream []byte, chunkSize int) [][]byte {
	s := NewScanner()
	var got [][]byte
	drain := func() {
		for {
			p, ok := s.Next()
			if !ok {
				return
			}
			got = append(got, p)
		}
	}
	if chunkSize <= 0 || chunkSize >= len(stream) {
		s.Append(stream)
		drain()
	} else {
		for i := 0; i < len(stream); i += chunkSize {
			end := i + chunkSize
			if end > len(stream) {
				end = len(stream)
			}
			s.Append(stream[i:end])
			drain()
		}
	}
	if p, ok := s.Flush(); ok {
		got = append(got, p)
	}
	return got
}

func TestScannerStreamingEquivalence(t *testing.T) {
	units := annexBTestUnits()
	streams := map[string][]byte{
		"alternating-start-codes": buildAnnexBStream(units, []bool{true, false}),
		"all-long-start-codes":    buildAnnexBStream(units, []bool{true}),
		"all-short-start-codes":   buildAnnexBStream(units, []bool{false}),
		"single-unit":             buildAnnexBStream(units[:1], []bool{false}),
		"single-unit-long-prefix": buildAnnexBStream(units[1:2], []bool{true}),
	}
	chunkSizes := []int{1, 2, 3, 5, 7, 0}

	for name, stream := range streams {
		want := SplitAnnexB(stream)
		for _, cs := range chunkSizes {
			label := fmt.Sprintf("%s/chunk-%d", name, cs)
			t.Run(label, func(t *testing.T) {
				got := collectViaScanner(stream, cs)
				if !equalPayloads(got, want) {
					t.Fatalf("chunk size %d: scanner produced %v, want %v", cs, got, want)
				}
			})
		}
	}
}

func TestScannerNoStartCode(t *testing.T) {
	s := NewScanner()
	s.Append([]byte{0x01, 0x02, 0x03, 0x04, 0x05})
	for i := 0; i < 3; i++ {
		if _, ok := s.Next(); ok {
			t.Fatalf("Next() returned true on data with no start code")
		}
	}
	if _, ok := s.Flush(); ok {
		t.Fatalf("Flush() returned true on data with no start code")
	}
}

func TestScannerResetBuffered(t *testing.T) {
	s := NewScanner()
	if got := s.Buffered(); got != 0 {
		t.Fatalf("Buffered() on fresh scanner = %d, want 0", got)
	}
	s.Append([]byte{0x00, 0x00, 0x01, 0x11, 0x22, 0x33})
	if got := s.Buffered(); got == 0 {
		t.Fatalf("Buffered() after Append = %d, want > 0", got)
	}
	s.Reset()
	if got := s.Buffered(); got != 0 {
		t.Fatalf("Buffered() after Reset = %d, want 0", got)
	}
	if _, ok := s.Next(); ok {
		t.Fatalf("Next() after Reset returned true")
	}
	if _, ok := s.Flush(); ok {
		t.Fatalf("Flush() after Reset returned true")
	}
}

func TestScannerFlushEmptyPayload(t *testing.T) {
	s := NewScanner()
	s.Append([]byte{0x00, 0x00, 0x01, 0x00, 0x00})
	if _, ok := s.Next(); ok {
		t.Fatalf("Next() returned true on an incomplete trailing unit")
	}
	if _, ok := s.Flush(); ok {
		t.Fatalf("Flush() returned true for a pending unit that trims to empty")
	}
}

func TestAppendAnnexB(t *testing.T) {
	for _, long := range []bool{true, false} {
		name := "short-start-code"
		wantPrefix := []byte{0x00, 0x00, 0x01}
		if long {
			name = "long-start-code"
			wantPrefix = []byte{0x00, 0x00, 0x00, 0x01}
		}
		t.Run(name, func(t *testing.T) {
			u := Unit{Header: Header{RefIDC: 2, Type: TypeSPS}, RBSP: []byte{0x00, 0x00, 0x00, 0x01, 0x02, 0x00, 0x00, 0x02, 0xAB}}
			out := AppendAnnexB(nil, u, long)
			if !bytes.Equal(out[:len(wantPrefix)], wantPrefix) {
				t.Fatalf("AppendAnnexB prefix = %v, want %v", out[:len(wantPrefix)], wantPrefix)
			}
			headerByte := out[len(wantPrefix)]
			if headerByte != u.Header.Byte() {
				t.Fatalf("AppendAnnexB header byte = 0x%02x, want 0x%02x", headerByte, u.Header.Byte())
			}
			parts := SplitAnnexB(out)
			if len(parts) != 1 {
				t.Fatalf("SplitAnnexB(AppendAnnexB(...)) produced %d parts, want 1", len(parts))
			}
			parsed, err := Parse(parts[0])
			if err != nil {
				t.Fatalf("Parse(AppendAnnexB round trip) unexpected error: %v", err)
			}
			if parsed.Header != u.Header {
				t.Fatalf("round trip header = %+v, want %+v", parsed.Header, u.Header)
			}
			if !bytes.Equal(parsed.RBSP, u.RBSP) {
				t.Fatalf("round trip RBSP = %v, want %v", parsed.RBSP, u.RBSP)
			}
		})
	}
}

func TestAppendAVCCSplitAVCCRoundTrip(t *testing.T) {
	units := annexBTestUnits()
	for _, ls := range []int{1, 2, 4} {
		ls := ls
		t.Run(fmt.Sprintf("length-size-%d", ls), func(t *testing.T) {
			var buf []byte
			var err error
			for _, u := range units {
				buf, err = AppendAVCC(buf, u, ls)
				if err != nil {
					t.Fatalf("AppendAVCC unexpected error: %v", err)
				}
			}
			parts, err := SplitAVCC(buf, ls)
			if err != nil {
				t.Fatalf("SplitAVCC unexpected error: %v", err)
			}
			if len(parts) != len(units) {
				t.Fatalf("SplitAVCC produced %d parts, want %d", len(parts), len(units))
			}
			for i, p := range parts {
				parsed, err := Parse(p)
				if err != nil {
					t.Fatalf("Parse(part %d) unexpected error: %v", i, err)
				}
				if parsed.Header != units[i].Header {
					t.Fatalf("part %d header = %+v, want %+v", i, parsed.Header, units[i].Header)
				}
				if !bytes.Equal(parsed.RBSP, units[i].RBSP) {
					t.Fatalf("part %d RBSP = %v, want %v", i, parsed.RBSP, units[i].RBSP)
				}
			}
		})
	}
}

func TestAppendAVCCLengthSizeError(t *testing.T) {
	u := Unit{Header: Header{RefIDC: 1, Type: TypeSPS}, RBSP: []byte{0x01}}
	for _, ls := range []int{0, 3, 8} {
		if _, err := AppendAVCC(nil, u, ls); !errors.Is(err, ErrLengthSize) {
			t.Fatalf("AppendAVCC(lengthSize=%d): want ErrLengthSize, got %v", ls, err)
		}
	}
}

func TestSplitAVCCLengthSizeError(t *testing.T) {
	for _, ls := range []int{0, 3, 8} {
		if _, err := SplitAVCC([]byte{0x01, 0x02, 0x03}, ls); !errors.Is(err, ErrLengthSize) {
			t.Fatalf("SplitAVCC(lengthSize=%d): want ErrLengthSize, got %v", ls, err)
		}
	}
}

func TestSplitAVCCTruncatedLength(t *testing.T) {
	t.Run("prefix-itself-truncated", func(t *testing.T) {
		_, err := SplitAVCC([]byte{0x00}, 2)
		if !errors.Is(err, ErrTruncatedLength) {
			t.Fatalf("want ErrTruncatedLength, got %v", err)
		}
	})
	t.Run("declared-length-exceeds-remaining", func(t *testing.T) {
		data := []byte{0x05, 0xAA, 0xBB}
		_, err := SplitAVCC(data, 1)
		if !errors.Is(err, ErrTruncatedLength) {
			t.Fatalf("want ErrTruncatedLength, got %v", err)
		}
	})
	t.Run("four-byte-length-exceeds-total", func(t *testing.T) {
		data := []byte{0x00, 0x00, 0x00, 0x0A, 0xAA, 0xBB}
		_, err := SplitAVCC(data, 4)
		if !errors.Is(err, ErrTruncatedLength) {
			t.Fatalf("want ErrTruncatedLength, got %v", err)
		}
	})
}

func TestSplitAVCCSkipsZeroLengthEntries(t *testing.T) {
	u1 := Unit{Header: Header{RefIDC: 1, Type: TypeSPS}, RBSP: []byte{0x11, 0x22}}
	u3 := Unit{Header: Header{RefIDC: 2, Type: TypePPS}, RBSP: []byte{0x33}}
	var data []byte
	var err error
	data, err = AppendAVCC(data, u1, 1)
	if err != nil {
		t.Fatalf("AppendAVCC(u1) unexpected error: %v", err)
	}
	data = append(data, 0x00)
	data, err = AppendAVCC(data, u3, 1)
	if err != nil {
		t.Fatalf("AppendAVCC(u3) unexpected error: %v", err)
	}
	parts, err := SplitAVCC(data, 1)
	if err != nil {
		t.Fatalf("SplitAVCC unexpected error: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("SplitAVCC produced %d parts, want 2", len(parts))
	}
	units := []Unit{u1, u3}
	for i, p := range parts {
		parsed, err := Parse(p)
		if err != nil {
			t.Fatalf("Parse(part %d) unexpected error: %v", i, err)
		}
		if parsed.Header != units[i].Header {
			t.Fatalf("part %d header = %+v, want %+v", i, parsed.Header, units[i].Header)
		}
		if !bytes.Equal(parsed.RBSP, units[i].RBSP) {
			t.Fatalf("part %d RBSP = %v, want %v", i, parsed.RBSP, units[i].RBSP)
		}
	}
}

func FuzzEscapeUnescapeRoundTrip(f *testing.F) {
	for _, u := range testutil.EBSPUnits() {
		f.Add(u)
	}
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00})
	f.Add([]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x02, 0x00, 0x00, 0x03})
	f.Add([]byte{0xFF, 0xFE, 0xFD})
	f.Add([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		got := Unescape(nil, Escape(nil, data))
		if !bytes.Equal(got, data) {
			t.Fatalf("round trip mismatch: data=%v got=%v", data, got)
		}
	})
}

func FuzzScannerNeverPanics(f *testing.F) {
	for _, st := range testutil.Streams() {
		f.Add(st)
	}
	f.Add(buildAnnexBStream(annexBTestUnits(), []bool{true, false}))
	f.Add([]byte{0x00, 0x00, 0x01})
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		s := NewScanner()
		for i := 0; i < len(data); i += 3 {
			end := i + 3
			if end > len(data) {
				end = len(data)
			}
			s.Append(data[i:end])
			for {
				if _, ok := s.Next(); !ok {
					break
				}
			}
		}
		s.Flush()
	})
}

func FuzzSplitAnnexB(f *testing.F) {
	for _, st := range testutil.Streams() {
		f.Add(st)
	}
	f.Add(buildAnnexBStream(annexBTestUnits(), []bool{true, false}))
	f.Add([]byte{0x00, 0x00, 0x01})
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x02, 0x03})
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, p := range SplitAnnexB(data) {
			Parse(p)
		}
	})
}

func FuzzSplitAVCC(f *testing.F) {
	for _, u := range testutil.EBSPUnits() {
		if b, err := AppendAVCC(nil, Unit{Header: Header{Type: Type(u[0] & 0x1F)}, RBSP: u[1:]}, 4); err == nil {
			f.Add(b)
		}
	}
	seed, _ := AppendAVCC(nil, Unit{Header: Header{RefIDC: 1, Type: TypeSPS}, RBSP: []byte{0x01, 0x02, 0x03}}, 4)
	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, ls := range []int{1, 2, 4} {
			SplitAVCC(data, ls)
		}
	})
}
