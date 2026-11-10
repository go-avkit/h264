package decoder

import (
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/syntax"
	"github.com/go-avkit/h264/internal/transform"
)

func flatTables(t *testing.T) *scalingTables {
	t.Helper()
	sps := &syntax.SPS{ProfileIDC: syntax.ProfileHigh, ChromaFormatIDC: syntax.Chroma420}
	pps := &syntax.PPS{Transform8x8Mode: true}
	s := buildScalingTables(sps, pps)
	if !s.flat {
		t.Fatal("a parameter set without scaling matrices must resolve to flat weights")
	}
	return s
}

func randomBlock(rng *rand.Rand) transform.Block {
	var b transform.Block
	for i := range b {
		b[i] = int32(rng.Intn(4001) - 2000)
	}
	return b
}

func TestFlatDequant4x4MatchesTransform(t *testing.T) {
	s := flatTables(t)
	rng := rand.New(rand.NewSource(20260901))
	for _, intra := range []bool{true, false} {
		scale := s.luma4x4(intra)
		for qp := 0; qp <= 51; qp++ {
			for _, skipDC := range []bool{false, true} {
				orig := randomBlock(rng)
				got, want := orig, orig
				dequant4x4(&got, qp, scale, skipDC)
				transform.Dequant4x4(&want, qp, skipDC)
				if got != want {
					t.Fatalf("qp %d skipDC %v: got %v want %v", qp, skipDC, got, want)
				}
			}
		}
	}
}

func TestFlatDequantLumaDCMatchesTransform(t *testing.T) {
	s := flatTables(t)
	rng := rand.New(rand.NewSource(11))
	scale := s.luma4x4(true)
	for qp := 0; qp <= 51; qp++ {
		orig := randomBlock(rng)
		got, want := orig, orig
		dequantLumaDC(&got, qp, scale)
		transform.DequantLumaDC(&want, qp)
		if got != want {
			t.Fatalf("qp %d: got %v want %v", qp, got, want)
		}
	}
}

func TestFlatDequantChromaDCMatchesTransform(t *testing.T) {
	s := flatTables(t)
	rng := rand.New(rand.NewSource(12))
	scale := s.chroma4x4(true, 0)
	for qp := 0; qp <= 51; qp++ {
		var orig transform.ChromaDC
		for i := range orig {
			orig[i] = int32(rng.Intn(2001) - 1000)
		}
		got, want := orig, orig
		dequantChromaDC(&got, qp, scale)
		transform.DequantChromaDC(&want, qp)
		if got != want {
			t.Fatalf("qp %d: got %v want %v", qp, got, want)
		}
	}
}

func TestFlatLevelScale8x8MatchesTransform(t *testing.T) {
	s := flatTables(t)
	want := transform.BuildLevelScale8x8(transform.FlatWeightScale8x8)
	for _, intra := range []bool{true, false} {
		if *s.luma8x8(intra) != want {
			t.Fatalf("flat 8x8 level scale (intra=%v) differs from the flat build", intra)
		}
	}
}

func TestRasterOrderConversionsAreInverses(t *testing.T) {
	var scan4 [16]uint8
	for i := range scan4 {
		scan4[i] = uint8(i + 1)
	}
	r4 := rasterOrder4x4(scan4)
	for i := 0; i < 16; i++ {
		if r4[transform.ZigZagIndex(i)] != scan4[i] {
			t.Fatalf("4x4 raster conversion lost scan position %d", i)
		}
	}
	var scan8 [64]uint8
	for i := range scan8 {
		scan8[i] = uint8(i + 1)
	}
	r8 := rasterOrder8x8(scan8)
	for i := 0; i < 64; i++ {
		if r8[transform.ZigZagScan8x8[i]] != scan8[i] {
			t.Fatalf("8x8 raster conversion lost scan position %d", i)
		}
	}
}

func TestDefaultScalingMatricesReachDequantisation(t *testing.T) {
	sps := &syntax.SPS{ProfileIDC: syntax.ProfileHigh, ChromaFormatIDC: syntax.Chroma420}
	pps := &syntax.PPS{Transform8x8Mode: true, PicScalingMatrixPresent: true}
	s := buildScalingTables(sps, pps)
	if s.flat {
		t.Fatal("a picture scaling matrix with no lists present falls back to the JVT defaults, which are not flat")
	}
	for m := 0; m < 6; m++ {
		for pos := 0; pos < 16; pos++ {
			want := dequant4CoeffInit[m][dequant4Class(pos)] * int32(transform.DefaultScalingList4x4Intra[pos])
			if got := s.list4x4[scaleIntraY][m][pos]; got != want {
				t.Fatalf("intra luma level scale [%d][%d] = %d, want %d", m, pos, got, want)
			}
			want = dequant4CoeffInit[m][dequant4Class(pos)] * int32(transform.DefaultScalingList4x4Inter[pos])
			if got := s.list4x4[scaleInterV][m][pos]; got != want {
				t.Fatalf("inter chroma level scale [%d][%d] = %d, want %d", m, pos, got, want)
			}
		}
	}
	if *s.luma8x8(true) != transform.BuildLevelScale8x8(transform.DefaultScalingList8x8Intra) {
		t.Fatal("intra 8x8 level scale does not use Default_8x8_Intra")
	}
	if *s.luma8x8(false) != transform.BuildLevelScale8x8(transform.DefaultScalingList8x8Inter) {
		t.Fatal("inter 8x8 level scale does not use Default_8x8_Inter")
	}
}

func TestDequant4CoeffInitClassesCoverTheThreePositions(t *testing.T) {
	counts := [3]int{}
	for pos := 0; pos < 16; pos++ {
		counts[dequant4Class(pos)]++
	}
	if counts != [3]int{4, 8, 4} {
		t.Fatalf("4x4 position classes split %v, want four corner, eight edge and four centre positions", counts)
	}
	for m := 0; m < 5; m++ {
		for c := 0; c < 3; c++ {
			if dequant4CoeffInit[m][c] >= dequant4CoeffInit[m+1][c] {
				t.Fatalf("dequant4CoeffInit[%d][%d] should be smaller than the next quantiser step", m, c)
			}
		}
	}
}
