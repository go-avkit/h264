package syntaxfixture

import (
	"fmt"
)

type SliceType uint8

const (
	SliceP  SliceType = 0
	SliceB  SliceType = 1
	SliceI  SliceType = 2
	SliceSP SliceType = 3
	SliceSI SliceType = 4
)

func (t SliceType) Base() SliceType { return t % 5 }

func (t SliceType) IsI() bool { return t.Base() == SliceI }

func (t SliceType) IsP() bool { return t.Base() == SliceP }

func (t SliceType) IsB() bool { return t.Base() == SliceB }

func (t SliceType) IsSI() bool { return t.Base() == SliceSI }

func (t SliceType) IsSP() bool { return t.Base() == SliceSP }

func (t SliceType) AllSlicesSameType() bool { return t >= 5 }

func (t SliceType) String() string {
	switch t.Base() {
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
	return fmt.Sprintf("slice-type(%d)", uint8(t))
}

type RefPicListModification struct {
	IDC   uint32
	Value uint32
}

type MMCO struct {
	Op                        uint32
	DifferenceOfPicNumsMinus1 uint32
	LongTermPicNum            uint32
	LongTermFrameIdx          uint32
	MaxLongTermFrameIdxPlus1  uint32
}

type WeightEntry struct {
	LumaWeightFlag   bool
	LumaWeight       int32
	LumaOffset       int32
	ChromaWeightFlag bool
	ChromaWeight     [2]int32
	ChromaOffset     [2]int32
}

type PredWeightTable struct {
	LumaLog2WeightDenom   uint32
	ChromaLog2WeightDenom uint32
	L0                    []WeightEntry
	L1                    []WeightEntry
}

type SliceHeader struct {
	FirstMBInSlice uint32
	SliceType      SliceType
	PPSID          uint32
	ColourPlaneID  uint32
	FrameNum       uint32

	FieldPic    bool
	BottomField bool

	IDRPicID uint32

	PicOrderCntLsb         uint32
	DeltaPicOrderCntBottom int32
	DeltaPicOrderCnt       [2]int32

	RedundantPicCnt uint32

	DirectSpatialMvPred bool

	NumRefIdxActiveOverride bool
	NumRefIdxL0ActiveMinus1 uint32
	NumRefIdxL1ActiveMinus1 uint32

	RefPicListModificationL0 []RefPicListModification
	RefPicListModificationL1 []RefPicListModification
	ModificationL0Present    bool
	ModificationL1Present    bool

	PredWeight PredWeightTable

	NoOutputOfPriorPics   bool
	LongTermReference     bool
	AdaptiveRefPicMarking bool
	MMCOs                 []MMCO

	CABACInitIDC uint32

	SliceQPDelta int32
	SPForSwitch  bool
	SliceQSDelta int32

	DisableDeblockingFilterIDC uint32
	SliceAlphaC0OffsetDiv2     int32
	SliceBetaOffsetDiv2        int32

	IDR       bool
	NalRefIDC uint8
}

func (h *SliceHeader) MbaffFrame(s *SPS) bool {
	return s.MBAdaptiveFrameField && !h.FieldPic
}

func (h *SliceHeader) NumRefIdxL0Active(p *PPS) int {
	if h.NumRefIdxActiveOverride {
		return int(h.NumRefIdxL0ActiveMinus1) + 1
	}
	return int(p.NumRefIdxL0DefaultActiveMinus1) + 1
}

func (h *SliceHeader) NumRefIdxL1Active(p *PPS) int {
	if h.NumRefIdxActiveOverride {
		return int(h.NumRefIdxL1ActiveMinus1) + 1
	}
	return int(p.NumRefIdxL1DefaultActiveMinus1) + 1
}

func (h *SliceHeader) QPY(p *PPS) int { return p.SliceQPY(h.SliceQPDelta) }

type ParameterSets interface {
	SPS(id uint32) *SPS
	PPS(id uint32) *PPS
}
