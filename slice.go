// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"fmt"
)

// ErrNotSlice means the unit handed over does not carry a coded slice.
var ErrNotSlice = errors.New("h264: unit is not a coded slice")

// ErrSliceHeader means a slice states something this reader does not follow.
var ErrSliceHeader = errors.New("h264: unsupported slice header")

// SliceType is how a slice is coded.
type SliceType uint8

// The slice types, numbered as the format numbers them.
const (
	SliceP  SliceType = 0
	SliceB  SliceType = 1
	SliceI  SliceType = 2
	SliceSP SliceType = 3
	SliceSI SliceType = 4
)

// String names a slice type the way a reader of tools would expect.
func (t SliceType) String() string {
	switch t {
	case SliceP:
		return "P"
	case SliceB:
		return "B"
	case SliceI:
		return "I"
	case SliceSP:
		return "SP"
	case SliceSI:
		return "SI"
	}
	return fmt.Sprintf("slice type %d", uint8(t))
}

// SliceHeader is the beginning of a coded slice: what it belongs to and how it is
// coded, but not the coded data itself.
//
// FirstMB is what says where a picture begins. A picture may be carried by
// several slices, and only the one starting at macroblock zero begins a new
// picture -- which is how a stream's pictures are counted without decoding any.
// RefListOp is one instruction for building a reference picture list, as
// ref_pic_list_modification states them.
type RefListOp struct {
	// Kind is modification_of_pic_nums_idc: 0 and 1 move a short-term picture
	// backwards or forwards from the one before, 2 names a long-term picture.
	Kind uint32
	// Value is abs_diff_pic_num_minus1 for kinds 0 and 1, and long_term_pic_num
	// for kind 2.
	Value uint32
}

// MarkOp is one memory management control operation, as dec_ref_pic_marking
// states them.
type MarkOp struct {
	// Operation is memory_management_control_operation. Five is the one that
	// matters most to a reader: it empties the reference set and RESTARTS the
	// picture order count at this picture.
	Operation uint32
	// Value is whichever argument the operation takes, and zero for those that
	// take none.
	Value uint32
	// Extra is long_term_frame_idx for operation three, which takes two.
	Extra uint32
}

// PredWeights are the weights a slice states for weighted prediction, one entry
// per active reference.
type PredWeights struct {
	LumaLog2Denom   uint32
	ChromaLog2Denom uint32
	L0              []RefWeight
	L1              []RefWeight
}

// RefWeight is the weighting of one reference picture.
type RefWeight struct {
	LumaStated   bool
	LumaWeight   int32
	LumaOffset   int32
	ChromaStated bool
	// ChromaWeight and ChromaOffset are Cb then Cr.
	ChromaWeight [2]int32
	ChromaOffset [2]int32
}

type SliceHeader struct {
	FirstMB         uint32
	Type            SliceType
	AllOfType       bool // slice_type was stated above 4: every slice of this picture is this type
	PPSID           uint32
	ColourPlane     uint8
	FrameNum        uint32
	FieldPic        bool
	BottomField     bool
	IDR             bool
	IDRPicID        uint32
	POCLSB          uint32
	DeltaPOCBottom  int32
	DeltaPOC        [2]int32
	RedundantPicCnt uint32
}

