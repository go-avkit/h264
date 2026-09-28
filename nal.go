// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

// Package h264 reads an H.264/AVC bitstream in pure Go, with no libavcodec
// linkage and no external binaries.
//
// The framing and the bit reading live in go-avkit/bitstream, which H.265 shares:
// the two formats separate their units identically and differ only in how the
// header bytes of a unit are read. This package is that difference, and what
// stands on it.
package h264

import "github.com/go-avkit/bitstream"

// UnitType is what a NAL unit holds. Only the ones this package acts on are
// named; the rest travel as their number.
type UnitType uint8

// The unit types a reader has to tell apart.
const (
	UnitNonIDR UnitType = 1
	UnitIDR    UnitType = 5
	UnitSEI    UnitType = 6
	UnitSPS    UnitType = 7
	UnitPPS    UnitType = 8
	UnitAUD    UnitType = 9
)

// Unit is one NAL unit: its header and its payload, still escaped.
//
// Payload is a window into the caller's bytes and is not copied. Unescape is what
// makes a readable copy, and it is separate because most units are walked past
// rather than read: a stream of a thousand slices costs one allocation per
// parameter set this way, not one per unit.
type Unit struct {
	Type    UnitType
	RefIDC  uint8 // nal_ref_idc: how much a unit matters to what follows
	Payload []byte
}

// SplitAnnexB finds the NAL units of a byte stream, the form a .264 file and an
// MPEG-TS stream carry, and reads each one's header.
//
// The separating is bitstream's, which knows nothing about headers; the ONE byte
// of header is read here, because that is what differs between this format and
// H.265.
func SplitAnnexB(data []byte) ([]Unit, error) {
	raw, err := bitstream.SplitAnnexB(data)
	if err != nil {
		return nil, err
	}
	return units(raw)
}

// SplitLengthPrefixed finds the NAL units of the form an MP4 sample carries,
// where each is preceded by its length in lengthSize bytes, and reads each one's
// header.
//
// lengthSize comes from the avcC record and is 1, 2 or 4 in practice.
func SplitLengthPrefixed(data []byte, lengthSize int) ([]Unit, error) {
	raw, err := bitstream.SplitLengthPrefixed(data, lengthSize)
	if err != nil {
		return nil, err
	}
	return units(raw)
}

// units reads the header byte of each raw unit.
//
// ⛔ bitstream drops a unit with no bytes at all, so nothing here can be empty:
// the framing hands back only units that hold something. A check for it would be a
// branch no input can reach.
func units(raw [][]byte) ([]Unit, error) {
	out := make([]Unit, 0, len(raw))
	for _, body := range raw {
		out = append(out, Unit{
			Type:    UnitType(body[0] & 0x1F),
			RefIDC:  body[0] >> 5 & 3,
			Payload: body[1:],
		})
	}
	return out, nil
}

// Unescape undoes the escaping inside this unit's payload.
func (u Unit) Unescape() []byte { return bitstream.Unescape(u.Payload) }

// sticky reads fields in a straight line, keeping the first error.
//
// The shape of a parameter set stays visible in the code instead of being buried
// under a check after every field, and a zero read past the end is never handed to
// a caller because err is checked before anything is returned.
type sticky struct {
	r   *bitstream.Reader
	err error
}

func newSticky(data []byte) *sticky { return &sticky{r: bitstream.NewReader(data)} }

func (s *sticky) bit() uint32 {
	if s.err != nil {
		return 0
	}
	v, err := s.r.Bit()
	s.err = err
	return v
}

func (s *sticky) bits(n int) uint32 {
	if s.err != nil {
		return 0
	}
	v, err := s.r.Bits(n)
	s.err = err
	return v
}

func (s *sticky) flag() bool { return s.bit() == 1 }

func (s *sticky) ue() uint32 {
	if s.err != nil {
		return 0
	}
	v, err := s.r.UE()
	s.err = err
	return v
}

func (s *sticky) se() int32 {
	if s.err != nil {
		return 0
	}
	v, err := s.r.SE()
	s.err = err
	return v
}

// moreData says whether any syntax element remains before the bits that end a
// payload. It is bitstream's question; this only stops asking it once a read has
// failed.
func (s *sticky) moreData() bool {
	if s.err != nil {
		return false
	}
	return s.r.MoreData()
}
