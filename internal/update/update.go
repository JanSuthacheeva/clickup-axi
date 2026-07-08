// Package update is the driven adapter for GitHub releases: explicit
// self-update, the passive once-per-24h update notice, and healing of
// installed skill copies. It never learns how the skill is generated -
// the CLI injects the rendered content at wiring time, which keeps the
// import graph acyclic (cli -> update, never back).
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/JanSuthacheeva/clickup-axi/internal/output"
	"github.com/JanSuthacheeva/clickup-axi/internal/version"
)

const releasesURL = "https://github.com/JanSuthacheeva/clickup-axi/releases"

// errNoRelease means the release server answered but no release has
// been published yet.
var errNoRelease = errors.New("no release published yet")

const updateHelp = `clickup-axi update

Updates this binary in place to the latest GitHub release: resolves
the newest version, downloads the asset for this platform, verifies it
against SHA256SUMS, and atomically replaces the executable. Running
the latest version already is a no-op. Installed skill copies refresh
on the next command after an update. Not supported on Windows.

examples:
  clickup-axi update`

// Updater carries everything the update paths need, injected like the
// ClickUp client so tests can point it at fakes. The zero value is
// inert: no cache path, no skill content, no network.
type Updater struct {
	Base         string // release page base, e.g. .../releases
	HTTP         *http.Client
	ExePath      string // running executable ("" = unknown)
	CachePath    string // passive-check cache file ("" = check disabled)
	SkillPath    string // installed skill copy to heal ("" = none)
	SkillContent string // this binary's rendered skill ("" = no healing)
	Disabled     bool   // CLICKUP_AXI_NO_UPDATE_CHECK: no post-command work
}

// NewFromEnv wires the real updater. skillContent is the skill as this
// binary would generate it, used to heal installed copies.
func NewFromEnv(skillContent string) *Updater {
	u := &Updater{
		Base:         releasesURL,
		HTTP:         &http.Client{Timeout: 30 * time.Second},
		SkillContent: skillContent,
		Disabled:     os.Getenv("CLICKUP_AXI_NO_UPDATE_CHECK") != "",
	}
	if p, err := os.Executable(); err == nil {
		u.ExePath = p
	}
	if dir, err := os.UserConfigDir(); err == nil {
		u.CachePath = filepath.Join(dir, "clickup-axi", "update-check")
	}
	if home, err := os.UserHomeDir(); err == nil {
		u.SkillPath = filepath.Join(home, ".claude", "skills", "clickup-axi", "SKILL.md")
	}
	return u
}

// AssetName is the release asset for this platform, exported for tests
// that fake the release server.
func AssetName() string {
	n := "clickup-axi_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

// latestTag resolves the newest release tag from the /latest redirect's
// Location header - one request, no GitHub API quota.
func (u *Updater) latestTag(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Base+"/latest", nil)
	if err != nil {
		return "", err
	}
	c := *u.HTTP
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
	// With zero releases GitHub redirects to /releases instead of
	// /releases/tag/<tag>; only a /tag/ redirect carries a version.
	if !strings.Contains(loc, "/tag/") {
		return "", errNoRelease
	}
	tag := path.Base(loc)
	if tag == "" || tag == "." || tag == "/" {
		return "", fmt.Errorf("release redirect carries no tag")
	}
	return tag, nil
}

