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
	if !strings.Contains(out, "workspaces[1]{id,name}:\n  \"9018\",Buzzwoo\n") {
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

// TestPostCommandAllowed pins which commands skip the post-command
// maintenance lines: byte-exact/self-referential outputs and context,
// the latency-critical session-start hook whose output is injected as
// ambient context.
func TestPostCommandAllowed(t *testing.T) {
	excluded := []string{"skill", "update", "version", "--version", "-v", "help", "--help", "-h", "context"}
	for _, cmd := range excluded {
		if postCommandAllowed([]string{cmd}) {
			t.Errorf("postCommandAllowed(%q) = true, want false", cmd)
		}
	}
	allowed := []string{"tasks", "search", "auth", "setup"}
	for _, cmd := range allowed {
		if !postCommandAllowed([]string{cmd}) {
			t.Errorf("postCommandAllowed(%q) = false, want true", cmd)
		}
	}
	if !postCommandAllowed(nil) {
		t.Errorf("postCommandAllowed(nil) = false, want true (home view)")
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
  tasks                    List open tasks (yours by default)
                           (--assignee <who> for a teammate, --space to narrow, --page N for more, --fields for extra columns)
  tasks <id>               Show one task, its relationships, and newest comments
                           (internal id like 86ey3tx8m or custom like HGAI-2316)
  search "<query>"         Find your tasks by words in the title or description
  spaces                   List active spaces (projects) in the workspace
  lists --space <name|id>  List active Lists in one space
                           (--archived shows archived Lists; folder context is included)
  members                  List workspace members (who tasks can be assigned to)
  tasks create "<name>"    Create a task in a list (--list <name|id>)
                           (--space scopes a list name; --parent makes a subtask; --status/--assignee/--priority/--due/--body/--tag set fields)
  tasks edit <id>          Change status, assignees, priority, name, due date, description, tags, parent
  tasks comment <id>       Add a comment to a task (--text "<text>")
  tasks move <id>          Move a task to another list (--list <name|id>)
                           (--space scopes a list name; --status picks the landing status when the target lacks the current one)
  tasks close <id>         Close a task (sets the list's closed status)
                           (a dry run without --yes; --yes closes)
  config                   Show layered defaults and where each value comes from
                           (set/unset default_list; flag > env > project > personal; --project writes .clickup-axi.toml at the git root)
  auth login               Store a personal API token (read from stdin)
  auth logout              Remove the stored token
  setup                    Install the session-start hook (Claude Code, Codex, OpenCode)
                           (--global or --project; --remove uninstalls)
  context                  Session-start dashboard printed by the installed hook
  update                   Update the binary to the latest release
  skill                    Generate or verify the agent skill (maintainer command)

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
