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
	"github.com/JanSuthacheeva/clickup-axi/internal/config"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

// contextTaskCap bounds the dashboard: it loads on every session, so
// the output stays a screenful (AXI principle 7 - ruthlessly minimize).
const contextTaskCap = 5

// contextBudget caps the whole ClickUp fetch. A session start must
// never hang on a slow network; past the budget the dashboard degrades
// instead. 5s fits the measured cold-start profile (a single ClickUp
// GET can take ~3s); an unreachable host still fails fast - the budget
// only bites while requests are genuinely in flight. Variable so tests
// can shrink it.
var contextBudget = 5 * time.Second

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

	fmt.Fprintln(out, "clickup-axi: ClickUp CLI (tasks, search, create, edit, comment)")

	// The list's name resolves concurrently with the task fetch: the
	// directive line below only lands when the agent can connect the
	// user's words ("the Sprint 45 list") to the configured default,
	// and only the name makes that connection.
	def, haveDefault := effectiveDefaultList()
	var defCh chan resolvedDefault
	if haveDefault {
		defCh = make(chan resolvedDefault, 1)
		go func() { defCh <- resolveDefaultList(c, def.Val) }()
	}

	type fetched struct {
		tasks    []clickup.Task
		lastPage bool
		err      *clickup.APIError
	}
	ch := make(chan fetched, 1)
	go func() {
		// SelectTeam and GetUser are independent - overlapping them
		// saves a round trip on the latency-critical session-start path.
		type teamResult struct {
			team *clickup.Team
			err  *clickup.APIError
		}
		tc := make(chan teamResult, 1)
		go func() {
			team, err := c.SelectTeam()
			tc <- teamResult{team: team, err: err}
		}()
		u, uErr := c.GetUser()
		tr := <-tc
		// The team error wins: it carries the workspace-pin recovery hint.
		if tr.err != nil {
			ch <- fetched{err: tr.err}
			return
		}
		if uErr != nil {
			ch <- fetched{err: uErr}
			return
		}
		// Prime the timezone from the user just fetched so the task page
		// render below does not issue a second, serial GetUser.
		c.SeedDateLocation(u.Timezone)
		tasks, last, err := c.GetTeamTasksPage(tr.team.ID, clickup.TaskQuery{Assignees: []int64{u.ID}})
		ch <- fetched{tasks: tasks, lastPage: last, err: err}
	}()

	timer := time.NewTimer(contextBudget)
	defer timer.Stop()
	var f fetched
	select {
	case f = <-ch:
	case <-timer.C:
		writeDefaultListLine(out, def, haveDefault, resolvedDefault{})
		return contextUnavailable(out, "right now",
			"Run `clickup-axi tasks` to retry your open tasks")
	}
	// The name lookup gets only what is left of the shared budget; a
	// slow or failed lookup degrades the line to the raw value.
	var res resolvedDefault
	if haveDefault {
		select {
		case res = <-defCh:
		case <-timer.C:
		}
	}
	writeDefaultListLine(out, def, haveDefault, res)
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
		// The header stays a plain row count; the total belongs in the
		// help hint (AXI: no pagination in TOON array headers).
		shown = shown[:contextTaskCap]
		firstHelp = fmt.Sprintf("Run `clickup-axi tasks` for all %s open tasks", total)
	}
	fmt.Fprintf(out, "tasks[%d]{id,title,status,due}:\n", len(shown))
	for i := range shown {
		t := &shown[i]
		fmt.Fprintf(out, "  %s,%s,%s,%s\n", output.ToonCell(displayID(t)), output.ToonCell(t.Name), output.ToonCell(t.Status.Status), t.DueDate.Date())
	}
	output.WriteHelp(out,
		firstHelp,
		"Run `clickup-axi tasks <id>` for details and comments")
	return 0
}

// effectiveDefaultList loads the configured default_list for the
// dashboard, telling the agent up front that a bare `tasks create`
// works here. Every failure - unreadable config, malformed value - is
// a silent skip: a broken dashboard must not break a session start,
// and tasks create reports the recovery when the default is actually
// used.
func effectiveDefaultList() (config.Value, bool) {
	cfg, err := loadConfig()
	if err != nil {
		return config.Value{}, false
	}
	v, found := cfg.Get("default_list")
	if !found || (!allDigits(v.Val) && !isFolderIDValue(v.Val)) {
		return config.Value{}, false
	}
	return v, true
}

// resolvedDefault is the concrete list a configured default currently
// means: the name is what lets an agent connect a user's "put it in
// Sprint 45" to the default without any lookup of its own.
type resolvedDefault struct {
	name, id string
	ok       bool
}

// resolveDefaultList fetches what the configured value points at: a
// numeric id fetches the list's name, a folder default derives the
// folder's current (sprint) list. Every failure returns !ok - the
// dashboard degrades to the raw value rather than breaking a session
// start.
func resolveDefaultList(c *clickup.Client, val string) resolvedDefault {
	if allDigits(val) {
		l, err := c.GetList(val)
		if err != nil || l.Name == "" {
			return resolvedDefault{}
		}
		return resolvedDefault{name: l.Name, id: val, ok: true}
	}
	folder, err := c.GetFolder(strings.TrimPrefix(val, "folder:"))
	if err != nil {
		return resolvedDefault{}
	}
	cur, ok := folder.CurrentList(time.Now())
	if !ok || cur.Name == "" {
		return resolvedDefault{}
	}
	return resolvedDefault{name: cur.Name, id: cur.ID, ok: true}
}

// writeDefaultListLine prints the default_list dashboard line. It is
// directive on purpose: benchmark runs show agents asking the user
// which list to create in even though a default is configured, so the
// line names the resolved list and states that a bare create lands
// there.
func writeDefaultListLine(out io.Writer, v config.Value, have bool, res resolvedDefault) {
	if !have {
		return
	}
	target := fmt.Sprintf("%s (%s)", v.Val, v.Scope)
	if res.ok {
		target = fmt.Sprintf("%s (%s, %s)", res.name, res.id, v.Scope)
	}
	fmt.Fprintf(out, "default_list: %s - a bare `tasks create \"<name>\"` lands here\n", target)
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