func (u *Updater) download(name string) ([]byte, error) {
	resp, err := u.HTTP.Get(u.Base + "/latest/download/" + name)
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

// Cmd is the `clickup-axi update` command handler.
func Cmd(args []string, up *Updater, out io.Writer) int {
	for _, a := range args {
		switch a {
		case "--help", "-h":
			fmt.Fprintln(out, updateHelp)
			return 0
		default:
			output.WriteError(out, fmt.Sprintf("unknown argument %q for update\n  valid: none (--help only)", a),
				"Run `clickup-axi update` with no flags")
			return 2
		}
	}
	if runtime.GOOS == "windows" {
		output.WriteError(out, "self-update cannot replace a running executable on Windows",
			"Download "+AssetName()+" from "+up.releasePage()+" and replace the binary manually")
		return 1
	}
	if up.ExePath == "" {
		output.WriteError(out, "could not locate the running executable")
		return 1
	}

	tag, err := up.latestTag(10 * time.Second)
	if errors.Is(err, errNoRelease) {
		output.WriteError(out, "no release has been published yet",
			"Check "+up.releasePage()+" and retry once a release exists")
		return 1
	}
	if err != nil {
		output.WriteError(out, "could not reach the release server",
			"Check network access and retry `clickup-axi update`")
		return 1
	}
	latest := strings.TrimPrefix(tag, "v")
	running := strings.TrimPrefix(version.String(), "v")
	if running == latest {
		fmt.Fprintf(out, "update: already at v%s (no-op)\n", latest)
		return 0
	}

	asset := AssetName()
	bin, err := up.download(asset)
	if err != nil {
		output.WriteError(out, "could not download "+asset,
			"Retry `clickup-axi update`; if it persists, download manually from "+up.releasePage())
		return 1
	}
	sums, err := up.download("SHA256SUMS")
	if err != nil {
		output.WriteError(out, "could not download SHA256SUMS",
			"Retry `clickup-axi update`; if it persists, download manually from "+up.releasePage())
		return 1
	}
	if !checksumOK(sums, asset, bin) {
		output.WriteError(out, "checksum mismatch for "+asset+" - existing binary left untouched",
			"Retry `clickup-axi update`; if it persists, download manually from "+up.releasePage())
		return 1
	}

	if err := replaceExecutable(up.ExePath, bin); err != nil {
		output.WriteError(out, "could not replace "+output.CollapseHome(up.ExePath),
			"Check write permissions for the binary's directory and retry `clickup-axi update`")
		return 1
	}
	fmt.Fprintf(out, "update: v%s -> v%s\n", running, latest)
	fmt.Fprintf(out, "  binary: %s (sha256 verified)\n", output.CollapseHome(up.ExePath))
	fmt.Fprintln(out, "  skill: installed copies refresh on the next command")
	output.WriteHelp(out, "Run `clickup-axi --version` to confirm the new version")
	return 0
}

func (u *Updater) releasePage() string {
	return u.Base + "/latest"
}

// PostCommand runs the best-effort maintenance that follows a normal
// command. It must never affect the command's output semantics or exit
// code, and it is skipped entirely under CLICKUP_AXI_NO_UPDATE_CHECK.
// The update notice is gated on ok: a failed command (often an offline
// one) must not pay the network check's latency or append a notice to
// error output. The skill heal is local and stays useful either way.
func (u *Updater) PostCommand(out io.Writer, ok bool) {
	if u.Disabled {
		return
	}
	u.healSkillCopy(out)
	if ok {
		u.notifyUpdate(out)
	}
}

// healSkillCopy rewrites the installed skill copy when it no longer
// matches this binary's embedded skill, so binary and skill cannot
// skew (the copy catches up one command after an update). Only a
// regular file carrying our generated-by marker is ever touched -
// symlinked checkout installs belong to git - and a rewrite is always
// announced, never silent.
func (u *Updater) healSkillCopy(out io.Writer) {
	if u.SkillPath == "" || u.SkillContent == "" {
		return
	}
	fi, err := os.Lstat(u.SkillPath)
	if err != nil || !fi.Mode().IsRegular() {
		return
	}
	got, err := os.ReadFile(u.SkillPath)
	if err != nil || !strings.Contains(string(got), "Generated by `clickup-axi skill --write`") {
		return
	}
	if string(got) == u.SkillContent {
		return
	}
	if os.WriteFile(u.SkillPath, []byte(u.SkillContent), 0o644) != nil {
		return
	}
	fmt.Fprintf(out, "skill: refreshed %s to match this binary\n", output.CollapseHome(u.SkillPath))
}

// notifyUpdate appends a one-line notice when a newer release is known.
// The latest tag is refreshed at most once per 24h with a hard 500ms
// budget; failures are silent and still stamp the cache so a broken
// network never causes per-command retries.
func (u *Updater) notifyUpdate(out io.Writer) {
	if u.CachePath == "" {
		return
	}
	running := strings.TrimPrefix(version.String(), "v")
	if !isReleaseVersion(running) {
		return // dev and checkout builds are not "outdated"
	}
	tag, fresh := u.cachedTag()
	if !fresh {
		var err error
		tag, err = u.latestTag(500 * time.Millisecond)
		if err != nil {
			tag = ""
		}
		u.writeCache(tag)
	}
	latest := strings.TrimPrefix(tag, "v")
	if latest == "" || latest == running {
		return
	}
	fmt.Fprintf(out, "update: v%s available (running v%s) - run `clickup-axi update`\n", latest, running)
}

// cachedTag reads the check cache; fresh reports whether the stamp is
// younger than 24h (an empty tag with a fresh stamp means the last
// check failed and should not be retried yet).
func (u *Updater) cachedTag() (tag string, fresh bool) {
	raw, err := os.ReadFile(u.CachePath)
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return "", false
	}
	ts, err := time.Parse(time.RFC3339, fields[0])
	if err != nil || time.Since(ts) > 24*time.Hour {
		return "", false
	}
	if len(fields) > 1 {
		tag = fields[1]
	}
	return tag, true
}

func (u *Updater) writeCache(tag string) {
	if os.MkdirAll(filepath.Dir(u.CachePath), 0o700) != nil {
		return
	}
	line := time.Now().UTC().Format(time.RFC3339) + " " + tag + "\n"
	_ = os.WriteFile(u.CachePath, []byte(line), 0o600)
}

// isReleaseVersion reports whether s looks like a plain release
// version (X.Y.Z), as opposed to "dev" or a VCS pseudo-version.
func isReleaseVersion(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
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
