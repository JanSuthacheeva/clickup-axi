package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/update"
)

type fakeClickUp struct {
	mux          *http.ServeMux
	putBodies    []map[string]any
	putRaw       []string
	postBodies   []map[string]string
	createBodies []map[string]any
	commentGET   int
	tagAdds      []string
	tagRems      []string
}

func newFakeClickUp(t *testing.T) (*fakeClickUp, *clickup.Client) {
	t.Helper()
	// Isolate the custom-id and workspace policies from the host
	// environment; tests that want forced mode or a pinned workspace
	// set the variables after calling this.
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "")
	t.Setenv(clickup.WorkspaceEnv, "")
	f := &fakeClickUp{mux: http.NewServeMux()}
	srv := httptest.NewServer(f.mux)
	t.Cleanup(srv.Close)
	c := clickup.New(srv.URL+"/api/v2", "pk_test", &http.Client{Timeout: 5 * time.Second})
	return f, c
}

func (f *fakeClickUp) task(t *testing.T, id string, body string) {
	t.Helper()
	f.mux.HandleFunc("GET /api/v2/task/"+id, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
}

func (f *fakeClickUp) comments(t *testing.T, taskID string, body string) {
	t.Helper()
	f.mux.HandleFunc("GET /api/v2/task/"+taskID+"/comment", func(w http.ResponseWriter, r *http.Request) {
		f.commentGET++
		w.Write([]byte(body))
	})
}

func (f *fakeClickUp) postComment(t *testing.T, taskID string, status int, response string) {
	t.Helper()
	f.mux.HandleFunc("POST /api/v2/task/"+taskID+"/comment", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("POST body did not decode: %v", err)
		}
		f.postBodies = append(f.postBodies, body)
		w.WriteHeader(status)
		w.Write([]byte(response))
	})
}

func (f *fakeClickUp) put(t *testing.T, taskID string, status int, response string) {
	t.Helper()
	f.mux.HandleFunc("PUT /api/v2/task/"+taskID, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.putRaw = append(f.putRaw, string(raw))
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("PUT body did not decode: %v", err)
		}
		f.putBodies = append(f.putBodies, body)
		w.WriteHeader(status)
		w.Write([]byte(response))
	})
}

