// Command jvtfetch downloads the ITU-T H.264.1 conformance bitstreams named by
// the manifests of testdata/jvt and unpacks each into DIR/NAME/, where the
// decoder's TestJVTConformance (H264_JVT_DIR=DIR) finds it.
//
//	jvtfetch --dir D [--manifests testdata/jvt] [--url BASE]
//
// It lives in its own module so that the library keeps its dependencies.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

const defaultURL = "https://www.itu.int/wftp3/av-arch/jvt-site/draft_conformance/AVCv1/"

const (
	workers    = 8
	maxZip     = 300 << 20
	maxExtract = 1 << 30
)

var osExit = os.Exit

var bitstreamExt = map[string]bool{".264": true, ".jvt": true, ".avc": true, ".26l": true, ".h264": true, ".jsv": true, ".bit": true, ".bits": true}

func main() {
	if err := RootCmd().Execute(); err != nil {
		osExit(1)
	}
}

// RootCmd builds the command.
func RootCmd() *cobra.Command {
	var dir, manifests, base string
	cmd := &cobra.Command{
		Use:          "jvtfetch",
		Short:        "download the JVT conformance streams the manifests name",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			files, err := filepath.Glob(filepath.Join(manifests, "*.md5"))
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("no manifests in %s", manifests)
			}
			// The archives are 1 to 60 MB each and the server is slow per
			// connection, so they are fetched several at a time.
			var (
				wg   sync.WaitGroup
				mu   sync.Mutex
				errs []error
				sem  = make(chan struct{}, workers)
			)
			for _, f := range files {
				name := strings.TrimSuffix(filepath.Base(f), ".md5")
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer func() { <-sem; wg.Done() }()
					err := fetch(base, dir, name)
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						errs = append(errs, fmt.Errorf("%s: %w", name, err))
						return
					}
					fmt.Fprintln(cmd.OutOrStdout(), "fetched", name)
				}()
			}
			wg.Wait()
			return errors.Join(errs...)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "directory to unpack into")
	cmd.Flags().StringVar(&manifests, "manifests", filepath.Join("testdata", "jvt"), "directory of NAME.md5 files")
	cmd.Flags().StringVar(&base, "url", defaultURL, "base URL of the NAME.zip archives")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

// fetch downloads NAME.zip into dir/NAME. It does nothing when a bitstream is
// already there.
func fetch(base, dir, name string) error {
	dst := filepath.Join(dir, name)
	if hasBitstream(dst) {
		return nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+name+".zip", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxZip+1))
	if err != nil {
		return err
	}
	if len(body) > maxZip {
		return errors.New("zip larger than the bound")
	}
	if err := unzip(body, dst); err != nil {
		return err
	}
	if !hasBitstream(dst) {
		return errors.New("the archive holds no bitstream")
	}
	return nil
}

// unzip extracts a flat archive into dst by base name, so no entry can leave
// dst, within a total size bound.
func unzip(body []byte, dst string) error {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return err
	}
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxExtract-total+1))
		rc.Close()
		if err != nil {
			return err
		}
		if total += int64(len(data)); total > maxExtract {
			return errors.New("archive expands past the bound")
		}
		if err := os.WriteFile(filepath.Join(dst, filepath.Base(f.Name)), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func hasBitstream(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if bitstreamExt[strings.ToLower(filepath.Ext(e.Name()))] {
			return true
		}
	}
	return false
}
