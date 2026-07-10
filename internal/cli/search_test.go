package cli

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
)

// searchCorpus is a small fixed set exercising title-only, description-
// only, both, and no-match tasks against the query "deploy pipeline".
const searchCorpus = `{"tasks": [
	{"id": "a1", "name": "Deploy pipeline hardening", "status": {"status": "in progress"}, "due_date": "1783339200000"},
	{"id": "b2", "name": "CI deploy step flaky", "text_content": "the deploy pipeline breaks nightly", "status": {"status": "in review"}, "due_date": null},
	{"id": "c3", "name": "Unrelated chore", "text_content": "nothing to see", "status": {"status": "to do"}, "due_date": null},
	{"id": "d4", "name": "pipeline docs", "text_content": "docs about deploy", "status": {"status": "open"}, "due_date": null}
]}`

func TestSearchRanksTitleMatchesAboveDescription(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "deploy pipeline")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		`search "deploy pipeline": 3 matches`,
		"scope: assignee=me; closed excluded; scanned 4 (complete)",
		"tasks[3]{id,title,status,match,due}:",
		"a1,Deploy pipeline hardening,in progress,title,2026-07-06",
		"b2,CI deploy step flaky,in review,title+desc,",
		"d4,pipeline docs,open,title+desc,",
		"Run `clickup-axi tasks <id>` for full detail",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if strings.Contains(out, "c3") {
		t.Errorf("non-matching task leaked into results\noutput:\n%s", out)
	}
	if !(strings.Index(out, "a1,") < strings.Index(out, "b2,") && strings.Index(out, "b2,") < strings.Index(out, "d4,")) {
		t.Errorf("ranking order wrong (want a1 < b2 < d4)\noutput:\n%s", out)
	}
}

func TestSearchDefaultsToMeAndExcludesClosed(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("assignees[]"); got != "42" {
			t.Errorf("assignees[] = %q, want 42 (default assignee=me)", got)
		}
		if got := q.Get("include_closed"); got != "" {
			t.Errorf("include_closed = %q, want unset (closed excluded by default)", got)
		}
		if got := q.Get("order_by"); got != "updated" {
			t.Errorf("order_by = %q, want updated", got)
		}
		if got := q.Get("reverse"); got != "" {
			t.Errorf("reverse = %q, want unset (endpoint defaults to newest-first)", got)
		}
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "deploy")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
}

func TestSearchAssigneeAllRequiresAnotherFilter(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "search", "deploy", "--assignee", "all")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "searching all assignees needs at least one more filter") {
		t.Errorf("output missing the at-least-one-filter guard\noutput:\n%s", out)
	}
}

func TestSearchAssigneeAllWithFilterScansEveryone(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("assignees[]"); got != "" {
			t.Errorf("assignees[] = %q, want unset for --assignee all", got)
		}
		if got := q.Get("statuses[]"); got != "in review" {
			t.Errorf("statuses[] = %q, want %q", got, "in review")
		}
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "deploy", "--assignee", "all", "--status", "in review")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "scope: assignee=all; status=in review; closed excluded") {
		t.Errorf("scope line wrong for --assignee all --status\noutput:\n%s", out)
	}
}

// teamWithMembersJSON is the workspace fixture for name-resolution
// tests: two members whose names share a substring ("n") but only one
// matches "ting".
const teamWithMembersJSON = `{"teams": [{"id": "9018", "name": "Buzzwoo", "members": [
	{"user": {"id": 42, "username": "Jan Suthacheeva", "email": "jan@buzzwoo.de"}},
	{"user": {"id": 77, "username": "Ting Nguyen", "email": "ting@buzzwoo.de"}}
]}]}`

func TestSearchAssigneeResolvesMemberByName(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", teamWithMembersJSON)
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("assignees[]"); got != "77" {
			t.Errorf("assignees[] = %q, want 77 (resolved from name)", got)
		}
		w.Write([]byte(searchCorpus))
	})

	// "ting" is lowercase and only a prefix of the username.
	out, code := runCLI(t, c, "search", "deploy", "--assignee", "ting")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `scope: assignee=77 "Ting Nguyen";`) {
		t.Errorf("scope must show the resolved member\noutput:\n%s", out)
	}
}

func TestSearchAssigneeUnknownNameListsMembers(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", teamWithMembersJSON)

	out, code := runCLI(t, c, "search", "deploy", "--assignee", "bob")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `assignee "bob" matches none of the members of Buzzwoo: 42 "Jan Suthacheeva", 77 "Ting Nguyen"`) {
		t.Errorf("error must inline the members for a one-step retry\noutput:\n%s", out)
	}
}

