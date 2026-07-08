package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const searchHelp = `clickup-axi search "<query>" [flags]

Find tasks by the words in their title or description. ClickUp's public
API has no text search, so this filters tasks server-side, then ranks
the matches locally (title above description; AND across words).

By default it searches only your own tasks and hides tasks in the final
"closed" status. Widen with --assignee all, but then at least one other
filter (--status/--space/--list/--updated-after/--updated-before) is
required so the scan stays bounded. Comment bodies are not searched.

flags:
  --assignee me|all|<id>   whose tasks to search (default: me)
  --status "<status>"      only tasks in this status
  --space <id>             only tasks in this space
  --list <id>              only tasks in this list
  --updated-after  <date>  updated on/after YYYY-MM-DD
  --updated-before <date>  updated before YYYY-MM-DD
  --include-closed         include tasks in the final closed status
  --limit N                most results to show (default 10; 0 = all)

examples:
  clickup-axi search "oauth redirect"
  clickup-axi search invoice --status "in review"
  clickup-axi search migration --assignee all --updated-after 2026-05-01`

const (
	searchDefaultLimit = 10
	// searchMaxPages bounds the scan so one search cannot exhaust the
	// ~100 req/min budget or hang. With the default assignee=me filter a
	// scan is almost always a single page; the budget only bites when a
	// broadened search still matches many tasks.
	searchMaxPages = 3
)

func cmdSearch(args []string, c *clickup.Client, out io.Writer) int {
	var (
		terms         []string
		assignee      = "me"
		status        string
		space         string
		list          string
		updatedAfter  string
		updatedBefore string
		dateUpdatedGt int64
		dateUpdatedLt int64
		includeClosed bool
		limit         = searchDefaultLimit
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--assignee", "--status", "--space", "--list", "--limit", "--updated-after", "--updated-before":
			flag := args[i]
			i++
			if i >= len(args) {
				output.WriteError(out, fmt.Sprintf("%s needs a value", flag),
					"Run `clickup-axi search \"<query>\" "+flag+" <value>`")
				return 2
			}
			val := args[i]
			switch flag {
			case "--assignee":
				if !strings.EqualFold(val, "me") && !strings.EqualFold(val, "all") {
					if _, err := strconv.ParseInt(val, 10, 64); err != nil {
						output.WriteError(out, fmt.Sprintf("--assignee takes me, all, or a numeric user id, got %q", val))
						return 2
					}
				}
				assignee = strings.ToLower(val)
			case "--status":
				status = val
			case "--space":
				space = val
			case "--list":
				list = val
			case "--updated-after", "--updated-before":
				ms, ok := parseSearchDate(val)
				if !ok {
					output.WriteError(out, fmt.Sprintf("%s needs a date as YYYY-MM-DD, got %q", flag, val))
					return 2
				}
				if flag == "--updated-after" {
					updatedAfter, dateUpdatedGt = val, ms
				} else {
					updatedBefore, dateUpdatedLt = val, ms
				}
			case "--limit":
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 {
					output.WriteError(out, fmt.Sprintf("--limit needs a non-negative number, got %q", val))
					return 2
				}
				limit = n
			}
		case "--include-closed":
			includeClosed = true
		case "--help", "-h":
			fmt.Fprintln(out, searchHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for search\n  valid: --assignee, --status, --space, --list, --updated-after, --updated-before, --include-closed, --limit", args[i]))
				return 2
			}
			terms = append(terms, args[i])
		}
	}

	query := strings.Join(terms, " ")
	if strings.TrimSpace(query) == "" {
		output.WriteError(out, "search needs a query",
			"Run `clickup-axi search \"<query>\"` (words to find in a task title or description)")
		return 2
	}

	otherFilter := status != "" || space != "" || list != "" || dateUpdatedGt > 0 || dateUpdatedLt > 0
	if assignee == "all" && !otherFilter {
		output.WriteError(out, "searching all assignees needs at least one more filter (a workspace-wide scan is unbounded)",
			"Add --status, --space <id>, --list <id>, or --updated-after/--updated-before")
		return 2
	}

	team, err := c.SelectTeam()
	if err != nil {
		return renderAPIError(out, err)
	}

	q := clickup.TaskQuery{
		IncludeClosed: includeClosed,
		DateUpdatedGt: dateUpdatedGt,
		DateUpdatedLt: dateUpdatedLt,
		// Order by last-updated so a bounded scan covers active tasks.
		OrderBy: "updated",
	}
	if status != "" {
		q.Statuses = []string{status}
	}
	if space != "" {
		q.SpaceIDs = []string{space}
	}
	if list != "" {
		q.ListIDs = []string{list}
	}
	if assignee != "all" {
		id, apiErr := resolveAssignee(assignee, c)
		if apiErr != nil {
			return renderAPIError(out, apiErr)
		}
		q.Assignees = []int64{id}
	}

	var candidates []clickup.Task
	scanned := 0
	complete := false
	for p := 0; p < searchMaxPages; p++ {
		q.Page = p
		page, last, err := c.GetTeamTasksPage(team.ID, q)
		if err != nil {
			return renderAPIError(out, err)
		}
		candidates = append(candidates, page...)
		scanned += len(page)
		if last {
			complete = true
			break
		}
	}

	matches := rankTasks(query, candidates)
	sc := searchScope{
		assignee:      assignee,
		status:        status,
		space:         space,
		list:          list,
		updatedAfter:  updatedAfter,
		updatedBefore: updatedBefore,
		includeClosed: includeClosed,
		scanned:       scanned,
		complete:      complete,
	}
	renderSearch(out, query, matches, sc, limit)
	return 0
}

