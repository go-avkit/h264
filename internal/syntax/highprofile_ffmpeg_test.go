package syntax

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-avkit/h264/internal/nal"
)

func x264HighProfileStream(t *testing.T, x264opts string) []byte {
	t.Helper()
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed, skipping the external High profile check")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "high.264")
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=176x144:rate=25:duration=0.4",
		"-c:v", "libx264", "-profile:v", "high"}
	if x264opts != "" {
		args = append(args, "-x264opts", x264opts)
	}
	args = append(args, "-pix_fmt", "yuv420p", out)
	if combined, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg refused to build a High profile stream: %v\n%s", err, combined)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parameterSetsOf(t *testing.T, stream []byte) (spsRBSP, ppsRBSP []byte) {
	t.Helper()
	for _, ebsp := range nal.SplitAnnexB(stream) {
		u, err := nal.Parse(ebsp)
		if err != nil {
			t.Fatalf("parsing a unit of the reference stream: %v", err)
		}
		switch u.Header.Type {
		case nal.TypeSPS:
			if spsRBSP == nil {
				spsRBSP = u.RBSP
			}
		case nal.TypePPS:
			if ppsRBSP == nil {
				ppsRBSP = u.RBSP
			}
		}
	}
	if spsRBSP == nil || ppsRBSP == nil {
		t.Fatal("the reference stream carries no parameter sets")
	}
	return spsRBSP, ppsRBSP
}

func TestHighProfileParameterSetsFromX264RoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		opts    string
		matrix  bool
		eightBy bool
	}{
		{name: "flat matrices", opts: "8x8dct=1", eightBy: true},
		{name: "jvt matrices", opts: "cqm=jvt:8x8dct=1", matrix: true, eightBy: true},
		{name: "jvt matrices without the 8x8 transform", opts: "cqm=jvt:8x8dct=0", matrix: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stream := x264HighProfileStream(t, c.opts)
			spsRBSP, ppsRBSP := parameterSetsOf(t, stream)

			sps, err := ParseSPS(spsRBSP)
			if err != nil {
				t.Fatalf("parsing x264's sequence parameter set: %v", err)
			}
			if sps.ProfileIDC != 100 {
				t.Fatalf("x264 announced profile_idc %d, want High", sps.ProfileIDC)
			}
			if sps.ScalingListsPresent() {
				t.Fatal("x264 put the quantisation matrices in the sequence parameter set, which it has never done")
			}

			pps, err := ParsePPS(ppsRBSP, func(uint32) *SPS { return sps })
			if err != nil {
				t.Fatalf("parsing x264's picture parameter set: %v", err)
			}
			if pps.Transform8x8Mode != c.eightBy {
				t.Fatalf("transform_8x8_mode_flag reads %v, want %v", pps.Transform8x8Mode, c.eightBy)
			}
			if pps.PicScalingMatrixPresent != c.matrix {
				t.Fatalf("pic_scaling_matrix_present_flag reads %v, want %v",
					pps.PicScalingMatrixPresent, c.matrix)
			}

			back, err := WriteSPS(sps)
			if err != nil {
				t.Fatalf("writing the sequence parameter set back: %v", err)
			}
			if !bytes.Equal(back, spsRBSP) {
				t.Fatalf("our sequence parameter set differs from x264's:\n ours %x\nx264 %x", back, spsRBSP)
			}
			back, err = WritePPS(pps, func(uint32) *SPS { return sps })
			if err != nil {
				t.Fatalf("writing the picture parameter set back: %v", err)
			}
			if !bytes.Equal(back, ppsRBSP) {
				t.Fatalf("our picture parameter set differs from x264's:\n ours %x\nx264 %x", back, ppsRBSP)
			}
		})
	}
}

func TestJVTMatricesResolveToTheDefaultLists(t *testing.T) {
	stream := x264HighProfileStream(t, "cqm=jvt:8x8dct=1")
	spsRBSP, ppsRBSP := parameterSetsOf(t, stream)
	sps, err := ParseSPS(spsRBSP)
	if err != nil {
		t.Fatal(err)
	}
	pps, err := ParsePPS(ppsRBSP, func(uint32) *SPS { return sps })
	if err != nil {
		t.Fatal(err)
	}
	list4x4, list8x8 := pps.ResolvedScalingLists(sps)

	for i := 0; i < 3; i++ {
		if list4x4[i] != scanDefaultScalingList4x4Intra {
			t.Fatalf("4x4 list %d is not the intra default:\n got %v\nwant %v",
				i, list4x4[i], scanDefaultScalingList4x4Intra)
		}
	}
	for i := 3; i < 6; i++ {
		if list4x4[i] != scanDefaultScalingList4x4Inter {
			t.Fatalf("4x4 list %d is not the inter default:\n got %v\nwant %v",
				i, list4x4[i], scanDefaultScalingList4x4Inter)
		}
	}
	if list8x8[0] != scanDefaultScalingList8x8Intra {
		t.Fatalf("the 8x8 intra list is not the intra default:\n got %v\nwant %v",
			list8x8[0], scanDefaultScalingList8x8Intra)
	}
	if list8x8[1] != scanDefaultScalingList8x8Inter {
		t.Fatalf("the 8x8 inter list is not the inter default:\n got %v\nwant %v",
			list8x8[1], scanDefaultScalingList8x8Inter)
	}
}
