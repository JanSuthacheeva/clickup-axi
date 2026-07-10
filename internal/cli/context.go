package cli

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

// contextTaskCap bounds the dashboard: it loads on every session, so
// the output stays a screenful (AXI principle 7 - ruthlessly minimize).
const contextTaskCap = 5

// contextBudget caps the whole ClickUp fetch. A session start must
// never hang on a slow network; past the budget the dashboard degrades
// instead. Variable so tests can shrink it.
var contextBudget = 3 * time.Second

var contextHelpText = fmt.Sprintf(`clickup-axi context

The session-start hook payload: prints a compact dashboard (your %d most
urgent open tasks) for the agent harness to inject as ambient context.
Installed as a SessionStart hook by `+"`clickup-axi setup`"+`; not meant
to be run by hand. Always exits 0: a broken dashboard must not break
a session start.

examples:
  clickup-axi setup --global    install the hook that runs this`, contextTaskCap)

func cmdContext(args []string, c *clickup.Client, out io.Writer) int {
	for _, a := range args {
		switch a {
		case "--help", "-h":
			fmt.Fprintln(out, contextHelpText)
			return 0
		default:
			output.WriteError(out, fmt.Sprintf("unknown argument %q for context\n  valid: none (--help only)", a),
				"Run `clickup-axi context` with no flags")
			return 2
		}
	}

	fmt.Fprintln(out, "clickup-axi: ClickUp CLI (tasks, search, edit, comment)")

	type fetched struct {
		tasks    []clickup.Task
		lastPage bool
		err      *clickup.APIError
	}
	ch := make(chan fetched, 1)
	go func() {
		team, err := c.SelectTeam()
		if err != nil {
			ch <- fetched{err: err}
			return
		}
		u, err := c.GetUser()
		if err != nil {
			ch <- fetched{err: err}
			return
		}
		tasks, last, err := c.GetTeamTasksPage(team.ID, clickup.TaskQuery{Assignees: []int64{u.ID}})
		ch <- fetched{tasks: tasks, lastPage: last, err: err}
	}()

	var f fetched
	select {
	case f = <-ch:
	case <-time.After(contextBudget):
		return contextUnavailable(out, "right now",
			"Run `clickup-axi tasks` to retry your open tasks")
	}
	if f.err != nil {
		switch {
		case f.err.Message == clickup.ErrNoAuth:
			return contextUnavailable(out, "(not authenticated)",
				"Ask the user to run `clickup-axi auth login` in their terminal")
		case strings.Contains(f.err.Message, clickup.WorkspaceEnv):
			return contextUnavailable(out, "("+f.err.Message+")",
				"Run `clickup-axi tasks` after setting the workspace")
		default:
			return contextUnavailable(out, "right now",
				"Run `clickup-axi tasks` to retry your open tasks")
		}
	}

	if len(f.tasks) == 0 {
		fmt.Fprintln(out, "tasks: 0 open tasks assigned to you")
		output.WriteHelp(out,
			"Run `clickup-axi tasks <id>` for details and comments",
			"Run `clickup-axi --help` for all commands")
		return 0
	}

	sort.SliceStable(f.tasks, func(i, j int) bool {
		return dueSortKey(&f.tasks[i]) < dueSortKey(&f.tasks[j])
	})
	shown := f.tasks
	total := strconv.Itoa(len(f.tasks))
	if !f.lastPage {
		total += "+"
	}
	firstHelp := "Run `clickup-axi tasks` for your open tasks"
	if len(shown) > contextTaskCap {
		shown = shown[:contextTaskCap]
		fmt.Fprintf(out, "tasks[%d/%s]{id,title,status,due}:\n", len(shown), total)
		firstHelp = fmt.Sprintf("Run `clickup-axi tasks` for all %s open tasks", total)
	} else {
		fmt.Fprintf(out, "tasks[%d]{id,title,status,due}:\n", len(shown))
	}
	for i := range shown {
		t := &shown[i]
		fmt.Fprintf(out, "  %s,%s,%s,%s\n", displayID(t), output.ToonCell(t.Name), output.ToonCell(t.Status.Status), t.DueDate.Date())
	}
	output.WriteHelp(out,
		firstHelp,
		"Run `clickup-axi tasks <id>` for details and comments",
		"Run `clickup-axi --help` for all commands")
	return 0
}

// contextUnavailable is every degraded path: the discovery line has
// already printed, the task block is replaced by one line, and the
// exit code stays 0 so the harness still injects the output.
func contextUnavailable(out io.Writer, reason, hint string) int {
	fmt.Fprintf(out, "tasks: unavailable %s\n", reason)
	output.WriteHelp(out, hint, "Run `clickup-axi --help` for all commands")
	return 0
}

// dueSortKey orders tasks due-soonest first; no due date sorts last.
func dueSortKey(t *clickup.Task) int64 {
	n, err := strconv.ParseInt(string(t.DueDate), 10, 64)
	if err != nil {
		return math.MaxInt64
	}
	return n
}