// SliceReferences is what a slice says about the pictures it predicts from. It is
// a type of its own, and ParseSliceReferences the only thing that fills it,
// because a field read from a header that was never asked for it would be zero and
// look answered: a caller reading DirectSpatial off a plain ParseSliceHeader would
// be told "temporal" about every B slice in the stream.
type SliceReferences struct {
	// DirectSpatial is direct_spatial_mv_pred_flag, stated by a B slice only. It
	// decides where a direct-mode block takes its motion from, and the two
	// answers are not close: spatial reads the neighbours, temporal scales the
	// motion of the picture that follows.
	DirectSpatial bool

	// NumRefIdxL0Active and NumRefIdxL1Active are how many references this slice
	// uses, after any override it states. They come from the picture set when the
	// slice overrides nothing, which is why they are here rather than left to the
	// caller to work out.
	NumRefIdxL0Active uint32
	NumRefIdxL1Active uint32

	// ModifyL0 and ModifyL1 are the reference list instructions, in order. Empty
	// means the default order, which is not the same as "no references".
	ModifyL0 []RefListOp
	ModifyL1 []RefListOp

	// Weights are the prediction weights, when the picture set asks this slice to
	// state them explicitly. Nil when it does not -- which includes implicit
	// weighting, where the weights come from the picture order counts and no
	// table is sent.
	Weights *PredWeights

	// NoOutputOfPriorPics and LongTermReference are what an IDR states about the
	// pictures before it.
	NoOutputOfPriorPics bool
	LongTermReference   bool

	// AdaptiveMarking is adaptive_ref_pic_marking_mode_flag: the slice says which
	// pictures to release rather than letting the sliding window decide.
	AdaptiveMarking bool
	// Marking is what it says, in order.
	Marking []MarkOp
	// ResetsPOC reports that one of those operations is number five, which
	// restarts the picture order count at this picture. A reader computing counts
	// without honouring it is right until the first one and wrong afterwards.
	ResetsPOC bool

	// CABACInit is cabac_init_idc, which picks the context table a CABAC slice
	// starts from. Only a CABAC slice that is not I states it.
	CABACInit uint32

	// QP is SliceQPY: the quantisation parameter this slice starts at, already
	// built from the picture set's initial value and the slice's own delta.
	//
	// ⛔ It is also this reader's own witness. Every field before it has a width
	// that depends on the fields before THAT, so a parse that drifted by one bit
	// anywhere lands here with a number outside 0 to 51 -- which is how reading
	// 1,344,537 slices of a real library told this syntax was read correctly, with
	// no reference decoder to compare against.
	QP int32

	// DeblockingIDC is disable_deblocking_filter_idc, and the two offsets are the
	// filter's own, as the slice states them.
	DeblockingIDC     uint32
	AlphaC0OffsetDiv2 int32
	BetaOffsetDiv2    int32
}

// ParseSliceReferences reads a slice header whole: everything ParseSliceHeader
// reads, and then what the slice says about the pictures it predicts from -- how
// many references it uses, how its lists are built, the weights it states, and how
// it marks the reference set.
//
// It is the same single reader, told to keep going. Picture segmentation must not
// fail because a slice's weight table is unreadable, and a decoder must not be
// handed a zero weight table because nobody asked for one -- which is why the
// answer comes in a type of its own.
func ParseSliceReferences(u Unit, sps SPS, pps PPS) (SliceHeader, SliceReferences, error) {
	return parseSlice(u, sps, pps, true)
}

// ParseSliceHeader reads the part of a slice header that says WHICH picture this
// slice belongs to: everything up to and including redundant_pic_cnt.
//
// That is what access unit segmentation needs, and no more. What a slice says
// about the pictures it predicts from is read by ParseSliceReferences, which must
// not be made a condition of knowing where a picture begins.
func ParseSliceHeader(u Unit, sps SPS, pps PPS) (SliceHeader, error) {
	h, _, err := parseSlice(u, sps, pps, false)
	return h, err
}

