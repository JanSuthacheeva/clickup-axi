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
