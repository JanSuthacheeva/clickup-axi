package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	descriptionLimit = 800
	commentLimit     = 200
	// The comments endpoint returns at most this many without pagination.
	commentsPageSize = 25
)

const taskHelp = `clickup-axi task <subcommand> [flags]

subcommands:
  view <id>   Show a task with its newest comments
              --comments N   comments to include (default 3)
              --no-comments  skip comments
              --full         complete description and all fetched comments
  edit <id>   Modify a task
              --status "<status>"  change status; valid statuses are
                                   echoed when the status does not match

examples:
  clickup-axi task view 86c2x1a
  clickup-axi task edit 86c2x1a --status "in review"`

func cmdTask(args []string, c *client, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, taskHelp)
		return 0
	}
	switch args[0] {
	case "view":
		return cmdTaskView(args[1:], c, out)
	case "edit":
		return cmdTaskEdit(args[1:], c, out)
	default:
		writeError(out, fmt.Sprintf("unknown task subcommand %q\n  valid: view, edit", args[0]),
			"Run `clickup-axi task --help`")
		return 2
	}
}

func cmdTaskView(args []string, c *client, out io.Writer) int {
	var id string
	showComments := 3
	full := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--comments":
			i++
			if i >= len(args) {
				writeError(out, "--comments needs a number", "Run `clickup-axi task view <id> --comments 5`")
				return 2
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				writeError(out, fmt.Sprintf("--comments needs a non-negative number, got %q", args[i]))
				return 2
			}
			showComments = n
		case "--no-comments":
			showComments = 0
		case "--full":
			full = true
		case "--help", "-h":
			fmt.Fprintln(out, taskHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "--") {
				writeError(out, fmt.Sprintf("unknown flag %q\n  valid: --comments N, --no-comments, --full", args[i]))
				return 2
			}
			if id != "" {
				writeError(out, "only one task id is accepted")
				return 2
			}
			id = args[i]
		}
	}
	if id == "" {
		writeError(out, "a task id is needed", "Run `clickup-axi tasks <id>` (internal like 86ey3tx8m or custom like HGAI-2316)")
		return 2
	}

	t, err := c.getTaskByID(id)
	if err != nil {
		return renderAPIError(out, err)
	}

	var comments []comment
	if showComments > 0 || full {
		// The task fetch already resolved a custom id, so follow-up
		// calls can use the internal id directly.
		comments, err = c.getComments(t.ID)
		if err != nil {
			return renderAPIError(out, err)
		}
	}

	renderTask(out, t, comments, showComments, full)
	return 0
}

func renderTask(out io.Writer, t *task, comments []comment, showComments int, full bool) {
	fmt.Fprintln(out, "task:")
	fmt.Fprintf(out, "  id: %s\n", t.ID)
	fmt.Fprintf(out, "  title: %s\n", t.Name)
	fmt.Fprintf(out, "  status: %s\n", t.Status.Status)
	fmt.Fprintf(out, "  list: %s (%s)\n", t.List.Name, t.List.ID)
	if names := usernames(t.Assignees); names != "" {
		fmt.Fprintf(out, "  assignees: %s\n", names)
	}
	if t.Priority != nil {
		fmt.Fprintf(out, "  priority: %s\n", t.Priority.Priority)
	}
	if d := t.DueDate.date(); d != "" {
		fmt.Fprintf(out, "  due: %s\n", d)
	}
	fmt.Fprintf(out, "  url: %s\n", t.URL)

	var help []string
	description := t.TextContent
	if description == "" {
		description = t.Description
	}
	if description != "" {
		shown := description
		if !full {
			var cut bool
			shown, cut = truncateRunes(description, descriptionLimit)
			if cut {
				shown += fmt.Sprintf("\n... (truncated, %d chars total)", len([]rune(description)))
				help = append(help, fmt.Sprintf("Run `clickup-axi tasks %s --full` for the complete description", t.ID))
			}
		}
		writeBlock(out, "description", shown, "  ")
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
		if len(comments) == commentsPageSize {
			total = strconv.Itoa(commentsPageSize) + "+"
		}
		fmt.Fprintf(out, "comments: showing %d of %s (newest first)\n", len(shown), total)
		fmt.Fprintf(out, "comments[%d]{author,date,text}:\n", len(shown))
		for _, cm := range shown {
			text := cm.Text
			if !full {
				var cut bool
				text, cut = truncateRunes(text, commentLimit)
				if cut {
					text += "..."
				}
			}
			fmt.Fprintf(out, "  %s,%s,%s\n", toonCell(cm.User.Username), cm.Date.date(), toonCell(text))
		}
		if len(shown) < len(comments) || len(comments) == commentsPageSize {
			help = append(help, fmt.Sprintf("Run `clickup-axi tasks %s --full` for all fetched comments", t.ID))
		}
	}

	help = append(help, fmt.Sprintf("Run `clickup-axi task edit %s --status \"<status>\"` to change status", t.ID))
	writeHelp(out, help...)
}

