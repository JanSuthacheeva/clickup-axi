package cli

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
)

func (f *fakeClickUp) me(t *testing.T, id int64, username string) {
	t.Helper()
	f.meWithTeams(t, id, username, `{"teams": [{"id": "9018", "name": "Buzzwoo"}]}`)
}

func (f *fakeClickUp) meWithTeams(t *testing.T, id int64, username, teamsJSON string) {
	t.Helper()
	f.mux.HandleFunc("GET /api/v2/user", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"user": {"id": %d, "username": %q}}`, id, username)
	})
	f.mux.HandleFunc("GET /api/v2/team", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(teamsJSON))
	})
}

const twoTeamsJSON = `{"teams": [{"id": "9001", "name": "BUZZWOO"}, {"id": "9002", "name": "Personal"}]}`

func TestTasksListsOpenAssignedTasks(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("assignees[]"); got != "42" {
			t.Errorf("assignees[] = %q, want %q", got, "42")
		}
		if got := r.URL.Query().Get("subtasks"); got != "true" {
			t.Errorf("subtasks = %q, want true", got)
		}
		w.Write([]byte(`{"tasks": [
			{"id": "86ey1", "name": "Fix login, redirect", "status": {"status": "in progress"}, "due_date": "1783296000000"},
			{"id": "86ey2", "name": "QA checkout", "status": {"status": "to do"}, "due_date": null}
		]}`))
	})

	out, code := runCLI(t, c, "tasks")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"tasks: 2 open tasks assigned to jan",
		"tasks[2]{id,title,status,due}:",
		`86ey1,"Fix login, redirect",in progress,2026-07-06`,
		"86ey2,QA checkout,to do,",
		"Run `clickup-axi tasks <id>`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	if strings.Contains(out, "more may exist") {
		t.Errorf("partial-page note shown for a short page\noutput:\n%s", out)
	}
}

func TestTasksEmptyStateIsExplicit(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": []}`))
	})

	out, code := runCLI(t, c, "tasks")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks: 0 open tasks assigned to jan in Buzzwoo") {
		t.Errorf("output missing definitive empty state\noutput:\n%s", out)
	}
}

func TestTasksFullPageHintsAtMore(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		var rows []string
		for i := 0; i < clickup.TeamTasksPageSize; i++ {
			rows = append(rows, fmt.Sprintf(`{"id": "t%d", "name": "task %d", "status": {"status": "open"}, "due_date": null}`, i, i))
		}
		fmt.Fprintf(w, `{"tasks": [%s]}`, strings.Join(rows, ","))
	})

	out, code := runCLI(t, c, "tasks")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "(first page; more may exist)") {
		t.Errorf("full page must state that more may exist\noutput:\n%s", out)
	}
}

func TestTasksUnknownFlagIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "tasks", "--mine")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "valid: --comments N, --no-comments, --full") {
		t.Errorf("usage error does not list valid flags inline\noutput:\n%s", out)
	}
}

func TestTasksIDFallsBackToCustomID(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	// No handler for the lowercase internal attempt: it 404s, which
	// must trigger the custom-id fallback (uppercased, with params).
	f.mux.HandleFunc("GET /api/v2/task/HGAI-2316", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("custom_task_ids"); got != "true" {
			t.Errorf("custom_task_ids = %q, want true", got)
		}
		if got := r.URL.Query().Get("team_id"); got != "9018" {
			t.Errorf("team_id = %q, want 9018", got)
		}
		w.Write([]byte(taskJSON))
	})
	f.comments(t, "abc123", `{"comments": []}`)

	out, code := runCLI(t, c, "tasks", "hgai-2316")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "title: Fix login redirect") {
		t.Errorf("output missing task detail\noutput:\n%s", out)
	}
}

func TestTasksForcedCustomIDsSkipInternalLookup(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "1")
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/task/hgai-2316", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("internal-id lookup attempted despite CLICKUP_AXI_CUSTOM_IDS")
	})
	f.mux.HandleFunc("GET /api/v2/task/HGAI-2316", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(taskJSON))
	})
	f.comments(t, "abc123", `{"comments": []}`)

	out, code := runCLI(t, c, "tasks", "hgai-2316")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
}