// list registers GET /list/{id} so a status edit can be pre-validated
// against the list's statuses.
func (f *fakeClickUp) list(t *testing.T, id, name string, statuses ...string) {
	t.Helper()
	items := make([]string, len(statuses))
	for i, s := range statuses {
		items[i] = fmt.Sprintf(`{"status": %q}`, s)
	}
	body := fmt.Sprintf(`{"id": %q, "name": %q, "statuses": [%s]}`, id, name, strings.Join(items, ", "))
	f.mux.HandleFunc("GET /api/v2/list/"+id, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
}

// spaceTags registers GET /space/{id}/tag so --add-tag validation can
// check the space's existing tags.
func (f *fakeClickUp) spaceTags(t *testing.T, spaceID string, names ...string) {
	t.Helper()
	items := make([]string, len(names))
	for i, n := range names {
		items[i] = fmt.Sprintf(`{"name": %q}`, n)
	}
	body := fmt.Sprintf(`{"tags": [%s]}`, strings.Join(items, ", "))
	f.mux.HandleFunc("GET /api/v2/space/"+spaceID+"/tag", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
}

// tagOps registers the per-tag add/remove endpoints and records calls.
func (f *fakeClickUp) tagOps(t *testing.T, taskID string) {
	t.Helper()
	f.mux.HandleFunc("POST /api/v2/task/"+taskID+"/tag/{tag}", func(w http.ResponseWriter, r *http.Request) {
		f.tagAdds = append(f.tagAdds, r.PathValue("tag"))
		w.Write([]byte(`{}`))
	})
	f.mux.HandleFunc("DELETE /api/v2/task/"+taskID+"/tag/{tag}", func(w http.ResponseWriter, r *http.Request) {
		f.tagRems = append(f.tagRems, r.PathValue("tag"))
		w.Write([]byte(`{}`))
	})
}

// failTagAdd makes adding one specific tag fail, for rollback tests.
// The Go 1.22 mux picks this literal pattern over tagOps' {tag}.
func (f *fakeClickUp) failTagAdd(t *testing.T, taskID, tag string) {
	t.Helper()
	f.mux.HandleFunc("POST /api/v2/task/"+taskID+"/tag/"+tag, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"err": "boom", "ECODE": "TAG_001"}`))
	})
}

// Fixture timestamps sit at 12:00 UTC so their local-date rendering is
// the same in every timezone from UTC-11 to UTC+12, keeping these tests
// deterministic on any dev machine or CI zone.
const taskJSON = `{
	"id": "abc123",
	"custom_id": "AIKK-99",
	"name": "Fix login redirect",
	"text_content": "After OAuth callback the user lands on a 404.",
	"status": {"status": "in progress"},
	"priority": {"priority": "high"},
	"due_date": "1783339200000",
	"url": "https://app.clickup.com/t/abc123",
	"assignees": [{"id": 1, "username": "jan"}],
	"list": {"id": "901234", "name": "Sprint 14"}
}`

const commentsJSON = `{"comments": [
	{"id": "3", "comment_text": "Repro'd on staging, with Safari", "user": {"id": 2, "username": "mia"}, "date": 1782993600000},
	{"id": "2", "comment_text": "Suspect the state param", "user": {"id": 1, "username": "jan"}, "date": "1782907200000"},
	{"id": "1", "comment_text": "Customer report", "user": {"id": 3, "username": "tom"}, "date": 1782820800000}
]}`

func runCLI(t *testing.T, c *clickup.Client, args ...string) (string, int) {
	t.Helper()
	return runCLIWithStdin(t, c, "", args...)
}

func runCLIWithStdin(t *testing.T, c *clickup.Client, stdin string, args ...string) (string, int) {
	t.Helper()
	// A zero updater is inert: no cache path, no skill path, no network.
	return runCLIWithUpdater(t, c, &update.Updater{}, stdin, args...)
}

func runCLIWithUpdater(t *testing.T, c *clickup.Client, up *update.Updater, stdin string, args ...string) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	code := Run(args, c, up, strings.NewReader(stdin), &buf)
	return buf.String(), code
}

func TestTaskViewIncludesComments(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.comments(t, "abc123", commentsJSON)

	out, code := runCLI(t, c, "tasks", "abc123")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"id: abc123",
		"title: Fix login redirect",
		"status: in progress",
		"list: Sprint 14 (901234)",
		"assignees: jan",
		"priority: high",
		"due: 2026-07-06",
		"description: After OAuth callback the user lands on a 404.",
		"count: 3 of 3 comments (newest first)",
		"comments[3]{author,date,text}:",
		`mia,2026-07-02,"Repro'd on staging, with Safari"`,
		"jan,2026-07-01,Suspect the state param",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	// A detail view is self-contained (AXI section 9): with nothing
	// truncated, no help[] hints are appended.
	if strings.Contains(out, "help[") {
		t.Errorf("self-contained detail view must not append help hints\noutput:\n%s", out)
	}
	// The URL is opt-in (--fields url): agents almost never browse, so
	// the default view does not spend the tokens.
	if strings.Contains(out, "url:") {
		t.Errorf("default detail view must not print the url\noutput:\n%s", out)
	}
}

func TestTaskViewFieldsURLOptsBackIn(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.comments(t, "abc123", `{"comments": []}`)

	out, code := runCLI(t, c, "tasks", "abc123", "--fields", "url")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "url: https://app.clickup.com/t/abc123") {
		t.Errorf("--fields url must print the task URL\noutput:\n%s", out)
	}
}

// The detail view shares the listing vocabulary; a field it already
// shows is a silent no-op, so agents can reuse one --fields value
// across commands without branching.
func TestTaskViewFieldsAlreadyShownIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.comments(t, "abc123", `{"comments": []}`)

	out, code := runCLI(t, c, "tasks", "abc123", "--fields", "assignees,priority")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if n := strings.Count(out, "assignees:"); n != 1 {
		t.Errorf("assignees printed %d times, want 1\noutput:\n%s", n, out)
	}
	if strings.Contains(out, "url:") {
		t.Errorf("url must stay opt-in\noutput:\n%s", out)
	}
}

func TestTaskViewFieldsUnknownNameIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "tasks", "abc123", "--fields", "checks")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	for _, want := range []string{`"checks"`, "valid: assignees, priority, tags, list, url"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestTaskViewTruncatesLongDescription(t *testing.T) {
	f, c := newFakeClickUp(t)
	long := strings.Repeat("x", 1000)
	f.task(t, "abc123", strings.Replace(taskJSON,
		"After OAuth callback the user lands on a 404.", long, 1))
	f.comments(t, "abc123", `{"comments": []}`)

	out, code := runCLI(t, c, "tasks", "abc123")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "... (truncated, 1000 chars total)") {
		t.Errorf("output missing truncation hint\noutput:\n%s", out)
	}
	if !strings.Contains(out, "--full` for the complete description") {
		t.Errorf("output missing --full escape hatch\noutput:\n%s", out)
	}
	if !strings.Contains(out, "comments: 0 comments on this task") {
		t.Errorf("output missing definitive empty state for comments\noutput:\n%s", out)
	}
}

// Truncated comment text must disclose itself like a truncated
// description does: total size shown, --full suggested - even when the
// comment count was not cut (2 of 2 shown, but one text was clipped).
func TestTaskViewTruncatedCommentTextHintsFull(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	long := strings.Repeat("y", 500)
	f.comments(t, "abc123", fmt.Sprintf(`{"comments": [
		{"id": "9", "comment_text": %q, "user": {"id": 2, "username": "mia"}, "date": 1782993600000},
		{"id": "8", "comment_text": "short", "user": {"id": 1, "username": "jan"}, "date": "1782907200000"}
	]}`, long))

	out, code := runCLI(t, c, "tasks", "abc123")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "count: 2 of 2 comments (newest first)") {
		t.Errorf("output missing count line\noutput:\n%s", out)
	}
	if !strings.Contains(out, "... (truncated, 500 chars total)") {
		t.Errorf("truncated comment text missing its size hint\noutput:\n%s", out)
	}
	if want := "--full` for full comment text"; !strings.Contains(out, want) {
		t.Errorf("output missing the --full escape hatch %q\noutput:\n%s", want, out)
	}
	if n := strings.Count(out, "--full` for full comment text"); n != 1 {
		t.Errorf("comment --full hint appears %d times, want 1\noutput:\n%s", n, out)
	}
}

