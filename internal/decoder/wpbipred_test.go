package decoder_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/decoder"
	"github.com/go-avkit/h264/internal/nal"
	"github.com/go-avkit/h264/internal/syntax"
)

const (
	wpMBsWide = 2
	wpMBsHigh = 2
	wpWidth   = wpMBsWide * 16
	wpHeight  = wpMBsHigh * 16
)

func wpSPS() *syntax.SPS {
	return &syntax.SPS{
		ProfileIDC:                  syntax.ProfileMain,
		ConstraintSet:               0x40,
		LevelIDC:                    30,
		ID:                          0,
		ChromaFormatIDC:             syntax.Chroma420,
		Log2MaxFrameNumMinus4:       4,
		PicOrderCntType:             0,
		Log2MaxPicOrderCntLsbMinus4: 4,
		MaxNumRefFrames:             2,
		PicWidthInMbsMinus1:         wpMBsWide - 1,
		PicHeightInMapUnitsMinus1:   wpMBsHigh - 1,
		FrameMbsOnly:                true,
		Direct8x8Inference:          true,
	}
}

func wpPPS() *syntax.PPS {
	return &syntax.PPS{
		ID:                             0,
		SPSID:                          0,
		NumRefIdxL0DefaultActiveMinus1: 0,
		NumRefIdxL1DefaultActiveMinus1: 0,
		WeightedBipredIDC:              1,
		DeblockingFilterControlPresent: true,
	}
}

func wpPlanarSample(frame, plane, x, y int) byte {
	switch plane {
	case 0:
		return byte((x*9 + y*23 + frame*57) & 0xFF)
	case 1:
		return byte((x*31 + y*11 + frame*97) & 0xFF)
	default:
		return byte((255 - (x*13+y*29+frame*41)&0xFF) & 0xFF)
	}
}

func wpPicture(frame int) []byte {
	out := make([]byte, wpWidth*wpHeight*3/2)
	for y := 0; y < wpHeight; y++ {
		for x := 0; x < wpWidth; x++ {
			out[y*wpWidth+x] = wpPlanarSample(frame, 0, x, y)
		}
	}
	n := wpWidth * wpHeight
	cw, chh := wpWidth/2, wpHeight/2
	for y := 0; y < chh; y++ {
		for x := 0; x < cw; x++ {
			out[n+y*cw+x] = wpPlanarSample(frame, 1, x, y)
			out[n+cw*chh+y*cw+x] = wpPlanarSample(frame, 2, x, y)
		}
	}
	return out
}

func wpWritePCMMacroblock(w *bits.Writer, pic []byte, mbx, mby int) {
	w.AlignZero()
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			w.WriteBits(uint32(pic[(mby*16+y)*wpWidth+mbx*16+x]), 8)
		}
	}
	n := wpWidth * wpHeight
	cw, chh := wpWidth/2, wpHeight/2
	for plane := 0; plane < 2; plane++ {
		base := n + plane*cw*chh
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				w.WriteBits(uint32(pic[base+(mby*8+y)*cw+mbx*8+x]), 8)
			}
		}
	}
}

func wpSliceRBSP(t *testing.T, hdr *syntax.SliceHeader, sps *syntax.SPS, pps *syntax.PPS,
	body func(w *bits.Writer)) []byte {

	t.Helper()
	w := bits.NewWriter()
	if err := syntax.WriteSliceHeader(w, hdr, sps, pps); err != nil {
		t.Fatalf("WriteSliceHeader: %v", err)
	}
	body(w)
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatalf("bit writer: %v", err)
	}
	return w.Bytes()
}

