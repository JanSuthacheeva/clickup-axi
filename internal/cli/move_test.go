package cli

import (
	"io"
	"net/http"
	"testing"
)

// moveTaskJSON backs the move tests: unlike taskJSON it carries the
// status id that a status mapping pairs with a target status id.
const moveTaskJSON = `{
	"id": "abc123",
	"name": "Fix login redirect",
	"status": {"id": "st_src", "status": "in progress", "type": "custom"},
	"list": {"id": "901234", "name": "Sprint 14"},
	"space": {"id": "90121"}
}`

// moveEndpoint registers the v3 home_list PUT and records its raw
// bodies, so tests can assert exactly what a move sends.
func (f *fakeClickUp) moveEndpoint(t *testing.T, teamID, taskID, listID string, status int, response string) {
	t.Helper()
	f.mux.HandleFunc("PUT /api/v3/workspaces/"+teamID+"/tasks/"+taskID+"/home_list/"+listID,
		func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			f.moveBodies = append(f.moveBodies, string(raw))
			w.WriteHeader(status)
			w.Write([]byte(response))
		})
}

func TestTaskMoveKeepsMatchingStatus(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.task(t, "abc123", moveTaskJSON)
	f.listJSON(t, "905678", `{"id": "905678", "name": "Sprint 15", "statuses": [
		{"id": "st_a", "status": "to do", "type": "open"},
		{"id": "st_b", "status": "in progress", "type": "custom"}
	]}`)
	f.moveEndpoint(t, "9018", "abc123", "905678", http.StatusOK, `{"data": {}}`)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "905678")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := `task: abc123 "Fix login redirect" moved
  list: Sprint 14 (901234) -> Sprint 15 (905678)
  status: in progress (kept)
`
	if out != want {
		t.Errorf("move output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.moveBodies) != 1 {
		t.Fatalf("move issued %d v3 PUTs, want 1", len(f.moveBodies))
	}
	// A status the target already has must not ride along as a mapping:
	// the endpoint rejects superfluous mappings.
	if f.moveBodies[0] != "{}" {
		t.Errorf("move body = %s, want {}", f.moveBodies[0])
	}
}

func TestTaskMoveRemapsWithExplicitStatus(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.task(t, "abc123", moveTaskJSON)
	f.listJSON(t, "905678", `{"id": "905678", "name": "Icebox", "statuses": [
		{"id": "st_a", "status": "Backlog", "type": "open"},
		{"id": "st_b", "status": "done", "type": "closed"}
	]}`)
	f.moveEndpoint(t, "9018", "abc123", "905678", http.StatusOK, `{"data": {}}`)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "905678", "--status", "backlog")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	// The confirmation echoes the target's stored casing, not the input's.
	want := `task: abc123 "Fix login redirect" moved
  list: Sprint 14 (901234) -> Icebox (905678)
  status: in progress -> Backlog
`
	if out != want {
		t.Errorf("move output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.moveBodies) != 1 {
		t.Fatalf("move issued %d v3 PUTs, want 1", len(f.moveBodies))
	}
	wantBody := `{"status_mappings":[{"source_status":"st_src","destination_status":"st_a"}]}`
	if f.moveBodies[0] != wantBody {
		t.Errorf("move body = %s\nwant %s", f.moveBodies[0], wantBody)
	}
}

func TestTaskMoveStatusMissingInTargetRefusesWithVocabulary(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", moveTaskJSON)
	f.listJSON(t, "905678", `{"id": "905678", "name": "Icebox", "statuses": [
		{"id": "st_a", "status": "Backlog", "type": "open"},
		{"id": "st_b", "status": "done", "type": "closed"}
	]}`)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "905678")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	want := `error: status "in progress" does not exist in list 905678 "Icebox"
  target list statuses: Backlog, done
help[1]: Run ` + "`clickup-axi tasks move abc123 --list 905678 --status \"<status>\"`" + ` to pick the status it lands in
`
	if out != want {
		t.Errorf("error output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.moveBodies) != 0 {
		t.Errorf("refusal issued %d v3 PUTs, want 0", len(f.moveBodies))
	}
}