// resolveAssignee turns the --assignee value into a user id: "me"
// resolves the token's own user, a numeric value is used as-is. "all"
// is handled by the caller (no assignee filter) and never reaches here.
func resolveAssignee(assignee string, c *clickup.Client) (int64, *clickup.APIError) {
	if assignee == "me" {
		u, err := c.GetUser()
		if err != nil {
			return 0, err
		}
		return u.ID, nil
	}
	id, _ := strconv.ParseInt(assignee, 10, 64) // validated during flag parsing
	return id, nil
}

// parseSearchDate accepts a YYYY-MM-DD date and returns its UTC
// millisecond epoch, matching how tasks render due/updated dates.
func parseSearchDate(s string) (int64, bool) {
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0, false
	}
	return tm.UnixMilli(), true
}

// searchMatch is one ranked hit: which fields matched (for the `match`
// column) and the score that ordered it.
type searchMatch struct {
	Task  clickup.Task
	Where string
	Score int
}

const (
	weightName = 3
	weightID   = 2
	weightDesc = 1
)

// rankTasks matches query against each task's title, custom id, and
// description and returns the hits ordered best-first. Matching is
// deterministic substring/token matching (not fuzzy): an agent needs
// reproducible results. Semantics are AND - every query term must
// appear in some field, or the task is excluded.
func rankTasks(query string, tasks []clickup.Task) []searchMatch {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	matches := make([]searchMatch, 0, len(tasks))
	for i := range tasks {
		if m, ok := scoreTask(terms, &tasks[i]); ok {
			matches = append(matches, m)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Task.Name < matches[j].Task.Name
	})
	return matches
}

func scoreTask(terms []string, t *clickup.Task) (searchMatch, bool) {
	name := strings.ToLower(t.Name)
	id := strings.ToLower(t.CustomID)
	desc := strings.ToLower(taskDescription(t))

	score := 0
	var hitName, hitID, hitDesc bool
	for _, term := range terms {
		found := false
		if strings.Contains(name, term) {
			score += weightName
			hitName = true
			found = true
		}
		if id != "" && strings.Contains(id, term) {
			score += weightID
			hitID = true
			found = true
		}
		if strings.Contains(desc, term) {
			score += weightDesc
			hitDesc = true
			found = true
		}
		if !found {
			return searchMatch{}, false
		}
	}
	// Reward a full-phrase title hit so "deploy pipeline" ranks the task
	// literally titled that above one that merely contains both words.
	if len(terms) > 1 && strings.Contains(name, strings.Join(terms, " ")) {
		score += weightName
	}
	return searchMatch{Task: *t, Where: whereLabel(hitName, hitID, hitDesc), Score: score}, true
}

