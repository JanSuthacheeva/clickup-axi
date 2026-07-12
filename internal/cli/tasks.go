package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const tasksHelp = `clickup-axi tasks [id | edit <id> | comment <id>] [flags]

Without arguments: open tasks assigned to you in your workspace,
including subtasks. --assignee lists a teammate's open tasks instead;
--space narrows the listing to one space (project).
With an id: that task's details and newest comments. Internal ids
(86ey3tx8m) and custom ids (HGAI-2316) both work: the id is tried as
internal first, then as custom. Set CLICKUP_AXI_CUSTOM_IDS=1 to skip
the internal attempt when your workspace always uses custom ids.
With more than one workspace visible, set CLICKUP_AXI_WORKSPACE=<id>
to pin the one to use; run ` + "`clickup-axi`" + ` to list the ids.

list flags (without an id):
  --assignee <who>   me (default), all, a member's name, or an id;
                     names resolve case-insensitively; all needs --space
  --space <name|id>  only tasks in this space (project); names
                     resolve case-insensitively

view flags (with an id):
  --comments N   comments to include (default 3)
  --no-comments  skip comments
  --full         complete description and all fetched comments

edit <id> (mutations; "edit" is a reserved word, not an id):
  --status "<status>"    change status; valid statuses are echoed
                         when the status does not match
  --assignee <who>       add an assignee (repeatable, comma-separated);
                         who = me | member name | id
  --unassign <who>       remove an assignee (repeatable, comma-separated)
  --priority <p>         urgent | high | normal | low | none (= clear)
  --name "<title>"       rename the task
  --due <date>           set the due date (YYYY-MM-DD) or none (= clear)
  --body "<markdown>"    replace the description
  --append-body "<md>"   append to the description instead
  --add-tag <tag>        add an existing space tag (repeatable, comma-separated)
  --remove-tag <tag>     remove a tag (repeatable, comma-separated)

comment <id> ("comment" is a reserved word, not an id):
  --text "<text>"  add a comment to the task

examples:
  clickup-axi tasks
  clickup-axi tasks --assignee ting
  clickup-axi tasks --assignee ting --space "Webshop"
  clickup-axi tasks HGAI-2316
  clickup-axi tasks 86ey3tx8m --full
  clickup-axi tasks edit HGAI-2316 --status "in review"
  clickup-axi tasks edit HGAI-2316 --assignee ting --unassign me
  clickup-axi tasks edit HGAI-2316 --priority high --due 2026-08-01
  clickup-axi tasks edit HGAI-2316 --append-body "QA: repro steps ..." --add-tag qa
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
		if !strings.HasPrefix(args[0], "-") {
			// A non-flag first argument is a task id (plus view flags);
			// a flag first means the list form with filters.
			return cmdTaskView(args, c, out)
		}
	}
	return cmdTasksList(args, c, out)
}

// cmdTasksList renders the workspace's open tasks: the user's own by
// default, a teammate's via --assignee, one space via --space. The same
// resolvers as search back both flags, so names work and every miss
// inlines candidates for a one-step retry.
func cmdTasksList(args []string, c *clickup.Client, out io.Writer) int {
	assignee := "me"
	var space string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--assignee", "--space":
			flag := args[i]
			i++
			if i >= len(args) {
				output.WriteError(out, fmt.Sprintf("%s needs a value", flag),
					fmt.Sprintf("Run `clickup-axi tasks %s <value>`", flag))
				return 2
			}
			if flag == "--space" {
				space = args[i]
				continue
			}
			// me/all are keywords; anything else is resolved later
			// (numeric id directly, otherwise by member name).
			switch {
			case strings.EqualFold(args[i], "me"):
				assignee = "me"
			case strings.EqualFold(args[i], "all"):
				assignee = "all"
			default:
				assignee = args[i]
			}
		case "--comments", "--no-comments", "--full":
			output.WriteError(out, fmt.Sprintf("%s needs a task id", args[i]),
				fmt.Sprintf("Run `clickup-axi tasks <id> %s`", args[i]))
			return 2
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks\n  valid: --assignee, --space (listing) or --comments N, --no-comments, --full (with a task id)", args[i]))
				return 2
			}
			output.WriteError(out, fmt.Sprintf("unexpected argument %q: a task id does not combine with listing flags", args[i]),
				fmt.Sprintf("Run `clickup-axi tasks %s` to view that task", args[i]))
			return 2
		}
	}

	if assignee == "all" && space == "" {
		output.WriteError(out, "listing all assignees needs --space (a workspace-wide scan is unbounded)",
			"Run `clickup-axi tasks --assignee all --space \"<name>\"`")
		return 2
	}

	team, err := c.SelectTeam()
	if err != nil {
		return renderAPIError(out, err)
	}

	var q clickup.TaskQuery
	scope := "for any assignee"
	if assignee != "all" {
		u, apiErr := resolveAssignee(assignee, team, c)
		if apiErr != nil {
			return renderAPIError(out, apiErr)
		}
		q.Assignees = []int64{u.ID}
		label := assignee
		if u.Username != "" {
			label = u.Username
		}
		scope = "assigned to " + label
	}
	where := " in " + team.Name
	var spaceSuffix string
	if space != "" {
		sp, apiErr := c.ResolveSpace(team.ID, space)
		if apiErr != nil {
			return renderAPIError(out, apiErr)
		}
		q.SpaceIDs = []string{sp.ID}
		spaceLabel := sp.ID
		if sp.Name != "" {
			spaceLabel = fmt.Sprintf("%s %q", sp.ID, sp.Name)
		}
		where = " in space " + spaceLabel
		spaceSuffix = " in space " + spaceLabel
	}

	tasks, _, err := c.GetTeamTasksPage(team.ID, q)
	if err != nil {
		return renderAPIError(out, err)
	}
	if len(tasks) == 0 {
		fmt.Fprintf(out, "tasks: 0 open tasks %s%s\n", scope, where)
		return 0
	}

	suffix := ""
	if len(tasks) == clickup.TeamTasksPageSize {
		suffix = " (first page; more may exist)"
	}
	// The summary key must differ from the array key below: duplicate
	// keys at one level are invalid strict TOON (and `count:` matches
	// the AXI aggregate example).
	fmt.Fprintf(out, "count: %d open task%s %s%s%s\n", len(tasks), pluralS(len(tasks)), scope, spaceSuffix, suffix)
	fmt.Fprintf(out, "tasks[%d]{id,title,status,due}:\n", len(tasks))
	for i := range tasks {
		t := &tasks[i]
		fmt.Fprintf(out, "  %s,%s,%s,%s\n", displayID(t), output.ToonCell(t.Name), output.ToonCell(t.Status.Status), t.DueDate.Date())
	}
	output.WriteHelp(out, "Run `clickup-axi tasks <id>` for details and comments")
	return 0
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
