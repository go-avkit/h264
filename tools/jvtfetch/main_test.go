package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := RootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// pinFor writes NAME.sha256 for the archive's bitstream files the way
// --write-pins does, from an unpacked copy.
func pinFor(t *testing.T, manifests, name string, files map[string]string) {
	t.Helper()
	tmp := t.TempDir()
	for f, body := range files {
		os.WriteFile(filepath.Join(tmp, filepath.Base(f)), []byte(body), 0o644)
	}
	if err := writePins(tmp, filepath.Join(manifests, name+".sha256")); err != nil {
		t.Fatal(err)
	}
}

func TestFetchUnpacksEachManifestEntryAndSkipsWhatIsThere(t *testing.T) {
	archives := map[string][]byte{
		"/A.zip": zipOf(t, map[string]string{"A.264": "bits", "../evil.txt": "x", "readme.txt": "r"}),
		"/B.zip": zipOf(t, map[string]string{"B.jsv": "bits"}),
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := archives[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		w.Write(body)
	}))
	defer srv.Close()
	manifests, dir := t.TempDir(), t.TempDir()
	for _, n := range []string{"A", "B"} {
		os.WriteFile(filepath.Join(manifests, n+".md5"), nil, 0o644)
	}
	pinFor(t, manifests, "A", map[string]string{"A.264": "bits"})
	pinFor(t, manifests, "B", map[string]string{"B.jsv": "bits"})
	out, err := run(t, "--dir", dir, "--manifests", manifests, "--url", srv.URL+"/")
	if err != nil || !strings.Contains(out, "fetched A") || !strings.Contains(out, "fetched B") {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, p := range []string{"A/A.264", "A/evil.txt", "B/B.jsv"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "evil.txt")); err == nil {
		t.Fatal("an entry left the directory")
	}
	if _, err := run(t, "--dir", dir, "--manifests", manifests, "--url", srv.URL+"/"); err != nil || hits.Load() != 2 {
		t.Fatalf("second run: %v, %d downloads, want 2 in all", err, hits.Load())
	}
}

// A stream that is there but does not match its pin (a restored cache that was
// swapped or truncated) is fetched again; one the network serves wrong, or with
// no pin at all, is refused; --write-pins writes what verification then accepts.
func TestPinsAreVerifiedAfterACacheRestore(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/A.zip":
			w.Write(zipOf(t, map[string]string{"A.264": "good"}))
		case "/C.zip":
			w.Write(zipOf(t, map[string]string{"C.264": "served-wrong"}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	manifests, dir := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(manifests, "A.md5"), nil, 0o644)
	pinFor(t, manifests, "A", map[string]string{"A.264": "good"})

	// A cache that holds a different stream: fetched again, and then right.
	os.MkdirAll(filepath.Join(dir, "A"), 0o755)
	os.WriteFile(filepath.Join(dir, "A", "A.264"), []byte("tampered"), 0o644)
	if _, err := run(t, "--dir", dir, "--manifests", manifests, "--url", srv.URL+"/"); err != nil || hits.Load() != 1 {
		t.Fatalf("tampered cache: %v, %d downloads, want 1", err, hits.Load())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "A", "A.264")); string(b) != "good" {
		t.Fatalf("the tampered stream was kept: %q", b)
	}
	// A good cache is not downloaded again.
	if _, err := run(t, "--dir", dir, "--manifests", manifests, "--url", srv.URL+"/"); err != nil || hits.Load() != 1 {
		t.Fatalf("good cache: %v, %d downloads, want 1 in all", err, hits.Load())
	}
	// The network serving something other than the pin: refused.
	os.WriteFile(filepath.Join(manifests, "C.md5"), nil, 0o644)
	pinFor(t, manifests, "C", map[string]string{"C.264": "pinned"})
	if _, err := run(t, "--dir", dir, "--manifests", manifests, "--url", srv.URL+"/"); err == nil || !strings.Contains(err.Error(), "pin") {
		t.Fatalf("a stream that does not match its pin was accepted: %v", err)
	}
	// No pin file: refused, and --write-pins creates it from what is there.
	os.Remove(filepath.Join(manifests, "C.sha256"))
	os.RemoveAll(filepath.Join(dir, "C"))
	if _, err := run(t, "--dir", dir, "--manifests", manifests, "--url", srv.URL+"/"); err == nil || !strings.Contains(err.Error(), "unpinned") {
		t.Fatalf("an unpinned stream was accepted: %v", err)
	}
	if _, err := run(t, "--dir", dir, "--manifests", manifests, "--url", srv.URL+"/", "--write-pins"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--dir", dir, "--manifests", manifests, "--url", srv.URL+"/"); err != nil {
		t.Fatalf("after --write-pins: %v", err)
	}
	if _, err := pinText(filepath.Join(dir, "nonexistent")); err == nil {
		t.Error("pinText of a missing directory: no error")
	}
}

func TestFetchRefusals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/NOBITS.zip" {
			w.Write(zipOf(t, map[string]string{"readme.txt": "r"}))
			return
		}
		if r.URL.Path == "/BAD.zip" {
			w.Write([]byte("not a zip"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	for _, name := range []string{"MISSING", "NOBITS", "BAD"} {
		manifests := t.TempDir()
		os.WriteFile(filepath.Join(manifests, name+".md5"), nil, 0o644)
		if _, err := run(t, "--dir", t.TempDir(), "--manifests", manifests, "--url", srv.URL+"/"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := run(t, "--dir", t.TempDir(), "--manifests", t.TempDir(), "--url", srv.URL+"/"); err == nil {
		t.Error("empty manifest directory: no error")
	}
	if _, err := run(t); err == nil {
		t.Error("no --dir: no error")
	}
}

func TestMainExitsOnError(t *testing.T) {
	code := 0
	osExit = func(c int) { code = c }
	defer func() { osExit = os.Exit }()
	os.Args = []string{"jvtfetch"}
	main()
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}
