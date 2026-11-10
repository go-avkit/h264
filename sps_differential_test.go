// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/go-avkit/h264/internal/syntax"
)

// sameSPS compares what this reader and the decoder's own (internal/syntax, which
// this one is replacing) made of the same set. It names the first field that
// differs, or returns "".
func sameSPS(a SPS, b *syntax.SPS) string {
	switch {
	case a.Profile != b.ProfileIDC:
		return "Profile"
	case a.ConstraintFlags != b.ConstraintSet:
		return "ConstraintFlags"
	case a.Level != b.LevelIDC:
		return "Level"
	case a.ID != b.ID:
		return "ID"
	case uint32(a.ChromaFormat) != b.ChromaFormatIDC:
		return "ChromaFormat"
	case a.SeparatePlanes != b.SeparateColourPlane:
		return "SeparatePlanes"
	case uint32(a.BitDepthLuma) != b.BitDepthLumaMinus8+8 || uint32(a.BitDepthChroma) != b.BitDepthChromaMinus8+8:
		return "BitDepth"
	case a.TransformBypass != b.QpprimeYZeroTransformBypass:
		return "TransformBypass"
	case a.ScalingPresent != b.SeqScalingMatrixPresent:
		return "ScalingPresent"
	case a.Scaling.List4x4 != b.ScalingList4x4 || a.Scaling.List8x8 != b.ScalingList8x8:
		return "Scaling lists"
	case a.Scaling.Present4x4 != b.ScalingList4x4Present || a.Scaling.Present8x8 != b.ScalingList8x8Present:
		return "Scaling present"
	case a.Scaling.UseDefault4x4 != b.UseDefaultScaling4x4 || a.Scaling.UseDefault8x8 != b.UseDefaultScaling8x8:
		return "Scaling defaults"
	case uint32(a.Log2MaxFrameNum) != b.Log2MaxFrameNumMinus4+4:
		return "Log2MaxFrameNum"
	case a.POCType != b.PicOrderCntType:
		return "POCType"
	case a.POCType == 0 && uint32(a.Log2MaxPOCLSB) != b.Log2MaxPicOrderCntLsbMinus4+4:
		return "Log2MaxPOCLSB"
	case a.POCType == 1 && (a.POCAlwaysZero != b.DeltaPicOrderAlwaysZero ||
		a.OffsetForNonRefPic != b.OffsetForNonRefPic ||
		a.OffsetForTopToBottomField != b.OffsetForTopToBottomField ||
		!slices.Equal(a.OffsetForRefFrame, b.OffsetForRefFrame)):
		return "POC offsets"
	case a.MaxNumRefFrames != b.MaxNumRefFrames:
		return "MaxNumRefFrames"
	case a.GapsAllowed != b.GapsInFrameNumValueAllowed:
		return "GapsAllowed"
	case int(a.MBWidth) != b.PicWidthInMbs() || int(a.MBHeight) != b.FrameHeightInMbs():
		return "size in macroblocks"
	case a.FrameMBSOnly != b.FrameMbsOnly || a.MBAdaptive != b.MBAdaptiveFrameField:
		return "frame coding"
	case a.Direct8x8 != b.Direct8x8Inference:
		return "Direct8x8"
	case int(a.Width) != b.CroppedWidth() || int(a.Height) != b.CroppedHeight():
		return "cropped size"
	case a.VUIPresent != b.VUIPresent:
		return "VUIPresent"
	case a.VUIPresent && (a.VUI.BitstreamRestriction != b.VUI.BitstreamRestriction ||
		a.VUI.MaxNumReorderFrames != b.VUI.MaxNumReorderFrames ||
		a.VUI.MaxDecFrameBuffering != b.VUI.MaxDecFrameBuffering ||
		a.VUI.Timing != b.VUI.TimingInfoPresent || a.VUI.NumUnitsInTick != b.VUI.NumUnitsInTick ||
		a.VUI.TimeScale != b.VUI.TimeScale || a.VUI.AspectRatioIDC != b.VUI.AspectRatioIDC):
		return "VUI"
	case a.MaxDpbFrames() != b.MaxDpbFrames() || a.MaxNumReorder() != b.MaxNumReorder():
		return "MaxDpbFrames or MaxNumReorder"
	case a.IsLevel1b() != b.IsLevel1b():
		return "IsLevel1b"
	}
	return ""
}

// spsPayloads are the sequence parameter sets of every stream the repository
// ships, as they are stored (escaped).
func spsPayloads(t testing.TB) [][]byte { return payloadsOf(t, UnitSPS) }

// payloadsOf are the payloads of the units of one type in every stream the
// repository ships.
func payloadsOf(t testing.TB, typ UnitType) [][]byte {
	t.Helper()
	var paths []string
	for _, pattern := range []string{"conformance/*.264", "fuzzseed/*"} {
		p, err := filepath.Glob(filepath.Join("testdata", pattern))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p...)
	}
	var out [][]byte
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		units, err := SplitAnnexB(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range units {
			if u.Type == typ {
				out = append(out, u.Payload)
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("only %d units of type %d found", len(out), typ)
	}
	return out
}

// TestTheSetsOfTheCorpusReadAsTheDecoderReadsThem: the two readers of this syntax
// agree on every field the decoder uses, on every sequence set in the repository.
func TestTheSetsOfTheCorpusReadAsTheDecoderReadsThem(t *testing.T) {
	for i, payload := range spsPayloads(t) {
		got, err := ParseSPS(Unit{Type: UnitSPS, Payload: payload})
		if err != nil {
			t.Fatalf("set %d: %v", i, err)
		}
		want, err := syntax.ParseSPS(Unit{Payload: payload}.Unescape())
		if err != nil {
			t.Fatalf("set %d: internal reader: %v", i, err)
		}
		if f := sameSPS(got, want); f != "" {
			t.Fatalf("set %d: %s differs:\n%+v\n%+v", i, f, got, want)
		}
	}
}

// TestMutatedSetsReadAsTheDecoderReadsThem: where both readers accept a corrupted
// set they read the same thing. This reader may refuse more (it keeps the range
// checks the other lacks), never read differently.
func TestMutatedSetsReadAsTheDecoderReadsThem(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	payloads := spsPayloads(t)
	agreed := 0
	for n := 0; n < 60000; n++ {
		p := slices.Clone(payloads[rng.IntN(len(payloads))])
		for k := rng.IntN(3) + 1; k > 0; k-- {
			p[rng.IntN(len(p))] ^= 1 << rng.IntN(8)
		}
		if rng.IntN(5) == 0 {
			p = p[:rng.IntN(len(p))+1]
		}
		got, err := ParseSPS(Unit{Type: UnitSPS, Payload: p})
		if err != nil {
			continue
		}
		want, err := syntax.ParseSPS(Unit{Payload: p}.Unescape())
		if err != nil {
			continue
		}
		agreed++
		if f := sameSPS(got, want); f != "" {
			t.Fatalf("%s differs on % x:\n%+v\n%+v", f, p, got, want)
		}
	}
	if agreed < 1000 {
		t.Fatalf("only %d mutated sets were accepted by both readers", agreed)
	}
}
