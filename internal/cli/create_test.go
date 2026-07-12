package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// postTask registers POST /list/{id}/task and records each request
// body, so tests can assert both the CLI output and the exact wire
// payload (or that no create was attempted at all).
func (f *fakeClickUp) postTask(t *testing.T, listID string, status int, response string) {
	t.Helper()
	f.mux.HandleFunc("POST /api/v2/list/"+listID+"/task", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("POST body did not decode: %v", err)
		}
		f.createBodies = append(f.createBodies, body)
		w.WriteHeader(status)
		w.Write([]byte(response))
	})
}

// listInSpace registers GET /list/{id} including the space context the
// create path uses for tag validation.
func (f *fakeClickUp) listInSpace(t *testing.T, id, name, spaceID string, statuses ...string) {
	t.Helper()
	items := make([]string, len(statuses))
	for i, s := range statuses {
		items[i] = fmt.Sprintf(`{"status": %q}`, s)
	}
	body := fmt.Sprintf(`{"id": %q, "name": %q, "space": {"id": %q}, "statuses": [%s]}`,
		id, name, spaceID, strings.Join(items, ", "))
	f.mux.HandleFunc("GET /api/v2/list/"+id, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
}

// sprintLists registers space 90121's inventory: a folderless "Sprint
// 12" and "Backlog", plus folder f1 "Development"; extraFolderLists
// lets a test add a colliding list inside the folder.
func (f *fakeClickUp) sprintLists(t *testing.T, extraFolderLists string) {
	t.Helper()
	f.mux.HandleFunc("GET /api/v2/space/90121/list", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"lists": [{"id": "901234", "name": "Sprint 12"}, {"id": "901235", "name": "Backlog"}]}`))
	})
	f.mux.HandleFunc("GET /api/v2/space/90121/folder", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"folders": [{"id": "f1", "name": "Development"}]}`))
	})
	f.mux.HandleFunc("GET /api/v2/folder/f1/list", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"lists": [%s]}`, extraFolderLists)
	})
}

const createdTaskJSON = `{
	"id": "new1",
	"name": "Fix login flow",
	"status": {"status": "to do"},
	"url": "https://app.clickup.com/t/new1",
	"list": {"id": "901234", "name": "Sprint 12"}
}`

func TestTaskCreateMinimalByListId(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.listInSpace(t, "901234", "Sprint 12", "90121", "to do", "done")
	f.postTask(t, "901234", http.StatusOK, createdTaskJSON)

	out, code := runCLI(t, c, "tasks", "create", "Fix login flow", "--list", "901234")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := `task: created new1 "Fix login flow"
  list: Sprint 12 (901234)
  status: to do
  url: https://app.clickup.com/t/new1
help[2]:
  Run ` + "`clickup-axi tasks new1`" + ` to see the task
  Run ` + "`clickup-axi tasks edit new1 ...`" + ` to change its fields
`
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if len(f.createBodies) != 1 {
		t.Fatalf("POST count = %d, want 1", len(f.createBodies))
	}
	if len(f.createBodies[0]) != 1 || f.createBodies[0]["name"] != "Fix login flow" {
		t.Errorf("POST body = %#v, want only the name", f.createBodies[0])
	}
}

func TestTaskCreateFullFieldsByListName(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", `{"teams": [{"id": "9018", "name": "Buzzwoo", "members": [{"user": {"id": 42, "username": "jan"}}]}]}`)
	f.spaces(t, "9018", twoSpacesJSON)
	f.sprintLists(t, "")
	f.listInSpace(t, "901234", "Sprint 12", "90121", "to do", "in review", "done")
	// The stored casing differs from the input to prove canonicalization.
	f.spaceTags(t, "90121", "API", "bug")
	f.postTask(t, "901234", http.StatusOK, createdTaskJSON)

	out, code := runCLI(t, c, "tasks", "create", "Fix login flow",
		"--list", "sprint 12", "--space", "webshop", "--status", "in review",
		"--assignee", "me", "--priority", "high", "--due", "2026-08-01",
		"--body", "## Steps", "--tag", "api")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `task: created new1 "Fix login flow"`) {
		t.Errorf("output missing creation line\noutput:\n%s", out)
	}
	if len(f.createBodies) != 1 {
		t.Fatalf("POST count = %d, want 1", len(f.createBodies))
	}
	body := f.createBodies[0]
	wantDue := float64(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC).UnixMilli())
	for key, want := range map[string]any{
		"name":             "Fix login flow",
		"status":           "in review",
		"priority":         float64(2),
		"due_date":         wantDue,
		"due_date_time":    false,
		"markdown_content": "## Steps",
	} {
		if body[key] != want {
			t.Errorf("POST body[%q] = %#v, want %#v", key, body[key], want)
		}
	}
	if got := fmt.Sprintf("%v", body["assignees"]); got != "[42]" {
		t.Errorf("POST assignees = %v, want [42]", body["assignees"])
	}
	if got := fmt.Sprintf("%v", body["tags"]); got != "[API]" {
		t.Errorf("POST tags = %v, want the stored casing [API]", body["tags"])
	}
}

func TestTaskCreateListNameNeedsSpace(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "tasks", "create", "Fix login", "--list", "Sprint 12")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "--list by name needs --space (list names are only unique within one space)") {
		t.Errorf("output missing the scoping error\noutput:\n%s", out)
	}
}

func TestTaskCreateAmbiguousListNameInlinesCandidates(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.spaces(t, "9018", twoSpacesJSON)
	f.sprintLists(t, `{"id": "901236", "name": "Sprint 12"}`)

	out, code := runCLI(t, c, "tasks", "create", "Fix login", "--list", "Sprint 12", "--space", "Webshop")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	want := `error: list "Sprint 12" is ambiguous: 901234 "Sprint 12" (folderless), 901236 "Sprint 12" (in Development)`
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
}

func TestTaskCreateNeedsAName(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "tasks", "create", "--list", "901234")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks create needs a task name") {
		t.Errorf("output missing the name error\noutput:\n%s", out)
	}
}

func TestTaskCreateNeedsAListOrParent(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "tasks", "create", "Fix login")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"tasks create needs --list (the list to create in) or --parent (for a subtask)",
		"Run `clickup-axi lists --space \"<space>\"` to discover lists",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestTaskCreateAggregatesUsageErrors(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "tasks", "create", "Fix login", "--list", "901234",
		"--priority", "asap", "--due", "tomorrow")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"2 fields cannot be applied (nothing was created):",
		`- priority "asap" not accepted`,
		`- due "tomorrow" is not a date`,
		"Fix all the values above, then rerun `clickup-axi tasks create ...` once",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestTaskCreateAggregatesPreflightErrors(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", `{"teams": [{"id": "9018", "name": "Buzzwoo", "members": [{"user": {"id": 7, "username": "ting"}}]}]}`)
	f.listInSpace(t, "901234", "Sprint 12", "90121", "to do", "done")
	f.spaceTags(t, "90121", "api", "bug")
	f.postTask(t, "901234", http.StatusOK, createdTaskJSON)

	out, code := runCLI(t, c, "tasks", "create", "Fix login", "--list", "901234",
		"--status", "qa", "--assignee", "mia", "--tag", "urgnt")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"3 fields cannot be applied (nothing was created):",
		`- status "qa" not accepted in list Sprint 12`,
		"valid: to do, done",
		`- assignee "mia" matches none of the members of Buzzwoo`,
		`- tag "urgnt" does not exist in the space`,
		"existing: api, bug",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if len(f.createBodies) != 0 {
		t.Errorf("POST count = %d, want 0 (nothing may be created)", len(f.createBodies))
	}
}

