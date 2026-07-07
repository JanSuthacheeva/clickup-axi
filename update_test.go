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

// passiveUpdater points the notice machinery at a temp cache file.
func passiveUpdater(t *testing.T, base string) *updater {
	t.Helper()
	return &updater{
		base:      base,
		http:      &http.Client{Timeout: 5 * time.Second},
		cachePath: filepath.Join(t.TempDir(), "update-check"),
	}
}

func TestPassiveCheckNotifiesAndCaches(t *testing.T) {
	setVersion(t, "0.1.0")
	base := newFakeReleases(t, "v9.9.9", nil)
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	up := passiveUpdater(t, base)

	out, code := runCLIWithUpdater(t, c, up, "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	notice := "update: v9.9.9 available (running v0.1.0) - run `clickup-axi update`"
	if !strings.Contains(out, notice) {
		t.Errorf("output missing notice %q\noutput:\n%s", notice, out)
	}
	cache, err := os.ReadFile(up.cachePath)
	if err != nil || !strings.Contains(string(cache), "v9.9.9") {
		t.Errorf("cache = %q, err %v; want the latest tag stamped", cache, err)
	}

	// Second run within 24h must serve the notice from the cache, not
	// the network: point the updater at a dead base to prove it.
	up.base = "http://127.0.0.1:0"
	out, _ = runCLIWithUpdater(t, c, up, "")
	if !strings.Contains(out, notice) {
		t.Errorf("cached notice missing on second run\noutput:\n%s", out)
	}
}

func TestPassiveCheckSilentOnFailureAndStampsCache(t *testing.T) {
	setVersion(t, "0.1.0")
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	up := passiveUpdater(t, "http://127.0.0.1:0") // unreachable

	out, code := runCLIWithUpdater(t, c, up, "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "update:") {
		t.Errorf("failed check leaked into output:\n%s", out)
	}
	if _, err := os.Stat(up.cachePath); err != nil {
		t.Errorf("failed check did not stamp the cache: %v", err)
	}
}

func TestPassiveCheckSuppressedForDevBuilds(t *testing.T) {
	setVersion(t, "") // versionString falls back to dev/pseudo
	base := newFakeReleases(t, "v9.9.9", nil)
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	up := passiveUpdater(t, base)

	out, _ := runCLIWithUpdater(t, c, up, "")
	if strings.Contains(out, "update:") {
		t.Errorf("dev build was nagged to update:\n%s", out)
	}
}

func TestPassiveCheckDisabledSkipsEverything(t *testing.T) {
	setVersion(t, "0.1.0")
	base := newFakeReleases(t, "v9.9.9", nil)
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	up := passiveUpdater(t, base)
	up.disabled = true

	out, _ := runCLIWithUpdater(t, c, up, "")
	if strings.Contains(out, "update:") {
		t.Errorf("disabled check still notified:\n%s", out)
	}
	if _, err := os.Stat(up.cachePath); err == nil {
		t.Errorf("disabled check still wrote the cache")
	}
}

func TestNoticeSuppressedOnSkillOutput(t *testing.T) {
	setVersion(t, "0.1.0")
	base := newFakeReleases(t, "v9.9.9", nil)
	_, c := newFakeClickUp(t)
	up := passiveUpdater(t, base)

	out, code := runCLIWithUpdater(t, c, up, "", "skill")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if out != generateSkill() {
		t.Errorf("skill output is no longer byte-exact:\n%s", out)
	}
}

// healUpdater points the self-heal at a temp skill copy path.
func healUpdater(t *testing.T) *updater {
	t.Helper()
	return &updater{skillPath: filepath.Join(t.TempDir(), "SKILL.md")}
}

func TestSkillCopySelfHeals(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	up := healUpdater(t)
	stale := strings.Replace(generateSkill(), "# clickup-axi", "# clickup-axi (stale)", 1)
	if err := os.WriteFile(up.skillPath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runCLIWithUpdater(t, c, up, "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "skill: refreshed "+up.skillPath+" to match this binary") {
		t.Errorf("heal was not announced\noutput:\n%s", out)
	}
	got, _ := os.ReadFile(up.skillPath)
	if string(got) != generateSkill() {
		t.Errorf("skill copy was not healed to the embedded content")
	}

	// A second run must be silent: the copy already matches.
	out, _ = runCLIWithUpdater(t, c, up, "")
	if strings.Contains(out, "skill: refreshed") {
		t.Errorf("heal announced without a rewrite\noutput:\n%s", out)
	}
}

func TestSkillCopyWithoutMarkerIsUntouched(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	up := healUpdater(t)
	foreign := "# my hand-written skill\n"
	if err := os.WriteFile(up.skillPath, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := runCLIWithUpdater(t, c, up, "")
	if strings.Contains(out, "skill: refreshed") {
		t.Errorf("foreign file was announced as refreshed\noutput:\n%s", out)
	}
	if got, _ := os.ReadFile(up.skillPath); string(got) != foreign {
		t.Errorf("foreign skill file was overwritten: %q", got)
	}
}

func TestSkillCopySymlinkIsUntouched(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	dir := t.TempDir()
	target := filepath.Join(dir, "checkout-SKILL.md")
	stale := strings.Replace(generateSkill(), "# clickup-axi", "# clickup-axi (stale)", 1)
	if err := os.WriteFile(target, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "SKILL.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	up := &updater{skillPath: link}

	out, _ := runCLIWithUpdater(t, c, up, "")
	if strings.Contains(out, "skill: refreshed") {
		t.Errorf("symlinked skill was announced as refreshed\noutput:\n%s", out)
	}
	if got, _ := os.ReadFile(target); string(got) != stale {
		t.Errorf("symlink target was overwritten")
	}
}

// TestNoReleasesYetIsHandled pins the zero-release edge: GitHub then
// redirects /releases/latest to /releases (no /tag/ segment), which
// must read as "no release", not as a tag named "releases".
func TestNoReleasesYetIsHandled(t *testing.T) {
	setVersion(t, "0.1.0")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")

	// Passive path: no notice, cache stamped.
	up := passiveUpdater(t, srv.URL)
	out, _ := runCLIWithUpdater(t, c, up, "")
	if strings.Contains(out, "update:") {
		t.Errorf("zero-release redirect produced a notice:\n%s", out)
	}

	// Explicit update: honest error (not a fake network failure), exit 1.
	out, code := runCLIWithUpdater(t, c, testUpdater(srv.URL, tempExe(t)), "", "update")
	if code != 1 || !strings.Contains(out, "error: no release has been published yet") {
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