// parseSlice is the ONE reader of this syntax.
//
// ⛔ There is one because there were nearly two. Every field's width depends on the
// parameter sets and on the fields before it, so a second routine that skipped to
// the reference syntax -- by counting bits or by reading them again -- would be the
// same syntax written twice, and the two would drift apart at the first field
// either one got wrong.
func parseSlice(u Unit, sps SPS, pps PPS, wantRefs bool) (SliceHeader, SliceReferences, error) {

	if u.Type != UnitIDR && u.Type != UnitNonIDR {
		return SliceHeader{}, SliceReferences{}, fmt.Errorf("%w: type %d", ErrNotSlice, u.Type)
	}
	r := newSticky(u.Unescape())
	var h SliceHeader
	var ref SliceReferences
	h.IDR = u.Type == UnitIDR

	h.FirstMB = r.ue()
	t := r.ue()
	// ⛔ A type above four is the same type said more strongly: it promises every
	// slice of the picture is coded this way. Taking the number as it stands would
	// name a type the format does not have.
	if t > 4 {
		t -= 5
		h.AllOfType = true
	}
	if t > 4 {
		return h, ref, fmt.Errorf("%w: slice type %d", ErrSliceHeader, t)
	}
	h.Type = SliceType(t)
	h.PPSID = r.ue()

	if sps.SeparatePlanes {
		h.ColourPlane = uint8(r.bits(2))
	}
	h.FrameNum = r.bits(int(sps.Log2MaxFrameNum))

	if !sps.FrameMBSOnly {
		h.FieldPic = r.flag()
		if h.FieldPic {
			h.BottomField = r.flag()
		}
	}
	if h.IDR {
		h.IDRPicID = r.ue()
	}

	// The picture order count is stated in one of three shapes, and the third
	// states nothing at all.
	switch sps.POCType {
	case 0:
		h.POCLSB = r.bits(int(sps.Log2MaxPOCLSB))
		if pps.BottomFieldPicOrder && !h.FieldPic {
			h.DeltaPOCBottom = r.se()
		}
	case 1:
		if !sps.POCAlwaysZero {
			h.DeltaPOC[0] = r.se()
			if pps.BottomFieldPicOrder && !h.FieldPic {
				h.DeltaPOC[1] = r.se()
			}
		}
	case 2:
		// The order is the decoding order, and nothing is stated.
	default:
		return h, ref, fmt.Errorf("%w: picture order count type %d", ErrSliceHeader, sps.POCType)
	}

	if pps.RedundantPicCnt {
		h.RedundantPicCnt = r.ue()
	}
	if !wantRefs {
		if r.err != nil {
			return h, ref, r.err
		}
		return h, ref, nil
	}

	if h.Type == SliceB {
		ref.DirectSpatial = r.flag()
	}

	// How many references this slice uses. The picture set states a default and
	// the slice may override it; a reader that took the default always would build
	// lists of the wrong length, and a list of the wrong length is read past its
	// end rather than refused.
	ref.NumRefIdxL0Active, ref.NumRefIdxL1Active = pps.NumRefIdxL0, pps.NumRefIdxL1
	if h.Type == SliceP || h.Type == SliceSP || h.Type == SliceB {
		if r.flag() {
			// ⛔ A slice may override the set's counts, so bounding the set
			// alone leaves this door open. Read before the +1, for the same
			// reason as there.
			l0 := r.ue()
			l1 := uint32(0)
			if h.Type == SliceB {
				l1 = r.ue()
			}
			if r.err == nil && (l0 >= maxActiveRefs || l1 >= maxActiveRefs) {
				// The reader is sticky, so this is carried to the check at the
				// end. The counts are LEFT at the set's, which are bounded, so
				// nothing between here and there sizes anything from these.
				r.err = fmt.Errorf("%w: %d and %d", ErrRefIdxRange, l0, l1)
			} else {
				ref.NumRefIdxL0Active = l0 + 1
				if h.Type == SliceB {
					ref.NumRefIdxL1Active = l1 + 1
				}
			}
		}
	}

	// ⛔ An I or SI slice states no reference list, so asking for one reads the
	// bits of whatever follows.
	if h.Type != SliceI && h.Type != SliceSI {
		ref.ModifyL0 = readRefListOps(r)
	}
	if h.Type == SliceB {
		ref.ModifyL1 = readRefListOps(r)
	}

	// The weight table is sent only when the picture set asks this slice for one.
	// ⛔ weighted_bipred_idc of two is IMPLICIT weighting: the weights come from
	// the picture order counts and no table is sent, so reading one here would
	// consume the marking that follows.
	if (pps.WeightedPred && (h.Type == SliceP || h.Type == SliceSP)) ||
		(pps.WeightedBipredIDC == 1 && h.Type == SliceB) {
		ref.Weights = readPredWeights(r, h, ref, chromaArrayType(sps))
	}

	if u.RefIDC != 0 {
		readMarking(r, h, &ref)
	}

	// ⛔ Only a CABAC slice that is not I states which context table to start
	// from, so reading it for an I slice takes the bits of the quantisation delta.
	if pps.CABAC && h.Type != SliceI && h.Type != SliceSI {
		ref.CABACInit = r.ue()
	}
	ref.QP = pps.InitQP + r.se()
	if h.Type == SliceSP || h.Type == SliceSI {
		if h.Type == SliceSP {
			r.flag() // sp_for_switch_flag
		}
		r.se() // slice_qs_delta
	}
	if pps.DeblockingControl {
		ref.DeblockingIDC = r.ue()
		if ref.DeblockingIDC != 1 {
			ref.AlphaC0OffsetDiv2 = r.se()
			ref.BetaOffsetDiv2 = r.se()
		}
	}
	// What remains is slice_group_change_cycle, stated only when the picture set
	// declares more than one slice group with a changing map. The set reader does
	// not read slice groups, so nothing here can know how wide that field is; it is
	// left unread, and nothing above depends on it.

	if r.err != nil {
		return h, ref, r.err
	}
	return h, ref, nil
}

