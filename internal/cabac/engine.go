package cabac

import (
	"errors"

	kcabac "github.com/go-videodsp/kernels/cabac"

	"github.com/go-avkit/h264/internal/bits"
)

var (
	ErrNotAligned = errors.New("go264/cabac: slice data does not start byte aligned")
	ErrAlignment  = errors.New("go264/cabac: cabac_alignment_one_bit is not one")
	ErrInitIDC    = errors.New("go264/cabac: cabac_init_idc out of range")
)

const NumContexts = 1024

type context struct {
	state uint8
	mps   uint8
}

// Decoder is the arithmetic decoding engine of 9.3.1.2 and 9.3.3.2.
//
// The specification reads codIOffset one bit at a time. This keeps the same
// arithmetic with the offset scaled by 2^7 and seven bits of lookahead below it
// (value = codIOffset<<7 | next seven bits), refilled a byte at a time, so a
// decision compares against range<<7 and a renormalisation is a shift. FFmpeg
// does the same (libavcodec/cabac_functions.h keeps its offset scaled and refills
// 16 bits at once). The bit position the specification would be at is
// recoverable (consumed), and the reader is put back there when a terminate
// bin of 1 ends the arithmetic code, which is the only time anything else reads
// the stream (I_PCM samples, the end of the slice).
//
// The engine state lives in a kernels cabac.State so that a whole residual
// block is decoded by one call of the kernel (ResidualBlock), which shares the
// state and the context array with the single-bin methods below.
type Decoder struct {
	r   *bits.Reader
	st  kcabac.State       // Data, Ptr, Value (codIOffset<<7 | lookahead), Rng, BitsNeeded (-8..-1), Overflow
	ctx [NumContexts]uint8 // pStateIdx<<1 | valMPS: one byte per context, see merged.go
}

// lpsShift is the renormalisation shift after a least probable symbol, by
// rangeTabLPS >> 3: the number of doublings that bring the new range to at least
// 256 (6 for 6 and 7, 5 for 8..15, ... 1 for 128..255).
var lpsShift = [32]uint8{
	6, 5, 4, 4, 3, 3, 3, 3,
	2, 2, 2, 2, 2, 2, 2, 2,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
}