// --full renders the clipped text completely, with no truncation marker.
func TestTaskViewFullShowsCompleteCommentText(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	long := strings.Repeat("y", 500)
	f.comments(t, "abc123", fmt.Sprintf(`{"comments": [
		{"id": "9", "comment_text": %q, "user": {"id": 2, "username": "mia"}, "date": 1782993600000}
	]}`, long))

	out, code := runCLI(t, c, "tasks", "abc123", "--full")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, long) {
		t.Errorf("--full did not render the complete comment text\noutput:\n%s", out)
	}
	if strings.Contains(out, "truncated") {
		t.Errorf("--full output still mentions truncation\noutput:\n%s", out)
	}
}

func TestTaskViewNoCommentsSkipsFetch(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.comments(t, "abc123", commentsJSON)

	out, code := runCLI(t, c, "tasks", "abc123", "--no-comments")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if f.commentGET != 0 {
		t.Errorf("comments endpoint was called %d times, want 0", f.commentGET)
	}
	if strings.Contains(out, "comments") {
		t.Errorf("output mentions comments despite --no-comments\noutput:\n%s", out)
	}
}

func TestTaskEditChangesStatus(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.list(t, "901234", "Sprint 14", "to do", "in progress", "in review", "done")
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "in review")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 status changed: in progress -> in review"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putBodies) != 1 || f.putBodies[0]["status"] != "in review" {
		t.Errorf("PUT bodies = %v, want one with status \"in review\"", f.putBodies)
	}
}

func TestTaskEditSameStatusIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	// No PUT handler registered: a PUT would 404 and fail the test output.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "In Progress")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (idempotent no-op)\noutput:\n%s", code, out)
	}
	if want := `no changes (already has status "in progress")`; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if strings.Contains(out, "assign") {
		t.Errorf("status-only no-op mentions assignees\noutput:\n%s", out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT was called for a no-op status change")
	}
}

// A combined status+assignee edit that fails at the API must not
// misattribute the failure to the status when the status is valid: the
// single PUT is atomic, so nothing partially committed, and a valid
// status gets the raw translated error instead of a "not accepted" list.
func TestTaskEditCombinedFailureWithValidStatusNotMisattributed(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.put(t, "abc123", http.StatusBadRequest, `{"err": "Assignee not found", "ECODE": "CRTSK_002"}`)
	f.mux.HandleFunc("GET /api/v2/list/901234", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id": "901234", "name": "Sprint 14", "statuses": [
			{"status": "to do"}, {"status": "in progress"}, {"status": "in review"}, {"status": "done"}
		]}`))
	})

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "in review", "--assignee", "ting")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "not accepted") {
		t.Errorf("valid status misattributed as rejected\noutput:\n%s", out)
	}
	if strings.Contains(out, "status changed") {
		t.Errorf("no field should be reported as changed on a failed atomic PUT\noutput:\n%s", out)
	}
	if len(f.putBodies) != 1 {
		t.Errorf("want 1 atomic PUT, got %d: %v", len(f.putBodies), f.putRaw)
	}
}

func TestTaskEditInvalidStatusListsValidOnes(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.list(t, "901234", "Sprint 14", "to do", "in progress", "in review", "done")
	// No PUT handler: an invalid status is caught pre-flight, before any write.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "qa")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if want := "valid: to do, in progress, in review, done"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT was called despite an invalid status: %v", f.putRaw)
	}
}

// A bad status and a bad assignee in the same call are reported together,
// so the agent fixes both before one retry - neither hides the other, and
// nothing is written while any field is invalid.
func TestTaskEditAggregatesInvalidStatusAndAssignee(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.list(t, "901234", "Sprint 14", "to do", "in progress", "in review", "done")
	// No PUT handler: nothing must be written when any field is invalid.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "qa", "--assignee", "zoe")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"valid: to do, in progress, in review, done",
		`assignee "zoe" matches none of the members`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("aggregated output missing %q\noutput:\n%s", want, out)
		}
	}
	if len(f.putBodies) != 0 {
		t.Errorf("no PUT should happen when a field is invalid: %v", f.putRaw)
	}
}

func TestTaskEditAggregatesMultipleBadTokensInOneField(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	// No PUT handler: nothing must be written when any token is invalid.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "zoe, xyz")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		`assignee "zoe" matches none of the members`,
		`assignee "xyz" matches none of the members`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("aggregated output missing %q\noutput:\n%s", want, out)
		}
	}
	if len(f.putBodies) != 0 {
		t.Errorf("no PUT should happen when a token is invalid: %v", f.putRaw)
	}
}

// editTaskJSON gives the task a current assignee (jan, id 42) that lines
// up with membersTeamJSON (jan 42, Ting Nguyen 189, Tinh Tran 190), plus
// a priority, due date, markdown body, space, and tags, so every edit
// field can prove set / clear / no-op behavior against known state.
const editTaskJSON = `{
	"id": "abc123",
	"custom_id": "AIKK-99",
	"name": "Fix login redirect",
	"status": {"status": "in progress"},
	"priority": {"priority": "high"},
	"due_date": "1783339200000",
	"markdown_description": "After OAuth the user lands on a 404.",
	"url": "https://app.clickup.com/t/abc123",
	"assignees": [{"id": 42, "username": "jan"}],
	"list": {"id": "901234", "name": "Sprint 14"},
	"space": {"id": "sp1"},
	"tags": [{"name": "backend"}]
}`

func TestTaskEditAddsAssigneeByName(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "ting")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 assignees +Ting Nguyen"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"add":[189]`) {
		t.Errorf("PUT raw = %v, want assignees.add [189]", f.putRaw)
	}
}

