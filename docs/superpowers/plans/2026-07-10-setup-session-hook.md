# `setup` + `context` Session Hook Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship v1.0.0 step 4 - a `setup` command that installs a
session-start hook into Claude Code, Codex, and OpenCode, and a
`context` command (the hook payload) that prints a capped open-task
dashboard the harness injects into every session.

**Architecture:** New driven adapter `internal/hostcfg` (sibling of
`internal/update`) owns agent-host config mechanics: target detection,
JSON hook merge for Claude Code and Codex, a managed plugin file for
OpenCode, install/repair/remove reports. `internal/cli` gains two thin
handlers: `cmdSetup` (flags, TTY scope prompt, report rendering) and
`cmdContext` (3-call ClickUp fetch behind a 3s watchdog, capped render,
always exit 0). Spec: `docs/superpowers/specs/2026-07-10-setup-context-design.md`.

**Tech Stack:** Go stdlib + `golang.org/x/term` (already a dependency).
No new dependencies.

## Global Constraints

- Branch: `feature/setup-session-hook` (already checked out; never commit to main).
- Dependencies: stdlib plus `golang.org/x/term` only.
- Hexagonal rule: `hostcfg` imports only `internal/output`; never `cli`, never `clickup`.
- AXI output contract: stdout carries data AND errors; exit 0 success / 1 error / 2 usage; zero results stated explicitly; every output ends with `help[]`; no interactive prompts on agent paths (TTY-only exception, like `auth login`); raw API errors never leak.
- Golden rule: every agent-visible behavior has a test asserting exact output.
- The generated skill (`skills/clickup-axi/SKILL.md`) is never hand-edited; regenerate with `go run ./cmd/clickup-axi skill --write` in the same commit as any surface change. `go test ./...` fails while it is stale.
- Verify gate before finishing any task: `gofmt -l . && go vet ./... && go test ./...` (gofmt must print nothing).
- Docs style: plain dash "-", never an em dash; no emojis.
- `context` exits 0 on every path except usage errors (exit 2 for unknown flags is fine: a misconfigured hook should still surface *something*, and harnesses discard non-zero stdout anyway).

## Verified host schemas (research 2026-07-10, do not re-derive)

**Claude Code** (`~/.claude/settings.json`, project `.claude/settings.json`; docs: code.claude.com/docs/en/hooks.md):
- `matcher` is REQUIRED for SessionStart, a single string: `startup` | `resume` | `clear` | `compact`. We install two groups: `startup` and `clear` (fresh-context events). `timeout` is seconds, per-hook.
- Exit 0: stdout injected as context. Non-zero: not injected. Invalid hook config rejects the whole settings file - the merge must always emit the exact schema.
- User and project hooks merge (both run) - a project install does not duplicate a global one from Claude Code's side, but we still treat scopes independently.

```json
{"hooks":{"SessionStart":[
  {"matcher":"startup","hooks":[{"type":"command","command":"clickup-axi context","timeout":10}]},
  {"matcher":"clear","hooks":[{"type":"command","command":"clickup-axi context","timeout":10}]}
]}}
```

