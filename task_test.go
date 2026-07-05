package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeClickUp struct {
	mux        *http.ServeMux
	putBodies  []map[string]string
	commentGET int
}

func newFakeClickUp(t *testing.T) (*fakeClickUp, *client) {
	t.Helper()
	f := &fakeClickUp{mux: http.NewServeMux()}
	srv := httptest.NewServer(f.mux)
	t.Cleanup(srv.Close)
	c := &client{base: srv.URL + "/api/v2", token: "pk_test", http: &http.Client{Timeout: 5 * time.Second}}
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

func (f *fakeClickUp) put(t *testing.T, taskID string, status int, response string) {
	t.Helper()
	f.mux.HandleFunc("PUT /api/v2/task/"+taskID, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("PUT body did not decode: %v", err)
		}
		f.putBodies = append(f.putBodies, body)
		w.WriteHeader(status)
		w.Write([]byte(response))
	})
}

const taskJSON = `{
	"id": "abc123",
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

func runCLI(t *testing.T, c *client, args ...string) (string, int) {
	t.Helper()
	return runCLIWithStdin(t, c, "", args...)
}

func runCLIWithStdin(t *testing.T, c *client, stdin string, args ...string) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	code := run(args, c, strings.NewReader(stdin), &buf)
	return buf.String(), code
}

func TestTaskViewIncludesComments(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.comments(t, "abc123", commentsJSON)

	out, code := runCLI(t, c, "task", "view", "abc123")
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

	out, code := runCLI(t, c, "task", "view", "abc123")
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

	out, code := runCLI(t, c, "task", "view", "abc123", "--no-comments")
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

	out, code := runCLI(t, c, "task", "edit", "abc123", "--status", "in review")
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

	out, code := runCLI(t, c, "task", "edit", "abc123", "--status", "In Progress")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (idempotent no-op)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "(no-op)") {
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

	out, code := runCLI(t, c, "task", "edit", "abc123", "--status", "qa")
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

func TestUnknownFlagExitsWithUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "task", "view", "abc123", "--bogus")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "valid: --comments N, --no-comments, --full") {
		t.Errorf("usage error does not list valid flags inline\noutput:\n%s", out)
	}
}

func TestMissingTokenIsStructuredError(t *testing.T) {
	c := &client{base: "http://127.0.0.1:1", token: "", http: http.DefaultClient}
	out, code := runCLI(t, c, "task", "view", "abc123")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, errNoAuth) {
		t.Errorf("output missing token guidance\noutput:\n%s", out)
	}
	if !strings.Contains(out, "auth login") {
		t.Errorf("output missing auth login hint\noutput:\n%s", out)
	}
}

func TestToonCellEscaping(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a, b", `"a, b"`},
		{`say "hi"`, `"say \"hi\""`},
		{"line\nbreak", "line break"},
	}
	for _, tc := range cases {
		if got := toonCell(tc.in); got != tc.want {
			t.Errorf("toonCell(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
