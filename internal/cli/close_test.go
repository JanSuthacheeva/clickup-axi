package cli

import (
	"net/http"
	"strings"
	"testing"
)

// sprintListJSON is the list backing the close tests: statuses carry
// their ClickUp type so close can resolve the closed-type target.
const sprintListJSON = `{"id": "901234", "name": "Sprint 14", "statuses": [
	{"status": "to do", "type": "open"},
	{"status": "in progress", "type": "custom"},
	{"status": "Closed", "type": "closed"}
]}`

// listJSON registers GET /list/{id} with a raw body, for statuses that
// need more than the name-only shape the list helper writes.
func (f *fakeClickUp) listJSON(t *testing.T, id, body string) {
	t.Helper()
	f.mux.HandleFunc("GET /api/v2/list/"+id, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
}

func TestTaskCloseDryRunPreviewsAndWritesNothing(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.listJSON(t, "901234", sprintListJSON)

	out, code := runCLI(t, c, "tasks", "close", "abc123")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := `task: abc123 "Fix login redirect" would be closed (dry run, nothing changed)
  status: in progress -> Closed
  a closed task leaves the default tasks, search, and context listings
help[2]:
  Run ` + "`clickup-axi tasks close abc123 --yes`" + ` to close it
  Run ` + "`clickup-axi tasks abc123`" + ` to review it first
`
	if out != want {
		t.Errorf("dry-run output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("dry run issued %d PUTs, want 0", len(f.putBodies))
	}
}

func TestTaskCloseYesClosesAndHintsReopen(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.listJSON(t, "901234", sprintListJSON)
	f.put(t, "abc123", http.StatusOK, `{}`)

	out, code := runCLI(t, c, "tasks", "close", "abc123", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := `task: abc123 closed: in progress -> Closed
help[1]: Run ` + "`clickup-axi tasks edit abc123 --status \"in progress\"`" + ` to reopen it
`
	if out != want {
		t.Errorf("close output mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
	if len(f.putBodies) != 1 {
		t.Fatalf("close issued %d PUTs, want 1", len(f.putBodies))
	}
	if got := f.putBodies[0]["status"]; got != "Closed" {
		t.Errorf(`PUT status = %v, want "Closed"`, got)
	}
	if len(f.putBodies[0]) != 1 {
		t.Errorf("PUT body carries extra fields: %v", f.putBodies[0])
	}
}

// Already closed is a stated idempotent no-op with or without --yes:
// no list fetch (none is registered) and no PUT.
func TestTaskCloseAlreadyClosedIsNoOp(t *testing.T) {
	closedTask := strings.Replace(taskJSON,
		`"status": {"status": "in progress"}`,
		`"status": {"status": "Closed", "type": "closed"}`, 1)
	for _, args := range [][]string{
		{"tasks", "close", "abc123"},
		{"tasks", "close", "abc123", "--yes"},
	} {
		f, c := newFakeClickUp(t)
		f.task(t, "abc123", closedTask)

		out, code := runCLI(t, c, args...)
		if code != 0 {
			t.Fatalf("%v: exit code = %d, want 0\noutput:\n%s", args, code, out)
		}
		if want := "task: abc123 no changes (already closed)\n"; out != want {
			t.Errorf("%v: output mismatch\ngot:\n%s\nwant:\n%s", args, out, want)
		}
		if len(f.putBodies) != 0 {
			t.Errorf("%v: no-op issued %d PUTs, want 0", args, len(f.putBodies))
		}
	}
}

// With forced custom ids the preview, hints, and confirmation all speak
// the workspace's custom-id language.
func TestTaskCloseForcedCustomIDs(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "1")
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/task/AIKK-99", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(taskJSON))
	})
	f.listJSON(t, "901234", sprintListJSON)

	out, code := runCLI(t, c, "tasks", "close", "AIKK-99")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		`task: AIKK-99 "Fix login redirect" would be closed`,
		"Run `clickup-axi tasks close AIKK-99 --yes` to close it",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if strings.Contains(out, "abc123") {
		t.Errorf("forced custom ids must hide the internal id\noutput:\n%s", out)
	}
}

func TestTaskCloseUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing id", []string{"tasks", "close"}, "tasks close needs a task id"},
		{"unknown flag", []string{"tasks", "close", "--force", "abc123"}, `unknown flag "--force" for tasks close`},
		{"second id", []string{"tasks", "close", "abc123", "def456"}, "tasks close takes exactly one task id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// No handlers registered: a usage error must be decided
			// before any API call.
			_, c := newFakeClickUp(t)
			out, code := runCLI(t, c, tc.args...)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output missing %q\noutput:\n%s", tc.want, out)
			}
		})
	}
	t.Run("unknown flag lists valid", func(t *testing.T) {
		_, c := newFakeClickUp(t)
		out, _ := runCLI(t, c, "tasks", "close", "--force", "abc123")
		if !strings.Contains(out, "valid: --yes") {
			t.Errorf("output missing the valid flag set\noutput:\n%s", out)
		}
	})
}

// A list without a closed-type status cannot be closed into; close
// refuses rather than guessing a terminal status.
func TestTaskCloseNoClosedStatusIsError(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.listJSON(t, "901234", `{"id": "901234", "name": "Sprint 14", "statuses": [
		{"status": "to do", "type": "open"},
		{"status": "in progress", "type": "custom"}
	]}`)

	out, code := runCLI(t, c, "tasks", "close", "abc123", "--yes")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if want := `list 901234 "Sprint 14" has no closed status`; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if want := "--status \"<status>\"` to set a status directly"; !strings.Contains(out, want) {
		t.Errorf("output missing the edit fallback hint\noutput:\n%s", want)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("refused close issued %d PUTs, want 0", len(f.putBodies))
	}
}

// Close must know its target status, so a failed list fetch is a hard
// error - unlike edit's advisory status check, which degrades silently.
func TestTaskCloseListFetchFailureIsError(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", taskJSON)
	f.mux.HandleFunc("GET /api/v2/list/901234", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"err": "boom", "ECODE": "LIST_001"}`))
	})

	out, code := runCLI(t, c, "tasks", "close", "abc123")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("failed close issued %d PUTs, want 0", len(f.putBodies))
	}
}

func TestTaskCloseTaskFetchFailureIsError(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.mux.HandleFunc("GET /api/v2/task/abc123", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"err": "Task not found, ECODE", "ECODE": "ITEM_013"}`))
	})

	out, code := runCLI(t, c, "tasks", "close", "abc123")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
}
