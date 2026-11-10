// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/nal"
	"github.com/go-avkit/h264/internal/syntax"
)

type oldSets struct {
	sps map[uint32]*syntax.SPS
	pps map[uint32]*syntax.PPS
}

func (o oldSets) SPS(id uint32) *syntax.SPS { return o.sps[id] }
func (o oldSets) PPS(id uint32) *syntax.PPS { return o.pps[id] }

// streamsOfTheCorpus are the paths of every stream the repository ships.
func streamsOfTheCorpus(t *testing.T) []string {
	t.Helper()
	var paths []string
	for _, pattern := range []string{"conformance/*.264", "fuzzseed/*"} {
		p, err := filepath.Glob(filepath.Join("testdata", pattern))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p...)
	}
	return paths
}

// TestSlicesReadAsTheDecoderReadsThem walks every stream of the repository with
// both readers of the syntax, the decoder's own (internal/syntax) and this
// package's, and requires that they agree on every slice header field the decoder
// uses, on the picture and sequence sets, on the scaling lists they resolve to
// and on where the slice data begins.
func TestSlicesReadAsTheDecoderReadsThem(t *testing.T) {
	slices, resolved := 0, 0
	for _, path := range streamsOfTheCorpus(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		units, err := SplitAnnexB(data)
		if err != nil {
			t.Fatal(err)
		}
		newSPS, newPPS := map[uint32]SPS{}, map[uint32]PPS{}
		old := oldSets{sps: map[uint32]*syntax.SPS{}, pps: map[uint32]*syntax.PPS{}}
		for _, u := range units {
			switch u.Type {
			case UnitSPS:
				s, err := ParseSPS(u)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				o, err := syntax.ParseSPS(u.Unescape())
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				newSPS[s.ID], old.sps[o.ID] = s, o
			case UnitPPS:
				var sps SPS
				for _, s := range newSPS { // the one set of these streams
					sps = s
				}
				p, err := ParsePPS(u, sps)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				o, err := syntax.ParsePPS(u.Unescape(), old.SPS)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				newPPS[p.ID], old.pps[o.ID] = p, o
				if p.CABAC != o.CABAC || p.Transform8x8 != o.Transform8x8Mode ||
					p.ScalingPresent != o.PicScalingMatrixPresent || p.SecondChromaQPOffset != o.SecondChromaQPIndexOffset ||
					p.InitQP != 26+o.PicInitQPMinus26 {
					t.Fatalf("%s: picture set %d differs:\n%+v\n%+v", path, p.ID, p, o)
				}
				osps := old.sps[o.SPSID]
				if got, want := resolvedOf(p.ResolvedScaling(newSPS[p.SPSID])), resolvedOf(o.ResolvedScalingLists(osps)); got != want {
					t.Fatalf("%s: resolved lists of picture set %d differ", path, p.ID)
				}
				resolved++
			case UnitIDR, UnitNonIDR:
				id, err := SlicePPSID(u)
				if err != nil {
					t.Fatal(err)
				}
				p := newPPS[id]
				h, ref, err := ParseSliceReferences(u, newSPS[p.SPSID], p)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				r := bits.NewReader(u.Unescape())
				o, _, _, err := syntax.ParseSliceHeader(r, nal.Header{Type: nal.Type(u.Type), RefIDC: u.RefIDC}, old)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				if f := sameSlice(h, ref, p, o, old.pps[id], r.BitPos()); f != "" {
					t.Fatalf("%s: slice %d: %s differs:\n%+v\n%+v", path, slices, f, h, o)
				}
				slices++
			}
		}
	}
	if slices < 200 || resolved < 20 {
		t.Fatalf("compared only %d slices and %d picture sets", slices, resolved)
	}
}

// resolvedOf makes a pair of resolved lists comparable.
func resolvedOf(a [6][16]uint8, b [6][64]uint8) [2]any { return [2]any{a, b} }

