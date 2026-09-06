package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewerThan(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"v0.1.1", "0.1.0", true},
		{"v0.1.0", "0.1.0", false},
		{"v0.1.0", "0.1.1", false},
		{"v1.0.0", "0.9.9", true},
		{"v0.10.0", "0.9.0", true},
		{"v0.2.0", "dev", true},     // a dev build should always be offered the release
		{"garbage", "0.1.0", false}, // an unreadable tag must never trigger a download
	}
	for _, c := range cases {
		if got := newerThan(c.candidate, c.current); got != c.want {
			t.Errorf("newerThan(%q, %q) = %v, want %v", c.candidate, c.current, got, c.want)
		}
	}
}

func TestExpectedChecksumParsesSha256sumFormat(t *testing.T) {
	sums := []byte("aaaa  L80_darwin_arm64.tar.gz\nbbbb  L80_linux_amd64.tar.gz\n")
	got, ok := expectedChecksum(sums, "L80_linux_amd64.tar.gz")
	if !ok || got != "bbbb" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := expectedChecksum(sums, "L80_windows_amd64.tar.gz"); ok {
		t.Fatal("unlisted asset must not match")
	}
}

// tarball builds a release-shaped archive containing one file named L80.
func tarball(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// fakeRelease stands in for GitHub: a latest-release index plus asset downloads.
// Returns the server and the "installed" binary path the update should replace.
func fakeRelease(t *testing.T, tag string, tgz []byte, checksums string) (target string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("GitHub requires a User-Agent; none was sent")
		}
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	})
	mux.HandleFunc("/releases/download/"+tag+"/"+assetName(), func(w http.ResponseWriter, r *http.Request) {
		w.Write(tgz)
	})
	mux.HandleFunc("/releases/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldSrc, oldExe := updateSource, currentExecutable
	updateSource = releaseSource{apiBase: srv.URL, downloadBase: srv.URL}
	target = filepath.Join(t.TempDir(), "L80")
	if err := os.WriteFile(target, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	currentExecutable = func() (string, error) { return target, nil }
	t.Cleanup(func() { updateSource, currentExecutable = oldSrc, oldExe })
	return target
}

func sumOf(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func run(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, er bytes.Buffer
	code = runUpdate(env{stdout: &o, stderr: &er}, args)
	return code, o.String(), er.String()
}

func TestUpdateReplacesBinaryInPlace(t *testing.T) {
	newBin := []byte("NEW BINARY")
	tgz := tarball(t, "L80", newBin)
	target := fakeRelease(t, "v99.0.0", tgz, sumOf(tgz)+"  "+assetName()+"\n")

	code, out, errOut := run(t)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "updated L80 "+Version+" -> 99.0.0") {
		t.Errorf("unexpected output: %s", out)
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(newBin) {
		t.Fatalf("binary not replaced: %q", got)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("replaced binary is not executable")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".L80.update-*")); len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestUpdateRefusesChecksumMismatchAndLeavesBinaryIntact(t *testing.T) {
	tgz := tarball(t, "L80", []byte("NEW BINARY"))
	target := fakeRelease(t, "v99.0.0", tgz, strings.Repeat("0", 64)+"  "+assetName()+"\n")

	code, _, errOut := run(t)
	if code == 0 || !strings.Contains(errOut, "E_UPDATE_FAILED") || !strings.Contains(errOut, "checksum mismatch") {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "OLD BINARY" {
		t.Fatalf("binary was replaced despite bad checksum: %q", got)
	}
}

func TestUpdateCheckOnlyReportsWithoutInstalling(t *testing.T) {
	tgz := tarball(t, "L80", []byte("NEW BINARY"))
	target := fakeRelease(t, "v99.0.0", tgz, sumOf(tgz)+"  "+assetName()+"\n")

	code, out, _ := run(t, "--check")
	if code != 0 || !strings.Contains(out, "update available: "+Version+" -> 99.0.0") {
		t.Fatalf("exit %d, out: %s", code, out)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "OLD BINARY" {
		t.Fatal("--check must not install")
	}

	code, out, _ = run(t, "--check", "--json")
	if code != 0 || !strings.Contains(out, `"update_available": true`) || !strings.Contains(out, `"installed": false`) {
		t.Fatalf("json: exit %d, out: %s", code, out)
	}
}

func TestUpdateWhenAlreadyCurrentDoesNothing(t *testing.T) {
	tgz := tarball(t, "L80", []byte("NEW BINARY"))
	target := fakeRelease(t, "v"+Version, tgz, sumOf(tgz)+"  "+assetName()+"\n")

	code, out, _ := run(t)
	if code != 0 || !strings.Contains(out, "is up to date") {
		t.Fatalf("exit %d, out: %s", code, out)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "OLD BINARY" {
		t.Fatal("binary must be untouched when already current")
	}
}

func TestUpdateRejectsArchiveWithoutTheBinary(t *testing.T) {
	tgz := tarball(t, "README.md", []byte("not a binary"))
	target := fakeRelease(t, "v99.0.0", tgz, sumOf(tgz)+"  "+assetName()+"\n")

	code, _, errOut := run(t)
	if code == 0 || !strings.Contains(errOut, "does not contain L80") {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "OLD BINARY" {
		t.Fatal("binary must be untouched")
	}
}