func TestTaskCreateParentDerivesTheList(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.postTask(t, "901234", http.StatusOK, createdTaskJSON)

	out, code := runCLI(t, c, "tasks", "create", "Test the redirect", "--parent", "abc123")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		`task: created new1 "Fix login flow"`,
		"list: Sprint 12 (901234)",
		"parent: abc123",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if len(f.createBodies) != 1 {
		t.Fatalf("POST count = %d, want 1", len(f.createBodies))
	}
	if f.createBodies[0]["parent"] != "abc123" {
		t.Errorf("POST parent = %#v, want abc123", f.createBodies[0]["parent"])
	}
}

func TestTaskCreateParentConflictingListIsRefused(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.postTask(t, "901234", http.StatusOK, createdTaskJSON)

	out, code := runCLI(t, c, "tasks", "create", "Test the redirect", "--parent", "abc123", "--list", "999999")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	want := `--list "999999" does not match the parent task's list 901234 "Sprint 14"`
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.createBodies) != 0 {
		t.Errorf("POST count = %d, want 0 (nothing may be created)", len(f.createBodies))
	}
}

func TestTaskCreateUnknownListIdSuggestsDiscovery(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.mux.HandleFunc("GET /api/v2/list/901234", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"err": "List not found"}`))
	})

	out, code := runCLI(t, c, "tasks", "create", "Fix login", "--list", "901234")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		`error: list "901234" not found`,
		"Run `clickup-axi lists --space \"<space>\"` to discover list ids",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestTaskCreatePostFailureIsTranslated(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.listInSpace(t, "901234", "Sprint 12", "90121", "to do")
	f.postTask(t, "901234", http.StatusInternalServerError, `{"err": "boom", "ECODE": "TASK_001"}`)

	out, code := runCLI(t, c, "tasks", "create", "Fix login", "--list", "901234")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "error: ClickUp rejected the request: boom (HTTP 500)") {
		t.Errorf("output missing the translated error\noutput:\n%s", out)
	}
}

func TestTaskCreateShowsCustomIdWhenForced(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "1")
	f.listInSpace(t, "901234", "Sprint 12", "90121", "to do")
	f.postTask(t, "901234", http.StatusOK, `{
		"id": "new1", "custom_id": "HGAI-77", "name": "Fix login flow",
		"status": {"status": "to do"}, "url": "https://app.clickup.com/t/new1",
		"list": {"id": "901234", "name": "Sprint 12"}
	}`)

	out, code := runCLI(t, c, "tasks", "create", "Fix login flow", "--list", "901234")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		`task: created HGAI-77 "Fix login flow"`,
		"Run `clickup-axi tasks HGAI-77` to see the task",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestTaskCreateUnknownFlagListsTheValidOnes(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "tasks", "create", "Fix login", "--list", "901234", "--bogus")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		`unknown flag "--bogus" for tasks create`,
		"valid: " + createValidFlags,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestTaskCreateEmptyBodyIsAUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "tasks", "create", "Fix login", "--list", "901234", "--body", "   ")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "--body must not be empty") {
		t.Errorf("output missing the empty-body error\noutput:\n%s", out)
	}
}

// TestTaskCreateSecondNameIsRefused pins the quoting recovery: a
// multi-word name passed unquoted must not silently create a task
// named by its first word.
func TestTaskCreateSecondNameIsRefused(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "tasks", "create", "Fix", "login", "--list", "901234")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks create takes exactly one name (quote it)") {
		t.Errorf("output missing the quoting error\noutput:\n%s", out)
	}
}
