package hostcfg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTargetsGlobalGatesOnHostDir(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	// no ~/.codex

	got := Targets(Global, home, "/unused")
	if len(got) != 3 {
		t.Fatalf("Targets returned %d entries, want 3", len(got))
	}
	if got[0].Host != "claude-code" || !got[0].Supported ||
		got[0].Path != filepath.Join(home, ".claude", "settings.json") {
		t.Errorf("claude-code target = %+v", got[0])
	}
	if got[1].Host != "codex" || got[1].Supported || got[1].Detail == "" {
		t.Errorf("codex should be unsupported with a detail, got %+v", got[1])
	}
	if got[2].Host != "opencode" || !got[2].Supported ||
		got[2].Path != filepath.Join(home, ".config", "opencode", "plugins", "clickup-axi.js") {
		t.Errorf("opencode target = %+v", got[2])
	}
}

func TestTargetsProjectSkipsOpenCodeOnly(t *testing.T) {
	cwd := t.TempDir() // no .claude/.codex exist: project scope installs anyway
	got := Targets(Project, "/unused", cwd)
	if !got[0].Supported || got[0].Path != filepath.Join(cwd, ".claude", "settings.json") {
		t.Errorf("claude-code project target = %+v", got[0])
	}
	if !got[1].Supported || got[1].Path != filepath.Join(cwd, ".codex", "hooks.json") {
		t.Errorf("codex project target = %+v", got[1])
	}
	if got[2].Supported || got[2].Detail != "global only" {
		t.Errorf("opencode project target = %+v", got[2])
	}
}

func TestInstallOnUnsupportedTargetReportsSkipped(t *testing.T) {
	r := Install(Target{Host: "codex", Detail: "~/.codex not found"}, "clickup-axi context")
	if r.Action != Skipped || r.Detail != "~/.codex not found" {
		t.Errorf("report = %+v, want Skipped with detail", r)
	}
}

func TestHookCommandFallsBackToQuotedAbsolutePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH semantics differ on windows")
	}
	// Empty PATH: LookPath fails, so the absolute exe path is used.
	t.Setenv("PATH", t.TempDir())
	if got := HookCommand("/opt/my tools/clickup-axi"); got != `"/opt/my tools/clickup-axi" context` {
		t.Errorf("HookCommand = %q", got)
	}
	if got := HookCommand("/usr/local/bin/clickup-axi"); got != "/usr/local/bin/clickup-axi context" {
		t.Errorf("HookCommand = %q (no quoting needed)", got)
	}
}

func TestHookCommandUsesBareNameWhenPathResolvesToExe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH semantics differ on windows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "clickup-axi")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got := HookCommand(exe); got != "clickup-axi context" {
		t.Errorf("HookCommand = %q, want bare name", got)
	}
}
