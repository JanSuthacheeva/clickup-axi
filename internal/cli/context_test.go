package cli

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
)

const contextHeader = "clickup-axi: ClickUp CLI (tasks, search, edit, comment)"

func TestContextShowsCappedDueSortedDashboard(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("assignees[]"); got != "42" {
			t.Errorf("assignees[] = %q, want 42", got)
		}
		w.Write([]byte(`{"tasks": [
			{"id": "86ey1", "name": "No due A", "status": {"status": "open"}, "due_date": null},
			{"id": "86ey2", "name": "Due later", "status": {"status": "open"}, "due_date": "1783339200000"},
			{"id": "86ey3", "name": "Due soon", "status": {"status": "in progress"}, "due_date": "1752192000000"},
			{"id": "86ey4", "name": "No due B", "status": {"status": "open"}, "due_date": null},
			{"id": "86ey5", "name": "Due mid", "status": {"status": "open"}, "due_date": "1760000000000"},
			{"id": "86ey6", "name": "No due C", "status": {"status": "open"}, "due_date": null}
		]}`))
	})

	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, contextHeader) {
		t.Errorf("missing discovery header\noutput:\n%s", out)
	}
	// The array header carries the row count only; the total lives in
	// the help hint (AXI: never encode pagination into TOON headers).
	if !strings.Contains(out, "tasks[5]{id,title,status,due}:") {
		t.Errorf("missing capped header\noutput:\n%s", out)
	}
	if strings.Contains(out, "tasks[5/") {
		t.Errorf("pagination leaked into the TOON array header\noutput:\n%s", out)
	}
	// Due-soonest first, no-due tail in stable order; the 6th task is cut.
	idx := func(s string) int { return strings.Index(out, s) }
	if !(idx("86ey3") < idx("86ey5") && idx("86ey5") < idx("86ey2") && idx("86ey2") < idx("86ey1") && idx("86ey1") < idx("86ey4")) {
		t.Errorf("rows not due-sorted\noutput:\n%s", out)
	}
	if strings.Contains(out, "86ey6") {
		t.Errorf("row past the cap leaked\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Run `clickup-axi tasks` for all 6 open tasks") {
		t.Errorf("missing total-hint help line\noutput:\n%s", out)
	}
}

func TestContextUncappedHeaderAndHelp(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": [
			{"id": "86ey1", "name": "Only one", "status": {"status": "open"}, "due_date": null}
		]}`))
	})
	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks[1]{id,title,status,due}:") {
		t.Errorf("uncapped header wrong\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Run `clickup-axi tasks` for your open tasks") {
		t.Errorf("missing plain help line\noutput:\n%s", out)
	}
}

func TestContextZeroTasksIsExplicit(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": []}`))
	})
	out, code := runCLI(t, c, "context")
	if code != 0 || !strings.Contains(out, "tasks: 0 open tasks assigned to you") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestContextWithoutTokenDegradesToLoginHint(t *testing.T) {
	// Isolate host env like newFakeClickUp does; this test builds its
	// own client because the point is the empty token.
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "")
	t.Setenv(clickup.WorkspaceEnv, "")
	c := clickup.New("http://127.0.0.1:0", "", &http.Client{Timeout: time.Second})
	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (session start must not fail)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, contextHeader) ||
		!strings.Contains(out, "tasks: unavailable (not authenticated)") ||
		!strings.Contains(out, "clickup-axi auth login") {
		t.Errorf("degraded output wrong:\n%s", out)
	}
}

func TestContextAPIErrorDegradesQuietly(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.mux.HandleFunc("GET /api/v2/team", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"err": "boom"}`))
	})
	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks: unavailable right now") || strings.Contains(out, "boom") {
		t.Errorf("raw API error leaked or degraded line missing:\n%s", out)
	}
}

func TestContextUnpinnedMultiWorkspaceShowsPinHint(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)
	out, code := runCLI(t, c, "context")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "tasks: unavailable (") || !strings.Contains(out, clickup.WorkspaceEnv) {
		t.Errorf("pin hint missing:\n%s", out)
	}
}

func TestContextBudgetBreachDegrades(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{"tasks": []}`))
	})
	old := contextBudget
	contextBudget = 50 * time.Millisecond
	t.Cleanup(func() { contextBudget = old })

	out, code := runCLI(t, c, "context")
	if code != 0 || !strings.Contains(out, "tasks: unavailable right now") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestContextForcedCustomIDsShowsCustomID(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "1")
	f.mux.HandleFunc("GET /api/v2/team/9018/task", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tasks": [
			{"id": "86ey1", "custom_id": "HGAI-77", "name": "One", "status": {"status": "open"}, "due_date": null}
		]}`))
	})
	out, _ := runCLI(t, c, "context")
	if !strings.Contains(out, "HGAI-77,One,open,") {
		t.Errorf("custom id not shown:\n%s", out)
	}
}

func TestContextRejectsUnknownFlags(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "context", "--full")
	if code != 2 || !strings.Contains(out, "unknown argument") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}
