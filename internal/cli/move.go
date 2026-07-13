package cli

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

// cmdTaskMove changes a task's home list. The server matches statuses
// across lists by name, so the pre-flight does too: a move whose status
// exists in the target just moves, one whose status is missing must
// name the landing status via --status (never a silent remap), and a
// --status that isn't needed is refused rather than smuggled into a
// second write behind one command.
// entryStatus is a list's open-type status - where new tasks land, so
// the safe default to suggest. "" when the list has none.
func entryStatus(l *clickup.List) string {
	for _, s := range l.Statuses {
		if strings.EqualFold(s.Type, "open") {
			return s.Status
		}
	}
	return ""
}

func cmdTaskMove(args []string, c *clickup.Client, out io.Writer) int {
	var id, list, space, status string
	statusSet := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--list":
			i++
			// Guarded flags reject a flag-shaped next token as a missing
			// value - the same rule create and edit apply.
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--list needs a value", "Run `clickup-axi tasks move <id> --list <name|id>`")
				return 2
			}
			list = args[i]
		case "--space":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--space needs a value", "Run `clickup-axi tasks move <id> --list \"<list>\" --space \"<space>\"`")
				return 2
			}
			space = args[i]
		case "--status":
			statusSet = true
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--status needs a value", "Run `clickup-axi tasks move <id> --list <name|id> --status \"<status>\"`")
				return 2
			}
			status = args[i]
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks move\n  valid: --list, --space, --status", args[i]))
				return 2
			}
			if id != "" {
				output.WriteError(out, "tasks move takes exactly one task id")
				return 2
			}
			id = args[i]
		}
	}
	if id == "" || list == "" {
		output.WriteError(out, "tasks move needs a task id and --list (the target list)",
			"Run `clickup-axi tasks move <id> --list <name|id>`",
			"Run `clickup-axi lists --space \"<space>\"` to discover lists")
		return 2
	}
	// A list name is only unique within one space (same rule as create);
	// decidable locally, so it is a usage error before any API call.
	if !allDigits(list) && space == "" {
		output.WriteError(out, "--list by name needs --space (list names are only unique within one space)",
			"Run `clickup-axi tasks move <id> --list \"<list>\" --space \"<space>\"`",
			"Or use the list id from `clickup-axi lists --space \"<space>\"`")
		return 2
	}

	t, err := c.GetTaskByID(id)
	if err != nil {
		return renderAPIError(out, err)
	}

	// Resolve the target to a concrete list id first; already-home is a
	// stated no-op decided before anything is written. A name is only
	// compared after resolving through its space - the same name can
	// exist in another space and must not be mistaken for the current
	// list.
	var target *clickup.List
	var team *clickup.Team
	if allDigits(list) {
		if list == t.List.ID {
			fmt.Fprintf(out, "task: %s no changes (already in list %s %q)\n", displayID(t), t.List.ID, t.List.Name)
			return 0
		}
		l, lerr := c.GetList(list)
		if lerr != nil {
			if lerr.Status == http.StatusNotFound {
				output.WriteError(out, fmt.Sprintf("list %q not found", list),
					"Run `clickup-axi lists --space \"<space>\"` to discover list ids")
				return 1
			}
			return renderAPIError(out, lerr)
		}
		target = l
	} else {
		tm, terr := c.SelectTeam()
		if terr != nil {
			return renderAPIError(out, terr)
		}
		team = tm
		sp, serr := c.ResolveSpace(team.ID, space)
		if serr != nil {
			return renderAPIError(out, serr)
		}
		ref, rerr := c.ResolveList(sp.ID, list)
		if rerr != nil {
			return renderAPIError(out, rerr)
		}
		if ref.ID == t.List.ID {
			fmt.Fprintf(out, "task: %s no changes (already in list %s %q)\n", displayID(t), t.List.ID, t.List.Name)
			return 0
		}
		l, lerr := c.GetList(ref.ID)
		if lerr != nil {
			return renderAPIError(out, lerr)
		}
		target = l
	}

	// The move endpoint matches statuses across lists by name and
	// rejects mappings for statuses the target already has, so exactly
	// one of three status paths applies: kept, explicitly remapped, or
	// refused with the target's vocabulary inlined.
	kept := containsFold(statusNames(target), t.Status.Status)
	var mappings []clickup.StatusMapping
	statusLine := fmt.Sprintf("  status: %s (kept)\n", t.Status.Status)
	switch {
	case kept && statusSet && !strings.EqualFold(status, t.Status.Status):
		output.WriteError(out, fmt.Sprintf("status %q exists in list %s %q, so the task keeps it on this move; --status only picks the landing status when the target list lacks the current one",
			t.Status.Status, target.ID, target.Name),
			fmt.Sprintf("Run `clickup-axi tasks move %s --list %s` and then `clickup-axi tasks edit %s --status %q` to change it",
				displayID(t), target.ID, displayID(t), status))
		return 1
	case !kept:
		if !statusSet {
			// A cautious agent stalls on "<status>" alone (asks the user
			// instead of acting), so the hint anchors on the target's
			// entry status - a concrete, safe landing spot.
			hint := fmt.Sprintf("Run `clickup-axi tasks move %s --list %s --status \"<status>\"` to pick the status it lands in", displayID(t), target.ID)
			if entry := entryStatus(target); entry != "" {
				hint = fmt.Sprintf("Run `clickup-axi tasks move %s --list %s --status %q` to land it in the entry status, or pick another of the statuses above", displayID(t), target.ID, entry)
			}
			output.WriteError(out, fmt.Sprintf("status %q does not exist in list %s %q\n  target list statuses: %s",
				t.Status.Status, target.ID, target.Name, strings.Join(statusNames(target), ", ")), hint)
			return 1
		}
		dest := ""
		destName := ""
		for _, s := range target.Statuses {
			if strings.EqualFold(s.Status, status) {
				dest, destName = s.ID, s.Status
			}
		}
		if dest == "" {
			output.WriteError(out, fmt.Sprintf("status %q does not exist in list %s %q\n  target list statuses: %s",
				status, target.ID, target.Name, strings.Join(statusNames(target), ", ")),
				fmt.Sprintf("Run `clickup-axi tasks move %s --list %s --status \"<status>\"` with one of the statuses above", displayID(t), target.ID))
			return 1
		}
		mappings = []clickup.StatusMapping{{SourceStatus: t.Status.ID, DestinationStatus: dest}}
		statusLine = fmt.Sprintf("  status: %s -> %s\n", t.Status.Status, destName)
	}

	if team == nil {
		tm, terr := c.SelectTeam()
		if terr != nil {
			return renderAPIError(out, terr)
		}
		team = tm
	}
	if merr := c.MoveTask(team.ID, t.ID, target.ID, mappings); merr != nil {
		return renderAPIError(out, merr)
	}

	// Self-contained confirmation (AXI section 9): both lists carry
	// their ids, so moving back needs no discovery step.
	fmt.Fprintf(out, "task: %s %q moved\n", displayID(t), t.Name)
	fmt.Fprintf(out, "  list: %s (%s) -> %s (%s)\n", t.List.Name, t.List.ID, target.Name, target.ID)
	fmt.Fprint(out, statusLine)
	return 0
}
