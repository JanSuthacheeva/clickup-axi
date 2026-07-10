// Package hostcfg is the driven adapter for agent-host configuration:
// it installs, repairs, and removes the clickup-axi session hook in
// Claude Code, Codex, and OpenCode configs. It knows nothing about
// ClickUp; the hook command string is an input. It never imports cli.
package hostcfg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

// Scope selects where the hook is installed.
type Scope int

const (
	Global Scope = iota
	Project
)

// Action is the outcome of an install or remove on one host.
type Action int

const (
	Installed Action = iota
	Repaired
	AlreadyInstalled
	Removed
	NotInstalled
	Skipped
	Failed
)

// Report is one host's outcome, rendered by the CLI as a single line.
type Report struct {
	Host   string
	Path   string
	Action Action
	Detail string
}

// Target is one host's config location for a scope. Unsupported targets
// carry the reason in Detail and are reported as skipped.
type Target struct {
	Host      string
	Path      string
	Supported bool
	Detail    string
}

// Targets lists the three hosts for a scope, always in the order
// claude-code, codex, opencode. Global targets are gated on the host's
// config dir existing (absent host = skipped, never create global
// state for absent software). Project targets install unconditionally
// for claude-code and codex - the user explicitly chose this project,
// so there is nothing to detect - and opencode has no project plugins.
func Targets(scope Scope, home, cwd string) []Target {
	if scope == Project {
		return []Target{
			{Host: "claude-code", Path: filepath.Join(cwd, ".claude", "settings.json"), Supported: true},
			{Host: "codex", Path: filepath.Join(cwd, ".codex", "hooks.json"), Supported: true},
			{Host: "opencode", Detail: "global only"},
		}
	}
	return []Target{
		dirGated("claude-code", filepath.Join(home, ".claude"), "settings.json"),
		dirGated("codex", filepath.Join(home, ".codex"), "hooks.json"),
		dirGated("opencode", filepath.Join(home, ".config", "opencode"), filepath.Join("plugins", "clickup-axi.js")),
	}
}

func dirGated(host, dir, rel string) Target {
	t := Target{Host: host, Path: filepath.Join(dir, rel)}
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		t.Supported = true
		return t
	}
	t.Detail = output.CollapseHome(dir) + " not found"
	return t
}

// Install writes the session hook for one target.
func Install(t Target, hookCmd string) Report {
	if !t.Supported {
		return Report{Host: t.Host, Path: t.Path, Action: Skipped, Detail: t.Detail}
	}
	switch t.Host {
	case "claude-code":
		return installHooksJSON(t, claudeGroups(hookCmd))
	case "codex":
		return installHooksJSON(t, codexGroups(hookCmd))
	case "opencode":
		return installOpenCode(t, hookCmd)
	}
	return Report{Host: t.Host, Action: Failed, Detail: "unknown host"}
}

// Remove deletes the session hook for one target, leaving everything
// else in the config untouched.
func Remove(t Target) Report {
	if !t.Supported {
		return Report{Host: t.Host, Path: t.Path, Action: Skipped, Detail: t.Detail}
	}
	switch t.Host {
	case "claude-code", "codex":
		return removeHooksJSON(t)
	case "opencode":
		return removeOpenCode(t)
	}
	return Report{Host: t.Host, Action: Failed, Detail: "unknown host"}
}

// HookCommand is the command string hooks will run: the bare binary
// name when `clickup-axi` on PATH is this executable (portable across
// reinstalls), else the absolute path (quoted if it needs it).
func HookCommand(exePath string) string {
	if exePath == "" {
		return "clickup-axi context"
	}
	if p, err := exec.LookPath("clickup-axi"); err == nil && sameFile(p, exePath) {
		return "clickup-axi context"
	}
	return shellQuote(exePath) + " context"
}

func sameFile(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	fa, err := os.Stat(ra)
	if err != nil {
		return false
	}
	fb, err := os.Stat(rb)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// shellQuote wraps s in double quotes when it contains characters the
// host's shell would split or expand. Hook commands run through a
// shell on every host.
func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t'\"\\$`") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`")
	return `"` + r.Replace(s) + `"`
}

func failf(host, path, format string, a ...any) Report {
	return Report{Host: host, Path: path, Action: Failed, Detail: fmt.Sprintf(format, a...)}
}
