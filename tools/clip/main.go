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

// decodeAll decodes the stream, fed in pieces of chunk bytes (the whole stream
// when chunk is 0), and returns one MD5 per picture, or, without hashing, a
// slice of the right length with empty strings.
//
// The pictures of a call are valid until the next one, and the decoder reuses
// the storage of those that are no longer references. Fed the whole stream in
// one call, every picture of the stream is alive at once, nothing can be
// reused, and the decoder allocates a picture per frame: on a 1080p clip 199 000
// page faults and 0.32 s of system time in 2.6 s, against 16 000 and 0.03 s
// fed in pieces, which is how a player feeds it.
func decodeAll(data []byte, hash bool, chunk int) ([]string, error) {
	d := decoder.New(decoder.Limits{})
	var got []string
	count := 0
	take := func(fr []*decoder.Frame) {
		count += len(fr)
		if hash {
			got = append(got, sums(fr)...) // before the next call, which may recycle them
		}
	}
	if chunk <= 0 || chunk > len(data) {
		chunk = max(len(data), 1)
	}
	for off := 0; off < len(data); off += chunk {
		fr, err := d.Decode(data[off:min(off+chunk, len(data))])
		if err != nil {
			return nil, err
		}
		take(fr)
	}
	rest, err := d.Flush()
	if err != nil {
		return nil, err
	}
	take(rest)
	if !hash {
		return make([]string, count), nil
	}
	return got, nil
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
	var rounds, chunk int
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
				got, err := decodeAll(data, true, chunk)
				if err != nil {
					return err
				}
				return compare(cmd.OutOrStdout(), got, want)
			}
			for r := 0; r < rounds; r++ {
				t := time.Now()
				if _, err := decodeAll(data, !nohash, chunk); err != nil {
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
	f.IntVar(&chunk, "chunk", 65536, "feed the stream in pieces of this many bytes; 0 feeds it whole, which keeps every picture alive and defeats the decoder's reuse of them")
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
