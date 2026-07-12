package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const tasksHelp = `clickup-axi tasks [id | create "<name>" | edit <id> | comment <id> | move <id> | close <id>] [flags]

Without arguments: open tasks assigned to you in your workspace,
including subtasks. --assignee lists a teammate's open tasks instead;
--space narrows the listing to one space (project).
With an id: that task's details, parent, direct subtasks, and newest
comments. Internal ids
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
  --page N           page of the listing (100 tasks per page; default 1);
                     a full page hints at the next one
  --fields <names>   add columns to the listing (comma-separated);
                     available: assignees, priority, tags, list, url

view flags (with an id):
  --comments N     comments to include (default 3)
  --no-comments    skip comments
  --full           complete description and all fetched comments
  --fields <names> add fields the view omits (url); fields already
                   shown are silently absorbed

create "<name>" (make a new task; "create" is a reserved word):
  --list <name|id>       target list (required); a name needs --space
  --space <name|id>      the space (project) whose list --list names
  --status "<status>"    initial status (else the list's default)
  --assignee <who>       assign on creation (repeatable, comma-separated);
                         who = me | member name | id
  --priority <p>         urgent | high | normal | low | none (= unset)
  --due <date>           due date (YYYY-MM-DD or +3days/-1week)
  --body "<markdown>"    description
  --parent <id>          create as a subtask; the list comes from the
                         parent, so --list is optional
  --tag <tag>            add an existing space tag (repeatable, comma-separated)

edit <id> (mutations; "edit" is a reserved word, not an id):
  --status "<status>"    change status; valid statuses are echoed
                         when the status does not match
  --assignee <who>       add an assignee (repeatable, comma-separated);
                         who = me | member name | id
  --unassign <who>       remove an assignee (repeatable, comma-separated)
  --priority <p>         urgent | high | normal | low | none (= clear)
  --name "<title>"       rename the task
  --due <date>           set due (YYYY-MM-DD or +3days/-1week), or none
  --body "<markdown>"    replace the description
  --append-body "<md>"   append to the description instead
  --parent <task-id>     make this a subtask or move it under another
                         parent in the same list; none is unsupported
  --add-tag <tag>        add an existing space tag (repeatable, comma-separated)
  --remove-tag <tag>     remove a tag (repeatable, comma-separated)

comment <id> ("comment" is a reserved word, not an id):
  --text "<text>"  add a comment to the task

move <id> (change the task's home list; "move" is a reserved word):
  --list <name|id>     target list (required); a name needs --space
  --space <name|id>    the space (project) whose list --list names
  --status "<status>"  the status to land in, only when the target
                       list lacks the task's current status; the
                       target's statuses are echoed when one is needed

close <id> (set the list's closed status; "close" is a reserved word):
  --yes  actually close the task; without it the command is a dry run
         that states what would change. Show the user the dry run and
         pass --yes only after they confirmed.

examples:
  clickup-axi tasks
  clickup-axi tasks --assignee ting
  clickup-axi tasks --assignee ting --space "Webshop"
  clickup-axi tasks --fields assignees,priority
  clickup-axi tasks --page 2
  clickup-axi tasks HGAI-2316
  clickup-axi tasks 86ey3tx8m --full
  clickup-axi tasks create "Fix login flow" --list "Sprint 12" --space "Webshop"
  clickup-axi tasks create "Fix login flow" --list 901234 --priority high --assignee me
  clickup-axi tasks create "Test the redirect" --parent HGAI-2316
  clickup-axi tasks edit HGAI-2316 --status "in review"
  clickup-axi tasks edit HGAI-2316 --assignee ting --unassign me
  clickup-axi tasks edit HGAI-2316 --priority high --due 2026-08-01
  clickup-axi tasks edit HGAI-2316 --parent HGAI-2300
  clickup-axi tasks edit HGAI-2316 --append-body "QA: repro steps ..." --add-tag qa
  clickup-axi tasks comment HGAI-2316 --text "Deployed to staging"
  clickup-axi tasks move HGAI-2316 --list "Sprint 13" --space "Webshop"
  clickup-axi tasks move HGAI-2316 --list 901234 --status "backlog"
  clickup-axi tasks close HGAI-2316
  clickup-axi tasks close HGAI-2316 --yes`