**Codex** (`~/.codex/hooks.json`, project `.codex/hooks.json`; stable and enabled by default since ~v0.144; docs: developers.openai.com/codex/hooks):
- Same nesting as Claude Code. `matcher` is optional and supports exact alternation: one group with `"matcher": "startup|clear"`. `timeout` seconds. Plain stdout on exit 0 becomes model context; non-zero discards stdout.
- No `[features]` flag work needed (default-on; the flag exists only to disable). We do not read or write config.toml at all.
- Project-scope hooks belong in `.codex/hooks.json` (inline config.toml hooks have an open bug, openai/codex#17532).

```json
{"hooks":{"SessionStart":[
  {"matcher":"startup|clear","hooks":[{"type":"command","command":"clickup-axi context","timeout":10}]}
]}}
```

**OpenCode** (`~/.config/opencode/plugins/clickup-axi.js`; docs: opencode.ai/docs/plugins):
- There is NO session-start context hook (PR anomalyco/opencode#15224 unmerged). The supported mechanism: a plugin's `"chat.message"` hook fires on every user message; inject the CLI's stdout as a synthetic text part on the FIRST message of each session (nothing reaches the model before that). Global plugin dir only.

---

### Task 1: hostcfg core - types, Targets, HookCommand

**Files:**
- Create: `internal/hostcfg/hostcfg.go`
- Test: `internal/hostcfg/hostcfg_test.go`

**Interfaces:**
- Consumes: nothing from this feature (stdlib + `internal/output`).
- Produces (used by Tasks 2, 3, 5):
  - `type Scope int` with `Global`, `Project` constants
  - `type Action int` with `Installed`, `Repaired`, `AlreadyInstalled`, `Removed`, `NotInstalled`, `Skipped`, `Failed`
  - `type Report struct { Host, Path string; Action Action; Detail string }`
  - `type Target struct { Host, Path string; Supported bool; Detail string }`
  - `Targets(scope Scope, home, cwd string) []Target` (hosts always in order claude-code, codex, opencode)
  - `HookCommand(exePath string) string`
  - `Install(t Target, hookCmd string) Report` and `Remove(t Target) Report` - dispatchers added here with per-host stubs that Tasks 2 and 3 fill in.

- [ ] **Step 1: Write the failing tests**

`internal/hostcfg/hostcfg_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hostcfg/`
Expected: FAIL - `package hostcfg` does not exist yet / undefined symbols.

- [ ] **Step 3: Write the implementation**

`internal/hostcfg/hostcfg.go`:

```go
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
```

Until Task 2 and 3 land, add temporary stubs at the bottom of
`hostcfg.go` so the package compiles (Tasks 2/3 replace them):

```go
// Stubs replaced by hooksjson.go (Task 2) and opencode.go (Task 3).
func claudeGroups(hookCmd string) []any { return nil }
func codexGroups(hookCmd string) []any  { return nil }
func installHooksJSON(t Target, groups []any) Report {
	return failf(t.Host, t.Path, "not implemented")
}
func removeHooksJSON(t Target) Report { return failf(t.Host, t.Path, "not implemented") }
func installOpenCode(t Target, hookCmd string) Report {
	return failf(t.Host, t.Path, "not implemented")
}
func removeOpenCode(t Target) Report { return failf(t.Host, t.Path, "not implemented") }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./internal/hostcfg/`
Expected: PASS (5 tests), gofmt silent.

- [ ] **Step 5: Commit**

```bash
git add internal/hostcfg/
git commit -m "feat(hostcfg): add host targets, reports, and hook command resolution" -m "New driven adapter for agent-host configs (v1.0.0 step 4). Global
targets gate on the host dir existing; project targets install
unconditionally except opencode (global-only plugins). The hook command
uses the bare binary name only when PATH resolves to this executable,
so global installs stay portable and moved binaries repair on re-run."
```

---

### Task 2: hostcfg JSON hosts - shared merge for Claude Code and Codex

**Files:**
- Create: `internal/hostcfg/hooksjson.go`
- Test: `internal/hostcfg/hooksjson_test.go`
- Modify: `internal/hostcfg/hostcfg.go` (delete the four JSON-related stubs: `claudeGroups`, `codexGroups`, `installHooksJSON`, `removeHooksJSON`)

**Interfaces:**
- Consumes: `Target`, `Report`, `Action` constants, `failf` from Task 1.
- Produces: `claudeGroups(hookCmd string) []any`, `codexGroups(hookCmd string) []any`, `installHooksJSON(t Target, groups []any) Report`, `removeHooksJSON(t Target) Report` - exactly the names the Task 1 dispatcher calls.

- [ ] **Step 1: Write the failing tests**

`internal/hostcfg/hooksjson_test.go`:

```go
package hostcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cmd = "clickup-axi context"

func claudeTarget(t *testing.T) Target {
	t.Helper()
	return Target{Host: "claude-code", Path: filepath.Join(t.TempDir(), "settings.json"), Supported: true}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, raw)
	}
	return m
}

func sessionStart(t *testing.T, m map[string]any) []any {
	t.Helper()
	hooks, ok := m["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("no hooks object: %v", m)
	}
	ss, ok := hooks["SessionStart"].([]any)
	if !ok {
		t.Fatalf("no SessionStart array: %v", hooks)
	}
	return ss
}

func TestInstallClaudeIntoMissingFileCreatesBothMatchers(t *testing.T) {
	tgt := claudeTarget(t)
	r := installHooksJSON(tgt, claudeGroups(cmd))
	if r.Action != Installed {
		t.Fatalf("action = %v, want Installed (detail %q)", r.Action, r.Detail)
	}
	ss := sessionStart(t, readJSON(t, tgt.Path))
	if len(ss) != 2 {
		t.Fatalf("SessionStart groups = %d, want 2 (startup + clear)", len(ss))
	}
	first := ss[0].(map[string]any)
	if first["matcher"] != "startup" {
		t.Errorf("matcher = %v, want startup", first["matcher"])
	}
	h := first["hooks"].([]any)[0].(map[string]any)
	if h["type"] != "command" || h["command"] != cmd || h["timeout"] != float64(10) {
		t.Errorf("hook entry = %v", h)
	}
	if second := ss[1].(map[string]any); second["matcher"] != "clear" {
		t.Errorf("second matcher = %v, want clear", second["matcher"])
	}
}

func TestInstallPreservesForeignSettingsAndHooks(t *testing.T) {
	tgt := claudeTarget(t)
	existing := `{
  "model": "opus",
  "permissions": {"allow": ["Bash(go test:*)"]},
  "hooks": {
    "SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "git status"}]}],
    "PostToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo done"}]}]
  }
}`
	if err := os.WriteFile(tgt.Path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := installHooksJSON(tgt, claudeGroups(cmd)); r.Action != Installed {
		t.Fatalf("action = %v, want Installed (detail %q)", r.Action, r.Detail)
	}
	m := readJSON(t, tgt.Path)
	if m["model"] != "opus" {
		t.Errorf("foreign top-level key lost: %v", m)
	}
	if _, ok := m["hooks"].(map[string]any)["PostToolUse"]; !ok {
		t.Errorf("foreign hook event lost")
	}
	ss := sessionStart(t, m)
	if len(ss) != 3 { // git status group + our two
		t.Fatalf("SessionStart groups = %d, want 3", len(ss))
	}
	if ss[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] != "git status" {
		t.Errorf("foreign SessionStart group lost or reordered: %v", ss[0])
	}
}

func TestReinstallSameCommandIsNoOp(t *testing.T) {
	tgt := claudeTarget(t)
	installHooksJSON(tgt, claudeGroups(cmd))
	before, _ := os.ReadFile(tgt.Path)
	r := installHooksJSON(tgt, claudeGroups(cmd))
	if r.Action != AlreadyInstalled {
		t.Fatalf("action = %v, want AlreadyInstalled", r.Action)
	}
	after, _ := os.ReadFile(tgt.Path)
	if string(before) != string(after) {
		t.Errorf("no-op reinstall rewrote the file")
	}
}

func TestReinstallWithNewPathRepairs(t *testing.T) {
	tgt := claudeTarget(t)
	installHooksJSON(tgt, claudeGroups(`"/old/clickup-axi" context`))
	r := installHooksJSON(tgt, claudeGroups(cmd))
	if r.Action != Repaired {
		t.Fatalf("action = %v, want Repaired", r.Action)
	}
	ss := sessionStart(t, readJSON(t, tgt.Path))
	if len(ss) != 2 {
		t.Fatalf("SessionStart groups = %d, want 2 after repair (no duplicates)", len(ss))
	}
	got := ss[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"]
	if got != cmd {
		t.Errorf("command = %v, want %q", got, cmd)
	}
}

func TestRemoveDeletesOnlyOurGroups(t *testing.T) {
	tgt := claudeTarget(t)
	existing := `{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "git status"}]}]}}`
	if err := os.WriteFile(tgt.Path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	installHooksJSON(tgt, claudeGroups(cmd))
	if r := removeHooksJSON(tgt); r.Action != Removed {
		t.Fatalf("action = %v, want Removed", r.Action)
	}
	ss := sessionStart(t, readJSON(t, tgt.Path))
	if len(ss) != 1 || strings.Contains(ss[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string), "clickup-axi") {
		t.Errorf("remove left wrong groups: %v", ss)
	}
}

func TestRemoveWhenAbsentIsNoOp(t *testing.T) {
	tgt := claudeTarget(t)
	if r := removeHooksJSON(tgt); r.Action != NotInstalled {
		t.Errorf("missing file: action = %v, want NotInstalled", r.Action)
	}
	if err := os.WriteFile(tgt.Path, []byte(`{"model": "opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := removeHooksJSON(tgt); r.Action != NotInstalled {
		t.Errorf("no entry: action = %v, want NotInstalled", r.Action)
	}
}

func TestRemoveCleansUpEmptyHookContainers(t *testing.T) {
	tgt := claudeTarget(t)
	installHooksJSON(tgt, claudeGroups(cmd))
	removeHooksJSON(tgt)
	m := readJSON(t, tgt.Path)
	if _, ok := m["hooks"]; ok {
		t.Errorf("empty hooks object left behind: %v", m)
	}
}

