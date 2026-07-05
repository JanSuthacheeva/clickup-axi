package main

import (
	"fmt"
	"io"
)

const tasksHelp = `clickup-axi tasks

Lists your open tasks: everything assigned to you in your workspace,
including subtasks. No flags.

example:
  clickup-axi tasks`

func cmdTasks(args []string, c *client, out io.Writer) int {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprintln(out, tasksHelp)
			return 0
		}
		writeError(out, fmt.Sprintf("tasks takes no arguments, got %q", a),
			"Run `clickup-axi tasks`")
		return 2
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
	writeHelp(out, "Run `clickup-axi task view <id>` for details and comments")
	return 0
}
