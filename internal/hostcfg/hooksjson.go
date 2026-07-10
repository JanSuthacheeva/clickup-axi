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

// jsonEqual compares two values by canonical JSON encoding: Marshal of
// map[string]any emits keys sorted, and re-encoding normalizes int vs
// float64 from disk to the same literal, so install-then-compare
// round-trips are stable.
func jsonEqual(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}
