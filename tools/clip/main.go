// Command clip decodes an Annex B file with go-avkit/h264 for two purposes.
//
// With --ref it compares the MD5 of every picture with an ffmpeg -f framemd5
// listing (the conformance check of a timing build). Without it, it prints the
// wall time of --rounds decodes. The timing must not include the per-picture
// MD5, which was 19 % of the cycles of a 1080p decode, so --nohash times the
// decode alone; the hashes are computed before Flush, which may recycle the
// pictures it returns.
package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-avkit/h264/decoder"
)

var osExit = os.Exit

func sums(fr []*decoder.Frame) []string {
	var out []string
	for _, f := range fr {
		s := md5.Sum(f.AppendI420(nil))
		out = append(out, hex.EncodeToString(s[:]))
	}
	return out
}

// decodeAll decodes the stream and returns one MD5 per picture, or, without
// hashing, a slice of the right length with empty strings.
func decodeAll(data []byte, hash bool) ([]string, error) {
	d := decoder.New(decoder.Limits{})
	fr, err := d.Decode(data)
	if err != nil {
		return nil, err
	}
	var got []string
	if hash {
		got = sums(fr)
	}
	rest, err := d.Flush()
	if err != nil {
		return nil, err
	}
	if !hash {
		return make([]string, len(fr)+len(rest)), nil
	}
	return append(got, sums(rest)...), nil
}

// refSums reads the last field of every line of an ffmpeg framemd5 listing.
func refSums(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l == "" || l[0] == '#' {
			continue
		}
		f := strings.Split(l, ",")
		out = append(out, strings.TrimSpace(f[len(f)-1]))
	}
	return out, nil
}

func compare(out io.Writer, got, want []string) error {
	if len(got) != len(want) {
		return fmt.Errorf("%d frames, ffmpeg has %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			return fmt.Errorf("frame %d differs", i)
		}
	}
	fmt.Fprintf(out, "OK: %d frames identical to ffmpeg\n", len(got))
	return nil
}

// RootCmd builds the command.
func RootCmd() *cobra.Command {
	var in, ref string
	var rounds int
	var nohash bool
	cmd := &cobra.Command{
		Use:          "clip",
		Short:        "decode an Annex B file: check it against a framemd5 listing or time it",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := os.ReadFile(in)
			if err != nil {
				return err
			}
			if ref != "" {
				want, err := refSums(ref)
				if err != nil {
					return err
				}
				got, err := decodeAll(data, true)
				if err != nil {
					return err
				}
				return compare(cmd.OutOrStdout(), got, want)
			}
			for r := 0; r < rounds; r++ {
				t := time.Now()
				if _, err := decodeAll(data, !nohash); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%.3f\n", time.Since(t).Seconds())
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&in, "in", "", "Annex B file")
	f.StringVar(&ref, "ref", "", "ffmpeg framemd5 listing to compare with")
	f.IntVar(&rounds, "rounds", 1, "timed decodes")
	f.BoolVar(&nohash, "nohash", false, "time the decode alone, without the per-picture MD5")
	_ = cmd.MarkFlagRequired("in")
	return cmd
}

func main() {
	if err := RootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "clip:", err)
		osExit(1)
	}
}
