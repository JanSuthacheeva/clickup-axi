package cli

import (
	"bytes"
	"encoding/json"
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
	mux        *http.ServeMux
	putBodies  []map[string]any
	putRaw     []string
	postBodies []map[string]string
	commentGET int
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

const taskJSON = `{
	"id": "abc123",
	"custom_id": "AIKK-99",
	"name": "Fix login redirect",
	"text_content": "After OAuth callback the user lands on a 404.",
	"status": {"status": "in progress"},
	"priority": {"priority": "high"},
	"due_date": "1783296000000",
	"url": "https://app.clickup.com/t/abc123",
	"assignees": [{"id": 1, "username": "jan"}],
	"list": {"id": "901234", "name": "Sprint 14"}
}`

const commentsJSON = `{"comments": [
	{"id": "3", "comment_text": "Repro'd on staging, with Safari", "user": {"id": 2, "username": "mia"}, "date": 1782950400000},
	{"id": "2", "comment_text": "Suspect the state param", "user": {"id": 1, "username": "jan"}, "date": "1782864000000"},
	{"id": "1", "comment_text": "Customer report", "user": {"id": 3, "username": "tom"}, "date": 1782777600000}
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
		"comments: showing 3 of 3 (newest first)",
		"comments[3]{author,date,text}:",
		`mia,2026-07-02,"Repro'd on staging, with Safari"`,
		"jan,2026-07-01,Suspect the state param",
		"help[",
	} {
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
	if !strings.Contains(out, "no changes") {
		t.Errorf("output missing no-op acknowledgement\noutput:\n%s", out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT was called for a no-op status change")
	}
}

func TestTaskEditInvalidStatusListsValidOnes(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.put(t, "abc123", http.StatusBadRequest, `{"err": "Status not found", "ECODE": "CRTSK_001"}`)
	f.mux.HandleFunc("GET /api/v2/list/901234", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id": "901234", "name": "Sprint 14", "statuses": [
			{"status": "to do"}, {"status": "in progress"}, {"status": "in review"}, {"status": "done"}
		]}`))
	})

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--status", "qa")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if want := "valid: to do, in progress, in review, done"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if strings.Contains(out, "Status not found") {
		t.Errorf("raw ClickUp error message leaked to output\noutput:\n%s", out)
	}
}

// editTaskJSON gives the task a current assignee (jan, id 42) that lines
// up with membersTeamJSON (jan 42, Ting Nguyen 189, Tinh Tran 190), so
// add / remove / idempotency all resolve against consistent ids.
const editTaskJSON = `{
	"id": "abc123",
	"custom_id": "AIKK-99",
	"name": "Fix login redirect",
	"status": {"status": "in progress"},
	"url": "https://app.clickup.com/t/abc123",
	"assignees": [{"id": 42, "username": "jan"}],
	"list": {"id": "901234", "name": "Sprint 14"}
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

func TestTaskEditAddsByNumericIDWithoutName(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)
	f.meWithTeams(t, 42, "jan", membersTeamJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--assignee", "190")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 assignees +190"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"add":[190]`) {
		t.Errorf("PUT raw = %v, want assignees.add [190]", f.putRaw)
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
	if len(f.putBodies) != 2 {
		t.Errorf("want 2 PUTs (status + assignees), got %d: %v", len(f.putBodies), f.putRaw)
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

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--priority", "high")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if want := "valid: --status, --assignee, --unassign"; !strings.Contains(out, want) {
		t.Errorf("output missing valid-flag list\noutput:\n%s", out)
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
	if !strings.Contains(out, "valid: --comments N, --no-comments, --full") {
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
