package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JanSuthacheeva/clickup-axi/internal/config"
)

// folderJSON registers GET /folder/{id}. The list dates sit in 2020,
// far in the past, so CurrentList deterministically picks the latest
// start on any test day.
func (f *fakeClickUp) folder(t *testing.T, id, body string) {
	t.Helper()
	f.mux.HandleFunc("GET /api/v2/folder/"+id, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
}

const sprintsFolderJSON = `{"id": "9012", "name": "Sprints", "lists": [
	{"id": "901", "name": "Sprint 1", "start_date": "1577865600000", "due_date": "1578988800000"},
	{"id": "902", "name": "Sprint 2", "start_date": "1579075200000", "due_date": "1580198400000"}
]}`

// personalConfigPath resolves the personal file inside the relocated
// test HOME.
func personalConfigPath(t *testing.T) string {
	t.Helper()
	p, err := config.PersonalPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfigShowEmpty(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "config")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	want := `config: no values set
help[2]:
  Run ` + "`clickup-axi config set default_list \"<list|id|folder:...>\"`" + ` to set a personal default for tasks create
  Run ` + "`clickup-axi config set default_list ... --project`" + ` to set it for everyone in this repository
`
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfigShowLayeredSources(t *testing.T) {
	_, c := newFakeClickUp(t)
	if err := config.Set(personalConfigPath(t), "default_list", "111"); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	projectFile := filepath.Join(cwd, config.ProjectFileName)
	if err := config.Set(projectFile, "default_list", "222"); err != nil {
		t.Fatal(err)
	}

	// The project file shadows the personal one.
	out, code := runCLI(t, c, "config")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	want := "config:\n  default_list: 222  (project " + projectFile + ")\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}

	// The environment shadows both files.
	t.Setenv("CLICKUP_AXI_DEFAULT_LIST", "333")
	out, code = runCLI(t, c, "config")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	want = "config:\n  default_list: 333  (env CLICKUP_AXI_DEFAULT_LIST)\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfigShowPersonalCollapsesHome(t *testing.T) {
	_, c := newFakeClickUp(t)
	if err := config.Set(personalConfigPath(t), "default_list", "111"); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, c, "config")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	want := "config:\n  default_list: 111  (personal ~/.config/clickup-axi/config.toml)\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfigShowReportsIgnoredUnknownKeys(t *testing.T) {
	_, c := newFakeClickUp(t)
	cwd, _ := os.Getwd()
	projectFile := filepath.Join(cwd, config.ProjectFileName)
	if err := os.WriteFile(projectFile, []byte("future_key = \"x\"\ndefault_list = \"222\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, c, "config")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	want := "config:\n  default_list: 222  (project " + projectFile + ")\n" +
		"ignored[1]:\n  " + projectFile + `:1: ignoring unknown key "future_key"` + "\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfigShowMalformedFileErrors(t *testing.T) {
	_, c := newFakeClickUp(t)
	cwd, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(cwd, config.ProjectFileName), []byte("default_list = oops\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, c, "config")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "config could not be read") ||
		!strings.Contains(out, config.ProjectFileName+":1") ||
		!strings.Contains(out, "must be a quoted string") {
		t.Errorf("output:\n%s", out)
	}
}

func TestConfigSetByListId(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.list(t, "901234", "Sprint 12", "to do")

	out, code := runCLI(t, c, "config", "set", "default_list", "901234")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	want := `config: default_list = "901234" (list "Sprint 12")
  written to: ~/.config/clickup-axi/config.toml
help[2]:
  Run ` + "`clickup-axi tasks create \"<name>\"`" + ` to create into the default list
  Run ` + "`clickup-axi config`" + ` to review the effective config
`
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	data, err := os.ReadFile(personalConfigPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# clickup-axi config\ndefault_list = \"901234\"\n" {
		t.Errorf("file = %q", data)
	}
}

func TestConfigSetByListIdNotFound(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "config", "set", "default_list", "999")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	want := `error: list "999" not found
help[1]: Run ` + "`clickup-axi lists --space \"<space>\"`" + ` to discover list ids
`
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfigSetByNameNeedsSpace(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "config", "set", "default_list", "Sprint 12")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "a list name needs --space") {
		t.Errorf("output:\n%s", out)
	}
}

func TestConfigSetByNameResolvesAndStoresTheId(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", `{"teams": [{"id": "9018", "name": "Buzzwoo"}]}`)
	f.spaces(t, "9018", twoSpacesJSON)
	f.sprintLists(t, "")

	out, code := runCLI(t, c, "config", "set", "default_list", "sprint 12", "--space", "webshop")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `config: default_list = "901234" (list "Sprint 12")`) {
		t.Errorf("output:\n%s", out)
	}
	data, _ := os.ReadFile(personalConfigPath(t))
	if !strings.Contains(string(data), `default_list = "901234"`) {
		t.Errorf("file = %q", data)
	}
}

func TestConfigSetFolderById(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.folder(t, "9012", sprintsFolderJSON)

	out, code := runCLI(t, c, "config", "set", "default_list", "folder:9012")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	want := `config: default_list = "folder:9012" (folder "Sprints")
  current list: 902 "Sprint 2" (derived again at create time)
  written to: ~/.config/clickup-axi/config.toml
help[2]:
  Run ` + "`clickup-axi tasks create \"<name>\"`" + ` to create into the default list
  Run ` + "`clickup-axi config`" + ` to review the effective config
`
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfigSetFolderByIdNotFound(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "config", "set", "default_list", "folder:404404")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `folder "404404" not found`) {
		t.Errorf("output:\n%s", out)
	}
}

