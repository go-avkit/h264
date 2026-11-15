package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const stream = "../../testdata/conformance/base_intra_qp26.264"

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := RootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// The hash mode and the listing agree with themselves, and a listing that
// differs is refused, by frame and by count.
func TestRefModeAgainstItsOwnListing(t *testing.T) {
	data, err := os.ReadFile(stream)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeAll(data, true)
	if err != nil || len(got) == 0 || got[0] == "" {
		t.Fatalf("decodeAll: %v %v", got, err)
	}
	dir := t.TempDir()
	list := filepath.Join(dir, "ref.framemd5")
	body := "#format: frame checksums\n" + strings.Join(append([]string{"0, 0, 0, 1, 100, " + got[0]}, func() []string {
		var rest []string
		for _, s := range got[1:] {
			rest = append(rest, "0, 0, 0, 1, 100, "+s)
		}
		return rest
	}()...), "\n") + "\n"
	if err := os.WriteFile(list, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "--in", stream, "--ref", list)
	if err != nil || !strings.HasPrefix(out, "OK: ") {
		t.Fatalf("own listing: %q %v", out, err)
	}
	bad := strings.Replace(body, got[0], strings.Repeat("0", 32), 1)
	os.WriteFile(list, []byte(bad), 0o644)
	if _, err := run(t, "--in", stream, "--ref", list); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("a different listing was accepted: %v", err)
	}
	os.WriteFile(list, []byte("#x\n0, 0, 0, 1, 100, "+got[0]+"\n"), 0o644)
	if len(got) > 1 {
		if _, err := run(t, "--in", stream, "--ref", list); err == nil || !strings.Contains(err.Error(), "frames") {
			t.Fatalf("a short listing was accepted: %v", err)
		}
	}
}

func TestTimingModesAndErrors(t *testing.T) {
	for _, nohash := range []string{"--nohash=true", "--nohash=false"} {
		out, err := run(t, "--in", stream, "--rounds", "2", nohash)
		if err != nil || strings.Count(out, "\n") != 2 {
			t.Fatalf("%s: %q %v", nohash, out, err)
		}
	}
	if _, err := run(t, "--in", "/nonexistent.264"); err == nil {
		t.Error("a missing stream was accepted")
	}
	if _, err := run(t, "--in", stream, "--ref", "/nonexistent.framemd5"); err == nil {
		t.Error("a missing listing was accepted")
	}
}

func TestMainExitsOnError(t *testing.T) {
	code := 0
	saved, savedArgs := osExit, os.Args
	defer func() { osExit, os.Args = saved, savedArgs }()
	osExit = func(c int) { code = c }
	os.Args = []string{"clip", "--in", "/nonexistent.264"}
	main()
	if code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
}