func TestInvalidJSONFailsWithoutWriting(t *testing.T) {
	tgt := claudeTarget(t)
	if err := os.WriteFile(tgt.Path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := installHooksJSON(tgt, claudeGroups(cmd))
	if r.Action != Failed || r.Detail == "" {
		t.Fatalf("action = %v detail = %q, want Failed with detail", r.Action, r.Detail)
	}
	raw, _ := os.ReadFile(tgt.Path)
	if string(raw) != "{not json" {
		t.Errorf("failed install modified the file")
	}
	if r := removeHooksJSON(tgt); r.Action != Failed {
		t.Errorf("remove on invalid JSON: action = %v, want Failed", r.Action)
	}
}

func TestProjectInstallCreatesParentDir(t *testing.T) {
	tgt := Target{Host: "codex", Path: filepath.Join(t.TempDir(), ".codex", "hooks.json"), Supported: true}
	if r := installHooksJSON(tgt, codexGroups(cmd)); r.Action != Installed {
		t.Fatalf("action = %v, want Installed (detail %q)", r.Action, r.Detail)
	}
	ss := sessionStart(t, readJSON(t, tgt.Path))
	if len(ss) != 1 {
		t.Fatalf("codex groups = %d, want 1", len(ss))
	}
	if ss[0].(map[string]any)["matcher"] != "startup|clear" {
		t.Errorf("codex matcher = %v, want startup|clear", ss[0].(map[string]any)["matcher"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hostcfg/`
Expected: FAIL - the Task 1 stubs return `Failed "not implemented"`.

- [ ] **Step 3: Write the implementation**

Delete the four JSON stubs from `hostcfg.go`, create
`internal/hostcfg/hooksjson.go`:

```go
package hostcfg

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// hookTimeoutSeconds bounds the hook at the harness layer, an outer
// belt over the context command's own internal watchdog.
const hookTimeoutSeconds = 10

// claudeGroups is the Claude Code SessionStart config: matcher is
// required there and takes a single source, so startup and clear (the
// fresh-context events) are two groups.
func claudeGroups(hookCmd string) []any {
	return []any{hookGroup("startup", hookCmd), hookGroup("clear", hookCmd)}
}

// codexGroups is the Codex equivalent: matchers support exact
// alternation, so one group covers both events.
func codexGroups(hookCmd string) []any {
	return []any{hookGroup("startup|clear", hookCmd)}
}

func hookGroup(matcher, hookCmd string) map[string]any {
	return map[string]any{
		"matcher": matcher,
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": hookCmd,
			"timeout": hookTimeoutSeconds,
		}},
	}
}

// installHooksJSON merges groups into the SessionStart array of a
// Claude Code settings.json or Codex hooks.json, touching nothing but
// our own entries (recognized by their command containing
// "clickup-axi").
func installHooksJSON(t Target, groups []any) Report {
	root, err := readJSONFile(t.Path)
	if err != nil {
		return failf(t.Host, t.Path, "%s", err)
	}
	foreign, ours := splitSessionStart(root)
	if len(ours) > 0 && jsonEqual(ours, groups) {
		return Report{Host: t.Host, Path: t.Path, Action: AlreadyInstalled}
	}
	action := Installed
	if len(ours) > 0 {
		action = Repaired
	}
	setSessionStart(root, append(foreign, groups...))
	if err := writeJSONFile(t.Path, root); err != nil {
		return failf(t.Host, t.Path, "%s", err)
	}
	return Report{Host: t.Host, Path: t.Path, Action: action}
}

// removeHooksJSON deletes our SessionStart groups and prunes emptied
// containers, leaving every foreign key intact. A missing file means
// nothing is installed - idempotent no-op, unlike the install path
// which treats missing as an empty config to merge into.
func removeHooksJSON(t Target) Report {
	root, err := readJSONFileIfExists(t.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Report{Host: t.Host, Path: t.Path, Action: NotInstalled}
	}
	if err != nil {
		return failf(t.Host, t.Path, "%s", err)
	}
	foreign, ours := splitSessionStart(root)
	if len(ours) == 0 {
		return Report{Host: t.Host, Path: t.Path, Action: NotInstalled}
	}
	setSessionStart(root, foreign)
	if err := writeJSONFile(t.Path, root); err != nil {
		return failf(t.Host, t.Path, "%s", err)
	}
	return Report{Host: t.Host, Path: t.Path, Action: Removed}
}

// readJSONFile parses path into an object; a missing or empty file is
// an empty object (install merges into nothing).
func readJSONFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, errors.New("could not read " + path)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, errors.New("existing file is not valid JSON; fix it and rerun setup")
	}
	return m, nil
}

// readJSONFileIfExists is readJSONFile for remove paths: a missing
// file propagates fs.ErrNotExist instead of becoming an empty object.
func readJSONFileIfExists(path string) (map[string]any, error) {
	if _, err := os.Lstat(path); err != nil {
		return nil, err
	}
	return readJSONFile(path)
}
```

```go
func writeJSONFile(path string, root map[string]any) error {
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return errors.New("could not encode " + path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.New("could not create " + filepath.Dir(path))
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return errors.New("could not write " + path)
	}
	return nil
}

// splitSessionStart partitions hooks.SessionStart into foreign groups
// and ours (any group with a hook command containing "clickup-axi").
func splitSessionStart(root map[string]any) (foreign, ours []any) {
	hooks, _ := root["hooks"].(map[string]any)
	ss, _ := hooks["SessionStart"].([]any)
	for _, g := range ss {
		if groupIsOurs(g) {
			ours = append(ours, g)
			continue
		}
		foreign = append(foreign, g)
	}
	return foreign, ours
}

func groupIsOurs(group any) bool {
	m, ok := group.(map[string]any)
	if !ok {
		return false
	}
	entries, _ := m["hooks"].([]any)
	for _, e := range entries {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := em["command"].(string); strings.Contains(cmd, "clickup-axi") {
			return true
		}
	}
	return false
}

// setSessionStart stores groups, pruning emptied containers so a
// remove leaves no dangling "hooks": {} behind.
func setSessionStart(root map[string]any, groups []any) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	if len(groups) == 0 {
		delete(hooks, "SessionStart")
	} else {
		hooks["SessionStart"] = groups
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
}

// jsonEqual compares two values by canonical JSON encoding; numbers
// parsed from disk are float64 while ours are int, so a byte compare
// of re-encoded forms is the reliable equality.
func jsonEqual(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}
```

Note on `jsonEqual`: `json.Marshal` of `map[string]any` emits keys
sorted, and re-encoding normalizes `10` vs `float64(10)` to the same
`10` literal, so install-then-compare round-trips are stable.

- [ ] **Step 4: Run tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./internal/hostcfg/`
Expected: PASS (all Task 1 + Task 2 tests), gofmt silent.

- [ ] **Step 5: Commit**

```bash
git add internal/hostcfg/
git commit -m "feat(hostcfg): merge session hook into claude and codex JSON configs" -m "Shared SessionStart merge: our groups are recognized by their command
containing clickup-axi, foreign settings and hook groups are never
touched, reinstall with an unchanged command is a byte-exact no-op,
and a changed binary path repairs in place. Claude Code requires a
single-source matcher so startup and clear are two groups; Codex
supports alternation so one group carries startup|clear."
```

---

### Task 3: hostcfg OpenCode managed plugin

**Files:**
- Create: `internal/hostcfg/opencode.go`
- Test: `internal/hostcfg/opencode_test.go`
- Modify: `internal/hostcfg/hostcfg.go` (delete the two opencode stubs)

**Interfaces:**
- Consumes: `Target`, `Report`, `Action`, `failf` from Task 1.
- Produces: `installOpenCode(t Target, hookCmd string) Report`, `removeOpenCode(t Target) Report` - the names the Task 1 dispatcher calls.

- [ ] **Step 1: Write the failing tests**

`internal/hostcfg/opencode_test.go`:

```go
package hostcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openCodeTarget(t *testing.T) Target {
	t.Helper()
	return Target{
		Host:      "opencode",
		Path:      filepath.Join(t.TempDir(), "plugins", "clickup-axi.js"),
		Supported: true,
	}
}

func TestInstallOpenCodeWritesManagedPlugin(t *testing.T) {
	tgt := openCodeTarget(t)
	r := installOpenCode(tgt, "clickup-axi context")
	if r.Action != Installed {
		t.Fatalf("action = %v, want Installed (detail %q)", r.Action, r.Detail)
	}
	raw, err := os.ReadFile(tgt.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"generated by `clickup-axi setup`",
		"chat.message",
		"$`clickup-axi context`",
		"seen.has(input.sessionID)",
		"synthetic: true",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("plugin missing %q\n%s", want, raw)
		}
	}
}

func TestReinstallOpenCodeSameCommandIsNoOp(t *testing.T) {
	tgt := openCodeTarget(t)
	installOpenCode(tgt, "clickup-axi context")
	if r := installOpenCode(tgt, "clickup-axi context"); r.Action != AlreadyInstalled {
		t.Errorf("action = %v, want AlreadyInstalled", r.Action)
	}
}

func TestReinstallOpenCodeNewCommandRepairs(t *testing.T) {
	tgt := openCodeTarget(t)
	installOpenCode(tgt, `"/old/clickup-axi" context`)
	if r := installOpenCode(tgt, "clickup-axi context"); r.Action != Repaired {
		t.Errorf("action = %v, want Repaired", r.Action)
	}
	raw, _ := os.ReadFile(tgt.Path)
	if strings.Contains(string(raw), "/old/") {
		t.Errorf("repair left the old path behind:\n%s", raw)
	}
}

func TestOpenCodeNeverTouchesUnmanagedFile(t *testing.T) {
	tgt := openCodeTarget(t)
	if err := os.MkdirAll(filepath.Dir(tgt.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	user := "export const MyPlugin = async () => ({})\n"
	if err := os.WriteFile(tgt.Path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := installOpenCode(tgt, "clickup-axi context"); r.Action != Failed {
		t.Errorf("install over user file: action = %v, want Failed", r.Action)
	}
	if r := removeOpenCode(tgt); r.Action != Failed {
		t.Errorf("remove of user file: action = %v, want Failed", r.Action)
	}
	raw, _ := os.ReadFile(tgt.Path)
	if string(raw) != user {
		t.Errorf("user file was modified")
	}
}

func TestRemoveOpenCode(t *testing.T) {
	tgt := openCodeTarget(t)
	if r := removeOpenCode(tgt); r.Action != NotInstalled {
		t.Errorf("missing file: action = %v, want NotInstalled", r.Action)
	}
	installOpenCode(tgt, "clickup-axi context")
	if r := removeOpenCode(tgt); r.Action != Removed {
		t.Errorf("action = %v, want Removed", r.Action)
	}
	if _, err := os.Lstat(tgt.Path); !os.IsNotExist(err) {
		t.Errorf("plugin file still exists")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hostcfg/`
Expected: FAIL - opencode stubs return `Failed "not implemented"` (and
the no-op/remove tests fail on wrong actions).

- [ ] **Step 3: Write the implementation**

Delete the two opencode stubs from `hostcfg.go`, create
`internal/hostcfg/opencode.go`:

```go
package hostcfg

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// openCodeMarker identifies a plugin file as ours; a file without it
// is the user's and is never overwritten or removed.
const openCodeMarker = "generated by `clickup-axi setup`"

// pluginJS renders the managed OpenCode plugin. OpenCode has no
// session-start context hook (anomalyco/opencode#15224 is unmerged),
// so the supported shape is a chat.message hook that injects the CLI's
// stdout as a synthetic text part on the first message of a session -
// nothing reaches the model before that, so the effect is the same.
func pluginJS(hookCmd string) string {
	return `// clickup-axi session context plugin
// ` + openCodeMarker + ` - do not edit; rerun ` + "`clickup-axi setup`" + ` to update,
// ` + "`clickup-axi setup --remove`" + ` to uninstall.
export const ClickupContextPlugin = async ({ $ }) => {
  const seen = new Set()
  return {
    "chat.message": async (input, output) => {
      if (seen.has(input.sessionID)) return
      seen.add(input.sessionID)
      let context
      try {
        context = (await $` + "`" + hookCmd + "`" + `.quiet().text()).trim()
      } catch {
        return // CLI missing or failing must never break the session
      }
      if (!context) return
      output.parts.push({
        messageID: output.message.id,
        sessionID: input.sessionID,
        type: "text",
        synthetic: true,
        text: "<clickup-context>\n" + context + "\n</clickup-context>",
      })
    },
  }
}
`
}

func installOpenCode(t Target, hookCmd string) Report {
	want := pluginJS(hookCmd)
	got, err := os.ReadFile(t.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
			return failf(t.Host, t.Path, "could not create %s", filepath.Dir(t.Path))
		}
		if err := os.WriteFile(t.Path, []byte(want), 0o644); err != nil {
			return failf(t.Host, t.Path, "could not write %s", t.Path)
		}
		return Report{Host: t.Host, Path: t.Path, Action: Installed}
	case err != nil:
		return failf(t.Host, t.Path, "could not read %s", t.Path)
	case !strings.Contains(string(got), openCodeMarker):
		return failf(t.Host, t.Path, "existing file is not managed by clickup-axi; move it aside and rerun setup")
	case string(got) == want:
		return Report{Host: t.Host, Path: t.Path, Action: AlreadyInstalled}
	default:
		if err := os.WriteFile(t.Path, []byte(want), 0o644); err != nil {
			return failf(t.Host, t.Path, "could not write %s", t.Path)
		}
		return Report{Host: t.Host, Path: t.Path, Action: Repaired}
	}
}

func removeOpenCode(t Target) Report {
	got, err := os.ReadFile(t.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Report{Host: t.Host, Path: t.Path, Action: NotInstalled}
	case err != nil:
		return failf(t.Host, t.Path, "could not read %s", t.Path)
	case !strings.Contains(string(got), openCodeMarker):
		return failf(t.Host, t.Path, "existing file is not managed by clickup-axi; not removing it")
	}
	if err := os.Remove(t.Path); err != nil {
		return failf(t.Host, t.Path, "could not remove %s", t.Path)
	}
	return Report{Host: t.Host, Path: t.Path, Action: Removed}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./internal/hostcfg/`
Expected: PASS, gofmt silent. All stubs are now gone from hostcfg.go.

- [ ] **Step 5: Commit**

```bash
git add internal/hostcfg/
git commit -m "feat(hostcfg): manage the opencode context plugin file" -m "OpenCode has no session-start hook (upstream PR unmerged), so setup
owns a whole plugin file that injects the CLI's stdout on the first
message of each session. A marker header gates every write and delete:
a user's own file at that path is never touched."
```

---

### Task 4: cli `context` command

**Files:**
- Create: `internal/cli/context.go`
- Test: `internal/cli/context_test.go`
- Modify: `internal/cli/cli.go` (dispatch case + unknown-command list)

**Interfaces:**
- Consumes: `clickup.Client` methods `SelectTeam`, `GetUser`, `GetTeamTasksPage`, `clickup.TaskQuery`, `clickup.ErrNoAuth`, `clickup.WorkspaceEnv`, `clickup.TeamTasksPageSize`; `displayID` (task.go), `output.ToonCell`, `output.WriteHelp` - all existing.
- Produces: `cmdContext(args []string, c *clickup.Client, out io.Writer) int` wired as dispatch case `"context"`. Task 5 installs a hook that runs it; Task 6 documents it.

- [ ] **Step 1: Write the failing tests**

`internal/cli/context_test.go`:

```go
package cli

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
)

const contextHeader = "clickup-axi: ClickUp CLI (tasks, search, edit, comment)"

func TestContextShowsCappedDueSortedDashboard(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("assignees[]"); got != "42" {
			t.Errorf("assignees[] = %q, want 42", got)
		}
		w.Write([]byte(`{"tasks": [
			{"id": "86ey1", "name": "No due A", "status": {"status": "open"}, "due_date": null},
			{"id": "86ey2", "name": "Due later", "status": {"status": "open"}, "due_date": "1783339200000"},
			{"id": "86ey3", "name": "Due soon", "status": {"status": "in progress"}, "due_date": "1752192000000"},
			{"id": "86ey4", "name": "No due B", "status": {"status": "open"}, "due_date": null},
			{"id": "86ey5", "name": "Due mid", "status": {"status": "open"}, "due_date": "1760000000000"},
			{"id": "86ey6", "name": "No due C", "status": {"status": "open"}, "due_date": null}
		]}`))
	})

	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, contextHeader) {
		t.Errorf("missing discovery header\noutput:\n%s", out)
	}
	if !strings.Contains(out, "tasks[5/6]{id,title,status,due}:") {
		t.Errorf("missing capped header\noutput:\n%s", out)
	}
	// Due-soonest first, no-due tail in stable order; the 6th task is cut.
	idx := func(s string) int { return strings.Index(out, s) }
	if !(idx("86ey3") < idx("86ey5") && idx("86ey5") < idx("86ey2") && idx("86ey2") < idx("86ey1") && idx("86ey1") < idx("86ey4")) {
		t.Errorf("rows not due-sorted\noutput:\n%s", out)
	}
	if strings.Contains(out, "86ey6") {
		t.Errorf("row past the cap leaked\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Run `clickup-axi tasks` for all 6 open tasks") {
		t.Errorf("missing total-hint help line\noutput:\n%s", out)
	}
}

func TestContextUncappedHeaderAndHelp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": [
			{"id": "86ey1", "name": "Only one", "status": {"status": "open"}, "due_date": null}
		]}`))
	})
	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks[1]{id,title,status,due}:") {
		t.Errorf("uncapped header wrong\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Run `clickup-axi tasks` for your open tasks") {
		t.Errorf("missing plain help line\noutput:\n%s", out)
	}
}