func wpBuildStream(t *testing.T, wt syntax.PredWeightTable) ([]byte, []byte, []byte) {
	t.Helper()
	sps, pps := wpSPS(), wpPPS()
	pic0, pic1 := wpPicture(0), wpPicture(1)

	spsRBSP, err := syntax.WriteSPS(sps)
	if err != nil {
		t.Fatalf("WriteSPS: %v", err)
	}
	ppsRBSP, err := syntax.WritePPS(pps, func(uint32) *syntax.SPS { return sps })
	if err != nil {
		t.Fatalf("WritePPS: %v", err)
	}
	out := nal.AppendAnnexB(nil, nal.Unit{
		Header: nal.Header{RefIDC: 3, Type: nal.TypeSPS}, RBSP: spsRBSP,
	}, true)
	out = nal.AppendAnnexB(out, nal.Unit{
		Header: nal.Header{RefIDC: 3, Type: nal.TypePPS}, RBSP: ppsRBSP,
	}, true)

	idr := wpSliceRBSP(t, &syntax.SliceHeader{
		SliceType: syntax.SliceI, FrameNum: 0, IDR: true, NalRefIDC: 3,
		PicOrderCntLsb: 0, DisableDeblockingFilterIDC: 1,
	}, sps, pps, func(w *bits.Writer) {
		for mb := 0; mb < wpMBsWide*wpMBsHigh; mb++ {
			w.WriteUE(25)
			wpWritePCMMacroblock(w, pic0, mb%wpMBsWide, mb/wpMBsWide)
		}
	})
	out = nal.AppendAnnexB(out, nal.Unit{
		Header: nal.Header{RefIDC: 3, Type: nal.TypeSliceIDR}, RBSP: idr,
	}, true)

	p := wpSliceRBSP(t, &syntax.SliceHeader{
		SliceType: syntax.SliceP, FrameNum: 1, NalRefIDC: 2,
		PicOrderCntLsb: 4, DisableDeblockingFilterIDC: 1,
	}, sps, pps, func(w *bits.Writer) {
		for mb := 0; mb < wpMBsWide*wpMBsHigh; mb++ {
			w.WriteUE(0)
			w.WriteUE(30)
			wpWritePCMMacroblock(w, pic1, mb%wpMBsWide, mb/wpMBsWide)
		}
	})
	out = nal.AppendAnnexB(out, nal.Unit{
		Header: nal.Header{RefIDC: 2, Type: nal.TypeSliceNonIDR}, RBSP: p,
	}, true)

	b := wpSliceRBSP(t, &syntax.SliceHeader{
		SliceType: syntax.SliceB, FrameNum: 2, NalRefIDC: 0,
		PicOrderCntLsb: 2, DirectSpatialMvPred: true,
		DisableDeblockingFilterIDC: 1, PredWeight: wt,
	}, sps, pps, func(w *bits.Writer) {
		w.WriteUE(uint32(wpMBsWide * wpMBsHigh))
	})
	out = nal.AppendAnnexB(out, nal.Unit{
		Header: nal.Header{RefIDC: 0, Type: nal.TypeSliceNonIDR}, RBSP: b,
	}, true)

	return out, pic0, pic1
}

