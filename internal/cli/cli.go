// Package cli is the driving adapter: it turns argv into API calls and
// renders every result following the AXI output contract. Run is the
// single entry point; cmd/clickup-axi wires the real dependencies in.
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
	"github.com/JanSuthacheeva/clickup-axi/internal/update"
	"github.com/JanSuthacheeva/clickup-axi/internal/version"
)

const description = "Manage ClickUp tasks - an AXI (agent-ergonomic) CLI"

// topHelp renders the top-level help from the command surface, so the
// help text and the generated agent skill can never disagree about
// which commands exist.
func topHelp() string {
	width := 20
	for _, c := range surface {
		if c.usage != "" && len(c.usage)+2 > width {
			width = len(c.usage) + 2
		}
	}
	var b strings.Builder
	b.WriteString("clickup-axi <command> <subcommand> [flags]\n\ncommands:\n")
	for _, c := range surface {
		if c.usage == "" {
			continue
		}
		fmt.Fprintf(&b, "  %-*s%s\n", width, c.usage, c.summary)
		if c.note != "" {
			fmt.Fprintf(&b, "  %-*s%s\n", width, "", c.note)
		}
	}
	b.WriteString(`
auth:
  clickup-axi auth login   (guides you to a token, hidden paste)
  CLICKUP_TOKEN, when set, takes precedence over the stored token

Run ` + "`clickup-axi tasks --help`" + ` for flags and examples.`)
	return b.String()
}

// Run dispatches one command and appends the post-command maintenance
// lines (update notice, skill heal) where the output contract allows.
func Run(args []string, c *clickup.Client, up *update.Updater, stdin io.Reader, out io.Writer) int {
	code := dispatch(args, c, up, stdin, out)
	if up != nil && postCommandAllowed(args) {
		up.PostCommand(out, code == 0)
	}
	return code
}

// postCommandAllowed excludes outputs that must stay byte-exact
// (skill) or are self-referential (update, version, help) from the
// post-command maintenance lines. It also excludes context: that is
// the latency-critical session-start hook, and its output is injected
// as ambient context into every session, where an update notice or a
// skill-heal line would be noise on the hot path.
func postCommandAllowed(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "skill", "update", "--version", "-v", "version", "--help", "-h", "help", "context":
		return false
	}
	return true
}

func dispatch(args []string, c *clickup.Client, up *update.Updater, stdin io.Reader, out io.Writer) int {
	if len(args) == 0 {
		return cmdHome(c, out)
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprintln(out, topHelp())
		return 0
	case "--version", "-v", "version":
		fmt.Fprintf(out, "clickup-axi %s\n", version.String())
		return 0
	case "tasks":
		return cmdTasks(args[1:], c, out)
	case "search":
		return cmdSearch(args[1:], c, out)
	case "spaces":
		return cmdSpaces(args[1:], c, out)
	case "lists":
		return cmdLists(args[1:], c, out)
	case "auth":
		return cmdAuth(args[1:], c, stdin, out)
	case "context":
		return cmdContext(args[1:], c, out)
	case "setup":
		return cmdSetup(args[1:], stdin, out)
	case "update":
		return update.Cmd(args[1:], up, out)
	case "skill":
		return cmdSkill(args[1:], out)
	default:
		output.WriteError(out, fmt.Sprintf("unknown command %q\n  valid: tasks, search, spaces, lists, auth, setup, context, update, skill", args[0]),
			"Run `clickup-axi --help`")
		return 2
	}
}

// cmdHome shows live state instead of help text (AXI principle 8).
func cmdHome(c *clickup.Client, out io.Writer) int {
	fmt.Fprintf(out, "bin: %s\n", output.CollapseHome(execPath()))
	fmt.Fprintf(out, "description: %s\n", description)

	u, err := c.GetUser()
	if err != nil {
		return renderAPIError(out, err)
	}
	fmt.Fprintf(out, "user: %s (id: %d)\n", u.Username, u.ID)

	teams, err := c.GetTeams()
	if err != nil {
		return renderAPIError(out, err)
	}
	if len(teams) == 0 {
		fmt.Fprintln(out, "workspaces: 0 workspaces visible to this token")
	} else {
		fmt.Fprintf(out, "workspaces[%d]{id,name}:\n", len(teams))
		for _, t := range teams {
			fmt.Fprintf(out, "  %s,%s\n", output.ToonCell(t.ID), output.ToonCell(t.Name))
		}
	}

	help := []string{
		"Run `clickup-axi tasks` for your open tasks",
		"Run `clickup-axi tasks <id>` for a task with its comments",
		"Run `clickup-axi tasks edit <id> --status \"<status>\"` to change status",
	}
	if pin := clickup.WorkspaceIDFromEnv(); pin != "" {
		fmt.Fprintln(out, pinnedWorkspaceLine(teams, pin))
	} else if len(teams) > 1 {
		help = append([]string{
			"Set " + clickup.WorkspaceEnv + "=<id> to pin a workspace (needed for `tasks` and custom ids)",
		}, help...)
	}
	output.WriteHelp(out, help...)
	return 0
}

// pinnedWorkspaceLine echoes the CLICKUP_AXI_WORKSPACE pin so agents
// can verify their setup at a glance, flagging a pin the token cannot
// see instead of failing the whole home view over it.
func pinnedWorkspaceLine(teams []clickup.Team, pin string) string {
	for _, t := range teams {
		if t.ID == pin {
			return fmt.Sprintf("workspace: %s %s (%s)", t.ID, t.Name, clickup.WorkspaceEnv)
		}
	}
	return fmt.Sprintf("workspace: %s (%s, not visible to this token)", pin, clickup.WorkspaceEnv)
}

func execPath() string {
	p, err := os.Executable()
	if err != nil {
		return "clickup-axi"
	}
	return p
}