func TestForcedCustomIDsAreShownEverywhere(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "1")
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": [
			{"id": "86ey1", "custom_id": "ECOM-7", "name": "Zeitstrahl", "status": {"status": "backlog"}, "due_date": null},
			{"id": "86ey2", "custom_id": null, "name": "No custom id", "status": {"status": "to do"}, "due_date": null}
		]}`))
	})
	f.mux.HandleFunc("GET /api/v2/task/AIKK-99", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(taskJSON))
	})
	f.comments(t, "abc123", `{"comments": []}`)

	out, code := runCLI(t, c, "tasks")
	if code != 0 {
		t.Fatalf("list exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "ECOM-7,Zeitstrahl,backlog,") {
		t.Errorf("list must show the custom id\noutput:\n%s", out)
	}
	if !strings.Contains(out, "86ey2,No custom id,to do,") {
		t.Errorf("list must fall back to the internal id when no custom id exists\noutput:\n%s", out)
	}

	out, code = runCLI(t, c, "tasks", "AIKK-99")
	if code != 0 {
		t.Fatalf("detail exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "id: AIKK-99") {
		t.Errorf("detail view must show the custom id\noutput:\n%s", out)
	}
	if !strings.Contains(out, "tasks edit AIKK-99 --status") {
		t.Errorf("help hints must reference the custom id\noutput:\n%s", out)
	}
	if strings.Contains(out, "id: abc123") {
		t.Errorf("internal id leaked into the id field in forced mode\noutput:\n%s", out)
	}
}

func TestTasksMultipleWorkspacesErrorInlinesThePins(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)

	out, code := runCLI(t, c, "tasks")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	want := `error: 2 workspaces are visible; set CLICKUP_AXI_WORKSPACE to one of: 9001 "BUZZWOO", 9002 "Personal"` + "\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestTasksPinnedWorkspaceListsItsTasks(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv(clickup.WorkspaceEnv, "9002")
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)
	f.mux.HandleFunc("GET /api/v2/team/9002/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": []}`))
	})

	out, code := runCLI(t, c, "tasks")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks: 0 open tasks assigned to jan in Personal") {
		t.Errorf("output missing empty state for the pinned workspace\noutput:\n%s", out)
	}
}

func TestTasksCustomIDResolvesAgainstPinnedWorkspace(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv(clickup.WorkspaceEnv, "9001")
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)
	f.mux.HandleFunc("GET /api/v2/task/HGAI-2316", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("team_id"); got != "9001" {
			t.Errorf("team_id = %q, want 9001", got)
		}
		w.Write([]byte(taskJSON))
	})
	f.comments(t, "abc123", `{"comments": []}`)

	out, code := runCLI(t, c, "tasks", "hgai-2316")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "title: Fix login redirect") {
		t.Errorf("output missing task detail\noutput:\n%s", out)
	}
}

func TestTasksCustomIDWithoutPinFailsRecoverably(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)

	out, code := runCLI(t, c, "tasks", "HGAI-2316")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `set CLICKUP_AXI_WORKSPACE to one of: 9001 "BUZZWOO", 9002 "Personal"`) {
		t.Errorf("output missing recoverable pin instruction\noutput:\n%s", out)
	}
}

func TestTasksInvisiblePinListsVisibleWorkspaces(t *testing.T) {
	f, c := newFakeClickUp(t)
	t.Setenv(clickup.WorkspaceEnv, "1234")
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)

	out, code := runCLI(t, c, "tasks")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	want := `error: CLICKUP_AXI_WORKSPACE="1234" does not match any workspace visible to this token (visible: 9001 "BUZZWOO", 9002 "Personal")` + "\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestTasksIDNotFoundEitherWay(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")

	out, code := runCLI(t, c, "tasks", "NOPE-1")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `task "NOPE-1" not found (tried as internal and as custom id)`) {
		t.Errorf("output missing combined not-found message\noutput:\n%s", out)
	}
}
