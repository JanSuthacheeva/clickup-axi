// Package config is the driven adapter for clickup-axi's layered
// configuration: a committable project file discovered by walking up
// from the working directory, a personal file next to the stored
// token, and per-key environment overrides. Precedence is
// env > project > personal; the explicit CLI flag beats all three and
// is the caller's business. The package knows the file format and the
// layering, nothing about ClickUp or the CLI, and never imports them.
//
// The format is a flat TOML subset: `key = "value"` lines, `#`
// comments, bare integers tolerated. Full-TOML features (tables,
// arrays, multiline strings) are rejected with an error naming file
// and line - a typo must never silently drop a configured default.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProjectFileName is the committable per-project config, discovered by
// walking up from the working directory (nearest wins).
const ProjectFileName = ".clickup-axi.toml"

// envPrefix maps a config key to its environment override:
// default_list -> CLICKUP_AXI_DEFAULT_LIST.
const envPrefix = "CLICKUP_AXI_"

// KnownKeys is the file schema. A key outside it parses fine but is
// ignored with a warning, so an older binary tolerates a newer config.
var KnownKeys = []string{"default_list"}

// Value is one resolved config value with the provenance an agent
// needs to know which file (or variable) to fix.
type Value struct {
	Val   string
	Scope string // "env", "project" or "personal"
	Path  string // the file for project/personal, the variable name for env
}

type layer struct {
	scope string
	path  string
	keys  map[string]string
}

// Config is the merged view over the discovered layers. Warnings carry
// ignored-unknown-key diagnostics for the caller's stderr.
type Config struct {
	layers   []layer
	Warnings []string
}

// Load reads the layered config: the nearest project file walking up
// from cwd, then the personal file. Missing files are fine; malformed
// ones are errors.
func Load(cwd string) (*Config, error) {
	c := &Config{}
	if path, ok := FindProjectFile(cwd); ok {
		if err := c.addLayer("project", path); err != nil {
			return nil, err
		}
	}
	personal, err := PersonalPath()
	if err == nil {
		if _, statErr := os.Stat(personal); statErr == nil {
			if err := c.addLayer("personal", personal); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

func (c *Config) addLayer(scope, path string) error {
	keys, warnings, err := parseFile(path)
	if err != nil {
		return err
	}
	c.layers = append(c.layers, layer{scope: scope, path: path, keys: keys})
	c.Warnings = append(c.Warnings, warnings...)
	return nil
}

// Get resolves one key across the layers: environment first, then the
// project file, then the personal file.
func (c *Config) Get(key string) (Value, bool) {
	env := EnvKey(key)
	if v := os.Getenv(env); v != "" {
		return Value{Val: v, Scope: "env", Path: env}, true
	}
	for _, l := range c.layers {
		if v, ok := l.keys[key]; ok {
			return Value{Val: v, Scope: l.scope, Path: l.path}, true
		}
	}
	return Value{}, false
}

// EnvKey is the environment variable overriding a config key.
func EnvKey(key string) string {
	return envPrefix + strings.ToUpper(key)
}

// PersonalPath is the personal config file, in the same directory as
// the stored token (os.UserConfigDir, with the same XDG caveats as
// TokenFilePath - tests isolate by relocating HOME).
func PersonalPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clickup-axi", "config.toml"), nil
}

// FindProjectFile walks up from dir to the filesystem root and returns
// the nearest project config file.
func FindProjectFile(dir string) (string, bool) {
	for d := filepath.Clean(dir); ; {
		p := filepath.Join(d, ProjectFileName)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

// GitRoot walks up from dir to the repository root: the first
// directory containing a .git entry (a directory, or a file for
// worktrees and submodules). `config set --project` creates the
// project file there.
func GitRoot(dir string) (string, bool) {
	for d := filepath.Clean(dir); ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

// Set writes `key = "value"` into the file at path, replacing the
// key's line if present and appending otherwise. Every other line -
// comments included - is preserved verbatim, and the write is atomic
// (temp file in the same directory, then rename). A malformed existing
// file is an error, never overwritten.
func Set(path, key, value string) error {
	if _, _, err := parseFile(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var lines []string
	if len(data) > 0 {
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}
	assignment := fmt.Sprintf("%s = %q", key, value)
	replaced := false
	for i, line := range lines {
		if lineKey(line) == key {
			lines[i] = assignment
			replaced = true
		}
	}
	if !replaced {
		if len(lines) == 0 {
			lines = append(lines, "# clickup-axi config")
		}
		lines = append(lines, assignment)
	}
	return writeAtomic(path, strings.Join(lines, "\n")+"\n")
}

// Unset removes the key's line from the file at path, preserving the
// rest. It reports whether the key was present; a missing file or
// absent key is a clean false, so unset is idempotent.
func Unset(path, key string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	kept := lines[:0]
	found := false
	for _, line := range lines {
		if lineKey(line) == key {
			found = true
			continue
		}
		kept = append(kept, line)
	}
	if !found {
		return false, nil
	}
	return true, writeAtomic(path, strings.Join(kept, "\n")+"\n")
}

func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func parseFile(path string) (map[string]string, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	keys := make(map[string]string)
	var warnings []string
	seen := make(map[string]bool)
	for i, line := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, val, err := parseAssignment(text)
		if err != nil {
			return nil, nil, fmt.Errorf("%s:%d: %v", path, i+1, err)
		}
		if seen[key] {
			return nil, nil, fmt.Errorf("%s:%d: duplicate key %q", path, i+1, key)
		}
		seen[key] = true
		if !known(key) {
			warnings = append(warnings, fmt.Sprintf("%s:%d: ignoring unknown key %q", path, i+1, key))
			continue
		}
		keys[key] = val
	}
	return keys, warnings, nil
}

func parseAssignment(line string) (string, string, error) {
	if strings.HasPrefix(line, "[") {
		return "", "", errors.New("tables are not supported (flat keys only)")
	}
	rawKey, rawVal, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", errors.New("expected `key = \"value\"`")
	}
	key := strings.TrimSpace(rawKey)
	if !bareKey(key) {
		return "", "", fmt.Errorf("invalid key %q (letters, digits, _ and - only)", key)
	}
	val := strings.TrimSpace(rawVal)
	if strings.HasPrefix(val, `"`) {
		quoted, err := strconv.QuotedPrefix(val)
		if err != nil {
			return "", "", errors.New("unterminated string")
		}
		rest := strings.TrimSpace(val[len(quoted):])
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return "", "", fmt.Errorf("unexpected content after the value: %q", rest)
		}
		unquoted, err := strconv.Unquote(quoted)
		if err != nil {
			return "", "", errors.New("invalid string escape")
		}
		return key, unquoted, nil
	}
	// A bare TOML integer is tolerated (list ids are numeric); anything
	// else must be quoted.
	bare, _, _ := strings.Cut(val, "#")
	bare = strings.TrimSpace(bare)
	if !isDigits(bare) {
		return "", "", fmt.Errorf("the value must be a quoted string: %s = \"...\"", key)
	}
	return key, bare, nil
}

// lineKey extracts the key of an assignment line for rewrite matching;
// blank lines, comments, and anything unparseable yield "".
func lineKey(line string) string {
	text := strings.TrimSpace(line)
	if text == "" || strings.HasPrefix(text, "#") {
		return ""
	}
	rawKey, _, ok := strings.Cut(text, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(rawKey)
}

func known(key string) bool {
	for _, k := range KnownKeys {
		if k == key {
			return true
		}
	}
	return false
}

func bareKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
