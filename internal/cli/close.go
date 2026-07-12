package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

// cmdTaskClose closes a task by setting the list's closed-type status.
// Closing is the first destructive operation on the agent surface, so
// it is guarded: without --yes the command is a dry run that states
// exactly what would change and writes nothing. The binary never
// prompts (AXI: flags-only operations); the reassurance loop runs
// through the agent, which the generated skill instructs to relay the
// dry run and add --yes only after the user confirmed.
func cmdTaskClose(args []string, c *clickup.Client, out io.Writer) int {
	var id string
	yes := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--yes":
			yes = true
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks close\n  valid: --yes", args[i]))
				return 2
			}
			if id != "" {
				output.WriteError(out, "tasks close takes exactly one task id")
				return 2
			}
			id = args[i]
		}
	}
	if id == "" {
		output.WriteError(out, "tasks close needs a task id",
			"Run `clickup-axi tasks close <id>` to preview the change, then add --yes")
		return 2
	}

	t, err := c.GetTaskByID(id)
	if err != nil {
		return renderAPIError(out, err)
	}
	// Already closed is a stated no-op, decided from the task status's
	// own type - no list fetch, no PUT, with or without --yes.
	if strings.EqualFold(t.Status.Type, "closed") {
		fmt.Fprintf(out, "task: %s no changes (already closed)\n", displayID(t))
		return 0
	}

	// The target is the list's closed-type status. Unlike edit's status
	// check (which degrades silently because the PUT stays the gate),
	// close cannot proceed without it, so a failed list fetch is a hard
	// error. ClickUp mandates one Closed status per list and orders it
	// last; taking the last closed-type entry tolerates API drift.
	l, err := c.GetList(t.List.ID)
	if err != nil {
		return renderAPIError(out, err)
	}
	target := ""
	for _, s := range l.Statuses {
		if strings.EqualFold(s.Type, "closed") {
			target = s.Status
		}
	}
	if target == "" {
		output.WriteError(out, fmt.Sprintf("list %s %q has no closed status", l.ID, l.Name),
			fmt.Sprintf("Run `clickup-axi tasks edit %s --status \"<status>\"` to set a status directly", displayID(t)))
		return 1
	}

	if !yes {
		// The preview names the task so the agent (and the user it
		// relays to) can verify the target before confirming.
		fmt.Fprintf(out, "task: %s %q would be closed (dry run, nothing changed)\n", displayID(t), t.Name)
		fmt.Fprintf(out, "  status: %s -> %s\n", t.Status.Status, target)
		fmt.Fprintln(out, "  a closed task leaves the default tasks, search, and context listings")
		output.WriteHelp(out,
			fmt.Sprintf("Run `clickup-axi tasks close %s --yes` to close it", displayID(t)),
			fmt.Sprintf("Run `clickup-axi tasks %s` to review it first", displayID(t)))
		return 0
	}

	// The fetch above resolved any custom id; mutate via internal id.
	// The status was read from the list moments ago, so a failure here
	// is exceptional (outage, workflow restriction) and gets the raw
	// translated error.
	if err := c.UpdateTask(t.ID, clickup.TaskEdit{Status: target}); err != nil {
		return renderAPIError(out, err)
	}
	fmt.Fprintf(out, "task: %s closed: %s -> %s\n", displayID(t), t.Status.Status, target)
	output.WriteHelp(out, fmt.Sprintf("Run `clickup-axi tasks edit %s --status %q` to reopen it", displayID(t), t.Status.Status))
	return 0
}
