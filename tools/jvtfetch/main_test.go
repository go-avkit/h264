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
