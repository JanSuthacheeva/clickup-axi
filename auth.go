package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const authHelp = `clickup-axi auth <subcommand>

subcommands:
  login    Store a personal API token; read from stdin only so the
           token never appears in process arguments or shell history
  logout   Remove the stored token (no-op when none is stored)

examples:
  echo -n pk_... | clickup-axi auth login
  clickup-axi auth logout

CLICKUP_TOKEN, when set, takes precedence over the stored token.`

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

func cmdAuthLogin(stdin io.Reader, c *client, out io.Writer) int {
	raw, err := io.ReadAll(io.LimitReader(stdin, 4096))
	token := strings.TrimSpace(string(raw))
	if err != nil || token == "" {
		writeError(out, "auth login reads the token from stdin and got nothing",
			"Run `echo -n pk_... | clickup-axi auth login` (token: ClickUp Settings -> Apps)")
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