func TestSearchAssigneeAmbiguousNameListsCandidates(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", teamWithMembersJSON)

	// "n" is a substring of both usernames.
	out, code := runCLI(t, c, "search", "deploy", "--assignee", "n")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `assignee "n" is ambiguous: 42 "Jan Suthacheeva", 77 "Ting Nguyen"`) {
		t.Errorf("ambiguity must list the candidates\noutput:\n%s", out)
	}
}

func TestSearchSpaceResolvesByName(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/space", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"spaces": [{"id": "111", "name": "Holy Grail"}, {"id": "222", "name": "Webshop"}]}`))
	})
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("space_ids[]"); got != "222" {
			t.Errorf("space_ids[] = %q, want 222 (resolved from name)", got)
		}
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "deploy", "--space", "webshop")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `space=222 "Webshop"`) {
		t.Errorf("scope must show the resolved space\noutput:\n%s", out)
	}
}

func TestSearchSpaceIDSkipsLookup(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/space", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("space lookup attempted for a numeric --space id")
	})
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("space_ids[]"); got != "333" {
			t.Errorf("space_ids[] = %q, want 333", got)
		}
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "deploy", "--space", "333")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "space=333;") {
		t.Errorf("scope must show the space id\noutput:\n%s", out)
	}
}

func TestSearchSpaceSubstringResolves(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/space", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"spaces": [{"id": "111", "name": "Holy Grail"}, {"id": "222", "name": "Webshop"}]}`))
	})
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("space_ids[]"); got != "222" {
			t.Errorf("space_ids[] = %q, want 222 (unique substring match)", got)
		}
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "deploy", "--space", "shop")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
}

func TestSearchSpaceUnknownNameListsSpaces(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/space", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"spaces": [{"id": "111", "name": "Holy Grail"}, {"id": "222", "name": "Webshop"}]}`))
	})

	out, code := runCLI(t, c, "search", "deploy", "--space", "nope")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `space "nope" matches none of the workspace's 2 spaces: 111 "Holy Grail", 222 "Webshop"`) {
		t.Errorf("error must inline the spaces for a one-step retry\noutput:\n%s", out)
	}
}

func TestSearchSpaceAmbiguousNameListsCandidates(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/space", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"spaces": [{"id": "111", "name": "OM Unit: Linda"}, {"id": "222", "name": "OM Unit: Marcel"}]}`))
	})

	out, code := runCLI(t, c, "search", "deploy", "--space", "om unit")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `space "om unit" is ambiguous: 111 "OM Unit: Linda", 222 "OM Unit: Marcel"`) {
		t.Errorf("ambiguity must list the candidates\noutput:\n%s", out)
	}
}

func TestSearchPushesDownDateWindow(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	wantGt, _ := parseSearchDate("2026-05-01")
	wantLt, _ := parseSearchDate("2026-06-01")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("date_updated_gt"); got != strconv.FormatInt(wantGt, 10) {
			t.Errorf("date_updated_gt = %q, want %d", got, wantGt)
		}
		if got := q.Get("date_updated_lt"); got != strconv.FormatInt(wantLt, 10) {
			t.Errorf("date_updated_lt = %q, want %d", got, wantLt)
		}
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "deploy", "--updated-after", "2026-05-01", "--updated-before", "2026-06-01")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "updated 2026-05-01..2026-06-01") {
		t.Errorf("scope line missing the date window\noutput:\n%s", out)
	}
}

func TestSearchRejectsMalformedDate(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "search", "deploy", "--updated-after", "05/01/2026")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "needs a date as YYYY-MM-DD") {
		t.Errorf("output missing date-format error\noutput:\n%s", out)
	}
}

func TestSearchIncludeClosedIsPushedDown(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("include_closed"); got != "true" {
			t.Errorf("include_closed = %q, want true", got)
		}
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "deploy", "--include-closed")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "closed included") {
		t.Errorf("scope line must state closed included\noutput:\n%s", out)
	}
}

func TestSearchZeroMatchesIsExplicit(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "kubernetes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		`search "kubernetes": 0 matches (searched title, custom id, description)`,
		"scope: assignee=me; closed excluded; scanned 4 (complete)",
		"Ask the user which project (space) the task is in",
		"Add --include-closed to also search the final closed status",
		"Comment bodies are not searched",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if strings.Contains(out, "--updated window") {
		t.Errorf("date hint shown although no date filter was set\noutput:\n%s", out)
	}
	if strings.Contains(out, "Not every task was scanned") {
		t.Errorf("incomplete-scan hint shown although the scan completed\noutput:\n%s", out)
	}
}

func TestSearchZeroMatchesWithDateHintsAtWindow(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": []}`))
	})

	out, code := runCLI(t, c, "search", "kubernetes", "--updated-after", "2026-05-01")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "Widen the --updated window") {
		t.Errorf("zero matches with a date filter must hint at the window\noutput:\n%s", out)
	}
}

