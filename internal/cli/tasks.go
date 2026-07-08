package cli

import (
	"fmt"
	"io"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const tasksHelp = `clickup-axi tasks [id | edit <id> | comment <id>] [flags]

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

comment <id> ("comment" is a reserved word, not an id):
  --text "<text>"  add a comment to the task

examples:
  clickup-axi tasks
  clickup-axi tasks HGAI-2316
  clickup-axi tasks 86ey3tx8m --full
  clickup-axi tasks edit HGAI-2316 --status "in review"
  clickup-axi tasks comment HGAI-2316 --text "Deployed to staging"`

func cmdTasks(args []string, c *clickup.Client, out io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		case "edit":
			return cmdTaskEdit(args[1:], c, out)
		case "comment":
			return cmdTaskComment(args[1:], c, out)
		}
		// Any other argument means a task id (plus detail-view flags).
		return cmdTaskView(args, c, out)
	}

	u, err := c.GetUser()
	if err != nil {
		return renderAPIError(out, err)
	}
	team, err := c.SelectTeam()
	if err != nil {
		return renderAPIError(out, err)
	}

	tasks, err := c.GetTeamTasks(team.ID, u.ID)
	if err != nil {
		return renderAPIError(out, err)
	}
	if len(tasks) == 0 {
		fmt.Fprintf(out, "tasks: 0 open tasks assigned to %s in %s\n", u.Username, team.Name)
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
		fmt.Fprintf(out, "  %s,%s,%s,%s\n", displayID(t), output.ToonCell(t.Name), output.ToonCell(t.Status.Status), t.DueDate.Date())
	}
	output.WriteHelp(out, "Run `clickup-axi tasks <id>` for details and comments")
	return 0
}
