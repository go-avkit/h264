// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import "errors"

// ErrShort means the bitstream ended in the middle of a field.
var ErrShort = errors.New("h264: bitstream ends inside a field")

// ErrTooLong means an Exp-Golomb code claims more leading zeros than any value
// H.264 states could need.
var ErrTooLong = errors.New("h264: Exp-Golomb code is longer than any value needs")

// maxLeadingZeros bounds an Exp-Golomb prefix.
//
// ⛔ It is here because a run of zero bytes -- padding, or a truncated unit read
// as payload -- otherwise counts leading zeros until the stream ends and then
// shifts a value nobody wrote. 32 is past anything the format states and keeps
// the refusal a refusal rather than a hang on a very long stream.
const maxLeadingZeros = 32

// Reader reads the fields of a NAL unit's payload: single bits, fixed-width
// values, and the Exp-Golomb integers H.264 writes its parameter sets with.
//
// It is given an UNESCAPED payload. Escaping is undone by Unit.Unescape, and
// keeping the two apart means a caller that only walks past units never pays for
// a copy of one.
type Reader struct {
	data []byte
	pos  int // bits consumed
}

// NewReader reads the bits of data.
func NewReader(data []byte) *Reader { return &Reader{data: data} }

// Pos is how many bits have been read, which is what lets a caller say where a
// refusal happened rather than only that one did.
func (r *Reader) Pos() int { return r.pos }

// Bit reads one bit.
func (r *Reader) Bit() (uint32, error) {
	if r.pos >= len(r.data)*8 {
		return 0, ErrShort
	}
	b := r.data[r.pos/8]
	shift := 7 - uint(r.pos%8)
	r.pos++
	return uint32(b>>shift) & 1, nil
}

// Bits reads n bits, most significant first.
func (r *Reader) Bits(n int) (uint32, error) {
	var v uint32
	for i := 0; i < n; i++ {
		b, err := r.Bit()
		if err != nil {
			return 0, err
		}
		v = v<<1 | b
	}
	return v, nil
}

// Flag reads one bit as a flag.
func (r *Reader) Flag() (bool, error) {
	b, err := r.Bit()
	return b == 1, err
}

// UE reads an unsigned Exp-Golomb integer: n zeros, a one, then n more bits,
// giving a value of 2^n - 1 plus those bits.
func (r *Reader) UE() (uint32, error) {
	zeros := 0
	for {
		b, err := r.Bit()
		if err != nil {
			return 0, err
		}
		if b == 1 {
			break
		}
		zeros++
		if zeros > maxLeadingZeros {
			return 0, ErrTooLong
		}
	}
	if zeros == 0 {
		return 0, nil
	}
	rest, err := r.Bits(zeros)
	if err != nil {
		return 0, err
	}
	return (1<<uint(zeros) - 1) + rest, nil
}

// SE reads a signed Exp-Golomb integer, which is the unsigned one folded so that
// 1 means +1, 2 means -1, 3 means +2, and so on.
func (r *Reader) SE() (int32, error) {
	v, err := r.UE()
	if err != nil {
		return 0, err
	}
	if v%2 == 0 {
		// An even code is a negative value, and zero is zero.
		return -int32(v / 2), nil
	}
	return int32(v/2) + 1, nil
}
