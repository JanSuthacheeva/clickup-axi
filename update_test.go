package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newFakeReleases serves a GitHub-releases-shaped fake: /latest redirects
// to the tag, /latest/download/<name> serves assets. sums maps asset
// names to file bytes; SHA256SUMS is derived from it unless overridden
// by an explicit "SHA256SUMS" entry.
func newFakeReleases(t *testing.T, tag string, assets map[string][]byte) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("GET /latest/download/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if body, ok := assets[name]; ok {
			w.Write(body)
			return
		}
		if name == "SHA256SUMS" {
			for asset, body := range assets {
				sum := sha256.Sum256(body)
				fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
			}
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func setVersion(t *testing.T, v string) {
	t.Helper()
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

// tempExe stands in for the running executable during update tests.
func tempExe(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "clickup-axi")
	if err := os.WriteFile(p, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func testUpdater(base, exePath string) *updater {
	return &updater{base: base, http: &http.Client{Timeout: 5 * time.Second}, exePath: exePath}
}

func TestUpdateReplacesBinary(t *testing.T) {
	setVersion(t, "0.1.0")
	exe := tempExe(t)
	base := newFakeReleases(t, "v9.9.9", map[string][]byte{assetName(): []byte("new binary")})
	_, c := newFakeClickUp(t)

	out, code := runCLIWithUpdater(t, c, testUpdater(base, exe), "", "update")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"update: v0.1.0 -> v9.9.9",
		"(sha256 verified)",
		"skill: installed copies refresh on the next command",
		"Run `clickup-axi --version` to confirm the new version",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "new binary" {
		t.Errorf("binary content = %q, err %v; want %q", got, err, "new binary")
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
		t.Errorf("binary mode = %v, want 0755", fi.Mode().Perm())
	}
}

func TestUpdateNoOpWhenAlreadyLatest(t *testing.T) {
	setVersion(t, "9.9.9")
	exe := tempExe(t)
	base := newFakeReleases(t, "v9.9.9", map[string][]byte{assetName(): []byte("new binary")})
	_, c := newFakeClickUp(t)

	out, code := runCLIWithUpdater(t, c, testUpdater(base, exe), "", "update")
	if code != 0 || !strings.Contains(out, "update: already at v9.9.9 (no-op)") {
		t.Errorf("exit %d\noutput:\n%s", code, out)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("no-op update touched the binary: %q", got)
	}
}

func TestUpdateChecksumMismatchAborts(t *testing.T) {
	setVersion(t, "0.1.0")
	exe := tempExe(t)
	base := newFakeReleases(t, "v9.9.9", map[string][]byte{
		assetName():  []byte("new binary"),
		"SHA256SUMS": []byte("deadbeef  " + assetName() + "\n"),
	})
	_, c := newFakeClickUp(t)

	out, code := runCLIWithUpdater(t, c, testUpdater(base, exe), "", "update")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "error: checksum mismatch for "+assetName()) {
		t.Errorf("missing checksum error\noutput:\n%s", out)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("failed update touched the binary: %q", got)
	}
}

func TestUpdateNetworkFailureIsTranslated(t *testing.T) {
	setVersion(t, "0.1.0")
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // unreachable from here on
	_, c := newFakeClickUp(t)

	out, code := runCLIWithUpdater(t, c, testUpdater(base, tempExe(t)), "", "update")
	if code != 1 || !strings.Contains(out, "error: could not reach the release server") {
		t.Errorf("exit %d\noutput:\n%s", code, out)
	}
}

func TestUpdateUnknownFlagIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLIWithUpdater(t, c, &updater{}, "", "update", "--force")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "valid: none (--help only)") {
		t.Errorf("usage error does not state valid flags\noutput:\n%s", out)
	}
}
