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

	// Resolve assignee tokens against the workspace members. Name
	// resolution reuses search's resolveAssignee (me / id / member name),
	// so a miss or ambiguity returns the inline-candidates error.
	var addUsers, remUsers []clickup.User
	if len(addTokens) > 0 || len(remTokens) > 0 {
		team, terr := c.SelectTeam()
		if terr != nil {
			return renderAPIError(out, terr)
		}
		if addUsers, err = resolveAssignees(addTokens, team, c); err != nil {
			return renderAPIError(out, err)
		}
		if remUsers, err = resolveAssignees(remTokens, team, c); err != nil {
			return renderAPIError(out, err)
		}
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

	statusChanges := statusSet && !strings.EqualFold(t.Status.Status, status)
	assigneeChanges := len(add) > 0 || len(rem) > 0
	if !statusChanges && !assigneeChanges {
		fmt.Fprintf(out, "task: %s no changes (already assigned / same status)\n", displayID(t))
		return 0
	}

	// The fetch above resolved any custom id; mutate via internal id.
	if statusChanges {
		if serr := c.SetTaskStatus(t.ID, status); serr != nil {
			// Enrich a status rejection with the list's valid statuses.
			if valid := validStatuses(c, t.List.ID); valid != "" {
				output.WriteError(out, fmt.Sprintf("status %q not accepted for task %s in list %s\n  valid: %s",
					status, displayID(t), t.List.Name, valid),
					fmt.Sprintf("Run `clickup-axi tasks edit %s --status \"<status>\"` with one of the valid statuses", displayID(t)))
				return 1
			}
			return renderAPIError(out, serr)
		}
		fmt.Fprintf(out, "task: %s status changed: %s -> %s\n", displayID(t), t.Status.Status, status)
	}
	if assigneeChanges {
		if aerr := c.UpdateTaskAssignees(t.ID, add, rem); aerr != nil {
			return renderAPIError(out, aerr)
		}
		fmt.Fprintf(out, "task: %s assignees%s%s\n", displayID(t), signedList("+", addNames), signedList("-", remNames))
	}
	output.WriteHelp(out, fmt.Sprintf("Run `clickup-axi tasks %s` to see the task", displayID(t)))
	return 0
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

func validStatuses(c *clickup.Client, listID string) string {
	l, err := c.GetList(listID)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(l.Statuses))
	for _, s := range l.Statuses {
		names = append(names, s.Status)
	}
	return strings.Join(names, ", ")
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
