// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

// Package h264 reads an H.264/AVC bitstream in pure Go, with no libavcodec
// linkage and no external binaries.
//
// It begins at the layer everything else stands on: finding the NAL units in a
// stream, undoing the escaping inside one, and reading the variable-length
// integers H.264 writes its parameter sets with.
package h264

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Errors a stream can be refused with.
var (
	// ErrNoStartCode means a byte stream holds no NAL unit at all.
	ErrNoStartCode = errors.New("h264: no start code in the byte stream")
	// ErrLengthOverrun means a length-prefixed unit claims more bytes than
	// the stream holds.
	ErrLengthOverrun = errors.New("h264: NAL length runs past the end")
	// ErrEmptyUnit means a NAL unit has no header byte.
	ErrEmptyUnit = errors.New("h264: NAL unit is empty")
)

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
// Payload is a window into the caller's bytes and is not copied. Unescape is
// what makes a readable copy, and it is separate because most units are walked
// past rather than read: a stream of a thousand slices costs one allocation per
// parameter set this way, not one per unit.
type Unit struct {
	Type    UnitType
	RefIDC  uint8 // nal_ref_idc: how much a unit matters to what follows
	Payload []byte
}

// SplitAnnexB finds the NAL units of a byte stream, the form a .264 file and an
// MPEG-TS stream carry.
//
// Units are separated by a start code of two or more zero bytes and a one. Three
// zeros and a one is the same separator with a leading zero, so the scan looks
// for the three-byte form and lets a fourth zero belong to the gap rather than
// to the unit -- a unit that began with a stray zero would have a nal_ref_idc
// and a type read out of it that were never written.
//
// Trailing zero bytes are dropped from each unit for the same reason in reverse:
// an encoder is allowed to pad, and the padding is not payload.
func SplitAnnexB(data []byte) ([]Unit, error) {
	starts := startCodes(data)
	if len(starts) == 0 {
		return nil, ErrNoStartCode
	}
	units := make([]Unit, 0, len(starts))
	for i, at := range starts {
		end := len(data)
		if i+1 < len(starts) {
			end = starts[i+1].at
		}
		body := data[at.after:end]
		// The next start code may have been found by its three-byte form while
		// a zero before it belongs to the gap.
		for len(body) > 0 && body[len(body)-1] == 0 {
			body = body[:len(body)-1]
		}
		u, err := newUnit(body)
		if err != nil {
			// A stream whose last separator is followed by nothing is a stream
			// that was cut, not one that is malformed: what came before it
			// still reads.
			if errors.Is(err, ErrEmptyUnit) && i == len(starts)-1 {
				break
			}
			return nil, fmt.Errorf("unit %d: %w", i+1, err)
		}
		units = append(units, u)
	}
	if len(units) == 0 {
		return nil, ErrNoStartCode
	}
	return units, nil
}

// startCode is where one starts and where the unit after it begins.
type startCode struct{ at, after int }

// startCodes finds every 00 00 01 in data.
func startCodes(data []byte) []startCode {
	var out []startCode
	for i := 0; i+2 < len(data); {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			out = append(out, startCode{at: i, after: i + 3})
			i += 3
			continue
		}
		i++
	}
	return out
}

// SplitLengthPrefixed finds the NAL units of the form an MP4 sample carries,
// where each unit is preceded by its length in lengthSize bytes.
//
// lengthSize comes from the avcC record and is 1, 2 or 4 in practice; anything
// else is refused rather than guessed, since a wrong size reads a length out of
// payload and would walk the stream into nonsense.
func SplitLengthPrefixed(data []byte, lengthSize int) ([]Unit, error) {
	switch lengthSize {
	case 1, 2, 3, 4:
	default:
		return nil, fmt.Errorf("h264: NAL length size %d is not 1 to 4", lengthSize)
	}
	var units []Unit
	for off := 0; off < len(data); {
		if off+lengthSize > len(data) {
			return nil, fmt.Errorf("%w: %d bytes left, a length needs %d",
				ErrLengthOverrun, len(data)-off, lengthSize)
		}
		n := int(readLength(data[off:off+lengthSize], lengthSize))
		off += lengthSize
		if n == 0 {
			continue
		}
		if off+n > len(data) {
			return nil, fmt.Errorf("%w: %d bytes claimed, %d left", ErrLengthOverrun, n, len(data)-off)
		}
		u, err := newUnit(data[off : off+n])
		if err != nil {
			return nil, fmt.Errorf("unit at %d: %w", off, err)
		}
		units = append(units, u)
		off += n
	}
	return units, nil
}

// readLength reads a big-endian length of 1 to 4 bytes.
func readLength(b []byte, size int) uint32 {
	switch size {
	case 1:
		return uint32(b[0])
	case 2:
		return uint32(binary.BigEndian.Uint16(b))
	case 3:
		return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
	default:
		return binary.BigEndian.Uint32(b)
	}
}

// newUnit reads a unit's one header byte.
func newUnit(body []byte) (Unit, error) {
	if len(body) == 0 {
		return Unit{}, ErrEmptyUnit
	}
	return Unit{
		Type:    UnitType(body[0] & 0x1F),
		RefIDC:  body[0] >> 5 & 3,
		Payload: body[1:],
	}, nil
}

// Unescape undoes the escaping inside a NAL unit's payload.
//
// H.264 may not carry three consecutive bytes that look like a start code, so an
// encoder writes 00 00 03 where it means 00 00 and the reader drops the three.
// ⛔ Only a three that follows exactly two zeros is an escape: dropping every
// three after any zero would eat payload, and the byte after the escape is
// whatever it is -- including another zero, which starts the count again.
func (u Unit) Unescape() []byte {
	out := make([]byte, 0, len(u.Payload))
	zeros := 0
	for _, b := range u.Payload {
		if zeros == 2 && b == 3 {
			zeros = 0
			continue
		}
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, b)
	}
	return out
}