func whereLabel(name, id, desc bool) string {
	parts := make([]string, 0, 3)
	if name {
		parts = append(parts, "name")
	}
	if id {
		parts = append(parts, "id")
	}
	if desc {
		parts = append(parts, "desc")
	}
	return strings.Join(parts, "+")
}

// tokenize lowercases the query and splits it into distinct whitespace-
// separated terms, preserving order.
func tokenize(query string) []string {
	seen := map[string]bool{}
	var terms []string
	for _, f := range strings.Fields(strings.ToLower(query)) {
		if !seen[f] {
			seen[f] = true
			terms = append(terms, f)
		}
	}
	return terms
}

// searchScope captures what corpus a search actually covered, so the
// output can state it honestly: an agent must know which tasks were and
// were not searched before treating a result (or a zero) as the answer.
type searchScope struct {
	assignee      string
	status        string
	space         string
	list          string
	updatedAfter  string
	updatedBefore string
	includeClosed bool
	scanned       int
	complete      bool
}

// line renders the scope as one parseable summary.
func (s searchScope) line() string {
	parts := []string{"assignee=" + s.assignee}
	if s.status != "" {
		parts = append(parts, "status="+s.status)
	}
	if s.space != "" {
		parts = append(parts, "space="+s.space)
	}
	if s.list != "" {
		parts = append(parts, "list="+s.list)
	}
	switch {
	case s.updatedAfter != "" && s.updatedBefore != "":
		parts = append(parts, "updated "+s.updatedAfter+".."+s.updatedBefore)
	case s.updatedAfter != "":
		parts = append(parts, "updated>="+s.updatedAfter)
	case s.updatedBefore != "":
		parts = append(parts, "updated<"+s.updatedBefore)
	}
	if s.includeClosed {
		parts = append(parts, "closed included")
	} else {
		parts = append(parts, "closed excluded")
	}
	coverage := fmt.Sprintf("scanned %d (complete)", s.scanned)
	if !s.complete {
		coverage = fmt.Sprintf("scanned %d (more may exist)", s.scanned)
	}
	parts = append(parts, coverage)
	return strings.Join(parts, "; ")
}

func renderSearch(out io.Writer, query string, matches []searchMatch, sc searchScope, limit int) {
	shown := matches
	if limit > 0 && len(shown) > limit {
		shown = shown[:limit]
	}

	switch {
	case len(matches) == 0:
		fmt.Fprintf(out, "search %q: 0 matches (searched title + description)\n", query)
	case len(shown) < len(matches):
		fmt.Fprintf(out, "search %q: showing top %d of %d matches\n", query, len(shown), len(matches))
	default:
		fmt.Fprintf(out, "search %q: %d match%s\n", query, len(matches), pluralES(len(matches)))
	}
	fmt.Fprintf(out, "scope: %s\n", sc.line())

	if len(matches) == 0 {
		help := []string{}
		if sc.assignee != "all" {
			help = append(help, "Widen with --assignee all plus a --status/--space/--list/--updated-after filter")
		}
		if !sc.includeClosed {
			help = append(help, "Add --include-closed to also search the final closed status")
		}
		help = append(help, "Comment bodies are not searched; the term may live in a comment")
		output.WriteHelp(out, help...)
		return
	}

	fmt.Fprintf(out, "tasks[%d]{id,title,status,match,due}:\n", len(shown))
	for i := range shown {
		t := &shown[i].Task
		fmt.Fprintf(out, "  %s,%s,%s,%s,%s\n",
			displayID(t), output.ToonCell(t.Name), output.ToonCell(t.Status.Status), shown[i].Where, t.DueDate.Date())
	}

	help := []string{"Run `clickup-axi tasks <id>` for full detail and comments"}
	if len(shown) < len(matches) {
		help = append(help, "Raise --limit to see more matches")
	}
	if !sc.complete {
		help = append(help, "Not every task was scanned; narrow with --status/--space/--list/--updated-after")
	}
	output.WriteHelp(out, help...)
}

func pluralES(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}
