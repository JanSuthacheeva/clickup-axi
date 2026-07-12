package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/config"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const configHelp = `clickup-axi config
clickup-axi config set default_list <value> [--space <name|id>] [--project]
clickup-axi config unset default_list [--project]

Layered defaults for other commands. Precedence: an explicit flag
beats the CLICKUP_AXI_DEFAULT_LIST environment variable, which beats
the project file (` + config.ProjectFileName + ` at the repository root, found
from any subdirectory), which beats the personal file
(~/.config/clickup-axi/config.toml). set writes the personal file
unless --project is given.

keys:
  default_list   the list tasks create uses when --list is omitted
                 value: a list id, a list name (needs --space), or
                 folder:<id|name> - the folder's current sprint list,
                 derived at create time (a folder name needs --space)

examples:
  clickup-axi config
  clickup-axi config set default_list 901808169633
  clickup-axi config set default_list "Sprint 44" --space Dev --project
  clickup-axi config set default_list "folder:Sprints" --space Dev --project
  clickup-axi config unset default_list --project`

func cmdConfig(args []string, c *clickup.Client, out io.Writer) int {
	if len(args) == 0 {
		return configShow(out)
	}
	switch args[0] {
	case "--help", "-h":
		fmt.Fprintln(out, configHelp)
		return 0
	case "set":
		return configSet(args[1:], c, out)
	case "unset":
		return configUnset(args[1:], out)
	default:
		output.WriteError(out, fmt.Sprintf("unknown subcommand %q for config\n  valid: set, unset", args[0]),
			"Run `clickup-axi config --help`")
		return 2
	}
}

// configShow renders the effective config with each value's
// provenance. It is purely local - no API call - so it also works
// unauthenticated.
func configShow(out io.Writer) int {
	cfg, err := loadConfig()
	if err != nil {
		output.WriteError(out, "config could not be read: "+output.CollapseHome(err.Error()),
			"Fix or remove the offending line, then rerun `clickup-axi config`")
		return 1
	}
	type entry struct {
		key string
		val config.Value
	}
	var entries []entry
	for _, key := range config.KnownKeys {
		if v, ok := cfg.Get(key); ok {
			entries = append(entries, entry{key, v})
		}
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "config: no values set")
		output.WriteHelp(out,
			"Run `clickup-axi config set default_list \"<list|id|folder:...>\"` to set a personal default for tasks create",
			"Run `clickup-axi config set default_list ... --project` to set it for everyone in this repository")
		return 0
	}
	fmt.Fprintln(out, "config:")
	for _, e := range entries {
		fmt.Fprintf(out, "  %s: %s  (%s)\n", e.key, e.val.Val, sourceLabel(e.val))
	}
	if len(cfg.Warnings) > 0 {
		fmt.Fprintf(out, "ignored[%d]:\n", len(cfg.Warnings))
		for _, w := range cfg.Warnings {
			fmt.Fprintf(out, "  %s\n", output.CollapseHome(w))
		}
	}
	return 0
}

func configSet(args []string, c *clickup.Client, out io.Writer) int {
	var positionals []string
	var space string
	var project bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--space":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--space needs a value",
					"Run `clickup-axi config set default_list \"<list>\" --space \"<space>\"`")
				return 2
			}
			space = args[i]
		case "--project":
			project = true
		case "--help", "-h":
			fmt.Fprintln(out, configHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for config set\n  valid: --space, --project", args[i]),
					"Run `clickup-axi config set default_list \"<list|id|folder:...>\"`")
				return 2
			}
			positionals = append(positionals, args[i])
		}
	}
	if len(positionals) != 2 {
		output.WriteError(out, "config set takes a key and a value",
			"Run `clickup-axi config set default_list \"<list|id|folder:...>\"`")
		return 2
	}
	key, value := positionals[0], positionals[1]
	if !configKnownKey(key) {
		output.WriteError(out, fmt.Sprintf("unknown config key %q\n  valid: %s", key, strings.Join(config.KnownKeys, ", ")),
			"Run `clickup-axi config --help`")
		return 2
	}

	stored, describe, extra, code := resolveDefaultListValue(value, space, c, out)
	if code != 0 {
		return code
	}
	path, err := configTargetPath(project)
	if err != nil {
		output.WriteError(out, err.Error(), "Run without --project to set the personal default")
		return 1
	}
	if werr := config.Set(path, key, stored); werr != nil {
		output.WriteError(out, "config could not be written: "+output.CollapseHome(werr.Error()))
		return 1
	}
	fmt.Fprintf(out, "config: %s = %q (%s)\n", key, stored, describe)
	for _, line := range extra {
		fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "  written to: %s\n", output.CollapseHome(path))
	output.WriteHelp(out,
		"Run `clickup-axi tasks create \"<name>\"` to create into the default list",
		"Run `clickup-axi config` to review the effective config")
	return 0
}