func wpClip1(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func wpSpecBi(a, b byte, w0, w1, o0, o1, logWD int) byte {
	return wpClip1(((int(a)*w0 + int(b)*w1 + 1<<uint(logWD)) >> uint(logWD+1)) + (o0+o1+1)>>1)
}

func wpExpected(pic0, pic1 []byte, wt syntax.PredWeightTable) []byte {
	out := make([]byte, len(pic0))
	e0, e1 := wt.L0[0], wt.L1[0]
	lumaWD := int(wt.LumaLog2WeightDenom)
	chromaWD := int(wt.ChromaLog2WeightDenom)
	n := wpWidth * wpHeight
	cw, chh := wpWidth/2, wpHeight/2
	for i := 0; i < n; i++ {
		out[i] = wpSpecBi(pic0[i], pic1[i],
			int(e0.LumaWeight), int(e1.LumaWeight),
			int(e0.LumaOffset), int(e1.LumaOffset), lumaWD)
	}
	for plane := 0; plane < 2; plane++ {
		base := n + plane*cw*chh
		for i := 0; i < cw*chh; i++ {
			out[base+i] = wpSpecBi(pic0[base+i], pic1[base+i],
				int(e0.ChromaWeight[plane]), int(e1.ChromaWeight[plane]),
				int(e0.ChromaOffset[plane]), int(e1.ChromaOffset[plane]), chromaWD)
		}
	}
	return out
}

func wpDecode(t *testing.T, stream []byte) [][]byte {
	t.Helper()
	d := decoder.New()
	pics, err := d.Decode(stream)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rest, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pics = append(pics, rest...)
	var out [][]byte
	for _, p := range pics {
		buf := make([]byte, p.Size())
		p.CopyOut(buf)
		out = append(out, buf)
	}
	return out
}

func wpFFmpegDecode(t *testing.T, stream []byte) [][]byte {
	t.Helper()
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.264")
	outPath := filepath.Join(dir, "out.yuv")
	if err := os.WriteFile(in, stream, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-hide_banner", "-loglevel", "error", "-y",
		"-i", in, "-pix_fmt", "yuv420p", "-f", "rawvideo", outPath)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg rejected the hand-built stream: %v\n%s", err, combined)
	}
	if len(combined) != 0 {
		t.Fatalf("ffmpeg reported problems decoding the hand-built stream:\n%s", combined)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	size := wpWidth * wpHeight * 3 / 2
	var out [][]byte
	for i := 0; i+size <= len(data); i += size {
		out = append(out, data[i:i+size])
	}
	return out
}

func wpEntry(lumaW, lumaO, cbW, cbO, crW, crO int32) syntax.WeightEntry {
	return syntax.WeightEntry{
		LumaWeightFlag:   true,
		LumaWeight:       lumaW,
		LumaOffset:       lumaO,
		ChromaWeightFlag: true,
		ChromaWeight:     [2]int32{cbW, crW},
		ChromaOffset:     [2]int32{cbO, crO},
	}
}

func wpCases() []struct {
	name string
	wt   syntax.PredWeightTable
} {
	return []struct {
		name string
		wt   syntax.PredWeightTable
	}{
		{
			name: "equal halves",
			wt: syntax.PredWeightTable{
				LumaLog2WeightDenom:   6,
				ChromaLog2WeightDenom: 6,
				L0:                    []syntax.WeightEntry{wpEntry(32, 0, 32, 0, 32, 0)},
				L1:                    []syntax.WeightEntry{wpEntry(32, 0, 32, 0, 32, 0)},
			},
		},
		{
			name: "fade with the sum at the conformance limit",
			wt: syntax.PredWeightTable{
				LumaLog2WeightDenom:   6,
				ChromaLog2WeightDenom: 6,
				L0:                    []syntax.WeightEntry{wpEntry(40, 7, 30, 12, 34, -9)},
				L1:                    []syntax.WeightEntry{wpEntry(88, -9, 98, -20, 94, 15)},
			},
		},
		{
			name: "negative list zero weight",
			wt: syntax.PredWeightTable{
				LumaLog2WeightDenom:   5,
				ChromaLog2WeightDenom: 3,
				L0:                    []syntax.WeightEntry{wpEntry(-50, 30, -20, 25, -8, 11)},
				L1:                    []syntax.WeightEntry{wpEntry(120, -25, 40, -14, 15, -3)},
			},
		},
		{
			name: "offsets that saturate both ends",
			wt: syntax.PredWeightTable{
				LumaLog2WeightDenom:   7,
				ChromaLog2WeightDenom: 0,
				L0:                    []syntax.WeightEntry{wpEntry(127, 120, 1, -120, -1, 127)},
				L1:                    []syntax.WeightEntry{wpEntry(-64, 118, 0, -118, 1, 125)},
			},
		},
	}
}

func wpConformsToEquation8298(wt syntax.PredWeightTable) bool {
	limitFor := func(denom uint32) int32 {
		if denom == 7 {
			return 127
		}
		return 128
	}
	sums := []struct {
		sum   int32
		denom uint32
	}{
		{wt.L0[0].LumaWeight + wt.L1[0].LumaWeight, wt.LumaLog2WeightDenom},
		{wt.L0[0].ChromaWeight[0] + wt.L1[0].ChromaWeight[0], wt.ChromaLog2WeightDenom},
		{wt.L0[0].ChromaWeight[1] + wt.L1[0].ChromaWeight[1], wt.ChromaLog2WeightDenom},
	}
	for _, s := range sums {
		if s.sum < -128 || s.sum > limitFor(s.denom) {
			return false
		}
	}
	return true
}

func TestExplicitWeightedBipredMatchesTheSpecFormula(t *testing.T) {
	for _, tc := range wpCases() {
		if !wpConformsToEquation8298(tc.wt) {
			t.Fatalf("%s: the weights break equation 8-298", tc.name)
		}
		stream, pic0, pic1 := wpBuildStream(t, tc.wt)
		pics := wpDecode(t, stream)
		if len(pics) != 3 {
			t.Fatalf("%s: decoded %d pictures, want 3", tc.name, len(pics))
		}
		for i, want := range map[int][]byte{0: pic0, 2: pic1} {
			for j := range want {
				if pics[i][j] != want[j] {
					t.Fatalf("%s: reference picture %d differs at sample %d, got %d want %d",
						tc.name, i, j, pics[i][j], want[j])
				}
			}
		}
		want := wpExpected(pic0, pic1, tc.wt)
		got := pics[1]
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("%s: bi-predicted picture differs at sample %d, got %d want %d",
					tc.name, j, got[j], want[j])
			}
		}
	}
}

func wpWithinFFmpegExactRange(wt syntax.PredWeightTable) bool {
	return wt.LumaLog2WeightDenom <= 6 && wt.ChromaLog2WeightDenom <= 6
}

func TestExplicitWeightedBipredAgreesWithFFmpeg(t *testing.T) {
	for _, tc := range wpCases() {
		if !wpWithinFFmpegExactRange(tc.wt) {
			continue
		}
		stream, _, _ := wpBuildStream(t, tc.wt)
		ref := wpFFmpegDecode(t, stream)
		if ref == nil {
			t.Skip("ffmpeg is not installed, skipping the external conformance check")
		}
		if len(ref) != 3 {
			t.Fatalf("%s: ffmpeg produced %d pictures, want 3", tc.name, len(ref))
		}
		pics := wpDecode(t, stream)
		for i := range ref {
			for j := range ref[i] {
				if pics[i][j] != ref[i][j] {
					t.Fatalf("%s: picture %d sample %d, ours %d ffmpeg %d",
						tc.name, i, j, pics[i][j], ref[i][j])
				}
			}
		}
	}
}
