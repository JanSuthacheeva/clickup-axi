package main

import (
	"fmt"
	"io"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
)

const tasksHelp = `clickup-axi tasks [id | edit <id>] [flags]

Without arguments: your open tasks - everything assigned to you in
your workspace, including subtasks.
With an id: that task's details and newest comments. Internal ids
(86ey3tx8m) and custom ids (HGAI-2316) both work: the id is tried as
internal first, then as custom. Set CLICKUP_AXI_CUSTOM_IDS=1 to skip
the internal attempt when your workspace always uses custom ids.

view flags (with an id):
  --comments N   comments to include (default 3)
  --no-comments  skip comments
  --full         complete description and all fetched comments

edit <id> (mutations; "edit" is a reserved word, not an id):
  --status "<status>"  change status; valid statuses are echoed
                       when the status does not match

examples:
  clickup-axi tasks
  clickup-axi tasks HGAI-2316
  clickup-axi tasks 86ey3tx8m --full
  clickup-axi tasks edit HGAI-2316 --status "in review"`

func cmdTasks(args []string, c *clickup.Client, out io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		case "edit":
			return cmdTaskEdit(args[1:], c, out)
		}
		// Any other argument means a task id (plus detail-view flags).
		return cmdTaskView(args, c, out)
	}

	u, err := c.GetUser()
	if err != nil {
		return renderAPIError(out, err)
	}
	teams, err := c.GetTeams()
	if err != nil {
		return renderAPIError(out, err)
	}
	switch {
	case len(teams) == 0:
		writeError(out, "no workspaces are visible to this token")
		return 1
	case len(teams) > 1:
		writeError(out, fmt.Sprintf("%d workspaces are visible and tasks cannot pick one yet", len(teams)),
			"Run `clickup-axi` to see the workspaces")
		return 1
	}

	tasks, err := c.GetTeamTasks(teams[0].ID, u.ID)
	if err != nil {
		return renderAPIError(out, err)
	}
	if len(tasks) == 0 {
		fmt.Fprintf(out, "tasks: 0 open tasks assigned to %s in %s\n", u.Username, teams[0].Name)
		return 0
	}

	suffix := ""
	if len(tasks) == clickup.TeamTasksPageSize {
		suffix = " (first page; more may exist)"
	}
	fmt.Fprintf(out, "tasks: %d open tasks assigned to %s%s\n", len(tasks), u.Username, suffix)
	fmt.Fprintf(out, "tasks[%d]{id,title,status,due}:\n", len(tasks))
	for i := range tasks {
		t := &tasks[i]
		fmt.Fprintf(out, "  %s,%s,%s,%s\n", displayID(t), toonCell(t.Name), toonCell(t.Status.Status), t.DueDate.Date())
	}
	writeHelp(out, "Run `clickup-axi tasks <id>` for details and comments")
	return 0
}
