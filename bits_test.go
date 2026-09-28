// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"testing"
)

// fromBits packs a string of '0' and '1' into bytes, so a test states the code
// it means instead of a hex value nobody can check by eye.
func fromBits(s string) []byte {
	var out []byte
	for i, c := range s {
		if i%8 == 0 {
			out = append(out, 0)
		}
		if c == '1' {
			out[i/8] |= 1 << (7 - uint(i%8))
		}
	}
	return out
}

func TestUnsignedExpGolomb(t *testing.T) {
	for _, tc := range []struct {
		code string
		want uint32
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
	} {
		r := NewReader(fromBits(tc.code))
		got, err := r.UE()
		if err != nil {
			t.Errorf("%s: %v", tc.code, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %d, want %d", tc.code, got, tc.want)
		}
	}
}

func TestSignedExpGolomb(t *testing.T) {
	// The folding: an odd code is positive, an even one negative, and zero is
	// the only code that means zero.
	for _, tc := range []struct {
		code string
		want int32
	}{
		{"1", 0},
		{"010", 1},
		{"011", -1},
		{"00100", 2},
		{"00101", -2},
		{"00110", 3},
		{"00111", -3},
	} {
		r := NewReader(fromBits(tc.code))
		got, err := r.SE()
		if err != nil {
			t.Errorf("%s: %v", tc.code, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %d, want %d", tc.code, got, tc.want)
		}
	}
}

// TestACodeThatRunsOffTheEndIsRefused.
//
// ⛔ A prefix whose suffix is missing must not be completed from nothing: a
// reader returning zeros would hand back a small, plausible value where the
// stream states none, and a parameter set built from it would describe a picture
// nobody encoded.
func TestACodeThatRunsOffTheEndIsRefused(t *testing.T) {
	// ⛔ The promise has to outrun the BYTE, not just the bits written: a NAL
	// payload is byte-aligned, so a reader ends at a byte boundary and a code
	// written short is completed by the padding of its own last byte. Seven
	// leading zeros and a one fill one byte exactly and promise seven more bits
	// that no second byte provides.
	r := NewReader(fromBits("00000001"))
	if _, err := r.UE(); !errors.Is(err, ErrShort) {
		t.Errorf("err = %v, want ErrShort", err)
	}
	// The premise of the paragraph above, asserted: within one byte the padding
	// does complete the code, and that is not a defect but what byte alignment
	// means.
	if v, err := NewReader(fromBits("00011")).UE(); err != nil {
		t.Errorf("a code completed by its own padding: %v", err)
	} else if v == 0 {
		t.Error("the padding read as nothing")
	}
	if _, err := NewReader(nil).Bit(); !errors.Is(err, ErrShort) {
		t.Errorf("empty: err = %v, want ErrShort", err)
	}
}

// TestARunOfZerosIsRefusedRatherThanCounted is the control on the bound: padding
// and a truncated unit both read as a long run of zeros, and counting them to the
// end of the stream would shift a value nobody wrote.
func TestARunOfZerosIsRefusedRatherThanCounted(t *testing.T) {
	zeros := make([]byte, 64) // 512 leading zeros
	if _, err := NewReader(zeros).UE(); !errors.Is(err, ErrTooLong) {
		t.Errorf("err = %v, want ErrTooLong", err)
	}
}

func TestBitsAndPosSayWhereAReaderIs(t *testing.T) {
	r := NewReader([]byte{0b1010_1100, 0b1111_0000})
	if v, err := r.Bits(4); err != nil || v != 0b1010 {
		t.Fatalf("Bits(4) = %b, %v", v, err)
	}
	if r.Pos() != 4 {
		t.Errorf("Pos = %d, want 4", r.Pos())
	}
	if v, err := r.Bits(8); err != nil || v != 0b1100_1111 {
		t.Errorf("Bits(8) across a byte boundary = %b, %v", v, err)
	}
}

// TestSignedCodeRefusesWhatTheUnsignedOneDid: SE is UE folded, so a refusal of
// the code underneath has to reach the caller rather than becoming a zero.
func TestSignedCodeRefusesWhatTheUnsignedOneDid(t *testing.T) {
	if _, err := NewReader(nil).SE(); !errors.Is(err, ErrShort) {
		t.Errorf("err = %v, want ErrShort", err)
	}
	if _, err := NewReader(make([]byte, 64)).SE(); !errors.Is(err, ErrTooLong) {
		t.Errorf("err = %v, want ErrTooLong", err)
	}
}

// TestBitsRefusesAWidthItCannotFill covers the middle of a fixed-width read: the
// first bits are there and the last are not, which is where a reader that padded
// would hand back a value nobody wrote.
func TestBitsRefusesAWidthItCannotFill(t *testing.T) {
	if _, err := NewReader([]byte{0xFF}).Bits(9); !errors.Is(err, ErrShort) {
		t.Errorf("err = %v, want ErrShort", err)
	}
}

func TestLeftAndPeekAnswerWithoutConsuming(t *testing.T) {
	r := NewReader([]byte{0b1010_0000})
	if r.Left() != 8 {
		t.Fatalf("Left = %d, want 8", r.Left())
	}
	if _, err := r.Bits(3); err != nil {
		t.Fatal(err)
	}
	if r.Left() != 5 {
		t.Errorf("Left = %d, want 5", r.Left())
	}
	// ⛔ Peek must not consume: the whole point is to ask what is left without
	// spending the field being asked about.
	v, err := r.Peek(5)
	if err != nil {
		t.Fatal(err)
	}
	if v != 0b0_0000 {
		t.Errorf("Peek = %05b", v)
	}
	if r.Left() != 5 {
		t.Errorf("Peek consumed %d bits", 5-r.Left())
	}
	if _, err := r.Peek(6); !errors.Is(err, ErrShort) {
		t.Errorf("Peek past the end: err = %v, want ErrShort", err)
	}
	// And a refused Peek leaves the position where it was.
	if r.Left() != 5 {
		t.Errorf("a refused Peek moved the reader to %d bits left", r.Left())
	}
}