func TestTaskMoveUnknownStatusRefusesWithVocabulary(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", moveTaskJSON)
	f.listJSON(t, "905678", `{"id": "905678", "name": "Icebox", "statuses": [
		{"id": "st_a", "status": "Backlog", "type": "open"}
	]}`)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "905678", "--status", "review")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	want := `error: status "review" does not exist in list 905678 "Icebox"
  target list statuses: Backlog
help[1]: Run ` + "`clickup-axi tasks move abc123 --list 905678 --status \"<status>\"`" + ` with one of the statuses above
`
	if out != want {
		t.Errorf("error output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.moveBodies) != 0 {
		t.Errorf("refusal issued %d v3 PUTs, want 0", len(f.moveBodies))
	}
}

// A --status the move does not need is refused, never smuggled into a
// second write: the endpoint rejects mappings for statuses the target
// already has, and a silent follow-up edit would break one-command
// atomicity.
func TestTaskMoveSuperfluousStatusRefused(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", moveTaskJSON)
	f.listJSON(t, "905678", `{"id": "905678", "name": "Sprint 15", "statuses": [
		{"id": "st_a", "status": "in progress", "type": "custom"},
		{"id": "st_b", "status": "done", "type": "closed"}
	]}`)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "905678", "--status", "done")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	want := `error: status "in progress" exists in list 905678 "Sprint 15", so the task keeps it on this move; --status only picks the landing status when the target list lacks the current one
help[1]: Run ` + "`clickup-axi tasks move abc123 --list 905678`" + ` and then ` + "`clickup-axi tasks edit abc123 --status \"done\"`" + ` to change it
`
	if out != want {
		t.Errorf("error output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.moveBodies) != 0 {
		t.Errorf("refusal issued %d v3 PUTs, want 0", len(f.moveBodies))
	}
}

// --status naming the status the task already has is not a remap and
// not an error: the move proceeds and keeps it.
func TestTaskMoveStatusEqualToCurrentIsKept(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.task(t, "abc123", moveTaskJSON)
	f.listJSON(t, "905678", `{"id": "905678", "name": "Sprint 15", "statuses": [
		{"id": "st_b", "status": "in progress", "type": "custom"}
	]}`)
	f.moveEndpoint(t, "9018", "abc123", "905678", http.StatusOK, `{"data": {}}`)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "905678", "--status", "IN PROGRESS")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := `task: abc123 "Fix login redirect" moved
  list: Sprint 14 (901234) -> Sprint 15 (905678)
  status: in progress (kept)
`
	if out != want {
		t.Errorf("move output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.moveBodies) != 1 || f.moveBodies[0] != "{}" {
		t.Errorf("move bodies = %v, want one {}", f.moveBodies)
	}
}