// resolveDefaultListValue validates a default_list value against the
// API before anything is written, so the config file can never hold a
// value create would reject. It returns the canonical stored form (a
// list id, or folder:<id>), a human echo, and extra confirmation
// lines; a non-zero code means the error is already rendered.
func resolveDefaultListValue(value, space string, c *clickup.Client, out io.Writer) (stored, describe string, extra []string, code int) {
	switch {
	case allDigits(value):
		l, err := c.GetList(value)
		if err != nil {
			if err.Status == http.StatusNotFound {
				output.WriteError(out, fmt.Sprintf("list %q not found", value),
					"Run `clickup-axi lists --space \"<space>\"` to discover list ids")
				return "", "", nil, 1
			}
			return "", "", nil, renderAPIError(out, err)
		}
		return value, fmt.Sprintf("list %q", l.Name), nil, 0

	case strings.HasPrefix(value, "folder:"):
		rest := strings.TrimPrefix(value, "folder:")
		if rest == "" {
			output.WriteError(out, `folder: needs a folder id or name, e.g. folder:90123456 or "folder:Sprints"`,
				"Run `clickup-axi config set default_list \"folder:<id|name>\"`")
			return "", "", nil, 2
		}
		var folder *clickup.Folder
		if allDigits(rest) {
			f, err := c.GetFolder(rest)
			if err != nil {
				if err.Status == http.StatusNotFound {
					output.WriteError(out, fmt.Sprintf("folder %q not found", rest),
						"Run `clickup-axi config set default_list \"folder:<name>\" --space \"<space>\"` to resolve one by name")
					return "", "", nil, 1
				}
				return "", "", nil, renderAPIError(out, err)
			}
			folder = f
		} else {
			if space == "" {
				output.WriteError(out, "folder: by name needs --space (folder names are only unique within one space)",
					"Run `clickup-axi config set default_list \"folder:<name>\" --space \"<space>\"`")
				return "", "", nil, 2
			}
			f, code := resolveFolderInSpace(rest, space, c, out)
			if code != 0 {
				return "", "", nil, code
			}
			folder = f
		}
		return "folder:" + folder.ID, fmt.Sprintf("folder %q", folder.Name), []string{currentListLine(folder)}, 0

	default: // a list name
		if space == "" {
			output.WriteError(out, "a list name needs --space (list names are only unique within one space)",
				"Run `clickup-axi config set default_list \"<list>\" --space \"<space>\"`",
				"Or use the list id from `clickup-axi lists --space \"<space>\"`")
			return "", "", nil, 2
		}
		team, err := c.SelectTeam()
		if err != nil {
			return "", "", nil, renderAPIError(out, err)
		}
		sp, err := c.ResolveSpace(team.ID, space)
		if err != nil {
			return "", "", nil, renderAPIError(out, err)
		}
		ref, err := c.ResolveList(sp.ID, value)
		if err != nil {
			return "", "", nil, renderAPIError(out, err)
		}
		return ref.ID, fmt.Sprintf("list %q", ref.Name), nil, 0
	}
}

func resolveFolderInSpace(name, space string, c *clickup.Client, out io.Writer) (*clickup.Folder, int) {
	team, err := c.SelectTeam()
	if err != nil {
		return nil, renderAPIError(out, err)
	}
	sp, err := c.ResolveSpace(team.ID, space)
	if err != nil {
		return nil, renderAPIError(out, err)
	}
	folder, err := c.ResolveFolder(sp.ID, name)
	if err != nil {
		return nil, renderAPIError(out, err)
	}
	return folder, 0
}

// currentListLine echoes which list a folder default resolves to right
// now, so the agent sees the effect of the setting immediately.
func currentListLine(f *clickup.Folder) string {
	cur, ok := f.CurrentList(time.Now())
	if !ok {
		return "  current list: none (the folder has no lists yet)"
	}
	return fmt.Sprintf("  current list: %s %q (derived again at create time)", cur.ID, cur.Name)
}

func configUnset(args []string, out io.Writer) int {
	var positionals []string
	var project bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			project = true
		case "--help", "-h":
			fmt.Fprintln(out, configHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for config unset\n  valid: --project", args[i]),
					"Run `clickup-axi config unset default_list`")
				return 2
			}
			positionals = append(positionals, args[i])
		}
	}
	if len(positionals) != 1 {
		output.WriteError(out, "config unset takes a key",
			"Run `clickup-axi config unset default_list`")
		return 2
	}
	key := positionals[0]
	if !configKnownKey(key) {
		output.WriteError(out, fmt.Sprintf("unknown config key %q\n  valid: %s", key, strings.Join(config.KnownKeys, ", ")),
			"Run `clickup-axi config --help`")
		return 2
	}
	path, err := configTargetPath(project)
	if err != nil {
		output.WriteError(out, err.Error(), "Run without --project to unset the personal default")
		return 1
	}
	found, uerr := config.Unset(path, key)
	if uerr != nil {
		output.WriteError(out, "config could not be written: "+output.CollapseHome(uerr.Error()))
		return 1
	}
	if !found {
		fmt.Fprintf(out, "config: %s is not set in %s (no changes)\n", key, output.CollapseHome(path))
		return 0
	}
	fmt.Fprintf(out, "config: %s removed from %s\n", key, output.CollapseHome(path))
	output.WriteHelp(out, "Run `clickup-axi config` to review the effective config")
	return 0
}

// configTargetPath picks the file a set/unset writes: the personal
// file by default; with --project the nearest existing project file
// walking up from the working directory, else a new one at the git
// root - the committable location a whole team shares.
func configTargetPath(project bool) (string, error) {
	if !project {
		return config.PersonalPath()
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if p, ok := config.FindProjectFile(cwd); ok {
		return p, nil
	}
	if root, ok := config.GitRoot(cwd); ok {
		return filepath.Join(root, config.ProjectFileName), nil
	}
	return "", fmt.Errorf("--project needs a repository: no %s above the working directory and no .git root to create one at", config.ProjectFileName)
}

// loadConfig loads the layered config from the working directory.
func loadConfig() (*config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return config.Load(cwd)
}

func sourceLabel(v config.Value) string {
	if v.Scope == "env" {
		return "env " + v.Path
	}
	return v.Scope + " " + output.CollapseHome(v.Path)
}

func configKnownKey(key string) bool {
	for _, k := range config.KnownKeys {
		if k == key {
			return true
		}
	}
	return false
}
