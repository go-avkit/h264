package syntax

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-avkit/h264/internal/nal"
)

func x264SEIStream(t *testing.T, duration, x264opts string) []byte {
	t.Helper()
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed, skipping the external SEI check")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "sei.264")
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=176x144:rate=25:duration=" + duration,
		"-c:v", "libx264"}
	if x264opts != "" {
		args = append(args, "-x264opts", x264opts)
	}
	args = append(args, "-pix_fmt", "yuv420p", out)
	if combined, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg refused to build the reference stream: %v\n%s", err, combined)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSEIRoundTripFromX264(t *testing.T) {
	cases := []struct {
		name         string
		duration     string
		opts         string
		wantBP       bool
		wantPT       bool
		wantRecovery bool
	}{
		{name: "plain", duration: "1.0"},
		{name: "hrd", duration: "1.0", opts: "nal-hrd=cbr:vbv-maxrate=1000:vbv-bufsize=2000", wantBP: true, wantPT: true},
		{name: "recovery", duration: "3.0", opts: "intra-refresh=1:keyint=20", wantRecovery: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stream := x264SEIStream(t, c.duration, c.opts)

			var sps *SPS
			lookup := func(id uint32) *SPS {
				if sps != nil && sps.ID == id {
					return sps
				}
				return nil
			}

			var seiCount int
			var sawRecovery, sawBufferingPeriod, sawPicTiming, sawUserData, sawOpaque bool

			for _, ebsp := range nal.SplitAnnexB(stream) {
				u, err := nal.Parse(ebsp)
				if err != nil {
					t.Fatalf("parsing a unit of the reference stream: %v", err)
				}
				switch u.Header.Type {
				case nal.TypeSPS:
					if sps == nil {
						s, err := ParseSPS(u.RBSP)
						if err != nil {
							t.Fatalf("parsing x264's sequence parameter set: %v", err)
						}
						sps = s
					}
				case nal.TypeSEI:
					seiCount++
					msgs, err := ParseSEI(u.RBSP, sps, lookup)
					if err != nil {
						t.Fatalf("ParseSEI: %v\nrbsp: % x", err, u.RBSP)
					}
					for _, m := range msgs {
						switch {
						case m.RecoveryPoint != nil:
							sawRecovery = true
						case m.BufferingPeriod != nil:
							sawBufferingPeriod = true
						case m.PicTiming != nil:
							sawPicTiming = true
						case m.UserDataUnregistered != nil:
							sawUserData = true
						default:
							sawOpaque = true
						}
					}
					back, err := WriteSEI(msgs, sps, lookup)
					if err != nil {
						t.Fatalf("WriteSEI: %v", err)
					}
					bytesEqual(t, "sei rbsp", back, u.RBSP)
				}
			}

			if seiCount == 0 {
				t.Fatalf("the %s reference stream carries no SEI NAL units", c.name)
			}
			if c.wantBP && !sawBufferingPeriod {
				t.Fatalf("the %s reference stream was expected to carry a buffering period message", c.name)
			}
			if c.wantPT && !sawPicTiming {
				t.Fatalf("the %s reference stream was expected to carry a picture timing message", c.name)
			}
			if c.wantRecovery && !sawRecovery {
				t.Fatalf("the %s reference stream was expected to carry a recovery point message", c.name)
			}
			t.Logf("%s: recovery=%v bufferingPeriod=%v picTiming=%v userData=%v opaque=%v",
				c.name, sawRecovery, sawBufferingPeriod, sawPicTiming, sawUserData, sawOpaque)
		})
	}
}
