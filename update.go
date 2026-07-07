package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releasesURL = "https://github.com/JanSuthacheeva/clickup-axi/releases"

const updateHelp = `clickup-axi update

Updates this binary in place to the latest GitHub release: resolves
the newest version, downloads the asset for this platform, verifies it
against SHA256SUMS, and atomically replaces the executable. Running
the latest version already is a no-op. Installed skill copies refresh
on the next command after an update. Not supported on Windows.

examples:
  clickup-axi update`

// updater carries everything the update paths need, injected like the
// ClickUp client so tests can point it at fakes.
type updater struct {
	base      string // release page base, e.g. .../releases
	http      *http.Client
	exePath   string // running executable ("" = unknown)
	cachePath string // passive-check cache file ("" = check disabled)
	skillPath string // installed skill copy to heal ("" = none)
	disabled  bool   // CLICKUP_AXI_NO_UPDATE_CHECK: no post-command work
}

func newUpdaterFromEnv() *updater {
	u := &updater{
		base:     releasesURL,
		http:     &http.Client{Timeout: 30 * time.Second},
		disabled: os.Getenv("CLICKUP_AXI_NO_UPDATE_CHECK") != "",
	}
	if p, err := os.Executable(); err == nil {
		u.exePath = p
	}
	if dir, err := os.UserConfigDir(); err == nil {
		u.cachePath = filepath.Join(dir, "clickup-axi", "update-check")
	}
	if home, err := os.UserHomeDir(); err == nil {
		u.skillPath = filepath.Join(home, ".claude", "skills", "clickup-axi", "SKILL.md")
	}
	return u
}

func assetName() string {
	n := "clickup-axi_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

// latestTag resolves the newest release tag from the /latest redirect's
// Location header - one request, no GitHub API quota.
func (u *updater) latestTag(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.base+"/latest", nil)
	if err != nil {
		return "", err
	}
	c := *u.http
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || loc == "" {
		return "", fmt.Errorf("no release redirect (status %d)", resp.StatusCode)
	}
	tag := path.Base(loc)
	if tag == "" || tag == "." || tag == "/" {
		return "", fmt.Errorf("release redirect carries no tag")
	}
	return tag, nil
}

func (u *updater) download(name string) ([]byte, error) {
	resp, err := u.http.Get(u.base + "/latest/download/" + name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download of %s failed (status %d)", name, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

// checksumOK verifies data against the asset's line in a SHA256SUMS file.
func checksumOK(sums []byte, asset string, data []byte) bool {
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == want
}

func cmdUpdate(args []string, up *updater, out io.Writer) int {
	for _, a := range args {
		switch a {
		case "--help", "-h":
			fmt.Fprintln(out, updateHelp)
			return 0
		default:
			writeError(out, fmt.Sprintf("unknown argument %q for update\n  valid: none (--help only)", a),
				"Run `clickup-axi update` with no flags")
			return 2
		}
	}
	if runtime.GOOS == "windows" {
		writeError(out, "self-update cannot replace a running executable on Windows",
			"Download "+assetName()+" from "+up.releasePage()+" and replace the binary manually")
		return 1
	}
	if up.exePath == "" {
		writeError(out, "could not locate the running executable")
		return 1
	}

	tag, err := up.latestTag(10 * time.Second)
	if err != nil {
		writeError(out, "could not reach the release server",
			"Check network access and retry `clickup-axi update`")
		return 1
	}
	latest := strings.TrimPrefix(tag, "v")
	running := strings.TrimPrefix(versionString(), "v")
	if running == latest {
		fmt.Fprintf(out, "update: already at v%s (no-op)\n", latest)
		return 0
	}

	asset := assetName()
	bin, err := up.download(asset)
	if err != nil {
		writeError(out, "could not download "+asset,
			"Retry `clickup-axi update`; if it persists, download manually from "+up.releasePage())
		return 1
	}
	sums, err := up.download("SHA256SUMS")
	if err != nil {
		writeError(out, "could not download SHA256SUMS",
			"Retry `clickup-axi update`; if it persists, download manually from "+up.releasePage())
		return 1
	}
	if !checksumOK(sums, asset, bin) {
		writeError(out, "checksum mismatch for "+asset+" - existing binary left untouched",
			"Retry `clickup-axi update`; if it persists, download manually from "+up.releasePage())
		return 1
	}

	if err := replaceExecutable(up.exePath, bin); err != nil {
		writeError(out, "could not replace "+collapseHome(up.exePath),
			"Check write permissions for the binary's directory and retry `clickup-axi update`")
		return 1
	}
	fmt.Fprintf(out, "update: v%s -> v%s\n", running, latest)
	fmt.Fprintf(out, "  binary: %s (sha256 verified)\n", collapseHome(up.exePath))
	fmt.Fprintln(out, "  skill: installed copies refresh on the next command")
	writeHelp(out, "Run `clickup-axi --version` to confirm the new version")
	return 0
}

func (u *updater) releasePage() string {
	return u.base + "/latest"
}

// replaceExecutable writes the new binary next to the old one (same
// filesystem) and renames it over the target atomically.
func replaceExecutable(exePath string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(exePath), ".clickup-axi-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), exePath)
}
