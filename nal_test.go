// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-avkit/bitstream"
)

func TestSplitAnnexBTakesBothStartCodeLengths(t *testing.T) {
	// A four-byte start code is a three-byte one with a leading zero, and the
	// zero belongs to the gap: a unit that kept it would have its type read out
	// of a byte nobody wrote there.
	stream := []byte{
		0, 0, 0, 1, 0x67, 'a', 'b', // SPS, four-byte start code
		0, 0, 1, 0x68, 'c', // PPS, three-byte
		0, 0, 0, 1, 0x65, 'd', 'e', 'f', 0, 0, // IDR, with padding
	}
	units, err := SplitAnnexB(stream)
	if err != nil {
		t.Fatalf("SplitAnnexB: %v", err)
	}
	if len(units) != 3 {
		t.Fatalf("%d units, want 3: %+v", len(units), units)
	}
	want := []struct {
		typ     UnitType
		payload string
	}{{UnitSPS, "ab"}, {UnitPPS, "c"}, {UnitIDR, "def"}}
	for i, w := range want {
		if units[i].Type != w.typ {
			t.Errorf("unit %d is type %d, want %d", i+1, units[i].Type, w.typ)
		}
		if got := string(units[i].Payload); got != w.payload {
			t.Errorf("unit %d payload %q, want %q -- padding or a start code byte was kept",
				i+1, got, w.payload)
		}
	}
}

func TestAStreamWithNoStartCodeIsRefused(t *testing.T) {
	if _, err := SplitAnnexB([]byte{0x67, 'a', 'b'}); !errors.Is(err, bitstream.ErrNoStartCode) {
		t.Errorf("err = %v, want bitstream.ErrNoStartCode", err)
	}
}

// TestAStreamCutAfterItsLastSeparatorKeepsWhatCameBefore: a cut stream is not a
// malformed one, and refusing the whole of it would lose every unit that did
// arrive -- which is the normal state of a download in progress.
func TestAStreamCutAfterItsLastSeparatorKeepsWhatCameBefore(t *testing.T) {
	stream := []byte{0, 0, 1, 0x67, 'a', 0, 0, 1}
	units, err := SplitAnnexB(stream)
	if err != nil {
		t.Fatalf("SplitAnnexB: %v", err)
	}
	if len(units) != 1 || units[0].Type != UnitSPS {
		t.Fatalf("%d units, want just the SPS: %+v", len(units), units)
	}
}