func TestContextZeroTasksIsExplicit(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": []}`))
	})
	out, code := runCLI(t, c, "context")
	if code != 0 || !strings.Contains(out, "tasks: 0 open tasks assigned to you") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestContextWithoutTokenDegradesToLoginHint(t *testing.T) {
	// Isolate host env like newFakeClickUp does; this test builds its
	// own client because the point is the empty token.
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "")
	t.Setenv(clickup.WorkspaceEnv, "")
	c := clickup.New("http://127.0.0.1:0", "", &http.Client{Timeout: time.Second})
	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (session start must not fail)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, contextHeader) ||
		!strings.Contains(out, "tasks: unavailable (not authenticated)") ||
		!strings.Contains(out, "clickup-axi auth login") {
		t.Errorf("degraded output wrong:\n%s", out)
	}
}

func TestContextAPIErrorDegradesQuietly(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.mux.HandleFunc("GET /api/v2/team", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"err": "boom"}`))
	})
	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks: unavailable right now") || strings.Contains(out, "boom") {
		t.Errorf("raw API error leaked or degraded line missing:\n%s", out)
	}
}

func TestContextUnpinnedMultiWorkspaceShowsPinHint(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)
	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks: unavailable (") || !strings.Contains(out, clickup.WorkspaceEnv) {
		t.Errorf("pin hint missing:\n%s", out)
	}
}

func TestContextBudgetBreachDegrades(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{"tasks": []}`))
	})
	old := contextBudget
	contextBudget = 50 * time.Millisecond
	t.Cleanup(func() { contextBudget = old })

	out, code := runCLI(t, c, "context")
	if code != 0 || !strings.Contains(out, "tasks: unavailable right now") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestContextForcedCustomIDsShowsCustomID(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "1")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": [
			{"id": "86ey1", "custom_id": "HGAI-77", "name": "One", "status": {"status": "open"}, "due_date": null}
		]}`))
	})
	out, _ := runCLI(t, c, "context")
	if !strings.Contains(out, "HGAI-77,One,open,") {
		t.Errorf("custom id not shown:\n%s", out)
	}
}

func TestContextRejectsUnknownFlags(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "context", "--full")
	if code != 2 || !strings.Contains(out, "unknown argument") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestContext`