func TestTaskEditUnassignByMeRemoves(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--unassign", "me")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 assignees -jan"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"rem":[42]`) {
		t.Errorf("PUT raw = %v, want assignees.rem [42]", f.putRaw)
	}
}

func TestTaskEditAddsByNumericID(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "190")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	// A valid numeric id is validated against membership and displays the
	// resolved member name, not the bare id.
	if want := "task: abc123 assignees +Tinh Tran"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"add":[190]`) {
		t.Errorf("PUT raw = %v, want assignees.add [190]", f.putRaw)
	}
}

func TestTaskEditRejectsUnknownNumericID(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "999")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if want := "assignee 999 matches none of the members"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	// A non-existent id fails pre-flight; no PUT is ever sent.
	if len(f.putRaw) != 0 {
		t.Errorf("PUT raw = %v, want no PUT for an unknown id", f.putRaw)
	}
}

func TestTaskEditCommaSeparatedAddsMultiple(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "ting, tinh")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"add":[189,190]`) {
		t.Errorf("PUT raw = %v, want assignees.add [189,190]", f.putRaw)
	}
	for _, want := range []string{"+Ting Nguyen", "+Tinh Tran"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestTaskEditRepeatedFlagEqualsComma(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	_, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "ting", "--assignee", "tinh")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"add":[189,190]`) {
		t.Errorf("PUT raw = %v, want assignees.add [189,190]", f.putRaw)
	}
}

func TestTaskEditCombinesStatusAndAssignee(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.list(t, "901234", "Sprint 14", "to do", "in progress", "in review", "done")
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "in review", "--assignee", "ting")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"task: abc123 status changed: in progress -> in review",
		"task: abc123 assignees +Ting Nguyen",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if len(f.putBodies) != 1 {
		t.Fatalf("want 1 atomic PUT (status + assignees), got %d: %v", len(f.putBodies), f.putRaw)
	}
	if f.putBodies[0]["status"] != "in review" {
		t.Errorf("PUT body = %v, want status \"in review\"", f.putBodies[0])
	}
	if !strings.Contains(f.putRaw[0], `"add":[189]`) {
		t.Errorf("PUT raw = %v, want assignees.add [189]", f.putRaw)
	}
}

func TestTaskEditIdempotentAddIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // jan (42) already assigned
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	// No PUT handler: a PUT would 404 and surface in output.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "jan")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (idempotent)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "no changes") {
		t.Errorf("output missing no-op acknowledgement\noutput:\n%s", out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT called for a no-op add: %v", f.putRaw)
	}
}

func TestTaskEditIdempotentUnassignIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // Ting (189) not assigned
	f.meWithTeams(t, 42, "jan", membersTeamJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--unassign", "ting")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (idempotent)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "no changes") {
		t.Errorf("output missing no-op acknowledgement\noutput:\n%s", out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT called for a no-op unassign: %v", f.putRaw)
	}
}

func TestTaskEditNameMissInlinesMembers(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "zoe")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if want := `assignee "zoe" matches none of the members`; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditAmbiguousNameInlinesCandidates(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "tin")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if want := `assignee "tin" is ambiguous`; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditSamePersonAddAndRemoveIsUsageError(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "ting", "--unassign", "ting")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if want := "both --assignee and --unassign"; !strings.Contains(out, want) {
		t.Errorf("output missing conflict message\noutput:\n%s", out)
	}
}

func TestTaskEditNoChangeFlagsIsUsageError(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if want := "needs a change"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditUnknownFlagListsValid(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--color", "red")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if want := "valid: --status, --assignee, --unassign, --priority, --name, --due, --body, --append-body, --add-tag, --remove-tag"; !strings.Contains(out, want) {
		t.Errorf("output missing valid-flag list\noutput:\n%s", out)
	}
}

func TestTaskEditSetsPriority(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--priority", "urgent")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 priority: high -> urgent"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"priority":1`) {
		t.Errorf("PUT raw = %v, want priority 1", f.putRaw)
	}
}

func TestTaskEditClearsPriority(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--priority", "none")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 priority: high -> none"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"priority":null`) {
		t.Errorf("PUT raw = %v, want priority null", f.putRaw)
	}
}

func TestTaskEditSamePriorityIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // priority high
	// No PUT handler: a PUT would 404 and surface in output.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--priority", "High")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (idempotent)\noutput:\n%s", code, out)
	}
	if want := "no changes (priority already high)"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT called for a no-op priority: %v", f.putRaw)
	}
}

// A priority name is a static local enum: a bad one is a usage error
// (exit 2, AXI section 6), caught before any API call.
func TestTaskEditInvalidPriorityListsValid(t *testing.T) {
	_, c := newFakeClickUp(t)
	// No handlers registered: any API call would 404 and surface in output.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--priority", "blocker")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)\noutput:\n%s", code, out)
	}
	if want := "valid: urgent, high, normal, low, none"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditSetsDueDate(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--due", "2026-07-20")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 due: 2026-07-06 -> 2026-07-20"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"due_date":1784548800000`) {
		t.Errorf("PUT raw = %v, want due_date 1784548800000", f.putRaw)
	}
	if !strings.Contains(f.putRaw[0], `"due_date_time":false`) {
		t.Errorf("PUT raw = %v, want due_date_time false", f.putRaw)
	}
}

func TestRelativeDaysAndDueParsing(t *testing.T) {
	saved := timeNow
	timeNow = func() time.Time { return time.Date(2026, 7, 11, 20, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { timeNow = saved })

	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	accepted := []struct {
		input string
		days  int
		date  time.Time
	}{
		{input: "+3days", days: 3, date: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)},
		{input: "+3day", days: 3, date: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)},
		{input: "+3d", days: 3, date: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)},
		{input: "-1week", days: -7, date: time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)},
		{input: "-1w", days: -7, date: time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)},
		{input: "+2WEEKS", days: 14, date: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)},
		{input: "+0days", days: 0, date: time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)},
	}
	for _, tc := range accepted {
		t.Run(tc.input, func(t *testing.T) {
			if got, ok := relativeDays(tc.input); !ok || got != tc.days {
				t.Errorf("relativeDays(%q) = %d, %v; want %d, true", tc.input, got, ok, tc.days)
			}
			if got, ok := parseDue(tc.input, loc); !ok || got != tc.date.UnixMilli() {
				t.Errorf("parseDue(%q) = %d, %v; want %d, true", tc.input, got, ok, tc.date.UnixMilli())
			}
		})
	}

	for _, input := range []string{"3days", "+3", "+days", "+3months", "+12345d", "tomorrow"} {
		t.Run("reject_"+input, func(t *testing.T) {
			if _, ok := relativeDays(input); ok {
				t.Errorf("relativeDays(%q) accepted invalid input", input)
			}
			if _, ok := parseDue(input, loc); ok {
				t.Errorf("parseDue(%q) accepted invalid input", input)
			}
		})
	}
}

func TestTaskEditSetsRelativeDueDateInWorkspaceTimezone(t *testing.T) {
	saved := timeNow
	timeNow = func() time.Time { return time.Date(2026, 7, 11, 20, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { timeNow = saved })

	for _, tc := range []struct {
		input string
		want  time.Time
	}{
		{input: "+3days", want: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)},
		{input: "-1week", want: time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.input, func(t *testing.T) {
			f, c := newFakeClickUp(t)
			f.timezone(t, "Asia/Bangkok")
			f.task(t, "abc123", editTaskJSON)
			f.put(t, "abc123", http.StatusOK, `{}`)

			out, code := runCLI(t, c, "tasks", "edit", "abc123", "--due", tc.input)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
			}
			wantDate := tc.want.Format("2006-01-02")
			if want := "task: abc123 due: 2026-07-06 -> " + wantDate; !strings.Contains(out, want) {
				t.Errorf("output missing %q\noutput:\n%s", want, out)
			}
			if len(f.putBodies) != 1 || f.putBodies[0]["due_date"] != float64(tc.want.UnixMilli()) {
				t.Errorf("PUT bodies = %#v, want due_date %d", f.putBodies, tc.want.UnixMilli())
			}
			if f.putBodies[0]["due_date_time"] != false {
				t.Errorf("PUT body = %#v, want due_date_time false", f.putBodies[0])
			}
		})
	}
}

func TestTaskEditRelativeDueDateNoOp(t *testing.T) {
	saved := timeNow
	timeNow = func() time.Time { return time.Date(2026, 7, 2, 20, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { timeNow = saved })

	f, c := newFakeClickUp(t)
	f.timezone(t, "Asia/Bangkok")
	f.task(t, "abc123", editTaskJSON) // workspace today July 3 + 3 days = current July 6

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--due", "+3days")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "no changes (due already 2026-07-06)") {
		t.Errorf("output missing relative no-op\noutput:\n%s", out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT called for a relative no-op: %#v", f.putBodies)
	}
}

func TestTaskEditClearsDueDate(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--due", "none")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 due: 2026-07-06 -> none"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"due_date":null`) {
		t.Errorf("PUT raw = %v, want due_date null", f.putRaw)
	}
}

func TestTaskEditSameDueIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // due 2026-07-06

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--due", "2026-07-06")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (idempotent)\noutput:\n%s", code, out)
	}
	if want := "no changes (due already 2026-07-06)"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT called for a no-op due date: %v", f.putRaw)
	}
}

