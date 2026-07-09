package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const (
	descriptionLimit = 800
	commentLimit     = 200
)

func cmdTaskView(args []string, c *clickup.Client, out io.Writer) int {
	var id string
	showComments := 3
	full := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--comments":
			i++
			if i >= len(args) {
				output.WriteError(out, "--comments needs a number", "Run `clickup-axi tasks <id> --comments 5`")
				return 2
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				output.WriteError(out, fmt.Sprintf("--comments needs a non-negative number, got %q", args[i]))
				return 2
			}
			showComments = n
		case "--no-comments":
			showComments = 0
		case "--full":
			full = true
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "--") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q\n  valid: --comments N, --no-comments, --full", args[i]))
				return 2
			}
			if id != "" {
				output.WriteError(out, "only one task id is accepted")
				return 2
			}
			id = args[i]
		}
	}
	if id == "" {
		output.WriteError(out, "a task id is needed", "Run `clickup-axi tasks <id>` (internal like 86ey3tx8m or custom like HGAI-2316)")
		return 2
	}

	t, err := c.GetTaskByID(id)
	if err != nil {
		return renderAPIError(out, err)
	}

	var comments []clickup.Comment
	if showComments > 0 || full {
		// The task fetch already resolved a custom id, so follow-up
		// calls can use the internal id directly.
		comments, err = c.GetComments(t.ID)
		if err != nil {
			return renderAPIError(out, err)
		}
	}

	renderTask(out, t, comments, showComments, full)
	return 0
}

// displayID is the id shown to the user everywhere. With
// CLICKUP_AXI_CUSTOM_IDS set the custom id is the workspace's lingua
// franca, so output and hints reference it instead of the internal id.
func displayID(t *clickup.Task) string {
	if clickup.CustomIDsForced() && t.CustomID != "" {
		return t.CustomID
	}
	return t.ID
}

func renderTask(out io.Writer, t *clickup.Task, comments []clickup.Comment, showComments int, full bool) {
	fmt.Fprintln(out, "task:")
	fmt.Fprintf(out, "  id: %s\n", displayID(t))
	fmt.Fprintf(out, "  title: %s\n", t.Name)
	fmt.Fprintf(out, "  status: %s\n", t.Status.Status)
	fmt.Fprintf(out, "  list: %s (%s)\n", t.List.Name, t.List.ID)
	if names := usernames(t.Assignees); names != "" {
		fmt.Fprintf(out, "  assignees: %s\n", names)
	}
	if t.Priority != nil {
		fmt.Fprintf(out, "  priority: %s\n", t.Priority.Priority)
	}
	if d := t.DueDate.Date(); d != "" {
		fmt.Fprintf(out, "  due: %s\n", d)
	}
	fmt.Fprintf(out, "  url: %s\n", t.URL)

	var help []string
	description := taskDescription(t)
	if description != "" {
		shown := description
		if !full {
			var cut bool
			shown, cut = output.TruncateRunes(description, descriptionLimit)
			if cut {
				shown += fmt.Sprintf("\n... (truncated, %d chars total)", len([]rune(description)))
				help = append(help, fmt.Sprintf("Run `clickup-axi tasks %s --full` for the complete description", displayID(t)))
			}
		}
		output.WriteBlock(out, "description", shown, "  ")
	}

	switch {
	case showComments == 0 && !full:
		// comments skipped on request; stay silent about them
	case len(comments) == 0:
		fmt.Fprintln(out, "comments: 0 comments on this task")
	default:
		shown := comments
		if !full && len(shown) > showComments {
			shown = shown[:showComments]
		}
		total := strconv.Itoa(len(comments))
		if len(comments) == clickup.CommentsPageSize {
			total = strconv.Itoa(clickup.CommentsPageSize) + "+"
		}
		fmt.Fprintf(out, "comments: showing %d of %s (newest first)\n", len(shown), total)
		fmt.Fprintf(out, "comments[%d]{author,date,text}:\n", len(shown))
		for _, cm := range shown {
			text := cm.Text
			if !full {
				var cut bool
				text, cut = output.TruncateRunes(text, commentLimit)
				if cut {
					text += "..."
				}
			}
			fmt.Fprintf(out, "  %s,%s,%s\n", output.ToonCell(cm.User.Username), cm.Date.Date(), output.ToonCell(text))
		}
		if len(shown) < len(comments) || len(comments) == clickup.CommentsPageSize {
			help = append(help, fmt.Sprintf("Run `clickup-axi tasks %s --full` for all fetched comments", displayID(t)))
		}
	}

	help = append(help,
		fmt.Sprintf("Run `clickup-axi tasks edit %s --status \"<status>\"` to change status", displayID(t)),
		fmt.Sprintf("Run `clickup-axi tasks comment %s --text \"<text>\"` to add a comment", displayID(t)))
	output.WriteHelp(out, help...)
}