func clip3(lo, hi, v int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (d *Decoder) initContexts(table *[1024][2]int8, sliceQPY int) {
	qp := clip3(0, 51, sliceQPY)
	for i := 0; i < NumContexts; i++ {
		m := int(table[i][0])
		n := int(table[i][1])
		pre := clip3(1, 126, ((m*qp)>>4)+n)
		if pre <= 63 {
			d.ctx[i] = uint8(63-pre) << 1
		} else {
			d.ctx[i] = uint8(pre-64)<<1 | 1
		}
	}
}

// consumed is the position, in bits from the start of the data, that the
// specification's bit-by-bit reader would be at: everything fetched, less the
// lookahead that was not yet shifted into codIOffset.
func (d *Decoder) consumed() int { return d.st.Ptr*8 + d.st.BitsNeeded + 1 }

// Overrun is how many bits the specification's reader would have had to read
// past the end of the data (each read past the end yields a zero bit).
func (d *Decoder) Overrun() int { return max(0, d.consumed()-len(d.st.Data)*8) }

// fetch ORs the next byte (zero past the end) into the low byte of the value.
func (d *Decoder) fetch(shift int) {
	if d.st.Ptr < len(d.st.Data) {
		d.st.Value |= uint32(d.st.Data[d.st.Ptr]) << uint(shift)
	}
	d.st.Ptr++
}

// sync puts the reader where the specification's would be.
func (d *Decoder) sync() {
	_ = d.r.Seek(min(d.consumed(), len(d.st.Data)*8))
}

func (d *Decoder) LevelOverflow() bool { return d.st.Overflow != 0 }

func (d *Decoder) Init(r *bits.Reader, sliceQPY int, intra bool, initIDC uint32) error {
	d.st.Overflow = 0
	if !r.ByteAligned() {
		if err := d.consumeAlignmentBits(r); err != nil {
			return err
		}
	}
	if intra {
		d.initContexts(&contextInitI, sliceQPY)
	} else {
		if initIDC > 2 {
			return ErrInitIDC
		}
		d.initContexts(contextInitPB[initIDC], sliceQPY)
	}
	return d.Restart(r)
}

func (d *Decoder) consumeAlignmentBits(r *bits.Reader) error {
	for !r.ByteAligned() {
		b, err := r.ReadBit()
		if err != nil {
			return err
		}
		if b != 1 {
			return ErrAlignment
		}
	}
	return nil
}

func (d *Decoder) State(ctxIdx int) (state, mps uint8) {
	return d.ctx[ctxIdx] >> 1, d.ctx[ctxIdx] & 1
}

// Restart starts the arithmetic code afresh at the reader's position, which must be
// on a byte boundary (9.3.1.2): at the start of the slice data, and after the
// samples of an I_PCM macroblock.
func (d *Decoder) Restart(r *bits.Reader) error {
	if !r.ByteAligned() {
		return ErrNotAligned
	}
	if r.BitsLeft() < 9 {
		return bits.ErrOverrun
	}
	d.r = r
	d.st.Data = r.Data()
	d.st.Ptr = r.BitPos() / 8
	d.st.Rng = 510
	// codIOffset is the first nine bits; the seven below them are the lookahead.
	d.st.Value = uint32(d.st.Data[d.st.Ptr])<<8 | uint32(d.st.Data[d.st.Ptr+1])
	d.st.Ptr += 2
	d.st.BitsNeeded = -8
	return nil
}

// DecodeDecision is DecodeDecision of 9.3.3.2.1 and its RenormD.
func (d *Decoder) DecodeDecision(ctxIdx int) uint32 {
	st := &d.st
	s := d.ctx[ctxIdx]
	lps := uint32(mergedLPS[s][st.Rng>>6&3])
	st.Rng -= lps
	scaled := st.Rng << 7
	if st.Value < scaled {
		d.ctx[ctxIdx] = mergedNextMPS[s]
		// codIRange is below 512: the range fell below 256 exactly when bit 8 is
		// clear, and then one doubling restores it. Written without a branch
		// (the unpredictable one of the engine); the refill below fires once
		// per eight shifts and is predictable.
		sh := uint(^st.Rng>>8) & 1
		st.Rng <<= sh
		st.Value <<= sh
		st.BitsNeeded += int(sh)
		if st.BitsNeeded >= 0 {
			d.fetch(0)
			st.BitsNeeded = -8
		}
		return uint32(s & 1)
	}
	n := uint(lpsShift[lps>>3])
	st.Value = (st.Value - scaled) << n
	st.Rng = lps << n
	d.ctx[ctxIdx] = mergedNextLPS[s]
	st.BitsNeeded += int(n)
	if st.BitsNeeded >= 0 {
		d.fetch(st.BitsNeeded)
		st.BitsNeeded -= 8
	}
	return uint32(s&1) ^ 1
}

// DecodeBypass is DecodeBypass of 9.3.3.2.3.
func (d *Decoder) DecodeBypass() uint32 {
	st := &d.st
	st.Value <<= 1
	st.BitsNeeded++
	if st.BitsNeeded >= 0 {
		d.fetch(0)
		st.BitsNeeded = -8
	}
	scaled := st.Rng << 7
	if st.Value >= scaled {
		st.Value -= scaled
		return 1
	}
	return 0
}

// DecodeTerminate is DecodeTerminate of 9.3.3.2.2.3. A bin of 1 ends the code with
// no renormalisation, and the reader is left at the bit the specification's would
// be at, where the I_PCM samples or the end of the slice follow.
func (d *Decoder) DecodeTerminate() uint32 {
	st := &d.st
	st.Rng -= 2
	scaled := st.Rng << 7
	if st.Value >= scaled {
		d.sync()
		return 1
	}
	if scaled < 256<<7 {
		st.Rng = scaled >> 6
		st.Value <<= 1
		st.BitsNeeded++
		if st.BitsNeeded >= 0 {
			d.fetch(0)
			st.BitsNeeded = -8
		}
	}
	return 0
}