Expected: FAIL - `context` is an unknown command (exit 2).

- [ ] **Step 3: Write the implementation**

`internal/cli/context.go`:

```go
package cli

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

// contextTaskCap bounds the dashboard: it loads on every session, so
// the output stays a screenful (AXI section 7 - ruthlessly minimize).
const contextTaskCap = 5

// contextBudget caps the whole ClickUp fetch. A session start must
// never hang on a slow network; past the budget the dashboard degrades
// instead. Variable so tests can shrink it.
var contextBudget = 3 * time.Second

const contextHelpText = `clickup-axi context

The session-start hook payload: prints a compact dashboard (your ` +
	strconv.Itoa(contextTaskCap) + ` most
urgent open tasks) for the agent harness to inject as ambient context.
Installed as a SessionStart hook by ` + "`clickup-axi setup`" + `; not meant
to be run by hand. Always exits 0: a broken dashboard must not break
a session start.

examples:
  clickup-axi setup --global    install the hook that runs this`

func cmdContext(args []string, c *clickup.Client, out io.Writer) int {
	for _, a := range args {
		switch a {
		case "--help", "-h":
			fmt.Fprintln(out, contextHelpText)
			return 0
		default:
			output.WriteError(out, fmt.Sprintf("unknown argument %q for context\n  valid: none (--help only)", a),
				"Run `clickup-axi context` with no flags")
			return 2
		}
	}

	fmt.Fprintln(out, "clickup-axi: ClickUp CLI (tasks, search, edit, comment)")

	type fetched struct {
		tasks    []clickup.Task
		lastPage bool
		err      *clickup.APIError
	}
	ch := make(chan fetched, 1)
	go func() {
		team, err := c.SelectTeam()
		if err != nil {
			ch <- fetched{err: err}
			return
		}
		u, err := c.GetUser()
		if err != nil {
			ch <- fetched{err: err}
			return
		}
		tasks, last, err := c.GetTeamTasksPage(team.ID, clickup.TaskQuery{Assignees: []int64{u.ID}})
		ch <- fetched{tasks: tasks, lastPage: last, err: err}
	}()

	var f fetched
	select {
	case f = <-ch:
	case <-time.After(contextBudget):
		return contextUnavailable(out, "right now",
			"Run `clickup-axi tasks` to retry your open tasks")
	}
	if f.err != nil {
		switch {
		case f.err.Message == clickup.ErrNoAuth:
			return contextUnavailable(out, "(not authenticated)",
				"Ask the user to run `clickup-axi auth login` in their terminal")
		case strings.Contains(f.err.Message, clickup.WorkspaceEnv):
			return contextUnavailable(out, "("+f.err.Message+")",
				"Run `clickup-axi tasks` after setting the workspace")
		default:
			return contextUnavailable(out, "right now",
				"Run `clickup-axi tasks` to retry your open tasks")
		}
	}

	if len(f.tasks) == 0 {
		fmt.Fprintln(out, "tasks: 0 open tasks assigned to you")
		output.WriteHelp(out,
			"Run `clickup-axi tasks <id>` for details and comments",
			"Run `clickup-axi --help` for all commands")
		return 0
	}

	sort.SliceStable(f.tasks, func(i, j int) bool {
		return dueSortKey(&f.tasks[i]) < dueSortKey(&f.tasks[j])
	})
	shown := f.tasks
	total := strconv.Itoa(len(f.tasks))
	if !f.lastPage {
		total += "+"
	}
	firstHelp := "Run `clickup-axi tasks` for your open tasks"
	if len(shown) > contextTaskCap {
		shown = shown[:contextTaskCap]
		fmt.Fprintf(out, "tasks[%d/%s]{id,title,status,due}:\n", len(shown), total)
		firstHelp = fmt.Sprintf("Run `clickup-axi tasks` for all %s open tasks", total)
	} else {
		fmt.Fprintf(out, "tasks[%d]{id,title,status,due}:\n", len(shown))
	}
	for i := range shown {
		t := &shown[i]
		fmt.Fprintf(out, "  %s,%s,%s,%s\n", displayID(t), output.ToonCell(t.Name), output.ToonCell(t.Status.Status), t.DueDate.Date())
	}
	output.WriteHelp(out,
		firstHelp,
		"Run `clickup-axi tasks <id>` for details and comments",
		"Run `clickup-axi --help` for all commands")
	return 0
}

// contextUnavailable is every degraded path: the discovery line has
// already printed, the task block is replaced by one line, and the
// exit code stays 0 so the harness still injects the output.
func contextUnavailable(out io.Writer, reason, hint string) int {
	fmt.Fprintf(out, "tasks: unavailable %s\n", reason)
	output.WriteHelp(out, hint, "Run `clickup-axi --help` for all commands")
	return 0
}

// dueSortKey orders tasks due-soonest first; no due date sorts last.
func dueSortKey(t *clickup.Task) int64 {
	n, err := strconv.ParseInt(string(t.DueDate), 10, 64)
	if err != nil {
		return math.MaxInt64
	}
	return n
}
```