func cmdTaskEdit(args []string, c *clickup.Client, out io.Writer) int {
	var id, status string
	statusSet := false
	var addTokens, remTokens []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			i++
			if i >= len(args) {
				output.WriteError(out, "--status needs a value", "Run `clickup-axi tasks edit <id> --status \"in review\"`")
				return 2
			}
			status = args[i]
			statusSet = true
		case "--assignee":
			i++
			if i >= len(args) {
				output.WriteError(out, "--assignee needs a value", "Run `clickup-axi tasks edit <id> --assignee <who>`")
				return 2
			}
			addTokens = append(addTokens, splitAssignees(args[i])...)
		case "--unassign":
			i++
			if i >= len(args) {
				output.WriteError(out, "--unassign needs a value", "Run `clickup-axi tasks edit <id> --unassign <who>`")
				return 2
			}
			remTokens = append(remTokens, splitAssignees(args[i])...)
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks edit\n  valid: --status, --assignee, --unassign", args[i]))
				return 2
			}
			if id != "" {
				output.WriteError(out, "tasks edit takes exactly one task id")
				return 2
			}
			id = args[i]
		}
	}
	if id == "" {
		output.WriteError(out, "tasks edit needs a task id", "Run `clickup-axi tasks edit <id> --status \"<status>\"`")
		return 2
	}
	if !statusSet && len(addTokens) == 0 && len(remTokens) == 0 {
		output.WriteError(out, "tasks edit needs a change (--status, --assignee, or --unassign)",
			fmt.Sprintf("Run `clickup-axi tasks edit %s --status \"<status>\"` or `--assignee <who>`", id))
		return 2
	}

	t, err := c.GetTaskByID(id)
	if err != nil {
		return renderAPIError(out, err)
	}

	// Pre-flight validation: every field is checked before anything is
	// written, so a single bad field is reported alongside the others
	// (not one at a time) and never leaves a half-applied task. Assignee
	// names resolve against the workspace; the status is checked against
	// the list. Only once all fields are known-good does the atomic PUT
	// run. When the edit grows more fields, each adds its check here.
	var fieldErrs []string

	var addUsers, remUsers []clickup.User
	if len(addTokens) > 0 || len(remTokens) > 0 {
		team, terr := c.SelectTeam()
		if terr != nil {
			return renderAPIError(out, terr)
		}
		var rerr *clickup.APIError
		if addUsers, rerr = resolveAssignees(addTokens, team, c); rerr != nil {
			fieldErrs = append(fieldErrs, rerr.Message)
		}
		if remUsers, rerr = resolveAssignees(remTokens, team, c); rerr != nil {
			fieldErrs = append(fieldErrs, rerr.Message)
		}
	}

	statusChanges := statusSet && !strings.EqualFold(t.Status.Status, status)
	if statusChanges {
		// A wrong status is caught here rather than after a failed PUT, so
		// it no longer hides behind (or gets hidden by) an assignee error.
		if valid := listStatuses(c, t.List.ID); len(valid) > 0 && !containsFold(valid, status) {
			fieldErrs = append(fieldErrs, fmt.Sprintf("status %q not accepted in list %s\n  valid: %s",
				status, t.List.Name, strings.Join(valid, ", ")))
		}
	}

	if len(fieldErrs) > 0 {
		return renderFieldErrors(out, displayID(t), fieldErrs)
	}

	// A person named in both lists is a self-contradictory request.
	for _, a := range addUsers {
		for _, r := range remUsers {
			if a.ID == r.ID {
				output.WriteError(out, fmt.Sprintf("%q is in both --assignee and --unassign", assigneeName(a)),
					"Name each person in only one of --assignee / --unassign")
				return 2
			}
		}
	}

	// Idempotency: drop adds already assigned and removes not assigned,
	// deduping within each set. A change that collapses to nothing is a
	// stated no-op, never a wasted PUT.
	assigned := make(map[int64]bool, len(t.Assignees))
	for _, u := range t.Assignees {
		assigned[u.ID] = true
	}
	var add, rem []int64
	var addNames, remNames []string
	seen := make(map[int64]bool)
	for _, u := range addUsers {
		if assigned[u.ID] || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		add = append(add, u.ID)
		addNames = append(addNames, assigneeName(u))
	}
	seen = make(map[int64]bool)
	for _, u := range remUsers {
		if !assigned[u.ID] || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		rem = append(rem, u.ID)
		remNames = append(remNames, assigneeName(u))
	}

	assigneeChanges := len(add) > 0 || len(rem) > 0
	if !statusChanges && !assigneeChanges {
		var reasons []string
		if statusSet {
			reasons = append(reasons, fmt.Sprintf("already has status %q", t.Status.Status))
		}
		if len(addTokens) > 0 || len(remTokens) > 0 {
			reasons = append(reasons, "assignees already as requested")
		}
		fmt.Fprintf(out, "task: %s no changes (%s)\n", displayID(t), strings.Join(reasons, ", "))
		return 0
	}

	// The fetch above resolved any custom id; mutate via internal id.
	// Status and assignee changes go out in one PUT so they commit
	// atomically - no partial-mutation window. Every field was validated
	// pre-flight, so a failure here is a workflow restriction (e.g. a
	// forbidden status transition), which gets the raw translated error.
	edit := clickup.TaskEdit{}
	if statusChanges {
		edit.Status = status
	}
	if assigneeChanges {
		edit.AddAssignees = add
		edit.RemAssignees = rem
	}
	if err := c.UpdateTask(t.ID, edit); err != nil {
		return renderAPIError(out, err)
	}
	if statusChanges {
		fmt.Fprintf(out, "task: %s status changed: %s -> %s\n", displayID(t), t.Status.Status, status)
	}
	if assigneeChanges {
		fmt.Fprintf(out, "task: %s assignees%s%s\n", displayID(t), signedList("+", addNames), signedList("-", remNames))
	}
	output.WriteHelp(out, fmt.Sprintf("Run `clickup-axi tasks %s` to see the task", displayID(t)))
	return 0
}

