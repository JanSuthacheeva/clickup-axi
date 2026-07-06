package main

import "testing"

// TestTopHelpGolden pins the exact top-level help output. The help is
// rendered from the command surface table; this test guarantees that
// rendering changes are deliberate, never a side effect of table edits.
func TestTopHelpGolden(t *testing.T) {
	_, c := newFakeClickUp(t)
	want := `clickup-axi <command> <subcommand> [flags]

commands:
  tasks            List your open tasks (assigned to you)
  tasks <id>       Show one task with its newest comments
                   (internal id like 86ey3tx8m or custom like HGAI-2316)
  tasks edit <id>  Change a task's status (--status "<status>")
  auth login       Store a personal API token (read from stdin)
  auth logout      Remove the stored token
  skill            Generate or verify the agent skill (maintainer command)

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