// A due date's format is decidable locally: a bad one is a usage error
// (exit 2, AXI section 6), caught before any API call.
func TestTaskEditBadDueDateIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)
	// No handlers registered: any API call would 404 and surface in output.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--due", "tomorrow")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)\noutput:\n%s", code, out)
	}
	if want := "valid: YYYY-MM-DD (e.g. 2026-08-01), a relative +3days / -1week, or none to clear"; !strings.Contains(out, want) {
		t.Errorf("output missing date-format hint\noutput:\n%s", out)
	}
}

func TestTaskEditRenames(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--name", "Fix OAuth redirect")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := `task: abc123 renamed: "Fix login redirect" -> "Fix OAuth redirect"`; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"name":"Fix OAuth redirect"`) {
		t.Errorf("PUT raw = %v, want the new name", f.putRaw)
	}
}

func TestTaskEditSameNameIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--name", "Fix login redirect")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (idempotent)\noutput:\n%s", code, out)
	}
	if want := `no changes (name already "Fix login redirect")`; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT called for a no-op rename: %v", f.putRaw)
	}
}

func TestTaskEditEmptyNameIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--name", "   ")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "--name must not be empty") {
		t.Errorf("output missing empty-name error\noutput:\n%s", out)
	}
}

func TestTaskEditMultipleFieldsOneAtomicPut(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.list(t, "901234", "Sprint 14", "to do", "in progress", "in review", "done")
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123",
		"--status", "in review", "--priority", "low", "--due", "2026-07-20", "--name", "Fix OAuth redirect")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if len(f.putBodies) != 1 {
		t.Fatalf("want 1 atomic PUT, got %d: %v", len(f.putBodies), f.putRaw)
	}
	for _, want := range []string{`"status":"in review"`, `"priority":4`, `"due_date":1784548800000`, `"name":"Fix OAuth redirect"`} {
		if !strings.Contains(f.putRaw[0], want) {
			t.Errorf("PUT raw missing %s\nraw: %s", want, f.putRaw[0])
		}
	}
	for _, want := range []string{"status changed", "priority: high -> low", "due: 2026-07-06 -> 2026-07-20", "renamed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestTaskEditReplacesBody(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--body", "New **desc**")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 description replaced (12 chars)"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"markdown_content":"New **desc**"`) {
		t.Errorf("PUT raw = %v, want markdown_content replacement", f.putRaw)
	}
}

func TestTaskEditAppendsBody(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // markdown: "After OAuth the user lands on a 404."
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--append-body", "Repro: safari only.")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 description appended (+19 chars)"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0],
		`"markdown_content":"After OAuth the user lands on a 404.\n\nRepro: safari only."`) {
		t.Errorf("PUT raw = %v, want existing body + blank line + appended text", f.putRaw)
	}
}