func TestConfigSetFolderByName(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", `{"teams": [{"id": "9018", "name": "Buzzwoo"}]}`)
	f.spaces(t, "9018", twoSpacesJSON)
	f.mux.HandleFunc("GET /api/v2/space/90121/folder", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"folders": [` + sprintsFolderJSON + `]}`))
	})

	out, code := runCLI(t, c, "config", "set", "default_list", "folder:sprints", "--space", "webshop")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `config: default_list = "folder:9012" (folder "Sprints")`) {
		t.Errorf("output:\n%s", out)
	}
}

func TestConfigSetFolderByNameNeedsSpace(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "config", "set", "default_list", "folder:Sprints")
	if code != 2 || !strings.Contains(out, "folder: by name needs --space") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestConfigSetEmptyFolderValue(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "config", "set", "default_list", "folder:")
	if code != 2 || !strings.Contains(out, "folder: needs a folder id or name") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestConfigSetUnknownKey(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "config", "set", "default_space", "90121")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `unknown config key "default_space"`) || !strings.Contains(out, "valid: default_list") {
		t.Errorf("output:\n%s", out)
	}
}

func TestConfigSetProjectWritesTheGitRoot(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.list(t, "901234", "Sprint 12", "to do")
	root, _ := os.Getwd() // the harness chdir'ed into a fresh temp dir
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "internal", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	out, code := runCLI(t, c, "config", "set", "default_list", "901234", "--project")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	projectFile := filepath.Join(root, config.ProjectFileName)
	if !strings.Contains(out, "written to: "+projectFile) {
		t.Errorf("output:\n%s", out)
	}
	if _, err := os.Stat(projectFile); err != nil {
		t.Errorf("project file not created at the git root: %v", err)
	}
}

func TestConfigSetProjectOutsideRepositoryErrors(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.list(t, "901234", "Sprint 12", "to do")
	out, code := runCLI(t, c, "config", "set", "default_list", "901234", "--project")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "--project needs a repository") ||
		!strings.Contains(out, "Run without --project") {
		t.Errorf("output:\n%s", out)
	}
}

func TestConfigUnset(t *testing.T) {
	_, c := newFakeClickUp(t)
	if err := config.Set(personalConfigPath(t), "default_list", "111"); err != nil {
		t.Fatal(err)
	}

	out, code := runCLI(t, c, "config", "unset", "default_list")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	want := `config: default_list removed from ~/.config/clickup-axi/config.toml
help[1]: Run ` + "`clickup-axi config`" + ` to review the effective config
`
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}

	// Unsetting again is an idempotent no-op.
	out, code = runCLI(t, c, "config", "unset", "default_list")
	if code != 0 {
		t.Fatalf("second unset exit code = %d\noutput:\n%s", code, out)
	}
	want = "config: default_list is not set in ~/.config/clickup-axi/config.toml (no changes)\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfigUnknownSubcommand(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "config", "delete")
	if code != 2 || !strings.Contains(out, `unknown subcommand "delete" for config`) {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}
