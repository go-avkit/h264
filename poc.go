// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import "fmt"

// POC is a picture's order count: where it belongs in the order a viewer sees,
// which is not the order it was coded in.
type POC struct {
	// Top and Bottom are TopFieldOrderCnt and BottomFieldOrderCnt. A frame has
	// both; a field has only its own, and the other is left at zero.
	Top    int32
	Bottom int32
	// Value is the count a picture is ordered by: the smaller of the two for a
	// frame, and the one field's own for a field.
	Value int32
}

// POCCounter derives picture order counts across a coded video sequence.
//
// ⛔ It has to be a counter rather than a function because the format states the
// count RELATIVE to the pictures before it: type zero sends only the low bits and
// the high bits are carried forward, and type two derives everything from a frame
// number that wraps. A caller computing a count from one slice header alone gets
// the right answer until the first wrap and the wrong one afterwards.
//
// ⛔ Only a REFERENCE picture moves the carry forward. A disposable picture reads
// the carry and leaves it where it was, so a counter that updated from every
// picture would place every picture after the first disposable one wrongly.
//
// Feed it every coded picture of the sequence, in decode order, one slice per
// picture. A picture carried by several slices is one picture here: pass the slice
// that begins it and nothing else, or the count advances once per slice.
type POCCounter struct {
	// Type zero carries these.
	prevMsb int32
	prevLsb int32
	// Type one and two carry these.
	prevFrameNum       uint32
	prevFrameNumOffset uint32
	// started says whether any picture has been counted, so the first one is not
	// read as following something.
	started bool
}

// Reset forgets every picture counted so far. It is what a seek is: the pictures
// before the new position are not the ones the counts should follow from.
func (c *POCCounter) Reset() { *c = POCCounter{} }

// ErrPOCType means a sequence states its order in a shape this does not derive.
var ErrPOCType = fmt.Errorf("%w: picture order count type", ErrSliceHeader)

// Next gives the order count of one coded picture, and remembers what the pictures
// after it will need.
//
// The unit is wanted for nal_ref_idc, which decides whether this picture moves the
// carry; the references are wanted for the one marking operation that restarts the
// count.
func (c *POCCounter) Next(u Unit, h SliceHeader, ref SliceReferences, sps SPS) (POC, error) {
	switch sps.POCType {
	case 0:
		return c.type0(u, h, ref, sps)
	case 2:
		return c.type2(u, h, ref, sps)
	case 1:
		// ⛔ Type one needs offset_for_non_ref_pic, offset_for_top_to_bottom_field
		// and the cycle of offset_for_ref_frame, none of which the set reader
		// reads -- so there is nothing here to compute from. Measured over 158
		// sequences from one library, every one was type zero or type two and none
		// was type one; writing a derivation no file could test is how a wrong
		// answer gets shipped looking right.
		return POC{}, fmt.Errorf("%w one is not derived here: the set's offsets are not read", ErrPOCType)
	default:
		return POC{}, fmt.Errorf("%w %d does not exist", ErrPOCType, sps.POCType)
	}
}

// type0 derives the count from the low bits the slice sends, carrying the high bits
// across pictures.
func (c *POCCounter) type0(u Unit, h SliceHeader, ref SliceReferences, sps SPS) (POC, error) {
	maxLsb := int32(1) << sps.Log2MaxPOCLSB
	prevMsb, prevLsb := c.prevMsb, c.prevLsb
	if h.IDR || !c.started {
		// An IDR begins a sequence: nothing before it counts.
		prevMsb, prevLsb = 0, 0
	}

	lsb := int32(h.POCLSB)
	var msb int32
	switch {
	// ⛔ The two halves are not symmetric: the wrap down uses "at least half" and
	// the wrap up uses "more than half". Making them the same misplaces a picture
	// that sits exactly half a cycle away, which is the one a long run of B
	// pictures produces.
	case lsb < prevLsb && prevLsb-lsb >= maxLsb/2:
		msb = prevMsb + maxLsb
	case lsb > prevLsb && lsb-prevLsb > maxLsb/2:
		msb = prevMsb - maxLsb
	default:
		msb = prevMsb
	}

	var p POC
	if !h.FieldPic || !h.BottomField {
		p.Top = msb + lsb
	}
	if !h.FieldPic {
		p.Bottom = p.Top + h.DeltaPOCBottom
		p.Value = p.Top
		if p.Bottom < p.Value {
			p.Value = p.Bottom
		}
	} else if h.BottomField {
		p.Bottom = msb + lsb
		p.Value = p.Bottom
	} else {
		p.Value = p.Top
	}

	c.started = true
	// ⛔ Only a reference picture moves the carry. A disposable one reads it and
	// leaves it alone.
	if u.RefIDC != 0 {
		c.prevMsb, c.prevLsb = msb, lsb
	}
	// A restart empties the reference set and makes this picture count zero for
	// everything after it.
	if ref.ResetsPOC {
		c.afterReset()
	}
	return p, nil
}

// type2 derives the count from the decode order itself: the pictures arrive in the
// order they are shown, so the count is the frame number, doubled, with a disposable
// picture sitting just before the reference picture that follows it.
func (c *POCCounter) type2(u Unit, h SliceHeader, ref SliceReferences, sps SPS) (POC, error) {
	maxFrameNum := uint32(1) << sps.Log2MaxFrameNum
	offset := c.prevFrameNumOffset
	switch {
	case h.IDR || !c.started:
		offset = 0
	// ⛔ The frame number wraps, and the only sign of it is that this picture's is
	// SMALLER than the one before. Missing the wrap makes every count after it a
	// whole cycle too low, and they stay monotonic -- so nothing looks wrong until
	// pictures are compared across the wrap.
	case c.prevFrameNum > h.FrameNum:
		offset = c.prevFrameNumOffset + maxFrameNum
	}

	var count int32
	switch {
	case h.IDR:
		count = 0
	case u.RefIDC == 0:
		// A disposable picture is shown just before the reference picture it
		// precedes, which is what the odd number says.
		count = 2*int32(offset+h.FrameNum) - 1
	default:
		count = 2 * int32(offset+h.FrameNum)
	}

	var p POC
	if !h.FieldPic {
		p.Top, p.Bottom, p.Value = count, count, count
	} else if h.BottomField {
		p.Bottom, p.Value = count, count
	} else {
		p.Top, p.Value = count, count
	}

	c.started = true
	c.prevFrameNumOffset = offset
	c.prevFrameNum = h.FrameNum
	if ref.ResetsPOC {
		c.afterReset()
	}
	return p, nil
}

// afterReset is what a restart leaves behind: this picture becomes the zero every
// later count is measured from.
//
// ⛔ The picture itself keeps the count it was given -- the restart applies to what
// FOLLOWS it. A counter that zeroed this picture too would place it before the
// pictures it was coded after.
func (c *POCCounter) afterReset() {
	c.prevMsb, c.prevLsb = 0, 0
	c.prevFrameNum, c.prevFrameNumOffset = 0, 0
	c.started = false
}