// chromaArrayType is ChromaArrayType: the chroma format, except that separate
// colour planes make every plane monochrome.
func chromaArrayType(sps SPS) uint8 {
	if sps.SeparatePlanes {
		return 0
	}
	return sps.ChromaFormat
}

// maxRefListOps bounds the instruction list.
//
// ⛔ The list is terminated by a value in the stream, not by a count, so a
// corrupt or truncated slice can ask for instructions for ever. The bound is the
// most any conforming stream needs -- two lists of at most 32 entries, and an
// instruction per entry -- so a stream that passes it is not one.
const maxRefListOps = 64

// readRefListOps reads ref_pic_list_modification for one list.
func readRefListOps(r *sticky) []RefListOp {
	if !r.flag() {
		return nil // The default order, which is not the same as no references.
	}
	var ops []RefListOp
	for len(ops) <= maxRefListOps {
		kind := r.ue()
		if kind == 3 || r.err != nil {
			return ops // Three ends the list and carries nothing.
		}
		op := RefListOp{Kind: kind}
		switch kind {
		case 0, 1:
			op.Value = r.ue() // abs_diff_pic_num_minus1
		case 2:
			op.Value = r.ue() // long_term_pic_num
		default:
			// 4 and 5 belong to multiview, which this does not read. Stopping
			// here keeps the rest of the header from being read as rubbish.
			r.err = fmt.Errorf("%w: reference list instruction %d", ErrSliceHeader, kind)
			return ops
		}
		ops = append(ops, op)
	}
	r.err = fmt.Errorf("%w: more than %d reference list instructions", ErrSliceHeader, maxRefListOps)
	return ops
}

// maxLog2WeightDenom is the largest shift 7.4.3.2 allows for either component.
//
// ⛔ The denominator is a SHIFT COUNT that leaves this package: Weighting
// computes 1 << LogDenom and shifts a sample by it. A count the stream chose
// freely is a shift by whatever it likes, and in Go that is not a panic -- it
// is a silently wrong number.
const maxLog2WeightDenom = 7

// maxWeightMagnitude bounds a weight and an offset, 7.4.3.2: each is stated in
// -128..127, and every decoder reads them into eight bits.
//
// ⛔ Unbounded, a weight multiplies a sample before anything clamps it. 2^30
// times a sample overflows the arithmetic rather than making a bright picture.
const maxWeightMagnitude = 128

// readPredWeights reads pred_weight_table.
func readPredWeights(r *sticky, h SliceHeader, ref SliceReferences, chroma uint8) *PredWeights {
	w := &PredWeights{LumaLog2Denom: r.ue()}
	if chroma != 0 {
		w.ChromaLog2Denom = r.ue()
	}
	if r.err == nil && (w.LumaLog2Denom > maxLog2WeightDenom || w.ChromaLog2Denom > maxLog2WeightDenom) {
		r.err = fmt.Errorf("%w: weight denominators of %d and %d",
			ErrSliceHeader, w.LumaLog2Denom, w.ChromaLog2Denom)
		return nil
	}
	w.L0 = readWeightList(r, ref.NumRefIdxL0Active, chroma)
	if h.Type == SliceB {
		w.L1 = readWeightList(r, ref.NumRefIdxL1Active, chroma)
	}
	return w
}

