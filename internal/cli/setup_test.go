package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupHome creates a fake $HOME with claude-code and opencode config
// dirs present and codex absent, and points the process at it.
// os.UserHomeDir honors $HOME on unix (precedent: config-path tests).
func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{".claude", filepath.Join(".config", "opencode")} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Pin the hook command: the real one embeds the test binary's path
	// (cli.test), which would defeat the "contains clickup-axi"
	// recognition that no-op, repair, and remove depend on.
	old := setupHookCommand
	setupHookCommand = func() string { return "clickup-axi context" }
	t.Cleanup(func() { setupHookCommand = old })
	return home
}

func TestSetupNonTTYRequiresScope(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "setup")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "--global") || !strings.Contains(out, "--project") {
		t.Errorf("usage error must inline both scope flags:\n%s", out)
	}
}

func TestSetupRejectsConflictingScopes(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "setup", "--global", "--project")
	if code != 2 || !strings.Contains(out, "cannot be combined") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestSetupRejectsUnknownApp(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "setup", "--global", "--app", "cursor")
	if code != 2 || !strings.Contains(out, "claude-code, codex, opencode") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestSetupGlobalInstallsDetectedHosts(t *testing.T) {
	home := setupHome(t)
	_, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "setup", "--global")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"setup: session hook (global)",
		"claude-code: installed",
		"codex: skipped",
		"not found",
		"opencode: installed",
		"Run `clickup-axi setup --global --remove` to uninstall",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}

	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings.json invalid: %v", err)
	}
	if !strings.Contains(string(raw), `"matcher": "startup"`) || !strings.Contains(string(raw), " context") {
		t.Errorf("hook entry missing:\n%s", raw)
	}
	plugin, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "plugins", "clickup-axi.js"))
	if err != nil || !strings.Contains(string(plugin), "clickup-axi setup") {
		t.Errorf("plugin not written: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Errorf("setup created ~/.codex for an absent host")
	}
}

func TestSetupNoHostsSuppressesLoadHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	old := setupHookCommand
	setupHookCommand = func() string { return "clickup-axi context" }
	t.Cleanup(func() { setupHookCommand = old })
	_, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "setup", "--global")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "Start a new agent session to load the ClickUp dashboard") {
		t.Errorf("load hint shown when nothing was installed:\n%s", out)
	}
	if !strings.Contains(out, "No supported host configs found") {
		t.Errorf("missing guidance when no hosts detected:\n%s", out)
	}
}

func TestSetupRerunIsNoOp(t *testing.T) {
	setupHome(t)
	_, c := newFakeClickUp(t)
	runCLI(t, c, "setup", "--global")
	out, code := runCLI(t, c, "setup", "--global")
	if code != 0 || !strings.Contains(out, "claude-code: already installed (no-op)") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestSetupRepairsStaleCommand(t *testing.T) {
	home := setupHome(t)
	_, c := newFakeClickUp(t)
	stale := `{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "/gone/clickup-axi context"}]}]}}`
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, c, "setup", "--global")
	if code != 0 || !strings.Contains(out, "claude-code: repaired") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if strings.Contains(string(raw), "/gone/") {
		t.Errorf("stale path survived repair:\n%s", raw)
	}
}

func TestSetupProjectScopeInstallsIntoCwd(t *testing.T) {
	setupHome(t)
	_, c := newFakeClickUp(t)
	dir := t.TempDir()
	t.Chdir(dir)

	out, code := runCLI(t, c, "setup", "--project")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "setup: session hook (project)") ||
		!strings.Contains(out, "opencode: skipped (global only)") {
		t.Errorf("output:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".claude", "settings.json")); err != nil {
		t.Errorf(".claude/settings.json not created: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".codex", "hooks.json")); err != nil {
		t.Errorf(".codex/hooks.json not created: %v", err)
	}
}

func TestSetupRemoveCycle(t *testing.T) {
	setupHome(t)
	_, c := newFakeClickUp(t)
	runCLI(t, c, "setup", "--global")

	out, code := runCLI(t, c, "setup", "--global", "--remove")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	for _, want := range []string{"claude-code: removed", "opencode: removed", "Run `clickup-axi setup --global` to reinstall"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}

	out, code = runCLI(t, c, "setup", "--global", "--remove")
	if code != 0 || !strings.Contains(out, "claude-code: not installed (no-op)") {
		t.Errorf("second remove: exit %d output:\n%s", code, out)
	}
}

func TestSetupAppFilterLimitsToOneHost(t *testing.T) {
	setupHome(t)
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "setup", "--global", "--app", "claude-code")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "claude-code: installed") || strings.Contains(out, "opencode:") {
		t.Errorf("app filter leaked other hosts:\n%s", out)
	}
}

func TestSetupFailureExitsOneButProcessesOtherHosts(t *testing.T) {
	home := setupHome(t)
	_, c := newFakeClickUp(t)
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, c, "setup", "--global")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "claude-code: error") || !strings.Contains(out, "opencode: installed") {
		t.Errorf("one failing host must not stop the others:\n%s", out)
	}
}

func TestParseScopeChoice(t *testing.T) {
	for input, want := range map[string]string{
		"": "global", "1": "global", "g": "global", "global": "global",
		"2": "project", "p": "project", "project": "project",
	} {
		got, ok := parseScopeChoice(input)
		if !ok || scopeName(got) != want {
			t.Errorf("parseScopeChoice(%q) = %v ok=%v, want %s", input, got, ok, want)
		}
	}
	if _, ok := parseScopeChoice("nonsense"); ok {
		t.Errorf("nonsense accepted as a scope")
	}
}
