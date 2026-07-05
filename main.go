package main

import (
	"fmt"
	"io"
	"os"
)

const (
	version     = "0.1.0"
	description = "Manage ClickUp tasks - an AXI (agent-ergonomic) CLI"
)

const topHelp = `clickup-axi <command> <subcommand> [flags]

commands:
  task view <id>   Show a task with its newest comments
  task edit <id>   Change a task's status (--status "<status>")

auth:
  export CLICKUP_TOKEN=pk_... (ClickUp: Settings -> Apps)

Run ` + "`clickup-axi task --help`" + ` for flags and examples.`

func main() {
	os.Exit(run(os.Args[1:], newClientFromEnv(), os.Stdout))
}

func run(args []string, c *client, out io.Writer) int {
	if len(args) == 0 {
		return cmdHome(c, out)
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprintln(out, topHelp)
		return 0
	case "--version", "-v", "version":
		fmt.Fprintf(out, "clickup-axi %s\n", version)
		return 0
	case "task":
		return cmdTask(args[1:], c, out)
	default:
		writeError(out, fmt.Sprintf("unknown command %q\n  valid: task", args[0]),
			"Run `clickup-axi --help`")
		return 2
	}
}

// cmdHome shows live state instead of help text (AXI principle 8).
func cmdHome(c *client, out io.Writer) int {
	fmt.Fprintf(out, "bin: %s\n", collapseHome(execPath()))
	fmt.Fprintf(out, "description: %s\n", description)

	u, err := c.getUser()
	if err != nil {
		return renderAPIError(out, err)
	}
	fmt.Fprintf(out, "user: %s (id: %d)\n", u.Username, u.ID)

	teams, err := c.getTeams()
	if err != nil {
		return renderAPIError(out, err)
	}
	if len(teams) == 0 {
		fmt.Fprintln(out, "workspaces: 0 workspaces visible to this token")
	} else {
		fmt.Fprintf(out, "workspaces[%d]{id,name}:\n", len(teams))
		for _, t := range teams {
			fmt.Fprintf(out, "  %s,%s\n", t.ID, toonCell(t.Name))
		}
	}
	writeHelp(out,
		"Run `clickup-axi task view <id>` for a task with its comments",
		"Run `clickup-axi task edit <id> --status \"<status>\"` to change status")
	return 0
}

func execPath() string {
	p, err := os.Executable()
	if err != nil {
		return "clickup-axi"
	}
	return p
}
