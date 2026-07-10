package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/JanSuthacheeva/clickup-axi/internal/hostcfg"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const setupHelp = `clickup-axi setup [--global | --project] [--app <host>] [--remove]

Installs a session-start hook so agent sessions begin with a compact
ClickUp dashboard (the ` + "`clickup-axi context`" + ` output) already in
context. Targets every detected host; rerunning repairs a moved
binary path and is otherwise a no-op.

hosts:
  claude-code   ~/.claude/settings.json     (project: .claude/settings.json)
  codex         ~/.codex/hooks.json         (project: .codex/hooks.json)
  opencode      ~/.config/opencode/plugins/clickup-axi.js (global only)

flags:
  --global        install for this user (recommended)
  --project       install into the current directory's project configs
  --app <host>    only this host: claude-code | codex | opencode
  --remove        uninstall (same scope and app selection)

In a terminal, the scope is prompted when omitted; on agent paths it
must be explicit.

examples:
  clickup-axi setup --global
  clickup-axi setup --global --remove`

var setupHosts = map[string]bool{"claude-code": true, "codex": true, "opencode": true}

func cmdSetup(args []string, stdin io.Reader, out io.Writer) int {
	var globalFlag, projectFlag, remove bool
	var app string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			fmt.Fprintln(out, setupHelp)
			return 0
		case "--global":
			globalFlag = true
		case "--project":
			projectFlag = true
		case "--remove":
			remove = true
		case "--app":
			i++
			if i >= len(args) {
				output.WriteError(out, "--app needs a value\n  valid: claude-code, codex, opencode",
					"Run `clickup-axi setup --global --app claude-code`")
				return 2
			}
			app = args[i]
			if !setupHosts[app] {
				output.WriteError(out, fmt.Sprintf("unknown app %q\n  valid: claude-code, codex, opencode", app),
					"Run `clickup-axi setup --help`")
				return 2
			}
		default:
			output.WriteError(out, fmt.Sprintf("unknown argument %q for setup\n  valid: --global, --project, --app, --remove", args[i]),
				"Run `clickup-axi setup --help`")
			return 2
		}
	}
	if globalFlag && projectFlag {
		output.WriteError(out, "--global and --project cannot be combined",
			"Run `clickup-axi setup --global` or `clickup-axi setup --project`")
		return 2
	}

	var scope hostcfg.Scope
	switch {
	case globalFlag:
		scope = hostcfg.Global
	case projectFlag:
		scope = hostcfg.Project
	default:
		s, code := promptScope(stdin, out)
		if code != 0 {
			return code
		}
		scope = s
	}

	home, err := os.UserHomeDir()
	if err != nil {
		output.WriteError(out, "could not locate the user home directory")
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		output.WriteError(out, "could not determine the current directory")
		return 1
	}

	hookCmd := setupHookCommand()
	fmt.Fprintf(out, "setup: session hook (%s)\n", scopeName(scope))
	exit := 0
	for _, t := range hostcfg.Targets(scope, home, cwd) {
		if app != "" && t.Host != app {
			continue
		}
		var r hostcfg.Report
		if remove {
			r = hostcfg.Remove(t)
		} else {
			r = hostcfg.Install(t, hookCmd)
		}
		fmt.Fprintf(out, "  %s\n", renderReport(r))
		if r.Action == hostcfg.Failed {
			exit = 1
		}
	}

	scopeFlag := "--" + scopeName(scope)
	if remove {
		output.WriteHelp(out, "Run `clickup-axi setup "+scopeFlag+"` to reinstall")
	} else {
		output.WriteHelp(out,
			"Start a new agent session to load the ClickUp dashboard",
			"Run `clickup-axi setup "+scopeFlag+" --remove` to uninstall")
	}
	return exit
}

// promptScope asks for the scope on a real terminal (the auth login
// TTY exception); agent paths get a usage error that inlines both
// flags so the retry is one step.
func promptScope(stdin io.Reader, out io.Writer) (hostcfg.Scope, int) {
	f, ok := stdin.(fdReader)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		output.WriteError(out, "setup needs an explicit scope when not run in a terminal\n  valid: --global (recommended), --project",
			"Run `clickup-axi setup --global`")
		return 0, 2
	}
	fmt.Fprintln(out, "setup: install the session-start hook")
	fmt.Fprintln(out, "scope? [1] global (recommended)  [2] project (this directory)")
	fmt.Fprint(out, "choice [1]: ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	scope, ok := parseScopeChoice(strings.TrimSpace(line))
	if !ok {
		output.WriteError(out, fmt.Sprintf("unrecognized choice %q\n  valid: 1/global, 2/project", strings.TrimSpace(line)),
			"Run `clickup-axi setup --global`")
		return 0, 2
	}
	return scope, 0
}

func parseScopeChoice(s string) (hostcfg.Scope, bool) {
	switch strings.ToLower(s) {
	case "", "1", "g", "global":
		return hostcfg.Global, true
	case "2", "p", "project":
		return hostcfg.Project, true
	}
	return 0, false
}

func scopeName(s hostcfg.Scope) string {
	if s == hostcfg.Project {
		return "project"
	}
	return "global"
}

func renderReport(r hostcfg.Report) string {
	switch r.Action {
	case hostcfg.Installed:
		return fmt.Sprintf("%s: installed (%s)", r.Host, output.CollapseHome(r.Path))
	case hostcfg.Repaired:
		return fmt.Sprintf("%s: repaired (%s)", r.Host, output.CollapseHome(r.Path))
	case hostcfg.AlreadyInstalled:
		return fmt.Sprintf("%s: already installed (no-op)", r.Host)
	case hostcfg.Removed:
		return fmt.Sprintf("%s: removed (%s)", r.Host, output.CollapseHome(r.Path))
	case hostcfg.NotInstalled:
		return fmt.Sprintf("%s: not installed (no-op)", r.Host)
	case hostcfg.Skipped:
		return fmt.Sprintf("%s: skipped (%s)", r.Host, r.Detail)
	default:
		return fmt.Sprintf("%s: error (%s)", r.Host, r.Detail)
	}
}

// setupHookCommand resolves the command the hooks will run. A var so
// tests can pin it: the real value embeds this binary's path, which in
// `go test` is the test binary and would defeat the "command contains
// clickup-axi" recognition that repair and remove rely on.
var setupHookCommand = func() string {
	return hostcfg.HookCommand(osExecutable())
}

// osExecutable mirrors execPath (cli.go) but returns "" on failure so
// HookCommand can fall back to the bare name.
func osExecutable() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}
