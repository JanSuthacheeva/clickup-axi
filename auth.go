package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

const tokenURL = "https://app.clickup.com/settings/apps"

const authHelp = `clickup-axi auth <subcommand>

subcommands:
  login    Store a personal API token. In a terminal it guides you to
           ` + tokenURL + ` and prompts for a
           hidden paste; piped stdin is accepted for scripted use
  logout   Remove the stored token (no-op when none is stored)

examples:
  clickup-axi auth login                          (interactive hidden paste)
  clickup-axi auth login < tokenfile              (scripted / agents)
  pass show clickup | clickup-axi auth login      (from a secret manager)
  clickup-axi auth logout

Never pipe a literal token (echo pk_... | ...): the command line lands
in shell history and agent transcripts. Pipe from a file or secret
manager instead. CLICKUP_TOKEN, when set, takes precedence over the
stored token.`

func cmdAuth(args []string, c *client, stdin io.Reader, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, authHelp)
		return 0
	}
	switch args[0] {
	case "login":
		return cmdAuthLogin(stdin, c, out)
	case "logout":
		return cmdAuthLogout(out)
	default:
		writeError(out, fmt.Sprintf("unknown auth subcommand %q\n  valid: login, logout", args[0]),
			"Run `clickup-axi auth --help`")
		return 2
	}
}

// fdReader is satisfied by *os.File and lets login detect a terminal.
type fdReader interface {
	Fd() uintptr
}

func cmdAuthLogin(stdin io.Reader, c *client, out io.Writer) int {
	var raw []byte
	var err error
	if f, ok := stdin.(fdReader); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(out, "auth: create a personal API token at %s\n", tokenURL)
		fmt.Fprint(out, "paste it here (input stays hidden): ")
		raw, err = term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(out)
	} else {
		raw, err = io.ReadAll(io.LimitReader(stdin, 4096))
	}
	token := strings.TrimSpace(string(raw))
	if err != nil || token == "" {
		writeError(out, "auth login needs a token and got none",
			fmt.Sprintf("Create a token at %s", tokenURL),
			"Run `clickup-axi auth login` in a terminal and paste it, or pipe it by reference: `clickup-axi auth login < tokenfile` (never echo a literal token; it lands in shell history)")
		return 2
	}

	// Validate before storing so a typo fails loudly now, not on first use.
	probe := &client{base: c.base, token: token, http: c.http}
	u, apiErr := probe.getUser()
	if apiErr != nil {
		writeError(out, apiErr.message)
		return 1
	}

	path, err := tokenFilePath()
	if err != nil {
		writeError(out, "could not locate the user config directory")
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		writeError(out, "could not create "+collapseHome(filepath.Dir(path)))
		return 1
	}
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		writeError(out, "could not write "+collapseHome(path))
		return 1
	}

	fmt.Fprintf(out, "auth: logged in as %s (id: %d)\n", u.Username, u.ID)
	fmt.Fprintf(out, "  token: %s (mode 600)\n", collapseHome(path))
	if env := os.Getenv("CLICKUP_TOKEN"); env != "" && env != token {
		fmt.Fprintln(out, "  note: CLICKUP_TOKEN is set in this environment and takes precedence")
	}
	writeHelp(out, "Run `clickup-axi` to see your workspaces")
	return 0
}

func cmdAuthLogout(out io.Writer) int {
	path, err := tokenFilePath()
	if err != nil {
		writeError(out, "could not locate the user config directory")
		return 1
	}
	switch err := os.Remove(path); {
	case errors.Is(err, fs.ErrNotExist):
		fmt.Fprintln(out, "auth: already logged out (no-op)")
	case err != nil:
		writeError(out, "could not remove "+collapseHome(path))
		return 1
	default:
		fmt.Fprintf(out, "auth: logged out (removed %s)\n", collapseHome(path))
	}
	if os.Getenv("CLICKUP_TOKEN") != "" {
		fmt.Fprintln(out, "  note: CLICKUP_TOKEN is still set in this environment and keeps authenticating")
	}
	return 0
}
