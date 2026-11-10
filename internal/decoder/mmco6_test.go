package decoder_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/decoder"
	"github.com/go-avkit/h264/internal/nal"
	syntax "github.com/go-avkit/h264/internal/syntaxfixture"
)

const (
	lt6MBsWide = 2
	lt6MBsHigh = 2
	lt6Width   = lt6MBsWide * 16
	lt6Height  = lt6MBsHigh * 16
)

func lt6SPS() *syntax.SPS {
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
		PicWidthInMbsMinus1:         lt6MBsWide - 1,
		PicHeightInMapUnitsMinus1:   lt6MBsHigh - 1,
		FrameMbsOnly:                true,
	}
}

func lt6PPS() *syntax.PPS {
	return &syntax.PPS{
		ID:                             0,
		SPSID:                          0,
		NumRefIdxL0DefaultActiveMinus1: 0,
		DeblockingFilterControlPresent: true,
	}
}

func lt6Sample(id, plane, x, y int) byte {
	return byte((x*17 + y*29 + id*83 + plane*13) & 0xFF)
}

func lt6Picture(id int) []byte {
	out := make([]byte, lt6Width*lt6Height*3/2)
	for y := 0; y < lt6Height; y++ {
		for x := 0; x < lt6Width; x++ {
			out[y*lt6Width+x] = lt6Sample(id, 0, x, y)
		}
	}
	n := lt6Width * lt6Height
	cw, chh := lt6Width/2, lt6Height/2
	for y := 0; y < chh; y++ {
		for x := 0; x < cw; x++ {
			out[n+y*cw+x] = lt6Sample(id, 1, x, y)
			out[n+cw*chh+y*cw+x] = lt6Sample(id, 2, x, y)
		}
	}
	return out
}

func lt6WritePCMMacroblock(w *bits.Writer, pic []byte, mbx, mby int) {
	w.AlignZero()
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			w.WriteBits(uint32(pic[(mby*16+y)*lt6Width+mbx*16+x]), 8)
		}
	}
	n := lt6Width * lt6Height
	cw, chh := lt6Width/2, lt6Height/2
	for plane := 0; plane < 2; plane++ {
		base := n + plane*cw*chh
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				w.WriteBits(uint32(pic[base+(mby*8+y)*cw+mbx*8+x]), 8)
			}
		}
	}
}

