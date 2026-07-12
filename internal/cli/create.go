package cli

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

// createRequest carries the parsed flags of one tasks create invocation.
type createRequest struct {
	name        string
	nameSet     bool
	list        string
	space       string
	status      string
	statusSet   bool
	priority    string
	prioritySet bool
	due         string
	dueSet      bool
	body        string
	bodySet     bool
	parent      string
	assignees   []string
	tags        []string
}

const createValidFlags = "--list, --space, --status, --assignee, --priority, --body, --due, --parent, --tag"

// createRerun is the retry command referenced by every create field
// error; unlike edit there is no id to parameterize, the agent re-runs
// its own invocation with the values fixed.
const createRerun = "`clickup-axi tasks create ...`"

func cmdTaskCreate(args []string, c *clickup.Client, out io.Writer) int {
	var r createRequest
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--list":
			i++
			// Guarded flags reject a flag-shaped next token as a missing
			// value; only the free-text inputs (--body, the name) accept
			// values starting with a dash.
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--list needs a value", "Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\"`")
				return 2
			}
			r.list = args[i]
		case "--space":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--space needs a value", "Run `clickup-axi tasks create \"<name>\" --list \"<list>\" --space \"<space>\"`")
				return 2
			}
			r.space = args[i]
		case "--status":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--status needs a value", "Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\" --status \"<status>\"`")
				return 2
			}
			r.status = args[i]
			r.statusSet = true
		case "--assignee":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--assignee needs a value", "Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\" --assignee <who>`")
				return 2
			}
			r.assignees = append(r.assignees, splitTokens(args[i])...)
		case "--priority":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--priority needs a value", "Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\" --priority <urgent|high|normal|low>`")
				return 2
			}
			r.priority = args[i]
			r.prioritySet = true
		case "--due":
			i++
			if i >= len(args) || (strings.HasPrefix(args[i], "-") && !relativeDue.MatchString(args[i])) {
				output.WriteError(out, "--due needs a value", "Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\" --due <YYYY-MM-DD|+3days>`")
				return 2
			}
			r.due = args[i]
			r.dueSet = true
		case "--body":
			i++
			if i >= len(args) {
				output.WriteError(out, "--body needs a value", "Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\" --body \"<markdown>\"`")
				return 2
			}
			r.body = args[i]
			r.bodySet = true
		case "--parent":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--parent needs a value", "Run `clickup-axi tasks create \"<name>\" --parent <task id>`")
				return 2
			}
			r.parent = args[i]
		case "--tag":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				output.WriteError(out, "--tag needs a value", "Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\" --tag <tag>`")
				return 2
			}
			r.tags = append(r.tags, splitTokens(args[i])...)
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks create\n  valid: %s", args[i], createValidFlags))
				return 2
			}
			if r.nameSet {
				output.WriteError(out, "tasks create takes exactly one name (quote it)",
					"Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\"`")
				return 2
			}
			r.name = args[i]
			r.nameSet = true
		}
	}
	if !r.nameSet {
		output.WriteError(out, "tasks create needs a task name (the first argument)",
			"Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\"`")
		return 2
	}
	if strings.TrimSpace(r.name) == "" {
		output.WriteError(out, "the task name must not be empty",
			"Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\"`")
		return 2
	}
	if r.bodySet && strings.TrimSpace(r.body) == "" {
		output.WriteError(out, "--body must not be empty",
			"Run `clickup-axi tasks create \"<name>\" --list \"<list|id>\" --body \"<markdown>\"`")
		return 2
	}
	if r.list == "" && r.parent == "" {
		output.WriteError(out, "tasks create needs --list (the list to create in) or --parent (for a subtask)",
			"Run `clickup-axi lists --space \"<space>\"` to discover lists, then rerun with --list")
		return 2
	}
	// A list name is only unique within one space, and a workspace-wide
	// scan would be unbounded (AXI: no hidden fan-out) - so a name needs
	// --space, while a numeric id or a parent-derived list works alone.
	if r.list != "" && !allDigits(r.list) && r.parent == "" && r.space == "" {
		output.WriteError(out, "--list by name needs --space (list names are only unique within one space)",
			"Run `clickup-axi tasks create \"<name>\" --list \"<list>\" --space \"<space>\"`",
			"Or use the list id from `clickup-axi lists --space \"<space>\"`")
		return 2
	}

	// Locally decidable values are usage errors, validated before any
	// API call (AXI section 6); they aggregate so one retry fixes both.
	var usageErrs []string
	prio := 0
	if r.prioritySet {
		if p, ok := parsePriority(r.priority); ok {
			prio = p // "none" parses to 0 = unset, the create default
		} else {
			usageErrs = append(usageErrs, fmt.Sprintf("priority %q not accepted\n  valid: urgent, high, normal, low, none", r.priority))
		}
	}
	var due int64
	if r.dueSet {
		if d, ok := parseDue(r.due, time.UTC); ok {
			due = d // "none" parses to 0 = unset, the create default
		} else {
			usageErrs = append(usageErrs, fmt.Sprintf("due %q is not a date\n  valid: YYYY-MM-DD (e.g. 2026-08-01) or a relative +3days / -1week", r.due))
		}
	}
	if len(usageErrs) > 0 {
		return renderFieldReport(out, usageErrs, 2, "created", createRerun)
	}
	if r.dueSet && relativeDue.MatchString(r.due) {
		due, _ = parseDue(r.due, c.DateLocation())
	}

	// The workspace is only fetched when a field actually resolves
	// against it (assignee names, or a list name via its space).
	var team *clickup.Team
	if len(r.assignees) > 0 || (r.list != "" && !allDigits(r.list) && r.parent == "") {
		t, err := c.SelectTeam()
		if err != nil {
			return renderAPIError(out, err)
		}
		team = t
	}

	// The target list is the anchor of a create - like the task id of an
	// edit it fails fast, while the fields on top of it aggregate below.
	var parentTask *clickup.Task
	var listID, listName, spaceID string
	var statuses []string
	var fieldErrs []string
	switch {
	case r.parent != "":
		t, err := c.GetTaskByID(r.parent)
		if err != nil {
			return renderAPIError(out, err)
		}
		parentTask = t
		listID, listName, spaceID = t.List.ID, t.List.Name, t.Space.ID
		// ClickUp puts a subtask in its parent's list; a contradicting
		// --list must not silently create the task somewhere else.
		if r.list != "" && !listInputMatches(r.list, t.List.ID, t.List.Name) {
			fieldErrs = append(fieldErrs, fmt.Sprintf(
				"--list %q does not match the parent task's list %s %q\n  a subtask is created in its parent's list; drop --list or change --parent",
				r.list, t.List.ID, t.List.Name))
		}
	case allDigits(r.list):
		l, err := c.GetList(r.list)
		if err != nil {
			if err.Status == http.StatusNotFound {
				output.WriteError(out, fmt.Sprintf("list %q not found", r.list),
					"Run `clickup-axi lists --space \"<space>\"` to discover list ids")
				return 1
			}
			return renderAPIError(out, err)
		}
		listID, listName, spaceID = r.list, l.Name, l.Space.ID
		statuses = statusNames(l)
	default:
		sp, err := c.ResolveSpace(team.ID, r.space)
		if err != nil {
			return renderAPIError(out, err)
		}
		ref, err := c.ResolveList(sp.ID, r.list)
		if err != nil {
			return renderAPIError(out, err)
		}
		listID, listName, spaceID = ref.ID, ref.Name, sp.ID
	}

	// Pre-flight validation of the remaining server-derived fields, all
	// collected before anything is written so one retry fixes them all -
	// the same validate-all-then-write architecture as tasks edit.
	if r.statusSet {
		if statuses == nil {
			statuses = listStatuses(c, listID)
		}
		label := listName
		if label == "" {
			label = listID
		}
		if len(statuses) > 0 && !containsFold(statuses, r.status) {
			fieldErrs = append(fieldErrs, fmt.Sprintf("status %q not accepted in list %s\n  valid: %s",
				r.status, label, strings.Join(statuses, ", ")))
		}
	}

	var assigneeIDs []int64
	if len(r.assignees) > 0 {
		users, errs := resolveAssignees(r.assignees, team, c)
		fieldErrs = append(fieldErrs, errs...)
		seen := make(map[int64]bool, len(users))
		for _, u := range users {
			if seen[u.ID] {
				continue
			}
			seen[u.ID] = true
			assigneeIDs = append(assigneeIDs, u.ID)
		}
	}

	// Tags must already exist in the space - a typo must not mint a new
	// tag from an agent path - and are written in the stored casing so a
	// create never mints a case-different duplicate either.
	var tags []string
	if len(r.tags) > 0 {
		canon, bad, terr := c.ResolveSpaceTags(spaceID, r.tags)
		if terr != nil {
			return renderAPIError(out, terr)
		}
		fieldErrs = append(fieldErrs, bad...)
		seen := make(map[string]bool, len(r.tags))
		for _, tg := range r.tags {
			k := strings.ToLower(tg)
			if seen[k] {
				continue
			}
			seen[k] = true
			if name, ok := canon[k]; ok {
				tags = append(tags, name)
			}
		}
	}

	if len(fieldErrs) > 0 {
		return renderFieldReport(out, fieldErrs, 1, "created", createRerun)
	}

	tc := clickup.TaskCreate{
		Name:      r.name,
		Body:      r.body,
		Status:    r.status,
		Priority:  prio,
		DueDate:   due,
		Assignees: assigneeIDs,
		Tags:      tags,
	}
	if parentTask != nil {
		// The parent fetch resolved any custom id; create via internal id.
		tc.Parent = parentTask.ID
	}
	created, err := c.CreateTask(listID, tc)
	if err != nil {
		return renderAPIError(out, err)
	}

	// The confirmation echoes server-stored facts (id, list, the
	// defaulted status, url), never the request's assumptions.
	name := created.Name
	if name == "" {
		name = r.name
	}
	fmt.Fprintf(out, "task: created %s %q\n", displayID(created), name)
	if created.List.ID != "" {
		listID, listName = created.List.ID, created.List.Name
	}
	if listName != "" {
		fmt.Fprintf(out, "  list: %s (%s)\n", listName, listID)
	} else {
		fmt.Fprintf(out, "  list: %s\n", listID)
	}
	if parentTask != nil {
		fmt.Fprintf(out, "  parent: %s\n", displayID(parentTask))
	}
	if created.Status.Status != "" {
		fmt.Fprintf(out, "  status: %s\n", created.Status.Status)
	}
	if created.URL != "" {
		fmt.Fprintf(out, "  url: %s\n", created.URL)
	}
	output.WriteHelp(out,
		fmt.Sprintf("Run `clickup-axi tasks %s` to see the task", displayID(created)),
		fmt.Sprintf("Run `clickup-axi tasks edit %s ...` to change its fields", displayID(created)))
	return 0
}

// listInputMatches reports whether a --list value names the given list:
// a numeric input compares against the id, anything else against the
// name (case-insensitively) - the same policy ResolveList applies.
func listInputMatches(input, id, name string) bool {
	if allDigits(input) {
		return input == id
	}
	return strings.EqualFold(input, name)
}

// allDigits mirrors the resolvers' id policy: an all-digit value is an
// id, never a name.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