func TestSplitLengthPrefixed(t *testing.T) {
	stream := []byte{
		0, 0, 0, 3, 0x67, 'a', 'b',
		0, 0, 0, 2, 0x68, 'c',
	}
	units, err := SplitLengthPrefixed(stream, 4)
	if err != nil {
		t.Fatalf("SplitLengthPrefixed: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("%d units, want 2", len(units))
	}
	if units[0].Type != UnitSPS || string(units[0].Payload) != "ab" {
		t.Errorf("first unit: %+v", units[0])
	}
	if units[1].Type != UnitPPS || string(units[1].Payload) != "c" {
		t.Errorf("second unit: %+v", units[1])
	}
}

// TestALengthPastTheEndIsRefused.
//
// ⛔ A wrong length size reads a length out of payload, and a length read out of
// payload is enormous. Walking on from it would either panic on a slice or, worse,
// land on a plausible boundary and report units nobody wrote.
func TestALengthPastTheEndIsRefused(t *testing.T) {
	if _, err := SplitLengthPrefixed([]byte{0, 0, 0, 99, 0x67}, 4); !errors.Is(err, bitstream.ErrLengthOverrun) {
		t.Errorf("err = %v, want bitstream.ErrLengthOverrun", err)
	}
	// A trailing fragment too short to hold a length at all.
	if _, err := SplitLengthPrefixed([]byte{0, 0}, 4); !errors.Is(err, bitstream.ErrLengthOverrun) {
		t.Errorf("short tail: err = %v, want bitstream.ErrLengthOverrun", err)
	}
	if _, err := SplitLengthPrefixed([]byte{0x67}, 5); err == nil {
		t.Error("a length size of 5 was accepted")
	}
}

func TestUnescapeDropsOnlyARealEscape(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want []byte
	}{
		{"an escape", []byte{0, 0, 3, 1}, []byte{0, 0, 1}},
		{"two escapes", []byte{0, 0, 3, 0, 0, 3, 2}, []byte{0, 0, 0, 0, 2}},
		// ⛔ The control: a three after ONE zero, or after none, is payload.
		// Dropping every three after a zero would eat data and misalign
		// everything a reader did afterwards.
		{"a three after one zero", []byte{0, 3, 1}, []byte{0, 3, 1}},
		{"a three after none", []byte{9, 3, 1}, []byte{9, 3, 1}},
		// The byte after an escape starts the count again, so 00 00 03 00 00 03
		// is two escapes and not one followed by payload.
		{"a zero after an escape", []byte{0, 0, 3, 0, 0, 0}, []byte{0, 0, 0, 0, 0}},
		{"nothing to do", []byte{1, 2, 3}, []byte{1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Unit{Payload: tc.in}.Unescape()
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Unescape(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestEveryLengthSizeIsReadBigEndian.
//
// ⛔ avcC states this size, and all four widths occur. A size read with the wrong
// byte order or the wrong width lands on a plausible boundary rather than
// failing, so each one is stated here against a length only the right reading
// produces.
func TestEveryLengthSizeIsReadBigEndian(t *testing.T) {
	for _, tc := range []struct {
		size   int
		prefix []byte
	}{
		{1, []byte{2}},
		{2, []byte{0, 2}},
		{3, []byte{0, 0, 2}},
		{4, []byte{0, 0, 0, 2}},
	} {
		stream := append(append([]byte(nil), tc.prefix...), 0x67, 'x')
		units, err := SplitLengthPrefixed(stream, tc.size)
		if err != nil {
			t.Errorf("size %d: %v", tc.size, err)
			continue
		}
		if len(units) != 1 || units[0].Type != UnitSPS || string(units[0].Payload) != "x" {
			t.Errorf("size %d: %+v", tc.size, units)
		}
	}
}

// TestAZeroLengthUnitIsSkippedRatherThanRefused: a zero-length entry is padding
// an encoder is allowed to write, and it describes no unit at all.
func TestAZeroLengthUnitIsSkippedRatherThanRefused(t *testing.T) {
	stream := []byte{0, 0, 0, 0, 0, 0, 0, 2, 0x68, 'y'}
	units, err := SplitLengthPrefixed(stream, 4)
	if err != nil {
		t.Fatalf("SplitLengthPrefixed: %v", err)
	}
	if len(units) != 1 || units[0].Type != UnitPPS {
		t.Fatalf("%d units, want just the PPS: %+v", len(units), units)
	}
}

// TestTwoSeparatorsInARowHoldNoUnitBetweenThem.
//
// ⛔ This used to be a refusal, and moving the framing into go-avkit/bitstream
// changed it: the shared framing drops a unit with no bytes rather than refusing
// the stream for it. The change is deliberate and worth pinning -- two separators
// in a row are padding some muxers write, and losing the units around them would
// be a worse answer than ignoring the gap. Nothing above can see an empty unit,
// which is why reading the header byte needs no check for one.
func TestTwoSeparatorsInARowHoldNoUnitBetweenThem(t *testing.T) {
	units, err := SplitAnnexB([]byte{0, 0, 1, 0, 0, 1, 0x67, 'a'})
	if err != nil {
		t.Fatalf("SplitAnnexB: %v", err)
	}
	if len(units) != 1 || units[0].Type != UnitSPS {
		t.Fatalf("%d units, want just the one with bytes in it: %+v", len(units), units)
	}
	// A length that claims a byte the stream does not hold is still refused: that
	// is a length read wrongly, not a gap.
	if _, err := SplitLengthPrefixed([]byte{0, 0, 0, 1}, 4); !errors.Is(err, bitstream.ErrLengthOverrun) {
		t.Errorf("length-prefixed: err = %v, want bitstream.ErrLengthOverrun", err)
	}
}

// TestAStreamOfNothingButSeparatorsHoldsNoUnit is the control on the refusal
// above: every separator is followed by nothing, so there is no unit at all and
// saying so is not the same as saying the stream is malformed.
func TestAStreamOfNothingButSeparatorsHoldsNoUnit(t *testing.T) {
	if _, err := SplitAnnexB([]byte{0, 0, 1}); !errors.Is(err, bitstream.ErrNoStartCode) {
		t.Errorf("err = %v, want bitstream.ErrNoStartCode", err)
	}
}
