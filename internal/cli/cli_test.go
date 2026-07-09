package cli

import (
	"strings"
	"testing"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
)

func TestHomeSingleWorkspaceNeedsNoPin(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")

	out, code := runCLI(t, c)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "workspaces[1]{id,name}:\n  9018,Buzzwoo\n") {
		t.Errorf("output missing workspaces table\noutput:\n%s", out)
	}
	if strings.Contains(out, "CLICKUP_AXI_WORKSPACE") {
		t.Errorf("pin guidance shown despite a single workspace\noutput:\n%s", out)
	}
}

func TestHomeMultipleWorkspacesHintAtPin(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)

	out, code := runCLI(t, c)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "Set CLICKUP_AXI_WORKSPACE=<id> to pin a workspace (needed for `tasks` and custom ids)") {
		t.Errorf("output missing pin hint\noutput:\n%s", out)
	}
	if strings.Contains(out, "workspace: ") {
		t.Errorf("workspace status line shown despite no pin\noutput:\n%s", out)
	}
}

func TestHomeEchoesThePinnedWorkspace(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv(clickup.WorkspaceEnv, "9002")
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)

	out, code := runCLI(t, c)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "workspace: 9002 Personal (CLICKUP_AXI_WORKSPACE)\n") {
		t.Errorf("output missing pinned workspace line\noutput:\n%s", out)
	}
	if strings.Contains(out, "Set CLICKUP_AXI_WORKSPACE") {
		t.Errorf("pin hint shown despite an active pin\noutput:\n%s", out)
	}
}

func TestHomeFlagsAnInvisiblePin(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv(clickup.WorkspaceEnv, "1234")
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)

	out, code := runCLI(t, c)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "workspace: 1234 (CLICKUP_AXI_WORKSPACE, not visible to this token)\n") {
		t.Errorf("output missing invisible-pin warning\noutput:\n%s", out)
	}
}

// TestVersionFallsBackToDev pins the source-build fallback; release
// binaries override it via -ldflags (asserted by the release workflow
// building with -X on internal/version.Version).
func TestVersionFallsBackToDev(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "--version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if out != "clickup-axi dev\n" {
		t.Errorf("version output = %q, want %q", out, "clickup-axi dev\n")
	}
}

// TestTopHelpGolden pins the exact top-level help output. The help is
// rendered from the command surface table; this test guarantees that
// rendering changes are deliberate, never a side effect of table edits.
func TestTopHelpGolden(t *testing.T) {
	_, c := newFakeClickUp(t)
	want := `clickup-axi <command> <subcommand> [flags]

commands:
  tasks               List open tasks (yours by default)
                      (--assignee <who> for a teammate, --space <name|id> to narrow)
  tasks <id>          Show one task with its newest comments
                      (internal id like 86ey3tx8m or custom like HGAI-2316)
  search "<query>"    Find your tasks by words in the title or description
  tasks edit <id>     Change status, add/remove assignees (--status, --assignee, --unassign)
  tasks comment <id>  Add a comment to a task (--text "<text>")
  auth login          Store a personal API token (read from stdin)
  auth logout         Remove the stored token
  update              Update the binary to the latest release
  skill               Generate or verify the agent skill (maintainer command)

auth:
  clickup-axi auth login   (guides you to a token, hidden paste)
  CLICKUP_TOKEN, when set, takes precedence over the stored token

Run ` + "`clickup-axi tasks --help`" + ` for flags and examples.
`

	out, code := runCLI(t, c, "--help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if out != want {
		t.Errorf("top-level help drifted from the golden output\ngot:\n%s\nwant:\n%s", out, want)
	}
}
