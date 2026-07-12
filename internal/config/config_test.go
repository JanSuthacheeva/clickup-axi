package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates path (and its directories) with content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isolate points HOME (and thus os.UserConfigDir on linux/darwin) at a
// temp dir and clears the env override, so host state never leaks in.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("CLICKUP_AXI_DEFAULT_LIST", "")
	return home
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	valid := `# clickup-axi config

default_list = "folder:90123456"  # current sprint folder
`
	writeFile(t, path, valid)
	keys, warnings, err := parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if got := keys["default_list"]; got != "folder:90123456" {
		t.Fatalf("default_list = %q", got)
	}
}

func TestParseFileBareInteger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "default_list = 901808169633\n")
	keys, _, err := parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if got := keys["default_list"]; got != "901808169633" {
		t.Fatalf("default_list = %q", got)
	}
}

func TestParseFileUnknownKeyWarns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "future_key = \"x\"\ndefault_list = \"901\"\n")
	keys, warnings, err := parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if got := keys["default_list"]; got != "901" {
		t.Fatalf("default_list = %q", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], `ignoring unknown key "future_key"`) {
		t.Fatalf("warnings = %v", warnings)
	}
	if !strings.Contains(warnings[0], path+":1") {
		t.Fatalf("warning lacks file:line: %v", warnings[0])
	}
}

func TestParseFileErrors(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"no equals", "default_list\n", "expected `key = \"value\"`"},
		{"table", "[section]\n", "tables are not supported"},
		{"bad key", "bad key = \"x\"\n", `invalid key "bad key"`},
		{"unquoted string", "default_list = folder:901\n", "must be a quoted string"},
		{"unterminated", "default_list = \"901\n", "unterminated string"},
		{"trailing junk", "default_list = \"901\" extra\n", "unexpected content after the value"},
		{"duplicate", "default_list = \"1\"\ndefault_list = \"2\"\n", `duplicate key "default_list"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			writeFile(t, path, tc.content)
			_, _, err := parseFile(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), path+":") {
				t.Fatalf("err lacks file:line: %v", err)
			}
		})
	}
}

func TestLoadPrecedence(t *testing.T) {
	home := isolate(t)
	writeFile(t, filepath.Join(home, ".config", "clickup-axi", "config.toml"),
		"default_list = \"personal\"\n")

	project := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(project, ProjectFileName), "default_list = \"project\"\n")
	nested := filepath.Join(project, "internal", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	// Project (found by walking up from a nested dir) beats personal.
	c, err := Load(nested)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := c.Get("default_list")
	if !ok || v.Val != "project" || v.Scope != "project" {
		t.Fatalf("Get = %+v, %v", v, ok)
	}
	if v.Path != filepath.Join(project, ProjectFileName) {
		t.Fatalf("Path = %q", v.Path)
	}

	// Env beats both files.
	t.Setenv("CLICKUP_AXI_DEFAULT_LIST", "from-env")
	v, ok = c.Get("default_list")
	if !ok || v.Val != "from-env" || v.Scope != "env" || v.Path != "CLICKUP_AXI_DEFAULT_LIST" {
		t.Fatalf("Get = %+v, %v", v, ok)
	}
}

func TestLoadPersonalFallback(t *testing.T) {
	home := isolate(t)
	writeFile(t, filepath.Join(home, ".config", "clickup-axi", "config.toml"),
		"default_list = \"personal\"\n")

	c, err := Load(t.TempDir()) // no project file anywhere above a temp dir
	if err != nil {
		t.Fatal(err)
	}
	v, ok := c.Get("default_list")
	if !ok || v.Val != "personal" || v.Scope != "personal" {
		t.Fatalf("Get = %+v, %v", v, ok)
	}
}

func TestLoadNothingConfigured(t *testing.T) {
	isolate(t)
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("default_list"); ok {
		t.Fatal("Get found a value with nothing configured")
	}
}

func TestLoadMalformedProjectFile(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ProjectFileName), "default_list = oops\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("Load accepted a malformed project file")
	}
}

func TestFindProjectFileNearestWins(t *testing.T) {
	outer := t.TempDir()
	writeFile(t, filepath.Join(outer, ProjectFileName), "default_list = \"outer\"\n")
	inner := filepath.Join(outer, "sub")
	writeFile(t, filepath.Join(inner, ProjectFileName), "default_list = \"inner\"\n")

	got, ok := FindProjectFile(filepath.Join(inner, "deep", "er"))
	if !ok || got != filepath.Join(inner, ProjectFileName) {
		t.Fatalf("FindProjectFile = %q, %v", got, ok)
	}
}

func TestGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := GitRoot(nested)
	if !ok || got != root {
		t.Fatalf("GitRoot = %q, %v", got, ok)
	}

	// A .git file (worktree/submodule) counts too.
	wt := t.TempDir()
	writeFile(t, filepath.Join(wt, ".git"), "gitdir: elsewhere\n")
	got, ok = GitRoot(wt)
	if !ok || got != wt {
		t.Fatalf("GitRoot(worktree) = %q, %v", got, ok)
	}

	if _, ok := GitRoot(t.TempDir()); ok {
		t.Fatal("GitRoot found a root outside any repository")
	}
}

func TestSetCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clickup-axi", "config.toml")
	if err := Set(path, "default_list", "folder:901"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# clickup-axi config\ndefault_list = \"folder:901\"\n"
	if string(data) != want {
		t.Fatalf("file = %q, want %q", data, want)
	}
}

func TestSetReplacesPreservingComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "# team defaults\n\ndefault_list = \"901\"\n")
	if err := Set(path, "default_list", "902"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "# team defaults\n\ndefault_list = \"902\"\n"
	if string(data) != want {
		t.Fatalf("file = %q, want %q", data, want)
	}
}

func TestSetRefusesMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "not toml at all\n")
	if err := Set(path, "default_list", "901"); err == nil {
		t.Fatal("Set overwrote a malformed file")
	}
}

func TestUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "# keep me\ndefault_list = \"901\"\n")

	found, err := Unset(path, "default_list")
	if err != nil || !found {
		t.Fatalf("Unset = %v, %v", found, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "# keep me\n" {
		t.Fatalf("file = %q", data)
	}

	// Absent key and missing file are clean no-ops.
	found, err = Unset(path, "default_list")
	if err != nil || found {
		t.Fatalf("second Unset = %v, %v", found, err)
	}
	found, err = Unset(filepath.Join(t.TempDir(), "nope.toml"), "default_list")
	if err != nil || found {
		t.Fatalf("missing-file Unset = %v, %v", found, err)
	}
}

func TestEnvKey(t *testing.T) {
	if got := EnvKey("default_list"); got != "CLICKUP_AXI_DEFAULT_LIST" {
		t.Fatalf("EnvKey = %q", got)
	}
}