func cmdTasks(args []string, c *clickup.Client, out io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		case "create":
			return cmdTaskCreate(args[1:], c, out)
		case "edit":
			return cmdTaskEdit(args[1:], c, out)
		case "comment":
			return cmdTaskComment(args[1:], c, out)
		case "move":
			return cmdTaskMove(args[1:], c, out)
		case "close":
			return cmdTaskClose(args[1:], c, out)
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
	var fieldTokens []string
	fieldsSet := false
	page := 1
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--assignee", "--space", "--fields":
			flag := args[i]
			i++
			// A flag-shaped next token is a missing value, not a value:
			// swallowing it would produce a misleading resolver error
			// about a member or space named like the flag.
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("%s needs a value", flag),
					fmt.Sprintf("Run `clickup-axi tasks %s <value>`", flag))
				return 2
			}
			if flag == "--fields" {
				fieldTokens = append(fieldTokens, splitTokens(args[i])...)
				fieldsSet = true
				continue
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
		case "--page":
			i++
			// 1-based for the agent; a bad value is decidable locally,
			// so it is a usage error caught before any API call.
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--page needs a positive number",
					"Run `clickup-axi tasks --page 2` for the second page")
				return 2
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				output.WriteError(out, fmt.Sprintf("--page needs a positive number (got %q)", args[i]),
					"Run `clickup-axi tasks --page 2` for the second page")
				return 2
			}
			page = n
		case "--comments", "--no-comments", "--full":
			output.WriteError(out, fmt.Sprintf("%s needs a task id", args[i]),
				fmt.Sprintf("Run `clickup-axi tasks <id> %s`", args[i]))
			return 2
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks\n  valid: --assignee, --space, --page, --fields (listing) or --comments N, --no-comments, --full, --fields (with a task id)", args[i]))
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

	// A --fields value that carried no names ("," or blanks) is a
	// missing value; unknown names are decidable locally - both are
	// usage errors caught before any API call.
	if fieldsSet && len(fieldTokens) == 0 {
		output.WriteError(out, "--fields needs a value",
			"Run `clickup-axi tasks --fields assignees,priority`")
		return 2
	}
	extra, unknown := resolveTaskFields(fieldTokens, []string{"id", "title", "status", "due"})
	if len(unknown) > 0 {
		return renderUnknownFields(out, unknown, "Run `clickup-axi tasks --fields assignees,priority`")
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

	// The flag is 1-based for the agent; the endpoint pages from 0.
	q.Page = page - 1

	tasks, lastPage, err := c.GetTeamTasksPage(team.ID, q)
	if err != nil {
		return renderAPIError(out, err)
	}

	// base reconstructs the current invocation so paging hints carry
	// the filters forward and the next call is copy-paste.
	base := "clickup-axi tasks"
	if assignee != "me" {
		base += fmt.Sprintf(" --assignee %q", assignee)
	}
	if space != "" {
		base += fmt.Sprintf(" --space %q", space)
	}
	if len(extra) > 0 {
		base += " --fields " + fieldNamesOf(extra)
	}

	if len(tasks) == 0 {
		if page > 1 {
			// Beyond the end: the zero is definitive, and the way back
			// to real results is one hint away.
			fmt.Fprintf(out, "tasks: 0 open tasks %s%s on page %d\n", scope, where, page)
			output.WriteHelp(out, fmt.Sprintf("Run `%s` for the first page", base))
			return 0
		}
		fmt.Fprintf(out, "tasks: 0 open tasks %s%s\n", scope, where)
		return 0
	}

	// The page qualifier appears only when it carries information: a
	// single-page listing (the common case) stays unqualified.
	suffix := ""
	if !lastPage {
		suffix = fmt.Sprintf(" (page %d; more may exist)", page)
	} else if page > 1 {
		suffix = fmt.Sprintf(" (page %d; last page)", page)
	}
	// The summary key must differ from the array key below: duplicate
	// keys at one level are invalid strict TOON (and `count:` matches
	// the AXI aggregate example).
	fmt.Fprintf(out, "count: %d open task%s %s%s%s\n", len(tasks), pluralS(len(tasks)), scope, spaceSuffix, suffix)
	fmt.Fprintf(out, "tasks[%d]{%s}:\n", len(tasks), fieldsHeader("id,title,status,due", extra))
	for i := range tasks {
		t := &tasks[i]
		fmt.Fprintf(out, "  %s,%s,%s,%s%s\n", displayID(t), output.ToonCell(t.Name), output.ToonCell(t.Status.Status), t.DueDate.Date(), fieldsCells(t, extra))
	}
	help := []string{"Run `clickup-axi tasks <id>` for details and comments"}
	if !lastPage {
		// A truncated listing must be escapable (AXI section 3): paging
		// is the guaranteed route, narrowing the cheaper one.
		help = append(help, fmt.Sprintf("Run `%s --page %d` for the next page", base, page+1))
		if space == "" {
			cmd := "clickup-axi tasks --space \"<name>\""
			if assignee != "me" {
				cmd = fmt.Sprintf("clickup-axi tasks --assignee %q --space \"<name>\"", assignee)
			}
			help = append(help, fmt.Sprintf("Run `%s` to narrow the listing to one project", cmd))
		}
	}
	output.WriteHelp(out, help...)
	return 0
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