In `internal/cli/cli.go` `dispatch`, add before the `default` case:

```go
	case "context":
		return cmdContext(args[1:], c, out)
```

and extend the unknown-command error's valid list:

```go
		output.WriteError(out, fmt.Sprintf("unknown command %q\n  valid: tasks, search, auth, setup, context, update, skill", args[0]),
			"Run `clickup-axi --help`")
```

(`setup` appears in the valid list one commit before its dispatch case
lands in Task 5 - an acceptable mid-branch state on a feature branch.)

Note the fixture `1752192000000` is 2025-07-11 and `1783339200000` is
2026-07-06 - values only matter relative to each other for the sort.

- [ ] **Step 4: Run tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./internal/cli/`
Expected: `TestContext*` PASS. `TestCommittedSkillIsFresh` also still
passes because the surface table is untouched so far (that changes in
Task 6).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/context.go internal/cli/context_test.go internal/cli/cli.go
git commit -m "feat(cli): add context command, the session-start hook payload" -m "A capped due-sorted dashboard (5 tasks, total stated) behind a hard
3s budget. Every degraded path - no token, API failure, unpinned
multi-workspace, budget breach - keeps the discovery line, states why
tasks are unavailable, and exits 0: a session start must never break
or leak raw API errors into every conversation."
```

---

### Task 5: cli `setup` command

**Files:**
- Create: `internal/cli/setup.go`
- Test: `internal/cli/setup_test.go`
- Modify: `internal/cli/cli.go` (dispatch case `"setup"`)

**Interfaces:**
- Consumes: `hostcfg.Targets`, `hostcfg.Install`, `hostcfg.Remove`, `hostcfg.HookCommand`, `hostcfg.Scope`/`Global`/`Project`, `hostcfg.Report`/`Action` constants (Tasks 1-3); `execPath()` (cli.go), `fdReader` (auth.go), `output.CollapseHome`, `output.WriteHelp`, `output.WriteError`.
- Produces: `cmdSetup(args []string, stdin io.Reader, out io.Writer) int` wired as dispatch case `"setup"`.