// A zero is only definitive when everything was scanned: hitting the
// page budget with no match must tell the agent how to narrow so a
// retry can cover the rest.
func TestSearchZeroMatchesIncompleteScanHintsAtNarrowing(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		var rows []string
		for i := 0; i < clickup.TeamTasksPageSize; i++ {
			rows = append(rows, fmt.Sprintf(`{"id": "t%d", "name": "unrelated chore %d", "status": {"status": "open"}, "due_date": null}`, i, i))
		}
		fmt.Fprintf(w, `{"tasks": [%s]}`, strings.Join(rows, ","))
	})

	out, code := runCLI(t, c, "search", "kubernetes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "more may exist") {
		t.Errorf("scope must state the scan was incomplete\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Not every task was scanned; narrow with --status/--space/--list/--updated-after") {
		t.Errorf("zero matches on an incomplete scan must hint at narrowing\noutput:\n%s", out)
	}
}

func TestSearchBoundedScanReportsUnscannedTasks(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	calls := 0
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		calls++
		var rows []string
		for i := 0; i < clickup.TeamTasksPageSize; i++ {
			rows = append(rows, fmt.Sprintf(`{"id": "t%d-%d", "name": "task item %d", "status": {"status": "open"}, "due_date": null}`, calls, i, i))
		}
		fmt.Fprintf(w, `{"tasks": [%s]}`, strings.Join(rows, ","))
	})

	out, code := runCLI(t, c, "search", "task")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if calls != searchMaxPages {
		t.Errorf("scanned %d pages, want the %d-page budget", calls, searchMaxPages)
	}
	if !strings.Contains(out, "more may exist") {
		t.Errorf("a truncated scan must say more may exist\noutput:\n%s", out)
	}
	if !strings.Contains(out, "showing top 10 of 300 matches") {
		t.Errorf("output must report shown-of-total\noutput:\n%s", out)
	}
}

func TestSearchLimitCapsResults(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(searchCorpus))
	})

	out, code := runCLI(t, c, "search", "deploy pipeline", "--limit", "2")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "showing top 2 of 3 matches") {
		t.Errorf("output must report the limit\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Raise --limit to see more matches") {
		t.Errorf("output must hint how to widen\noutput:\n%s", out)
	}
	if strings.Contains(out, "d4,") {
		t.Errorf("third match should be cut by --limit 2\noutput:\n%s", out)
	}
}

func TestSearchLimitRejectsZero(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "search", "deploy", "--limit", "0")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "--limit needs a positive number") {
		t.Errorf("output missing limit validation\noutput:\n%s", out)
	}
}

func TestSearchNeedsQuery(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "search")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "search needs a query") {
		t.Errorf("output missing query usage error\noutput:\n%s", out)
	}
}

func TestSearchUnknownFlagIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "search", "foo", "--bogus")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "valid: --assignee, --status, --space, --list, --updated-after, --updated-before, --include-closed, --limit") {
		t.Errorf("usage error does not list valid flags inline\noutput:\n%s", out)
	}
}

func TestSearchRequiresResolvedWorkspace(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)

	out, code := runCLI(t, c, "search", "deploy")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `set CLICKUP_AXI_WORKSPACE to one of: 9001 "BUZZWOO", 9002 "Personal"`) {
		t.Errorf("search must surface the recoverable pin instruction\noutput:\n%s", out)
	}
}

// --- pure ranker unit tests ---

func TestRankTasksAndSemanticsExcludePartialTerms(t *testing.T) {
	tasks := []clickup.Task{
		{ID: "1", Name: "deploy pipeline"},
		{ID: "2", Name: "deploy only"},
	}
	got := rankTasks("deploy pipeline", tasks)
	if len(got) != 1 || got[0].Task.ID != "1" {
		t.Fatalf("AND semantics failed: got %+v, want only task 1", got)
	}
}

func TestRankTasksCustomIDMatches(t *testing.T) {
	tasks := []clickup.Task{{ID: "abc", CustomID: "HGAI-2316", Name: "some task"}}
	got := rankTasks("hgai-2316", tasks)
	if len(got) != 1 || got[0].Where != "id" {
		t.Fatalf("custom-id match failed: got %+v", got)
	}
}

func TestRankTasksPhraseBonusOrders(t *testing.T) {
	tasks := []clickup.Task{
		{ID: "spread", Name: "pipeline for deploy work"},
		{ID: "phrase", Name: "deploy pipeline"},
	}
	got := rankTasks("deploy pipeline", tasks)
	if len(got) != 2 || got[0].Task.ID != "phrase" {
		t.Fatalf("phrase bonus failed to rank contiguous title first: got %+v", got)
	}
}