// Already home in the target is a stated no-op decided before any list
// fetch or write (neither is registered on the fake).
func TestTaskMoveAlreadyInListIsNoOp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", moveTaskJSON)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "901234")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if want := "task: abc123 no changes (already in list 901234 \"Sprint 14\")\n"; out != want {
		t.Errorf("no-op output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.moveBodies) != 0 {
		t.Errorf("no-op issued %d v3 PUTs, want 0", len(f.moveBodies))
	}
}

func TestTaskMoveResolvesListNameThroughSpace(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.task(t, "abc123", moveTaskJSON)
	f.spaces(t, "9018", twoSpacesJSON)
	f.sprintLists(t, "")
	f.listJSON(t, "901235", `{"id": "901235", "name": "Backlog", "statuses": [
		{"id": "st_b", "status": "in progress", "type": "custom"}
	]}`)
	f.moveEndpoint(t, "9018", "abc123", "901235", http.StatusOK, `{"data": {}}`)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "backlog", "--space", "Webshop")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := `task: abc123 "Fix login redirect" moved
  list: Sprint 14 (901234) -> Backlog (901235)
  status: in progress (kept)
`
	if out != want {
		t.Errorf("move output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestTaskMoveListNameNeedsSpace(t *testing.T) {
	f, c := newFakeClickUp(t)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "Sprint 12")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	want := `error: --list by name needs --space (list names are only unique within one space)
help[2]:
  Run ` + "`clickup-axi tasks move <id> --list \"<list>\" --space \"<space>\"`" + `
  Or use the list id from ` + "`clickup-axi lists --space \"<space>\"`" + `
`
	if out != want {
		t.Errorf("usage error mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.moveBodies) != 0 {
		t.Errorf("usage error issued %d v3 PUTs, want 0", len(f.moveBodies))
	}
}

func TestTaskMoveUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing id and list", []string{"tasks", "move"},
			"error: tasks move needs a task id and --list (the target list)\nhelp[2]:\n  Run `clickup-axi tasks move <id> --list <name|id>`\n  Run `clickup-axi lists --space \"<space>\"` to discover lists\n"},
		{"missing list", []string{"tasks", "move", "abc123"},
			"error: tasks move needs a task id and --list (the target list)\nhelp[2]:\n  Run `clickup-axi tasks move <id> --list <name|id>`\n  Run `clickup-axi lists --space \"<space>\"` to discover lists\n"},
		{"unknown flag", []string{"tasks", "move", "abc123", "--folder", "x"},
			"error: unknown flag \"--folder\" for tasks move\n  valid: --list, --space, --status\n"},
		{"two ids", []string{"tasks", "move", "abc123", "def456", "--list", "901234"},
			"error: tasks move takes exactly one task id\n"},
		{"list needs value", []string{"tasks", "move", "abc123", "--list"},
			"error: --list needs a value\nhelp[1]: Run `clickup-axi tasks move <id> --list <name|id>`\n"},
		{"status needs value", []string{"tasks", "move", "abc123", "--list", "901234", "--status"},
			"error: --status needs a value\nhelp[1]: Run `clickup-axi tasks move <id> --list <name|id> --status \"<status>\"`\n"},
		{"list swallows flag", []string{"tasks", "move", "abc123", "--list", "--status", "done"},
			"error: --list needs a value\nhelp[1]: Run `clickup-axi tasks move <id> --list <name|id>`\n"},
		{"space swallows flag", []string{"tasks", "move", "abc123", "--list", "901234", "--space", "--status"},
			"error: --space needs a value\nhelp[1]: Run `clickup-axi tasks move <id> --list \"<list>\" --space \"<space>\"`\n"},
		{"status swallows flag", []string{"tasks", "move", "abc123", "--list", "901234", "--status", "--space"},
			"error: --status needs a value\nhelp[1]: Run `clickup-axi tasks move <id> --list <name|id> --status \"<status>\"`\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c := newFakeClickUp(t)
			out, code := runCLI(t, c, tc.args...)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
			}
			if out != tc.want {
				t.Errorf("usage error mismatch\ngot:\n%s\nwant:\n%s", out, tc.want)
			}
		})
	}
}

func TestTaskMoveUnknownListID(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", moveTaskJSON)
	f.mux.HandleFunc("GET /api/v2/list/999999", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"err": "List not found", "ECODE": "LIST_001"}`))
	})

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "999999")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	want := `error: list "999999" not found
help[1]: Run ` + "`clickup-axi lists --space \"<space>\"`" + ` to discover list ids
`
	if out != want {
		t.Errorf("error output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
}

// The v3 move failure translates like every other API error; v3 error
// bodies carry {message} instead of v2's {err}.
func TestTaskMoveAPIFailureTranslates(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.task(t, "abc123", moveTaskJSON)
	f.listJSON(t, "905678", `{"id": "905678", "name": "Sprint 15", "statuses": [
		{"id": "st_b", "status": "in progress", "type": "custom"}
	]}`)
	f.moveEndpoint(t, "9018", "abc123", "905678", http.StatusBadRequest,
		`{"status": 400, "message": "Invalid status mappings"}`)

	out, code := runCLI(t, c, "tasks", "move", "abc123", "--list", "905678")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if want := "error: ClickUp rejected the request: Invalid status mappings (HTTP 400)\n"; out != want {
		t.Errorf("error output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
}