func lt6SliceRBSP(t *testing.T, hdr *syntax.SliceHeader, sps *syntax.SPS, pps *syntax.PPS,
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

func lt6WritePCMPicture(w *bits.Writer, pic []byte) {
	for mb := 0; mb < lt6MBsWide*lt6MBsHigh; mb++ {
		w.WriteUE(25)
		lt6WritePCMMacroblock(w, pic, mb%lt6MBsWide, mb/lt6MBsWide)
	}
}

func lt6WritePCMPictureAsP(w *bits.Writer, pic []byte) {
	for mb := 0; mb < lt6MBsWide*lt6MBsHigh; mb++ {
		w.WriteUE(0)
		w.WriteUE(30)
		lt6WritePCMMacroblock(w, pic, mb%lt6MBsWide, mb/lt6MBsWide)
	}
}

func lt6BuildStream(t *testing.T) (stream []byte, longTermPic []byte) {
	t.Helper()
	sps, pps := lt6SPS(), lt6PPS()
	pic0 := lt6Picture(0)
	pic1 := lt6Picture(1)
	pic2 := lt6Picture(2)
	pic3 := lt6Picture(3)

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

	idr := lt6SliceRBSP(t, &syntax.SliceHeader{
		SliceType: syntax.SliceI, FrameNum: 0, IDR: true, NalRefIDC: 3,
		PicOrderCntLsb: 0, DisableDeblockingFilterIDC: 1,
	}, sps, pps, func(w *bits.Writer) {
		lt6WritePCMPicture(w, pic0)
	})
	out = nal.AppendAnnexB(out, nal.Unit{
		Header: nal.Header{RefIDC: 3, Type: nal.TypeSliceIDR}, RBSP: idr,
	}, true)

	longTerm := lt6SliceRBSP(t, &syntax.SliceHeader{
		SliceType: syntax.SliceP, FrameNum: 1, NalRefIDC: 2,
		PicOrderCntLsb: 2, DisableDeblockingFilterIDC: 1,
		AdaptiveRefPicMarking: true,
		MMCOs:                 []syntax.MMCO{{Op: 6, LongTermFrameIdx: 0}},
	}, sps, pps, func(w *bits.Writer) {
		lt6WritePCMPictureAsP(w, pic1)
	})
	out = nal.AppendAnnexB(out, nal.Unit{
		Header: nal.Header{RefIDC: 2, Type: nal.TypeSliceNonIDR}, RBSP: longTerm,
	}, true)

	filler1 := lt6SliceRBSP(t, &syntax.SliceHeader{
		SliceType: syntax.SliceP, FrameNum: 2, NalRefIDC: 2,
		PicOrderCntLsb: 4, DisableDeblockingFilterIDC: 1,
	}, sps, pps, func(w *bits.Writer) {
		lt6WritePCMPictureAsP(w, pic2)
	})
	out = nal.AppendAnnexB(out, nal.Unit{
		Header: nal.Header{RefIDC: 2, Type: nal.TypeSliceNonIDR}, RBSP: filler1,
	}, true)

	filler2 := lt6SliceRBSP(t, &syntax.SliceHeader{
		SliceType: syntax.SliceP, FrameNum: 3, NalRefIDC: 2,
		PicOrderCntLsb: 6, DisableDeblockingFilterIDC: 1,
	}, sps, pps, func(w *bits.Writer) {
		lt6WritePCMPictureAsP(w, pic3)
	})
	out = nal.AppendAnnexB(out, nal.Unit{
		Header: nal.Header{RefIDC: 2, Type: nal.TypeSliceNonIDR}, RBSP: filler2,
	}, true)

	predict := lt6SliceRBSP(t, &syntax.SliceHeader{
		SliceType: syntax.SliceP, FrameNum: 4, NalRefIDC: 0,
		PicOrderCntLsb: 8, DisableDeblockingFilterIDC: 1,
		ModificationL0Present:    true,
		RefPicListModificationL0: []syntax.RefPicListModification{{IDC: 2, Value: 0}},
	}, sps, pps, func(w *bits.Writer) {
		w.WriteUE(uint32(lt6MBsWide * lt6MBsHigh))
	})
	out = nal.AppendAnnexB(out, nal.Unit{
		Header: nal.Header{RefIDC: 0, Type: nal.TypeSliceNonIDR}, RBSP: predict,
	}, true)

	return out, pic1
}

func lt6Decode(t *testing.T, stream []byte) [][]byte {
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

func lt6FFmpegDecode(t *testing.T, stream []byte) [][]byte {
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
	size := lt6Width * lt6Height * 3 / 2
	var out [][]byte
	for i := 0; i+size <= len(data); i += size {
		out = append(out, data[i:i+size])
	}
	return out
}

func TestMMCO6KeepsThePictureThroughTheSlidingWindow(t *testing.T) {
	stream, longTermPic := lt6BuildStream(t)
	pics := lt6Decode(t, stream)
	if len(pics) != 5 {
		t.Fatalf("decoded %d pictures, want 5", len(pics))
	}
	got := pics[4]
	for i := range longTermPic {
		if got[i] != longTermPic[i] {
			t.Fatalf("the predicted picture differs at sample %d, got %d want %d (it did not predict from the picture marked long term by MMCO op 6)",
				i, got[i], longTermPic[i])
		}
	}
}

func TestMMCO6AgreesWithFFmpeg(t *testing.T) {
	stream, _ := lt6BuildStream(t)
	ref := lt6FFmpegDecode(t, stream)
	if ref == nil {
		t.Skip("ffmpeg is not installed, skipping the external conformance check")
	}
	if len(ref) != 5 {
		t.Fatalf("ffmpeg produced %d pictures, want 5", len(ref))
	}
	pics := lt6Decode(t, stream)
	if len(pics) != len(ref) {
		t.Fatalf("decoded %d pictures, ffmpeg decoded %d", len(pics), len(ref))
	}
	for i := range ref {
		for j := range ref[i] {
			if pics[i][j] != ref[i][j] {
				t.Fatalf("picture %d sample %d, ours %d ffmpeg %d", i, j, pics[i][j], ref[i][j])
			}
		}
	}
}