- [ ] **Step 1: Write the failing tests**

`internal/cli/setup_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestSetup|TestParseScope'`
Expected: FAIL - `setup` dispatches to the unknown-command error.

- [ ] **Step 3: Write the implementation**

`internal/cli/setup.go`:

```go
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/JanSuthacheeva/clickup-axi/internal/hostcfg"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const setupHelp = `clickup-axi setup [--global | --project] [--app <host>] [--remove]

Installs a session-start hook so agent sessions begin with a compact
ClickUp dashboard (the ` + "`clickup-axi context`" + ` output) already in
context. Targets every detected host; rerunning repairs a moved
binary path and is otherwise a no-op.

hosts:
  claude-code   ~/.claude/settings.json     (project: .claude/settings.json)
  codex         ~/.codex/hooks.json         (project: .codex/hooks.json)
  opencode      ~/.config/opencode/plugins/clickup-axi.js (global only)

flags:
  --global        install for this user (recommended)
  --project       install into the current directory's project configs
  --app <host>    only this host: claude-code | codex | opencode
  --remove        uninstall (same scope and app selection)

In a terminal, the scope is prompted when omitted; on agent paths it
must be explicit.

examples:
  clickup-axi setup --global
  clickup-axi setup --global --remove`

var setupHosts = map[string]bool{"claude-code": true, "codex": true, "opencode": true}

func cmdSetup(args []string, stdin io.Reader, out io.Writer) int {
	var globalFlag, projectFlag, remove bool
	var app string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			fmt.Fprintln(out, setupHelp)
			return 0
		case "--global":
			globalFlag = true
		case "--project":
			projectFlag = true
		case "--remove":
			remove = true
		case "--app":
			i++
			if i >= len(args) {
				output.WriteError(out, "--app needs a value\n  valid: claude-code, codex, opencode",
					"Run `clickup-axi setup --global --app claude-code`")
				return 2
			}
			app = args[i]
			if !setupHosts[app] {
				output.WriteError(out, fmt.Sprintf("unknown app %q\n  valid: claude-code, codex, opencode", app),
					"Run `clickup-axi setup --help`")
				return 2
			}
		default:
			output.WriteError(out, fmt.Sprintf("unknown argument %q for setup\n  valid: --global, --project, --app, --remove", args[i]),
				"Run `clickup-axi setup --help`")
			return 2
		}
	}
	if globalFlag && projectFlag {
		output.WriteError(out, "--global and --project cannot be combined",
			"Run `clickup-axi setup --global` or `clickup-axi setup --project`")
		return 2
	}

	var scope hostcfg.Scope
	switch {
	case globalFlag:
		scope = hostcfg.Global
	case projectFlag:
		scope = hostcfg.Project
	default:
		s, code := promptScope(stdin, out)
		if code != 0 {
			return code
		}
		scope = s
	}

	home, err := os.UserHomeDir()
	if err != nil {
		output.WriteError(out, "could not locate the user home directory")
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		output.WriteError(out, "could not determine the current directory")
		return 1
	}

	hookCmd := hostcfg.HookCommand(osExecutable())
	verb := "session hook"
	fmt.Fprintf(out, "setup: %s (%s)\n", verb, scopeName(scope))
	exit := 0
	for _, t := range hostcfg.Targets(scope, home, cwd) {
		if app != "" && t.Host != app {
			continue
		}
		var r hostcfg.Report
		if remove {
			r = hostcfg.Remove(t)
		} else {
			r = hostcfg.Install(t, hookCmd)
		}
		fmt.Fprintf(out, "  %s\n", renderReport(r))
		if r.Action == hostcfg.Failed {
			exit = 1
		}
	}

	scopeFlag := "--" + scopeName(scope)
	if remove {
		output.WriteHelp(out, "Run `clickup-axi setup "+scopeFlag+"` to reinstall")
	} else {
		output.WriteHelp(out,
			"Start a new agent session to load the ClickUp dashboard",
			"Run `clickup-axi setup "+scopeFlag+" --remove` to uninstall")
	}
	return exit
}

// promptScope asks for the scope on a real terminal (the auth login
// TTY exception); agent paths get a usage error that inlines both
// flags so the retry is one step.
func promptScope(stdin io.Reader, out io.Writer) (hostcfg.Scope, int) {
	f, ok := stdin.(fdReader)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		output.WriteError(out, "setup needs an explicit scope when not run in a terminal\n  valid: --global (recommended), --project",
			"Run `clickup-axi setup --global`")
		return 0, 2
	}
	fmt.Fprintln(out, "setup: install the session-start hook")
	fmt.Fprintln(out, "scope? [1] global (recommended)  [2] project (this directory)")
	fmt.Fprint(out, "choice [1]: ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	scope, ok := parseScopeChoice(strings.TrimSpace(line))
	if !ok {
		output.WriteError(out, fmt.Sprintf("unrecognized choice %q\n  valid: 1/global, 2/project", strings.TrimSpace(line)),
			"Run `clickup-axi setup --global`")
		return 0, 2
	}
	return scope, 0
}

func parseScopeChoice(s string) (hostcfg.Scope, bool) {
	switch strings.ToLower(s) {
	case "", "1", "g", "global":
		return hostcfg.Global, true
	case "2", "p", "project":
		return hostcfg.Project, true
	}
	return 0, false
}

func scopeName(s hostcfg.Scope) string {
	if s == hostcfg.Project {
		return "project"
	}
	return "global"
}

func renderReport(r hostcfg.Report) string {
	switch r.Action {
	case hostcfg.Installed:
		return fmt.Sprintf("%s: installed (%s)", r.Host, output.CollapseHome(r.Path))
	case hostcfg.Repaired:
		return fmt.Sprintf("%s: repaired (%s)", r.Host, output.CollapseHome(r.Path))
	case hostcfg.AlreadyInstalled:
		return fmt.Sprintf("%s: already installed (no-op)", r.Host)
	case hostcfg.Removed:
		return fmt.Sprintf("%s: removed (%s)", r.Host, output.CollapseHome(r.Path))
	case hostcfg.NotInstalled:
		return fmt.Sprintf("%s: not installed (no-op)", r.Host)
	case hostcfg.Skipped:
		return fmt.Sprintf("%s: skipped (%s)", r.Host, r.Detail)
	default:
		return fmt.Sprintf("%s: error (%s)", r.Host, r.Detail)
	}
}

// osExecutable mirrors execPath (cli.go) but returns "" on failure so
// HookCommand can fall back to the bare name.
func osExecutable() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}
```

In `internal/cli/cli.go` `dispatch`, add:

```go
	case "setup":
		return cmdSetup(args[1:], stdin, out)
```