// renderFieldErrors reports every field that failed pre-flight validation
// in one message, so the agent fixes them all before a single retry. The
// edit is atomic, so nothing was changed while any field was invalid.
func renderFieldErrors(out io.Writer, id string, errs []string) int {
	if len(errs) == 1 {
		output.WriteError(out, errs[0],
			fmt.Sprintf("Fix the value above, then rerun `clickup-axi tasks edit %s ...`", id))
		return 1
	}
	// Indent each error's continuation lines (e.g. a status "valid:" list)
	// so they nest under their bullet instead of dedenting to the margin.
	items := make([]string, len(errs))
	for i, e := range errs {
		items[i] = strings.ReplaceAll(e, "\n", "\n  ")
	}
	msg := fmt.Sprintf("%d fields cannot be applied (nothing was changed):\n  - %s",
		len(items), strings.Join(items, "\n  - "))
	output.WriteError(out, msg,
		fmt.Sprintf("Fix all the values above, then rerun `clickup-axi tasks edit %s ...` once", id))
	return 1
}

// splitAssignees turns a --assignee/--unassign value into tokens: a
// comma is always a separator, so "ting, me" is two people. Empty
// tokens (trailing commas, stray spaces) are dropped.
func splitAssignees(v string) []string {
	parts := strings.Split(v, ",")
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			tokens = append(tokens, p)
		}
	}
	return tokens
}

