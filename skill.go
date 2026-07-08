package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

// The agent skill is generated output: skill_template.md carries the
// prose, the command surface table (surface.go) fills in the Commands
// block. Run `clickup-axi skill --write` after changing either;
// TestCommittedSkillIsFresh and `skill --check` in CI fail on drift.

//go:embed skill_template.md
var skillTemplate string

const skillPath = "skills/clickup-axi/SKILL.md"

const skillHelp = `clickup-axi skill [--check | --write]

Maintainer command, run from a clickup-axi checkout: the agent skill
at ` + skillPath + ` is generated from
skill_template.md and the command surface table (surface.go).
Without flags the generated skill is printed to stdout.

flags:
  --check   exit 1 when the committed skill file is stale
  --write   regenerate the skill file (no-op when already current)

examples:
  clickup-axi skill --check
  clickup-axi skill --write`

// generateSkill renders the complete SKILL.md content.
func generateSkill() string {
	var b strings.Builder
	for _, c := range surface {
		if c.skill == "" {
			continue
		}
		if c.comment == "" {
			fmt.Fprintf(&b, "%s\n", c.skill)
			continue
		}
		fmt.Fprintf(&b, "%-43s# %s\n", c.skill, c.comment)
	}
	block := strings.TrimRight(b.String(), "\n")
	return strings.ReplaceAll(skillTemplate, "{{COMMANDS}}", block)
}

func cmdSkill(args []string, out io.Writer) int {
	var check, write bool
	for _, a := range args {
		switch a {
		case "--help", "-h":
			fmt.Fprintln(out, skillHelp)
			return 0
		case "--check":
			check = true
		case "--write":
			write = true
		default:
			output.WriteError(out, fmt.Sprintf("unknown argument %q for skill\n  valid: --check, --write", a),
				"Run `clickup-axi skill --help`")
			return 2
		}
	}
	if check && write {
		output.WriteError(out, "--check and --write cannot be combined",
			"Run `clickup-axi skill --check` or `clickup-axi skill --write`")
		return 2
	}

	want := generateSkill()
	switch {
	case check:
		got, err := os.ReadFile(skillPath)
		if err != nil {
			output.WriteError(out, skillPath+" was not found - run from a clickup-axi checkout",
				"Run `clickup-axi skill --write` from the repository root to regenerate it")
			return 1
		}
		if string(got) != want {
			output.WriteError(out, skillPath+" is stale",
				"Run `clickup-axi skill --write` to regenerate it")
			return 1
		}
		fmt.Fprintf(out, "skill: %s is up to date\n", skillPath)
		return 0
	case write:
		if got, err := os.ReadFile(skillPath); err == nil && string(got) == want {
			fmt.Fprintf(out, "skill: %s already up to date (no-op)\n", skillPath)
			return 0
		}
		if _, err := os.Stat(filepath.Dir(skillPath)); err != nil {
			output.WriteError(out, filepath.Dir(skillPath)+" was not found - run from a clickup-axi checkout",
				"Run `clickup-axi skill --write` from the repository root to regenerate it")
			return 1
		}
		if err := os.WriteFile(skillPath, []byte(want), 0o644); err != nil {
			output.WriteError(out, "could not write "+skillPath,
				"Check write permissions for "+skillPath+" and retry `clickup-axi skill --write`")
			return 1
		}
		fmt.Fprintf(out, "skill: wrote %s\n", skillPath)
		return 0
	default:
		fmt.Fprint(out, want)
		return 0
	}
}