func sameSlice(h SliceHeader, ref SliceReferences, p PPS, o *syntax.SliceHeader, op *syntax.PPS, bitPos int) string {
	switch {
	case h.FirstMB != o.FirstMBInSlice:
		return "FirstMB"
	case uint32(h.Type) != uint32(o.SliceType.Base()):
		return "Type"
	case h.AllOfType != o.SliceType.AllSlicesSameType():
		return "AllOfType"
	case h.PPSID != o.PPSID || h.FrameNum != o.FrameNum || h.FieldPic != o.FieldPic || h.BottomField != o.BottomField:
		return "identity"
	case h.IDR != o.IDR || h.RefIDC != o.NalRefIDC || h.IDRPicID != o.IDRPicID:
		return "IDR"
	case h.POCLSB != o.PicOrderCntLsb || h.DeltaPOCBottom != o.DeltaPicOrderCntBottom || h.DeltaPOC != o.DeltaPicOrderCnt:
		return "picture order count"
	case h.RedundantPicCnt != o.RedundantPicCnt:
		return "RedundantPicCnt"
	case h.Type == SliceB && ref.DirectSpatial != o.DirectSpatialMvPred:
		return "DirectSpatial"
	case int(ref.NumRefIdxL0Active) != o.NumRefIdxL0Active(op) || (h.Type == SliceB && int(ref.NumRefIdxL1Active) != o.NumRefIdxL1Active(op)):
		return "active references"
	case len(ref.ModifyL0) != len(o.RefPicListModificationL0) || len(ref.ModifyL1) != len(o.RefPicListModificationL1):
		return "list modification count"
	case ref.NoOutputOfPriorPics != o.NoOutputOfPriorPics || ref.LongTermReference != o.LongTermReference || ref.AdaptiveMarking != o.AdaptiveRefPicMarking:
		return "marking"
	case len(ref.Marking) != len(o.MMCOs):
		return "marking operations"
	case h.Type != SliceI && p.CABAC && ref.CABACInit != o.CABACInitIDC:
		return "CABACInit"
	case int(ref.QP) != o.QPY(op):
		return "QP"
	case p.DeblockingControl && (ref.DeblockingIDC != o.DisableDeblockingFilterIDC ||
		(ref.DeblockingIDC != 1 && (ref.AlphaC0OffsetDiv2 != o.SliceAlphaC0OffsetDiv2 || ref.BetaOffsetDiv2 != o.SliceBetaOffsetDiv2))):
		return "deblocking"
	case ref.DataBit != bitPos:
		return "DataBit"
	}
	for i, m := range ref.ModifyL0 {
		if m.Kind != o.RefPicListModificationL0[i].IDC || m.Value != o.RefPicListModificationL0[i].Value {
			return "list 0 modification"
		}
	}
	for i, m := range ref.Marking {
		n := o.MMCOs[i]
		want := map[uint32]uint32{1: n.DifferenceOfPicNumsMinus1, 2: n.LongTermPicNum, 3: n.DifferenceOfPicNumsMinus1, 4: n.MaxLongTermFrameIdxPlus1, 6: n.LongTermFrameIdx}[m.Operation]
		if m.Operation != n.Op || m.Value != want || (m.Operation == 3 && m.Extra != n.LongTermFrameIdx) {
			return "marking operation"
		}
	}
	if ref.Weights != nil {
		w := ref.Weights
		if w.LumaLog2Denom != o.PredWeight.LumaLog2WeightDenom || len(w.L0) != len(o.PredWeight.L0) || len(w.L1) != len(o.PredWeight.L1) {
			return "weight table shape"
		}
		for i, e := range w.L0 {
			n := o.PredWeight.L0[i]
			if e.LumaStated != n.LumaWeightFlag || (e.LumaStated && (e.LumaWeight != n.LumaWeight || e.LumaOffset != n.LumaOffset)) ||
				e.ChromaStated != n.ChromaWeightFlag || (e.ChromaStated && (e.ChromaWeight != n.ChromaWeight || e.ChromaOffset != n.ChromaOffset)) {
				return "weights"
			}
		}
	}
	return ""
}
