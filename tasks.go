package main

import (
	"fmt"
	"io"
)

const tasksHelp = `clickup-axi tasks [id] [flags]

Without an id: your open tasks - everything assigned to you in your
workspace, including subtasks.
With an id: that task's details and newest comments. Internal ids
(86ey3tx8m) and custom ids (HGAI-2316) both work: the id is tried as
internal first, then as custom. Set CLICKUP_AXI_CUSTOM_IDS=1 to skip
the internal attempt when your workspace always uses custom ids.

flags (with an id):
  --comments N   comments to include (default 3)
  --no-comments  skip comments
  --full         complete description and all fetched comments

examples:
  clickup-axi tasks
  clickup-axi tasks HGAI-2316
  clickup-axi tasks 86ey3tx8m --full`

func cmdTasks(args []string, c *client, out io.Writer) int {
	if len(args) > 0 {
		if args[0] == "--help" || args[0] == "-h" {
			fmt.Fprintln(out, tasksHelp)
			return 0
		}
		// Any argument means a task id (plus detail-view flags).
		return cmdTaskView(args, c, out)
	}

	u, err := c.getUser()
	if err != nil {
		return renderAPIError(out, err)
	}
	teams, err := c.getTeams()
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

	tasks, err := c.getTeamTasks(teams[0].ID, u.ID)
	if err != nil {
		return renderAPIError(out, err)
	}
	if len(tasks) == 0 {
		fmt.Fprintf(out, "tasks: 0 open tasks assigned to %s in %s\n", u.Username, teams[0].Name)
		return 0
	}

	suffix := ""
	if len(tasks) == teamTasksPageSize {
		suffix = " (first page; more may exist)"
	}
	fmt.Fprintf(out, "tasks: %d open tasks assigned to %s%s\n", len(tasks), u.Username, suffix)
	fmt.Fprintf(out, "tasks[%d]{id,title,status,due}:\n", len(tasks))
	for _, t := range tasks {
		fmt.Fprintf(out, "  %s,%s,%s,%s\n", t.ID, toonCell(t.Name), toonCell(t.Status.Status), t.DueDate.date())
	}
	writeHelp(out, "Run `clickup-axi tasks <id>` for details and comments")
	return 0
}