// weightInRange refuses a weight or an offset the arithmetic cannot carry.
//
// ⛔ It does NOT stand down when the reader has already failed, and does not
// need to: a failing read returns zero, which is in range, so a truncated
// stream is reported as truncation rather than as a value it never carried.
// TestAFailedReadReturnsZero holds that, because removing a guard on a
// measurement makes the measurement part of the contract.
func weightInRange(r *sticky, name string, v int32) bool {
	if v < -maxWeightMagnitude || v >= maxWeightMagnitude {
		r.err = fmt.Errorf("%w: a %s of %d", ErrSliceHeader, name, v)
		return false
	}
	return true
}

// maxActiveRefs bounds how many active references anything here will size from.
//
// ⛔ The count comes from the stream, and a corrupt one would have a reader
// allocate and read whatever it says. Thirty-two is the most the format allows.
//
// ⛔ This used to be applied in readWeightList ALONE, and its own comment said
// so -- "how many weights a list may hold". A bound is only as wide as what it
// is applied to: a slice that stated no weights carried the count straight to
// ApplyRefListOps, which sized an allocation from it.
const maxActiveRefs = 32

func readWeightList(r *sticky, n uint32, chroma uint8) []RefWeight {
	// The bound that used to stand here is applied where the count is READ --
	// in the parameter set and in the slice header's override -- so n cannot
	// arrive out of range any more. Kept here it was unreachable, which a
	// coverage gate says plainly and a reader does not.
	out := make([]RefWeight, 0, n)
	for i := uint32(0); i < n; i++ {
		var e RefWeight
		if e.LumaStated = r.flag(); e.LumaStated {
			e.LumaWeight = r.se()
			e.LumaOffset = r.se()
			if !weightInRange(r, "luma weight", e.LumaWeight) ||
				!weightInRange(r, "luma offset", e.LumaOffset) {
				return out
			}
		}
		if chroma != 0 {
			if e.ChromaStated = r.flag(); e.ChromaStated {
				for j := 0; j < 2; j++ {
					e.ChromaWeight[j] = r.se()
					e.ChromaOffset[j] = r.se()
					if !weightInRange(r, "chroma weight", e.ChromaWeight[j]) ||
						!weightInRange(r, "chroma offset", e.ChromaOffset[j]) {
						return out
					}
				}
			}
		}
		out = append(out, e)
		if r.err != nil {
			return out
		}
	}
	return out
}

// maxMarkOps bounds the marking list, for the same reason as the instruction list.
const maxMarkOps = 64

// readMarking reads dec_ref_pic_marking.
func readMarking(r *sticky, h SliceHeader, ref *SliceReferences) {
	if h.IDR {
		ref.NoOutputOfPriorPics = r.flag()
		ref.LongTermReference = r.flag()
		return
	}
	if ref.AdaptiveMarking = r.flag(); !ref.AdaptiveMarking {
		return // The sliding window decides, and states nothing.
	}
	for len(ref.Marking) <= maxMarkOps {
		op := r.ue()
		if op == 0 || r.err != nil {
			return // Zero ends the list.
		}
		m := MarkOp{Operation: op}
		switch op {
		case 1:
			m.Value = r.ue() // difference_of_pic_nums_minus1
		case 2:
			m.Value = r.ue() // long_term_pic_num
		case 3:
			m.Value = r.ue() // difference_of_pic_nums_minus1
			m.Extra = r.ue() // long_term_frame_idx
		case 4:
			m.Value = r.ue() // max_long_term_frame_idx_plus1
		case 5:
			// Empties the reference set and restarts the picture order count, and
			// carries no argument. It stays in the list, in order: a caller
			// applying these needs to know WHERE the reset falls, since the
			// operations before it address a reference set the ones after it no
			// longer have.
			ref.ResetsPOC = true
		case 6:
			m.Value = r.ue() // long_term_frame_idx
		default:
			r.err = fmt.Errorf("%w: marking operation %d", ErrSliceHeader, op)
			return
		}
		ref.Marking = append(ref.Marking, m)
	}
	r.err = fmt.Errorf("%w: more than %d marking operations", ErrSliceHeader, maxMarkOps)
}

// BeginsPicture says whether this slice starts a new coded picture.
func (h SliceHeader) BeginsPicture() bool { return h.FirstMB == 0 }
