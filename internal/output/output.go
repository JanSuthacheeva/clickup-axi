// Package output holds the AXI output conventions shared by every
// command: structured errors on stdout, parameterized help[] hints,
// TOON tabular cells, and truncation helpers.
package output

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ToonCell escapes a value for use in a TOON tabular row. Cells are joined
// with commas, so values containing commas, quotes, or newlines get quoted.
func ToonCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if strings.ContainsAny(s, ",\"") || s != strings.TrimSpace(s) {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// TruncateRunes cuts s to at most n runes, reporting whether it was cut.
func TruncateRunes(s string, n int) (string, bool) {
	r := []rune(s)
	if len(r) <= n {
		return s, false
	}
	return string(r[:n]), true
}

// WriteBlock prints a possibly multi-line value under a key, indenting
// continuation lines so the structure stays parseable.
func WriteBlock(w io.Writer, key, value string, indent string) {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	fmt.Fprintf(w, "%s%s: %s\n", indent, key, lines[0])
	for _, l := range lines[1:] {
		fmt.Fprintf(w, "%s  %s\n", indent, l)
	}
}

func WriteHelp(w io.Writer, lines ...string) {
	if len(lines) == 1 {
		fmt.Fprintf(w, "help[1]: %s\n", lines[0])
		return
	}
	fmt.Fprintf(w, "help[%d]:\n", len(lines))
	for _, l := range lines {
		fmt.Fprintf(w, "  %s\n", l)
	}
}

// WriteError prints a structured error to stdout (agents consume stdout).
func WriteError(w io.Writer, msg string, help ...string) {
	fmt.Fprintf(w, "error: %s\n", msg)
	if len(help) > 0 {
		WriteHelp(w, help...)
	}
}

func CollapseHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