Note: in tests, `os.Executable()` is the test binary and `PATH` has no
`clickup-axi`, so `HookCommand` embeds the test binary's absolute
path - which is why `TestSetupGlobalInstallsDetectedHosts` asserts
`" context"` rather than a full command literal.

- [ ] **Step 4: Run tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./internal/cli/ ./internal/hostcfg/`
Expected: PASS, gofmt silent.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/setup.go internal/cli/setup_test.go internal/cli/cli.go
git commit -m "feat(cli): add setup command installing the session hook" -m "Detect-and-report across claude-code, codex, and opencode: global
scope gates on the host dir existing, project scope installs
unconditionally (opencode is global-only). Scope is prompted on a real
TTY - the auth login exception - and must be --global/--project on
agent paths, where the usage error inlines both flags for a one-step
retry. One failing host exits 1 without stopping the others."
```

---

### Task 6: surface, skill, help, README, changelog

**Files:**
- Modify: `internal/cli/surface.go` (two new rows)
- Modify: `internal/cli/skill_template.md` (mention setup)
- Modify: `README.md` (session hook section)
- Modify: `docs/v1.0.0.md` (tick the checklist row)
- Modify: `CHANGELOG.md` (unreleased entry, matching the file's existing format)
- Regenerate: `skills/clickup-axi/SKILL.md` via `go run ./cmd/clickup-axi skill --write`

**Interfaces:**
- Consumes: the `setup` and `context` commands from Tasks 4-5.
- Produces: documentation only; `TestCommittedSkillIsFresh` green.

- [ ] **Step 1: Add the surface rows**

In `internal/cli/surface.go`, insert between the `auth logout` and
`update` entries (keeping the listing order: user commands before
maintenance):

```go
	{
		usage:   "setup",
		summary: "Install the session-start hook (Claude Code, Codex, OpenCode)",
		note:    "(--global or --project; --remove uninstalls)",
		skill:   "clickup-axi setup --global",
		comment: "install the session-start dashboard hook (only after user consent)",
	},
	{
		usage:   "context",
		summary: "Session-start dashboard printed by the installed hook",
	},
```

`context` has no `skill:` line on purpose (decision Q4): agents never
invoke it themselves - the harness does.

- [ ] **Step 2: Regenerate the skill and update the template**

In `internal/cli/skill_template.md`, add one sentence to the prose
(near the auth/update guidance; read the file and match its tone):

```markdown
`clickup-axi setup --global` installs a session-start hook (Claude
Code, Codex, OpenCode) so new sessions begin with the user's open
tasks in context; only run it when the user asks for it.
```

Then:

Run: `go run ./cmd/clickup-axi skill --write`
Expected: `skill: wrote skills/clickup-axi/SKILL.md`

- [ ] **Step 3: README, v1.0.0 checklist, changelog**

- `README.md`: add a `## Session hook` section after the install
  section: what `setup --global` does (one paragraph), the three host
  file paths, the remove command, and that `context` is the payload
  (never run by hand, capped at 5 tasks, exits 0 on failure so a
  broken network cannot break a session).
- `docs/v1.0.0.md`: change `- [ ] `setup` session hook (Claude Code, Codex, OpenCode)`
  to `- [x] ...` in the build checklist.
- `CHANGELOG.md`: add the feature under the unreleased/next section in
  the file's existing style (read it first; create the section if the
  file starts at 0.5.0).

- [ ] **Step 4: Verify everything**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: PASS - including `TestCommittedSkillIsFresh`, which fails if
step 2 was skipped.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/surface.go internal/cli/skill_template.md skills/clickup-axi/SKILL.md README.md docs/v1.0.0.md CHANGELOG.md
git commit -m "docs: document setup and context across surface, skill, README" -m "setup enters the generated skill with a consent note like update;
context is listed in --help only - agents never invoke the hook
payload themselves, the harness does."
```

---

### Task 7: E2E verification

**Files:** none (verification only; fix-forward commits if issues surface)

**Interfaces:**
- Consumes: the built binary with everything from Tasks 1-6.

- [ ] **Step 1: Build and run the full gate**

```bash
go build -o clickup-axi ./cmd/clickup-axi
gofmt -l . && go vet ./... && go test ./...
./clickup-axi skill --check
```
Expected: build succeeds, all green, skill up to date.

- [ ] **Step 2: context against the real API (read-only)**

```bash
./clickup-axi context
```
Expected: discovery line, up to 5 real open tasks due-sorted, `help[3]`,
exit 0. Then degrade check without credentials:

```bash
env -u CLICKUP_TOKEN HOME=$(mktemp -d) ./clickup-axi context; echo "exit: $?"
```
Expected: `tasks: unavailable (not authenticated)`, login hint, `exit: 0`.

- [ ] **Step 3: setup cycle in a scratch HOME (never the real one)**

```bash
SCRATCH=$(mktemp -d)
mkdir -p "$SCRATCH/.claude" "$SCRATCH/.codex" "$SCRATCH/.config/opencode"
HOME="$SCRATCH" ./clickup-axi setup --global
HOME="$SCRATCH" ./clickup-axi setup --global          # expect no-op lines
cat "$SCRATCH/.claude/settings.json"                   # expect two matcher groups
cat "$SCRATCH/.codex/hooks.json"                       # expect startup|clear group
head -3 "$SCRATCH/.config/opencode/plugins/clickup-axi.js"
HOME="$SCRATCH" ./clickup-axi setup --global --remove  # expect removed lines
cat "$SCRATCH/.claude/settings.json"                   # expect no hooks key
```
Expected: install / no-op / remove all exit 0, JSON stays valid at
every step (`jq . file` if in doubt), non-TTY stdin means the scope
prompt never fires (flags are explicit here anyway).

- [ ] **Step 4: real install on this machine (the actual smoke test)**

```bash
./clickup-axi setup --global
cat ~/.claude/settings.json
```
Expected: `claude-code: installed`, existing settings preserved
byte-for-content (key order may change once - known trade-off).
Start a new Claude Code session and confirm the dashboard appears as
session context. Leave the hook installed - this machine is the
feature's first user. If anything is wrong: `./clickup-axi setup
--global --remove` restores the previous behavior.

- [ ] **Step 5: report**

No commit. Report E2E results (including the real session-start
observation) back for review before the branch is merged.

---

## Deviations from the approved design (evidence-driven, flag in review)

1. Q3 (Codex config.toml check + hint) is dropped entirely: research
   showed hooks are stable and enabled by default since ~v0.144; the
   flag exists only to disable. No TOML interaction at all.
2. Claude Code needs two matcher groups (`startup`, `clear`) because
   its SessionStart matcher takes a single source - the spec's diff
   sketch showed one unmatched group, which the docs say is invalid.
3. OpenCode injection rides `chat.message` (first message per session),
   not a session-created event - no such context hook exists upstream.
   Same observable effect: context present before the model's first turn.
