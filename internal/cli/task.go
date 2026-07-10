package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

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

// editRequest carries the parsed flags of one tasks edit invocation.
type editRequest struct {
	id                   string
	status               string
	statusSet            bool
	addTokens, remTokens []string
	priority             string
	prioritySet          bool
	name                 string
	nameSet              bool
	due                  string
	dueSet               bool
}

// hasChange reports whether any mutation flag was given.
func (r *editRequest) hasChange() bool {
	return r.statusSet || r.prioritySet || r.nameSet || r.dueSet ||
		len(r.addTokens) > 0 || len(r.remTokens) > 0
}

const editValidFlags = "--status, --assignee, --unassign, --priority, --name, --due, --body, --append-body, --add-tag, --remove-tag"

func cmdTaskEdit(args []string, c *clickup.Client, out io.Writer) int {
	var r editRequest
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			i++
			if i >= len(args) {
				output.WriteError(out, "--status needs a value", "Run `clickup-axi tasks edit <id> --status \"in review\"`")
				return 2
			}
			r.status = args[i]
			r.statusSet = true
		case "--assignee":
			i++
			if i >= len(args) {
				output.WriteError(out, "--assignee needs a value", "Run `clickup-axi tasks edit <id> --assignee <who>`")
				return 2
			}
			r.addTokens = append(r.addTokens, splitTokens(args[i])...)
		case "--unassign":
			i++
			if i >= len(args) {
				output.WriteError(out, "--unassign needs a value", "Run `clickup-axi tasks edit <id> --unassign <who>`")
				return 2
			}
			r.remTokens = append(r.remTokens, splitTokens(args[i])...)
		case "--priority":
			i++
			if i >= len(args) {
				output.WriteError(out, "--priority needs a value", "Run `clickup-axi tasks edit <id> --priority <urgent|high|normal|low|none>`")
				return 2
			}
			r.priority = args[i]
			r.prioritySet = true
		case "--name":
			i++
			if i >= len(args) {
				output.WriteError(out, "--name needs a value", "Run `clickup-axi tasks edit <id> --name \"<title>\"`")
				return 2
			}
			r.name = args[i]
			r.nameSet = true
		case "--due":
			i++
			if i >= len(args) {
				output.WriteError(out, "--due needs a value", "Run `clickup-axi tasks edit <id> --due <YYYY-MM-DD|none>`")
				return 2
			}
			r.due = args[i]
			r.dueSet = true
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks edit\n  valid: %s", args[i], editValidFlags))
				return 2
			}
			if r.id != "" {
				output.WriteError(out, "tasks edit takes exactly one task id")
				return 2
			}
			r.id = args[i]
		}
	}
	if r.id == "" {
		output.WriteError(out, "tasks edit needs a task id", "Run `clickup-axi tasks edit <id> --status \"<status>\"`")
		return 2
	}
	if r.nameSet && strings.TrimSpace(r.name) == "" {
		output.WriteError(out, "--name must not be empty",
			fmt.Sprintf("Run `clickup-axi tasks edit %s --name \"<title>\"`", r.id))
		return 2
	}
	if !r.hasChange() {
		output.WriteError(out, "tasks edit needs a change\n  valid: "+editValidFlags,
			fmt.Sprintf("Run `clickup-axi tasks edit %s --status \"<status>\"` or any change flag", r.id))
		return 2
	}

	t, err := c.GetTaskByID(r.id)
	if err != nil {
		return renderAPIError(out, err)
	}

	// Pre-flight validation: every field is checked before anything is
	// written, so a single bad field is reported alongside the others
	// (not one at a time) and never leaves a half-applied task. Assignee
	// names resolve against the workspace; the status is checked against
	// the list; priority and due date parse locally. Only once all
	// fields are known-good does the atomic PUT run. When the edit grows
	// more fields, each adds its check here.
	var fieldErrs []string

	var addUsers, remUsers []clickup.User
	if len(r.addTokens) > 0 || len(r.remTokens) > 0 {
		team, terr := c.SelectTeam()
		if terr != nil {
			return renderAPIError(out, terr)
		}
		var tokErrs []string
		if addUsers, tokErrs = resolveAssignees(r.addTokens, team, c); len(tokErrs) > 0 {
			fieldErrs = append(fieldErrs, tokErrs...)
		}
		if remUsers, tokErrs = resolveAssignees(r.remTokens, team, c); len(tokErrs) > 0 {
			fieldErrs = append(fieldErrs, tokErrs...)
		}
	}

	statusChanges := r.statusSet && !strings.EqualFold(t.Status.Status, r.status)
	if statusChanges {
		// A wrong status is caught here rather than after a failed PUT, so
		// it no longer hides behind (or gets hidden by) another field error.
		if valid := listStatuses(c, t.List.ID); len(valid) > 0 && !containsFold(valid, r.status) {
			fieldErrs = append(fieldErrs, fmt.Sprintf("status %q not accepted in list %s\n  valid: %s",
				r.status, t.List.Name, strings.Join(valid, ", ")))
		}
	}

	var prio *int
	if r.prioritySet {
		if p, ok := parsePriority(r.priority); ok {
			prio = &p
		} else {
			fieldErrs = append(fieldErrs, fmt.Sprintf("priority %q not accepted\n  valid: urgent, high, normal, low, none", r.priority))
		}
	}

	var due *int64
	if r.dueSet {
		if d, ok := parseDue(r.due); ok {
			due = &d
		} else {
			fieldErrs = append(fieldErrs, fmt.Sprintf("due %q is not a date\n  valid: YYYY-MM-DD (e.g. 2026-08-01) or none to clear", r.due))
		}
	}

	if len(fieldErrs) > 0 {
		return renderFieldErrors(out, displayID(t), fieldErrs)
	}

	// A person named in both lists is a self-contradictory request.
	for _, a := range addUsers {
		for _, rm := range remUsers {
			if a.ID == rm.ID {
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
	priorityChanges := prio != nil && !strings.EqualFold(currentPriority(t), priorityLabel(*prio))
	dueChanges := due != nil && t.DueDate.Date() != dueLabel(*due)
	nameChanges := r.nameSet && t.Name != r.name

	if !statusChanges && !assigneeChanges && !priorityChanges && !dueChanges && !nameChanges {
		var reasons []string
		if r.statusSet {
			reasons = append(reasons, fmt.Sprintf("already has status %q", t.Status.Status))
		}
		if len(r.addTokens) > 0 || len(r.remTokens) > 0 {
			reasons = append(reasons, "assignees already as requested")
		}
		if r.prioritySet {
			reasons = append(reasons, "priority already "+currentPriority(t))
		}
		if r.dueSet {
			if d := t.DueDate.Date(); d == "" {
				reasons = append(reasons, "already has no due date")
			} else {
				reasons = append(reasons, "due already "+d)
			}
		}
		if r.nameSet {
			reasons = append(reasons, fmt.Sprintf("name already %q", t.Name))
		}
		fmt.Fprintf(out, "task: %s no changes (%s)\n", displayID(t), strings.Join(reasons, ", "))
		return 0
	}

	// The fetch above resolved any custom id; mutate via internal id.
	// All field changes go out in one PUT so they commit atomically -
	// no partial-mutation window. Every field was validated pre-flight,
	// so a failure here is a workflow restriction (e.g. a forbidden
	// status transition), which gets the raw translated error.
	edit := clickup.TaskEdit{}
	if statusChanges {
		edit.Status = r.status
	}
	if assigneeChanges {
		edit.AddAssignees = add
		edit.RemAssignees = rem
	}
	if priorityChanges {
		edit.Priority = prio
	}
	if dueChanges {
		edit.DueDate = due
	}
	if nameChanges {
		edit.Name = r.name
	}
	if err := c.UpdateTask(t.ID, edit); err != nil {
		return renderAPIError(out, err)
	}
	if statusChanges {
		fmt.Fprintf(out, "task: %s status changed: %s -> %s\n", displayID(t), t.Status.Status, r.status)
	}
	if assigneeChanges {
		fmt.Fprintf(out, "task: %s assignees%s%s\n", displayID(t), signedList("+", addNames), signedList("-", remNames))
	}
	if priorityChanges {
		fmt.Fprintf(out, "task: %s priority: %s -> %s\n", displayID(t), currentPriority(t), priorityLabel(*prio))
	}
	if dueChanges {
		fmt.Fprintf(out, "task: %s due: %s -> %s\n", displayID(t), orNone(t.DueDate.Date()), orNone(dueLabel(*due)))
	}
	if nameChanges {
		fmt.Fprintf(out, "task: %s renamed: %q -> %q\n", displayID(t), t.Name, r.name)
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

// splitTokens turns a repeatable flag value into tokens: a comma is
// always a separator, so "ting, me" is two people and "api, bug" two
// tags. Empty tokens (trailing commas, stray spaces) are dropped.
func splitTokens(v string) []string {
	parts := strings.Split(v, ",")
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			tokens = append(tokens, p)
		}
	}
	return tokens
}

// resolveAssignees resolves each token to a member, collecting every
// miss/ambiguity (with its inline candidates) so all bad tokens in a
// field are reported together and one retry can clear them.
func resolveAssignees(tokens []string, team *clickup.Team, c *clickup.Client) ([]clickup.User, []string) {
	users := make([]clickup.User, 0, len(tokens))
	var errs []string
	for _, tok := range tokens {
		u, err := resolveEditAssignee(tok, team, c)
		if err != nil {
			errs = append(errs, err.Message)
			continue
		}
		users = append(users, *u)
	}
	return users, errs
}

// resolveEditAssignee resolves one token for a mutation: "me" is the
// caller, a numeric token is validated against membership (unlike the
// read-only filter path, which trusts any id), and a name/email goes
// through ResolveMember. Validating ids keeps a non-existent id from
// passing pre-flight and printing a false success on a no-op PUT.
func resolveEditAssignee(token string, team *clickup.Team, c *clickup.Client) (*clickup.User, *clickup.APIError) {
	if token == "me" {
		return c.GetUser()
	}
	if id, err := strconv.ParseInt(token, 10, 64); err == nil {
		return team.ResolveMemberID(id)
	}
	return team.ResolveMember(token)
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

// parsePriority maps the human name to ClickUp's 1-4 scale; "none"
// maps to 0, which clears the priority.
func parsePriority(s string) (int, bool) {
	switch strings.ToLower(s) {
	case "urgent":
		return 1, true
	case "high":
		return 2, true
	case "normal":
		return 3, true
	case "low":
		return 4, true
	case "none":
		return 0, true
	}
	return 0, false
}

// priorityLabel is the inverse of parsePriority, for no-op comparison
// and the change summary.
func priorityLabel(rank int) string {
	switch rank {
	case 1:
		return "urgent"
	case 2:
		return "high"
	case 3:
		return "normal"
	case 4:
		return "low"
	}
	return "none"
}

// currentPriority names the task's priority for comparison and output;
// an unset priority reads as "none".
func currentPriority(t *clickup.Task) string {
	if t.Priority == nil {
		return "none"
	}
	return t.Priority.Priority
}

// parseDue turns --due input into a millisecond epoch: "none" clears
// (0), otherwise the date is read as UTC midnight so the view's UTC
// rendering round-trips exactly.
func parseDue(s string) (int64, bool) {
	if strings.EqualFold(s, "none") {
		return 0, true
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0, false
	}
	return d.UnixMilli(), true
}

// dueLabel renders a due edit the way the task view renders due dates
// ("" when cleared), keeping no-op comparison and output consistent.
func dueLabel(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}

// orNone renders an absent value ("") as the word agents pass to clear
// it, keeping set and clear summaries symmetric.
func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
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