// resolveAssignees resolves each token to a member, propagating the
// first miss/ambiguity error (with its inline candidates) unchanged.
func resolveAssignees(tokens []string, team *clickup.Team, c *clickup.Client) ([]clickup.User, *clickup.APIError) {
	users := make([]clickup.User, 0, len(tokens))
	for _, tok := range tokens {
		u, err := resolveAssignee(tok, team, c)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, nil
}

// assigneeName prefers the resolved username; a numeric-id input
// resolves to an id-only user, so fall back to the id.
func assigneeName(u clickup.User) string {
	if u.Username != "" {
		return u.Username
	}
	return strconv.FormatInt(u.ID, 10)
}

// signedList renders " +Name +Name" (or "-") for the change summary.
func signedList(sign string, names []string) string {
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, " %s%s", sign, n)
	}
	return b.String()
}

func cmdTaskComment(args []string, c *clickup.Client, out io.Writer) int {
	var id, text string
	textSet := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--text":
			i++
			if i >= len(args) {
				output.WriteError(out, "--text needs a value", "Run `clickup-axi tasks comment <id> --text \"<text>\"`")
				return 2
			}
			text = args[i]
			textSet = true
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks comment\n  valid: --text", args[i]))
				return 2
			}
			if id != "" {
				output.WriteError(out, "tasks comment takes exactly one task id (quote the comment text)")
				return 2
			}
			id = args[i]
		}
	}
	if id == "" {
		output.WriteError(out, "tasks comment needs a task id", "Run `clickup-axi tasks comment <id> --text \"<text>\"`")
		return 2
	}
	if !textSet {
		output.WriteError(out, "tasks comment needs --text",
			fmt.Sprintf("Run `clickup-axi tasks comment %s --text \"<text>\"`", id))
		return 2
	}
	if strings.TrimSpace(text) == "" {
		output.WriteError(out, "comment text must not be empty",
			fmt.Sprintf("Run `clickup-axi tasks comment %s --text \"<text>\"`", id))
		return 2
	}

	t, err := c.GetTaskByID(id)
	if err != nil {
		return renderAPIError(out, err)
	}
	// The fetch above resolved any custom id; mutate via internal id.
	if err := c.CreateComment(t.ID, text); err != nil {
		return renderAPIError(out, err)
	}
	fmt.Fprintf(out, "comment: added to task %s\n", displayID(t))
	output.WriteHelp(out, fmt.Sprintf("Run `clickup-axi tasks %s` to see the task with its comments", displayID(t)))
	return 0
}

func containsFold(names []string, want string) bool {
	for _, n := range names {
		if strings.EqualFold(n, want) {
			return true
		}
	}
	return false
}

func listStatuses(c *clickup.Client, listID string) []string {
	l, err := c.GetList(listID)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(l.Statuses))
	for _, s := range l.Statuses {
		names = append(names, s.Status)
	}
	return names
}

// taskDescription is the human-readable body of a task: ClickUp's plain
// text_content when present (markdown stripped), falling back to the raw
// description. Both the detail view and search read it through here.
func taskDescription(t *clickup.Task) string {
	if t.TextContent != "" {
		return t.TextContent
	}
	return t.Description
}

func usernames(users []clickup.User) string {
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Username)
	}
	return strings.Join(names, ", ")
}

func renderAPIError(out io.Writer, err *clickup.APIError) int {
	if err.Message == clickup.ErrNoAuth {
		output.WriteError(out, err.Message,
			"Run `clickup-axi auth login` and paste a token from "+tokenURL,
			"Agents: `clickup-axi auth login < tokenfile` or export CLICKUP_TOKEN from a secret store")
		return 1
	}
	output.WriteError(out, err.Message)
	return 1
}
