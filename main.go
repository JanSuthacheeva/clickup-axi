package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

const description = "Manage ClickUp tasks - an AXI (agent-ergonomic) CLI"

// version is injected by release builds via
// -ldflags "-X main.version=<tag>". Source builds fall back to the
// module version that `go install pkg@version` embeds, then to "dev".
var version string

func versionString() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// topHelp renders the top-level help from the command surface, so the
// help text and the generated agent skill can never disagree about
// which commands exist.
func topHelp() string {
	var b strings.Builder
	b.WriteString("clickup-axi <command> <subcommand> [flags]\n\ncommands:\n")
	for _, c := range surface {
		if c.usage == "" {
			continue
		}
		fmt.Fprintf(&b, "  %-17s%s\n", c.usage, c.summary)
		if c.note != "" {
			fmt.Fprintf(&b, "  %-17s%s\n", "", c.note)
		}
	}
	b.WriteString(`
auth:
  clickup-axi auth login   (guides you to a token, hidden paste)
  CLICKUP_TOKEN, when set, takes precedence over the stored token

Run ` + "`clickup-axi tasks --help`" + ` for flags and examples.`)
	return b.String()
}

func main() {
	os.Exit(run(os.Args[1:], newClientFromEnv(), newUpdaterFromEnv(), os.Stdin, os.Stdout))
}

func run(args []string, c *client, up *updater, stdin io.Reader, out io.Writer) int {
	code := dispatch(args, c, up, stdin, out)
	if up != nil && postCommandAllowed(args) {
		up.postCommand(out)
	}
	return code
}

// postCommandAllowed excludes outputs that must stay byte-exact
// (skill) or are self-referential (update, version, help) from the
// post-command maintenance lines.
func postCommandAllowed(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "skill", "update", "--version", "-v", "version", "--help", "-h", "help":
		return false
	}
	return true
}

func dispatch(args []string, c *client, up *updater, stdin io.Reader, out io.Writer) int {
	if len(args) == 0 {
		return cmdHome(c, out)
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprintln(out, topHelp())
		return 0
	case "--version", "-v", "version":
		fmt.Fprintf(out, "clickup-axi %s\n", versionString())
		return 0
	case "tasks":
		return cmdTasks(args[1:], c, out)
	case "auth":
		return cmdAuth(args[1:], c, stdin, out)
	case "update":
		return cmdUpdate(args[1:], up, out)
	case "skill":
		return cmdSkill(args[1:], out)
	default:
		writeError(out, fmt.Sprintf("unknown command %q\n  valid: tasks, auth, update, skill", args[0]),
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
		"Run `clickup-axi tasks` for your open tasks",
		"Run `clickup-axi tasks <id>` for a task with its comments",
		"Run `clickup-axi tasks edit <id> --status \"<status>\"` to change status")
	return 0
}

func execPath() string {
	p, err := os.Executable()
	if err != nil {
		return "clickup-axi"
	}
	return p
}