func TestTaskEditAppendToEmptyBodySkipsSeparator(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", strings.Replace(editTaskJSON,
		`"markdown_description": "After OAuth the user lands on a 404.",`, "", 1))
	f.put(t, "abc123", http.StatusOK, `{}`)

	_, code := runCLI(t, c, "tasks", "edit", "abc123", "--append-body", "First note.")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"markdown_content":"First note."`) {
		t.Errorf("PUT raw = %v, want the appended text without a leading separator", f.putRaw)
	}
}

func TestTaskEditBodyAndAppendConflict(t *testing.T) {
	_, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--body", "a", "--append-body", "b")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "--body and --append-body cannot be combined") {
		t.Errorf("output missing conflict message\noutput:\n%s", out)
	}
}

func TestTaskEditEmptyBodyIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--body", "  ")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "--body must not be empty") {
		t.Errorf("output missing empty-body error\noutput:\n%s", out)
	}
}

// Two locally-invalid values are still reported together, but as one
// aggregated usage error (exit 2) rather than a runtime error.
func TestTaskEditAggregatesPriorityAndDueErrors(t *testing.T) {
	_, c := newFakeClickUp(t)
	// No handlers registered: any API call would 404 and surface in output.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--priority", "blocker", "--due", "+3fortnights")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "2 fields cannot be applied") {
		t.Errorf("output missing aggregated-failure header\noutput:\n%s", out)
	}
	for _, want := range []string{"urgent, high, normal, low, none", `due "+3fortnights" is not a date`, "a relative +3days / -1week"} {
		if !strings.Contains(out, want) {
			t.Errorf("aggregated output missing %q\noutput:\n%s", want, out)
		}
	}
}

// A locally-invalid value fails fast (exit 2) even when a server-side
// field in the same call may also be bad: local syntax is validated
// before any API call, so the server-derived check never runs.
func TestTaskEditLocalUsageErrorPrecedesServerValidation(t *testing.T) {
	_, c := newFakeClickUp(t)
	// No handlers registered: resolving the assignee would need the API
	// and fail loudly; exit 2 proves the local check came first.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--priority", "blocker", "--assignee", "zoe")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)\noutput:\n%s", code, out)
	}
	if want := "valid: urgent, high, normal, low, none"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditAddsTag(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // space sp1, tags: backend
	f.spaceTags(t, "sp1", "backend", "urgent-fix", "qa")
	f.tagOps(t, "abc123")

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--add-tag", "urgent-fix")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 tags +urgent-fix"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.tagAdds) != 1 || f.tagAdds[0] != "urgent-fix" {
		t.Errorf("tag adds = %v, want [urgent-fix]", f.tagAdds)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("a tags-only edit must not PUT the task: %v", f.putRaw)
	}
}

func TestTaskEditUnknownTagRefusedWithExisting(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.spaceTags(t, "sp1", "backend", "qa")
	f.tagOps(t, "abc123")

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--add-tag", "urgnt")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{`tag "urgnt" does not exist in the space`, "existing: backend, qa"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if len(f.tagAdds) != 0 || len(f.putBodies) != 0 {
		t.Errorf("nothing may be written when a tag is unknown (adds=%v puts=%v)", f.tagAdds, f.putRaw)
	}
}

func TestTaskEditRemovesTag(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // tag backend on the task
	f.tagOps(t, "abc123")

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--remove-tag", "backend")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 tags -backend"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.tagRems) != 1 || f.tagRems[0] != "backend" {
		t.Errorf("tag removes = %v, want [backend]", f.tagRems)
	}
}

func TestTaskEditAddTagUsesSpaceCasing(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // space sp1, tags: backend
	f.spaceTags(t, "sp1", "backend", "qa")
	f.tagOps(t, "abc123")

	// The space tag is "qa"; a case-different input must write "qa",
	// not mint a duplicate "QA".
	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--add-tag", "QA")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if len(f.tagAdds) != 1 || f.tagAdds[0] != "qa" {
		t.Errorf("tag adds = %v, want [qa] (space casing)", f.tagAdds)
	}
	if want := "task: abc123 tags +qa"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditRemoveTagUsesTaskCasing(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // tag "backend" on the task
	f.tagOps(t, "abc123")

	// The tag on the task is "backend"; a case-different input must
	// DELETE "backend" so the removal actually targets the stored tag.
	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--remove-tag", "BACKEND")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if len(f.tagRems) != 1 || f.tagRems[0] != "backend" {
		t.Errorf("tag removes = %v, want [backend] (task casing)", f.tagRems)
	}
	if want := "task: abc123 tags -backend"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditAddExistingTagIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // backend already on the task
	f.spaceTags(t, "sp1", "backend", "qa")
	// No tagOps handler: a tag call would 404 and surface in output.

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--add-tag", "Backend")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (idempotent)\noutput:\n%s", code, out)
	}
	if want := "no changes (tags already as requested)"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditRemoveAbsentTagIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON) // qa not on the task

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--remove-tag", "qa")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (idempotent)\noutput:\n%s", code, out)
	}
	if want := "no changes (tags already as requested)"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditSameTagAddAndRemoveIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--add-tag", "qa", "--remove-tag", "QA")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "both --add-tag and --remove-tag") {
		t.Errorf("output missing conflict message\noutput:\n%s", out)
	}
}

func TestTaskEditTagsComposeWithFields(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.list(t, "901234", "Sprint 14", "to do", "in progress", "in review", "done")
	f.spaceTags(t, "sp1", "backend", "qa")
	f.put(t, "abc123", http.StatusOK, `{}`)
	f.tagOps(t, "abc123")

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "in review", "--add-tag", "qa")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{"status changed: in progress -> in review", "tags +qa"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if len(f.putBodies) != 1 || len(f.tagAdds) != 1 {
		t.Errorf("want 1 PUT and 1 tag add, got %d PUTs, adds %v", len(f.putBodies), f.tagAdds)
	}
}

func TestTaskEditInvalidStatusBlocksTags(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.list(t, "901234", "Sprint 14", "to do", "in progress", "in review", "done")
	f.spaceTags(t, "sp1", "backend", "qa")
	f.tagOps(t, "abc123")

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "bogus", "--add-tag", "qa")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if len(f.tagAdds) != 0 || len(f.putBodies) != 0 {
		t.Errorf("a bad field must block the whole edit, tags included (adds=%v puts=%v)", f.tagAdds, f.putRaw)
	}
}

func TestTaskEditPutFailureRollsBackTags(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.list(t, "901234", "Sprint 14", "to do", "in progress", "in review", "done")
	f.spaceTags(t, "sp1", "backend", "qa")
	f.tagOps(t, "abc123")
	f.put(t, "abc123", http.StatusInternalServerError, `{"err": "boom", "ECODE": "PUT_001"}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "in review", "--add-tag", "qa")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	// Tags go out before the PUT, so the add happened and was reverted.
	if len(f.tagAdds) != 1 || f.tagAdds[0] != "qa" {
		t.Errorf("tag adds = %v, want [qa] (applied before the PUT)", f.tagAdds)
	}
	if len(f.tagRems) != 1 || f.tagRems[0] != "qa" {
		t.Errorf("tag removes = %v, want [qa] (rolled back after the failed PUT)", f.tagRems)
	}
	if want := "tag changes rolled back, nothing applied"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskEditTagFailureRollsBackAppliedTags(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.spaceTags(t, "sp1", "backend", "qa", "api")
	f.tagOps(t, "abc123")
	f.failTagAdd(t, "abc123", "api")

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--add-tag", "qa,api")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	// qa was applied, then rolled back when api failed.
	if len(f.tagAdds) != 1 || f.tagAdds[0] != "qa" {
		t.Errorf("tag adds = %v, want [qa]", f.tagAdds)
	}
	if len(f.tagRems) != 1 || f.tagRems[0] != "qa" {
		t.Errorf("tag removes = %v, want [qa] (rollback of the applied add)", f.tagRems)
	}
	for _, want := range []string{`tag "api" could not be applied`, "tag changes rolled back, nothing applied"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if len(f.putBodies) != 0 {
		t.Errorf("no PUT may happen when a tag call fails first: %v", f.putRaw)
	}
}

func TestTaskViewShowsTags(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.comments(t, "abc123", `{"comments": []}`)

	out, code := runCLI(t, c, "tasks", "abc123")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "tags: backend"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskCommentPostsComment(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.postComment(t, "abc123", http.StatusOK, `{"id": "458", "hist_id": "26508", "date": 1568036964079}`)

	out, code := runCLI(t, c, "tasks", "comment", "abc123", "--text", "Deployed to staging")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "comment: added to task abc123"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if want := "Run `clickup-axi tasks abc123` to see the task with its comments"; !strings.Contains(out, want) {
		t.Errorf("output missing help hint %q\noutput:\n%s", want, out)
	}
	if len(f.postBodies) != 1 || f.postBodies[0]["comment_text"] != "Deployed to staging" {
		t.Errorf("POST bodies = %v, want one with comment_text \"Deployed to staging\"", f.postBodies)
	}
	if _, ok := f.postBodies[0]["notify_all"]; ok {
		t.Errorf("POST body carries notify_all, want it omitted")
	}
}

func TestTaskCommentResolvesCustomID(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "1")
	f.mux.HandleFunc("GET /api/v2/team", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"teams": [{"id": "1", "name": "Buzzwoo"}]}`))
	})
	f.task(t, "AIKK-99", taskJSON)
	f.postComment(t, "abc123", http.StatusOK, `{"id": "459"}`)

	out, code := runCLI(t, c, "tasks", "comment", "aikk-99", "--text", "ping")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "comment: added to task AIKK-99"; !strings.Contains(out, want) {
		t.Errorf("output missing %q (custom id display)\noutput:\n%s", want, out)
	}
	if len(f.postBodies) != 1 {
		t.Errorf("POST bodies = %v, want exactly one on the internal id", f.postBodies)
	}
}

func TestUnknownCustomIDIsNotFoundNotAuthError(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "1")
	f.mux.HandleFunc("GET /api/v2/team", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"teams": [{"id": "1", "name": "Buzzwoo"}]}`))
	})
	// ClickUp answers 401 for custom ids outside the token's scope.
	f.mux.HandleFunc("GET /api/v2/task/NOPE-1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"err": "Team not authorized", "ECODE": "OAUTH_027"}`))
	})

	out, code := runCLI(t, c, "tasks", "NOPE-1")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `task "NOPE-1" not found`) {
		t.Errorf("output missing not-found message\noutput:\n%s", out)
	}
	if strings.Contains(out, "token") {
		t.Errorf("unknown task id misreported as an auth failure\noutput:\n%s", out)
	}
}

