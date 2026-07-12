package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const spacesHelp = `clickup-axi spaces

List the active spaces (projects) in the selected workspace. With more
than one workspace visible, set CLICKUP_AXI_WORKSPACE=<id> first; run
` + "`clickup-axi`" + ` to list the ids.

examples:
  clickup-axi spaces
  clickup-axi lists --space "Webshop"`

const listsHelp = `clickup-axi lists --space <name|id> [--archived]

List the active Lists in one space (project), whether they are directly
in the space or inside a Folder. --archived shows archived Lists instead.
Space names resolve case-insensitively: an exact match wins, otherwise a
unique substring works. A mismatch or ambiguity inlines candidates.

flags:
  --space <name|id>  required space (project) to inspect
  --archived         show archived lists instead of active lists

examples:
  clickup-axi lists --space "Webshop"
  clickup-axi lists --space 90121 --archived`

func cmdSpaces(args []string, c *clickup.Client, out io.Writer) int {
	for _, arg := range args {
		switch arg {
		case "--help", "-h":
			fmt.Fprintln(out, spacesHelp)
			return 0
		default:
			kind := "argument"
			if strings.HasPrefix(arg, "-") {
				kind = "flag"
			}
			output.WriteError(out, fmt.Sprintf("unknown %s %q for spaces\n  valid flags: --help", kind, arg),
				"Run `clickup-axi spaces`")
			return 2
		}
	}

	team, err := c.SelectTeam()
	if err != nil {
		return renderAPIError(out, err)
	}
	spaces, err := c.GetSpaces(team.ID)
	if err != nil {
		return renderAPIError(out, err)
	}
	sort.Slice(spaces, func(i, j int) bool {
		left, right := strings.ToLower(spaces[i].Name), strings.ToLower(spaces[j].Name)
		if left != right {
			return left < right
		}
		return spaces[i].ID < spaces[j].ID
	})

	workspace := fmt.Sprintf("workspace %s %q", team.ID, team.Name)
	if len(spaces) == 0 {
		fmt.Fprintf(out, "spaces: 0 active spaces in %s\n", workspace)
		output.WriteHelp(out, "Run `clickup-axi tasks` to see your open tasks")
		return 0
	}
	fmt.Fprintf(out, "count: %d active space%s in %s\n", len(spaces), pluralS(len(spaces)), workspace)
	fmt.Fprintf(out, "spaces[%d]{id,name}:\n", len(spaces))
	for _, space := range spaces {
		fmt.Fprintf(out, "  %s,%s\n", output.ToonCell(space.ID), output.ToonCell(space.Name))
	}
	output.WriteHelp(out, "Run `clickup-axi lists --space \"<name|id>\"` to list a space's lists")
	return 0
}

func cmdLists(args []string, c *clickup.Client, out io.Writer) int {
	var (
		space    string
		spaceSet bool
		archived bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--space":
			if spaceSet {
				output.WriteError(out, "--space may only be provided once",
					"Run `clickup-axi lists --space \"<name|id>\"`")
				return 2
			}
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--space needs a value",
					"Run `clickup-axi lists --space \"<name|id>\"`")
				return 2
			}
			space, spaceSet = args[i], true
		case "--archived":
			archived = true
		case "--help", "-h":
			fmt.Fprintln(out, listsHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for lists\n  valid flags: --space, --archived", args[i]),
					"Run `clickup-axi lists --space \"<name|id>\"`")
			} else {
				output.WriteError(out, fmt.Sprintf("unexpected argument %q for lists", args[i]),
					"Run `clickup-axi lists --space \"<name|id>\"`")
			}
			return 2
		}
	}
	if !spaceSet {
		output.WriteError(out, "lists needs --space (a project to inspect)",
			"Run `clickup-axi lists --space \"<name|id>\"`")
		return 2
	}

	team, err := c.SelectTeam()
	if err != nil {
		return renderAPIError(out, err)
	}
	resolved, apiErr := c.ResolveSpace(team.ID, space)
	if apiErr != nil {
		return renderAPIError(out, apiErr)
	}
	refs, apiErr := c.GetSpaceLists(resolved.ID, archived)
	if apiErr != nil {
		return renderAPIError(out, apiErr)
	}
	sortListRefs(refs)

	state := "active"
	if archived {
		state = "archived"
	}
	spaceLabel := resolved.ID
	if resolved.Name != "" {
		spaceLabel = fmt.Sprintf("%s %q", resolved.ID, resolved.Name)
	}
	if len(refs) == 0 {
		fmt.Fprintf(out, "lists: 0 %s lists in space %s\n", state, spaceLabel)
		output.WriteHelp(out, listsModeHint(archived))
		return 0
	}
	fmt.Fprintf(out, "count: %d %s list%s in space %s\n", len(refs), state, pluralS(len(refs)), spaceLabel)
	fmt.Fprintf(out, "lists[%d]{id,name,folder}:\n", len(refs))
	for _, ref := range refs {
		fmt.Fprintf(out, "  %s,%s,%s\n", output.ToonCell(ref.ID), output.ToonCell(ref.Name), output.ToonCell(ref.Folder))
	}
	output.WriteHelp(out, listsModeHint(archived))
	return 0
}

func sortListRefs(refs []clickup.ListRef) {
	sort.Slice(refs, func(i, j int) bool {
		left, right := refs[i], refs[j]
		leftFolderless := left.Folder == clickup.FolderlessList
		rightFolderless := right.Folder == clickup.FolderlessList
		if leftFolderless != rightFolderless {
			return leftFolderless
		}
		for _, pair := range [][2]string{{left.Folder, right.Folder}, {left.Name, right.Name}, {left.ID, right.ID}} {
			a, b := strings.ToLower(pair[0]), strings.ToLower(pair[1])
			if a != b {
				return a < b
			}
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
}

func listsModeHint(archived bool) string {
	if archived {
		return "Run `clickup-axi lists --space \"<name|id>\"` to see active lists"
	}
	return "Run `clickup-axi lists --space \"<name|id>\" --archived` to see archived lists"
}
