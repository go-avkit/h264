package decoder

import (
	"fmt"

	h264 "github.com/go-avkit/h264"
)

// sliceType is a slice's type with the questions the decoder asks of it.
type sliceType h264.SliceType

func (t sliceType) IsI() bool  { return h264.SliceType(t) == h264.SliceI }
func (t sliceType) IsP() bool  { return h264.SliceType(t) == h264.SliceP }
func (t sliceType) IsB() bool  { return h264.SliceType(t) == h264.SliceB }
func (t sliceType) IsSI() bool { return h264.SliceType(t) == h264.SliceSI }
func (t sliceType) IsSP() bool { return h264.SliceType(t) == h264.SliceSP }

func (t sliceType) String() string { return h264.SliceType(t).String() }

// refListMod is one instruction of ref_pic_list_modification.
type refListMod struct {
	IDC   uint32
	Value uint32
}

// mmco is one memory_management_control_operation with its arguments named as 7.4.3.3
// names them.
type mmco struct {
	Op                        uint32
	DifferenceOfPicNumsMinus1 uint32
	LongTermPicNum            uint32
	LongTermFrameIdx          uint32
	MaxLongTermFrameIdxPlus1  uint32
}

// weightEntry is the weighting of one reference, with the default (2^denominator,
// offset 0) filled in where the slice states none: the arithmetic of 8.4.2.3 is
// the same either way, and a consumer should not have to ask whether it was sent.
type weightEntry struct {
	LumaWeight   int32
	LumaOffset   int32
	ChromaWeight [2]int32
	ChromaOffset [2]int32
}

// predWeights are the weights of a slice.
type predWeights struct {
	LumaLog2WeightDenom   uint32
	ChromaLog2WeightDenom uint32
	L0                    []weightEntry
	L1                    []weightEntry
}

// sliceHeader is a slice header as the decoder uses it: the two halves
// h264.ParseSliceReferences returns, joined, with what the decoder derives from
// them (the references the slice uses, the quantiser, the weights with their
// defaults) done once.
type sliceHeader struct {
	FirstMBInSlice uint32
	SliceType      sliceType
	PPSID          uint32
	FrameNum       uint32
	IDR            bool
	NalRefIDC      uint8

	PicOrderCntLsb   uint32
	DeltaPicOrderCnt [2]int32

	DirectSpatialMvPred bool

	numRefIdxL0Active int
	numRefIdxL1Active int

	ModificationL0Present    bool
	ModificationL1Present    bool
	RefPicListModificationL0 []refListMod
	RefPicListModificationL1 []refListMod

	PredWeight predWeights

	NoOutputOfPriorPics   bool
	LongTermReference     bool
	AdaptiveRefPicMarking bool
	MMCOs                 []mmco

	CABACInitIDC uint32
	qpY          int

	DisableDeblockingFilterIDC uint32
	SliceAlphaC0OffsetDiv2     int32
	SliceBetaOffsetDiv2        int32

	// DataBit is where slice_data() begins in the unescaped payload.
	DataBit int
}

func (h *sliceHeader) NumRefIdxL0Active() int { return h.numRefIdxL0Active }
func (h *sliceHeader) NumRefIdxL1Active() int { return h.numRefIdxL1Active }
func (h *sliceHeader) QPY() int               { return h.qpY }

func newSliceHeader(h h264.SliceHeader, r h264.SliceReferences) *sliceHeader {
	s := &sliceHeader{
		FirstMBInSlice:             h.FirstMB,
		SliceType:                  sliceType(h.Type),
		PPSID:                      h.PPSID,
		FrameNum:                   h.FrameNum,
		IDR:                        h.IDR,
		NalRefIDC:                  h.RefIDC,
		PicOrderCntLsb:             h.POCLSB,
		DeltaPicOrderCnt:           h.DeltaPOC,
		DirectSpatialMvPred:        r.DirectSpatial,
		numRefIdxL0Active:          int(r.NumRefIdxL0Active),
		numRefIdxL1Active:          int(r.NumRefIdxL1Active),
		NoOutputOfPriorPics:        r.NoOutputOfPriorPics,
		LongTermReference:          r.LongTermReference,
		AdaptiveRefPicMarking:      r.AdaptiveMarking,
		CABACInitIDC:               r.CABACInit,
		qpY:                        int(r.QP),
		DisableDeblockingFilterIDC: r.DeblockingIDC,
		SliceAlphaC0OffsetDiv2:     r.AlphaC0OffsetDiv2,
		SliceBetaOffsetDiv2:        r.BetaOffsetDiv2,
		DataBit:                    r.DataBit,
	}
	if r.ModifyL0 != nil {
		s.ModificationL0Present = true
		for _, m := range r.ModifyL0 {
			s.RefPicListModificationL0 = append(s.RefPicListModificationL0, refListMod{IDC: m.Kind, Value: m.Value})
		}
	}
	if r.ModifyL1 != nil {
		s.ModificationL1Present = true
		for _, m := range r.ModifyL1 {
			s.RefPicListModificationL1 = append(s.RefPicListModificationL1, refListMod{IDC: m.Kind, Value: m.Value})
		}
	}
	for _, m := range r.Marking {
		o := mmco{Op: m.Operation}
		switch m.Operation {
		case 1:
			o.DifferenceOfPicNumsMinus1 = m.Value
		case 2:
			o.LongTermPicNum = m.Value
		case 3:
			o.DifferenceOfPicNumsMinus1, o.LongTermFrameIdx = m.Value, m.Extra
		case 4:
			o.MaxLongTermFrameIdxPlus1 = m.Value
		case 6:
			o.LongTermFrameIdx = m.Value
		}
		s.MMCOs = append(s.MMCOs, o)
	}
	if w := r.Weights; w != nil {
		s.PredWeight = predWeights{LumaLog2WeightDenom: w.LumaLog2Denom, ChromaLog2WeightDenom: w.ChromaLog2Denom}
		s.PredWeight.L0 = weightEntries(w.L0, w)
		s.PredWeight.L1 = weightEntries(w.L1, w)
	}
	return s
}

func weightEntries(in []h264.RefWeight, w *h264.PredWeights) []weightEntry {
	out := make([]weightEntry, len(in))
	for i, e := range in {
		out[i] = weightEntry{
			LumaWeight:   1 << w.LumaLog2Denom,
			ChromaWeight: [2]int32{1 << w.ChromaLog2Denom, 1 << w.ChromaLog2Denom},
		}
		if e.LumaStated {
			out[i].LumaWeight, out[i].LumaOffset = e.LumaWeight, e.LumaOffset
		}
		if e.ChromaStated {
			out[i].ChromaWeight, out[i].ChromaOffset = e.ChromaWeight, e.ChromaOffset
		}
	}
	return out
}

// header errors the decoder maps from the root readers' own.
var errNoSet = fmt.Errorf("slice refers to a parameter set that has not arrived")