func TestTaskCommentMissingTextIsUsageError(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)

	out, code := runCLI(t, c, "tasks", "comment", "abc123")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks comment needs --text") {
		t.Errorf("output missing --text requirement\noutput:\n%s", out)
	}
	if len(f.postBodies) != 0 {
		t.Errorf("POST was called despite the usage error")
	}
}

func TestTaskCommentEmptyTextIsUsageError(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)

	out, code := runCLI(t, c, "tasks", "comment", "abc123", "--text", "   ")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "comment text must not be empty") {
		t.Errorf("output missing empty-text error\noutput:\n%s", out)
	}
}

func TestTaskCommentUnquotedTextIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "tasks", "comment", "abc123", "--text", "two", "words")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "quote the comment text") {
		t.Errorf("output missing quoting hint\noutput:\n%s", out)
	}
}

func TestTaskCommentUnknownFlagIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "tasks", "comment", "abc123", "--body", "hi")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "valid: --text") {
		t.Errorf("usage error does not list valid flags inline\noutput:\n%s", out)
	}
}

func TestUnknownFlagExitsWithUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "tasks", "abc123", "--bogus")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "valid: --comments N, --no-comments, --full, --fields") {
		t.Errorf("usage error does not list valid flags inline\noutput:\n%s", out)
	}
}

func TestMissingTokenIsStructuredError(t *testing.T) {
	c := clickup.New("http://127.0.0.1:1", "", http.DefaultClient)
	out, code := runCLI(t, c, "tasks", "abc123")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, clickup.ErrNoAuth) {
		t.Errorf("output missing token guidance\noutput:\n%s", out)
	}
	if !strings.Contains(out, "auth login") {
		t.Errorf("output missing auth login hint\noutput:\n%s", out)
	}
}