func cmdTaskEdit(args []string, c *client, out io.Writer) int {
	var id, status string
	statusSet := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			i++
			if i >= len(args) {
				writeError(out, "--status needs a value", "Run `clickup-axi task edit <id> --status \"in review\"`")
				return 2
			}
			status = args[i]
			statusSet = true
		case "--help", "-h":
			fmt.Fprintln(out, taskHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				writeError(out, fmt.Sprintf("unknown flag %q for task edit\n  valid: --status", args[i]))
				return 2
			}
			if id != "" {
				writeError(out, "task edit takes exactly one task id")
				return 2
			}
			id = args[i]
		}
	}
	if id == "" {
		writeError(out, "task edit needs a task id", "Run `clickup-axi task edit <id> --status \"<status>\"`")
		return 2
	}
	if !statusSet {
		writeError(out, "task edit needs --status (the only supported change for now)",
			fmt.Sprintf("Run `clickup-axi task edit %s --status \"<status>\"`", id))
		return 2
	}

	t, err := c.getTaskByID(id)
	if err != nil {
		return renderAPIError(out, err)
	}
	if strings.EqualFold(t.Status.Status, status) {
		fmt.Fprintf(out, "task: %s already has status %q (no-op)\n", t.ID, t.Status.Status)
		return 0
	}
	// The fetch above resolved any custom id; mutate via internal id.
	if err := c.setTaskStatus(t.ID, status); err != nil {
		// The only mutation here is a status change, so enrich any rejection
		// with the list's valid statuses for one-turn recovery.
		if valid := validStatuses(c, t.List.ID); valid != "" {
			writeError(out, fmt.Sprintf("status %q not accepted for task %s in list %s\n  valid: %s",
				status, t.ID, t.List.Name, valid),
				fmt.Sprintf("Run `clickup-axi task edit %s --status \"<status>\"` with one of the valid statuses", t.ID))
			return 1
		}
		return renderAPIError(out, err)
	}
	fmt.Fprintf(out, "task: %s status changed: %s -> %s\n", t.ID, t.Status.Status, status)
	return 0
}

func validStatuses(c *client, listID string) string {
	l, err := c.getList(listID)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(l.Statuses))
	for _, s := range l.Statuses {
		names = append(names, s.Status)
	}
	return strings.Join(names, ", ")
}

func usernames(users []user) string {
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Username)
	}
	return strings.Join(names, ", ")
}

func renderAPIError(out io.Writer, err *apiError) int {
	if err.message == errNoAuth {
		writeError(out, err.message,
			"Run `clickup-axi auth login` and paste a token from "+tokenURL,
			"Agents: `clickup-axi auth login < tokenfile` or export CLICKUP_TOKEN from a secret store")
		return 1
	}
	writeError(out, err.message)
	return 1
}
