# tasks edit full field set - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `tasks edit` with `--priority`, `--name`, `--due`, `--body`/`--append-body`, and `--add-tag`/`--remove-tag`, completing the edit field set of v1.0.0 step 3.

**Architecture:** Every new field registers a check in the existing validate-all-then-write pre-flight pass (`fieldErrs` aggregation) and a mapping in the `TaskEdit` build; the single atomic `PUT /task/{id}` stays the only field write. Tags are separate ClickUp endpoints (`POST`/`DELETE /task/{id}/tag/{name}`), so they are validated pre-flight against the space's existing tags (one `GET /space/{id}/tag`) and applied before the PUT; if a later tag call or the PUT itself fails, the already-applied tag ops are rolled back (POST/DELETE invert exactly) - from the agent's view the command validates everything first and either fully applies or changes nothing (a failed rollback is disclosed). The human -> ClickUp mappings built here (priority 1-4, due epoch ms, markdown body) are the ones `tasks create` reuses in step 6.

**Tech Stack:** Go stdlib + `golang.org/x/term`; `httptest`-backed fakes for tests; AXI output conventions in `internal/output`.

**Branch:** `feature/tasks-edit-fields` off current `main` (no direct commits to main).

## Global Constraints

- stdout carries structured data AND errors; stderr is diagnostics only.
- Exit codes: 0 = success (including idempotent no-ops), 1 = error, 2 = usage error (unknown flags rejected with the valid set listed inline).
- Zero/no-op results are stated explicitly.
- Every command output ends with a parameterized `help[]` next-step hint.
- Raw ClickUp API errors never leak; translate them (reuse `renderAPIError`).
- No interactive prompts on this path.
- Dependencies: stdlib plus `golang.org/x/term` only - add nothing.
- Every agent-visible behavior has a test asserting exact output (`internal/cli/*_test.go`, `newFakeClickUp` harness).
- The generated skill (`skills/clickup-axi/SKILL.md`) is never hand-edited; regenerate via `go run ./cmd/clickup-axi skill --write`. `go test ./...` and CI fail while it is stale.
- Verify gate per task: `gofmt -l .` (empty), `go vet ./...`, `go test ./...`, `go build -o clickup-axi ./cmd/clickup-axi`.
- Edits are atomic, never partial: all fields validate before anything is written; a validation failure means nothing was written. Validated tag calls run before the PUT; an exceptional runtime failure mid-write rolls back the already-applied tag ops (best effort, POST/DELETE invert exactly) so the edit stays all-or-nothing, and anything that could not be rolled back is disclosed.

## Design decisions (agreed with Jan, 2026-07-10)

- **Tags + fields in one command:** everything (tags included) validates first; only when all checks pass does the write phase run. No partial updates on any validation failure.
- **Write-phase failure (agreed 2026-07-10, artifact review):** tags apply first, the atomic PUT last; if a tag call or the PUT fails, the applied tag ops are rolled back best-effort (POST/DELETE invert exactly) so the edit stays all-or-nothing. A rollback failure is disclosed. Chosen over disclose-only and over a compensating field-revert PUT (which could clobber concurrent edits or hit a forbidden status transition).
- **Unknown tag on `--add-tag`:** refused pre-flight with the space's existing tags inlined (a typo must not silently mint a tag). Creating tags is out of scope for `edit`.
- **Body:** `--body` replaces, `--append-body` appends (separator: blank line). Jan asked for both modes. Empty values are usage errors; clearing a description is deferred (no `--body ""`).
- **`--priority none` / `--due none`** clear their field (JSON `null`). Task 5 verifies clearing against the real API; the n8n community reports null-clearing as finicky, so the E2E step has an explicit contingency.
- **`--due`** accepts `YYYY-MM-DD` only; relative dates ("friday", "+3d") stay deferred to `create` per the v1.0.0 doc. (Amended after E2E, 2026-07-10: ClickUp re-derives the date in the workspace timezone and stores it at 04:00 local, so dates are written anchored at 12:00 UTC and rendered in local time - UTC midnight/rendering was off by one day east or west of Greenwich.)

---

### Task 1: `--priority`, `--name`, `--due` on tasks edit

**Files:**
- Modify: `internal/clickup/api.go` (extend `TaskEdit` + `UpdateTask`, lines 24-51)
- Modify: `internal/cli/task.go` (rewrite `cmdTaskEdit` lines 159-330; add helpers)
- Test: `internal/cli/task_test.go` (extend `editTaskJSON` fixture; new tests; fix `TestTaskEditUnknownFlagListsValid`)

**Interfaces:**
- Consumes: `clickup.TaskEdit{Status string; AddAssignees, RemAssignees []int64}` and `(*clickup.Client).UpdateTask(taskID string, edit TaskEdit) *clickup.APIError` (`internal/clickup/api.go:28-51`); `fieldErrs` aggregation + `renderFieldErrors(out, id, errs) int` (`internal/cli/task.go:335`); `listStatuses`, `containsFold`, `resolveAssignees`, `displayID`, `renderAPIError`; `clickup.Task{Name string; Priority *struct{Priority string}; DueDate MsEpoch}`; `MsEpoch.Date() string`.
- Produces: `editRequest` struct; extended `clickup.TaskEdit{..., Name string, Priority *int, DueDate *int64}`; helpers `parsePriority(string) (int, bool)`, `priorityLabel(int) string`, `currentPriority(*clickup.Task) string`, `parseDue(string) (int64, bool)`, `dueLabel(int64) string`, `orNone(string) string`. Tasks 2 and 3 extend `editRequest` and this rewritten `cmdTaskEdit`.

- [ ] **Step 1: Extend the fixture**

In `internal/cli/task_test.go`, replace `editTaskJSON` (lines 342-350) with a version carrying priority, due date, markdown body, space, and tags (the extra fields are consumed by Tasks 2-3; adding them once avoids fixture churn):

```go
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
	"due_date": "1783296000000",
	"markdown_description": "After OAuth the user lands on a 404.",
	"url": "https://app.clickup.com/t/abc123",
	"assignees": [{"id": 42, "username": "jan"}],
	"list": {"id": "901234", "name": "Sprint 14"},
	"space": {"id": "sp1"},
	"tags": [{"name": "backend"}]
}`
```

(`1783296000000` renders as `2026-07-06` in UTC - the same value `taskJSON` uses.)

- [ ] **Step 2: Write the failing tests**

Add to `internal/cli/task_test.go`:

```go
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

func TestTaskEditInvalidPriorityListsValid(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--priority", "blocker")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if want := "valid: urgent, high, normal, low, none"; !strings.Contains(out, want) {
		t.Errorf("output missing %q\noutput:\n%s", want, out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT called despite invalid priority: %v", f.putRaw)
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
	if len(f.putRaw) != 1 || !strings.Contains(f.putRaw[0], `"due_date":1784505600000`) {
		t.Errorf("PUT raw = %v, want due_date 1784505600000", f.putRaw)
	}
	if !strings.Contains(f.putRaw[0], `"due_date_time":false`) {
		t.Errorf("PUT raw = %v, want due_date_time false", f.putRaw)
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

func TestTaskEditBadDueDateIsFieldError(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--due", "tomorrow")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if want := "valid: YYYY-MM-DD"; !strings.Contains(out, want) {
		t.Errorf("output missing date-format hint\noutput:\n%s", out)
	}
	if len(f.putBodies) != 0 {
		t.Errorf("PUT called despite invalid due date: %v", f.putRaw)
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
	for _, want := range []string{`"status":"in review"`, `"priority":4`, `"due_date":1784505600000`, `"name":"Fix OAuth redirect"`} {
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

func TestTaskEditAggregatesPriorityAndDueErrors(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.task(t, "abc123", editTaskJSON)

	out, code := runCLI(t, c, "tasks", "edit", "abc123", "--priority", "blocker", "--due", "tomorrow")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "2 fields cannot be applied") {
		t.Errorf("output missing aggregated-failure header\noutput:\n%s", out)
	}
	for _, want := range []string{"urgent, high, normal, low, none", "YYYY-MM-DD"} {
		if !strings.Contains(out, want) {
			t.Errorf("aggregated output missing %q\noutput:\n%s", want, out)
		}
	}
	if len(f.putBodies) != 0 {
		t.Errorf("no PUT should happen when a field is invalid: %v", f.putRaw)
	}
}
```

Then fix the test that used `--priority` as its unknown flag - it becomes valid now. Replace `TestTaskEditUnknownFlagListsValid` (lines 582-593) with:

```go
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
```

(The valid-flag list already names the Task 2/3 flags; those parse cases arrive in their tasks, but the inline list is one string updated once here.)

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run TestTaskEdit -v`
Expected: FAIL - the new tests exit 2 on the unknown `--priority`/`--due`/`--name` flags; `TestTaskEditUnknownFlagListsValid` fails on the new list.

- [ ] **Step 4: Extend `TaskEdit` and `UpdateTask`**

In `internal/clickup/api.go`, replace lines 24-51 with:

```go
// TaskEdit is a mutation of a task's fields. Zero values leave a field
// unchanged: "" for Status/Name, nil for Priority/DueDate and the
// assignee lists. Priority 0 and DueDate 0 clear their field (JSON
// null). It maps to a single PUT /task/{id} so all field changes
// commit atomically in one request.
type TaskEdit struct {
	Status       string
	Name         string
	Priority     *int   // nil = unchanged, 0 = clear, 1 (urgent) .. 4 (low)
	DueDate      *int64 // nil = unchanged, 0 = clear, else millisecond epoch
	AddAssignees []int64
	RemAssignees []int64
}

func (c *Client) UpdateTask(taskID string, edit TaskEdit) *APIError {
	body := map[string]any{}
	if edit.Status != "" {
		body["status"] = edit.Status
	}
	if edit.Name != "" {
		body["name"] = edit.Name
	}
	if edit.Priority != nil {
		if *edit.Priority == 0 {
			body["priority"] = nil
		} else {
			body["priority"] = *edit.Priority
		}
	}
	if edit.DueDate != nil {
		if *edit.DueDate == 0 {
			body["due_date"] = nil
		} else {
			body["due_date"] = *edit.DueDate
			// Date-only: the CLI takes and renders dates, not times.
			body["due_date_time"] = false
		}
	}
	if len(edit.AddAssignees) > 0 || len(edit.RemAssignees) > 0 {
		add := edit.AddAssignees
		if add == nil {
			add = []int64{}
		}
		rem := edit.RemAssignees
		if rem == nil {
			rem = []int64{}
		}
		body["assignees"] = map[string][]int64{"add": add, "rem": rem}
	}
	return c.do(http.MethodPut, "/task/"+taskID, body, nil)
}
```

- [ ] **Step 5: Rewrite `cmdTaskEdit` with the new fields**

In `internal/cli/task.go`, replace `cmdTaskEdit` (lines 159-330) with the version below, and add the helpers after `renderFieldErrors`. The parse loop moves to an `editRequest` struct (Tasks 2-3 add fields to it), and each new field follows the pattern: parse -> pre-flight check into `fieldErrs` -> no-op filter -> `TaskEdit` mapping -> change summary line.

```go
// editRequest carries the parsed flags of one tasks edit invocation.
type editRequest struct {
	id                   string
	status               string
	statusSet            bool
	addTokens, remTokens []string
	priority             string
	prioritySet          bool
	name                 string
	nameSet              bool
	due                  string
	dueSet               bool
}

// hasChange reports whether any mutation flag was given.
func (r *editRequest) hasChange() bool {
	return r.statusSet || r.prioritySet || r.nameSet || r.dueSet ||
		len(r.addTokens) > 0 || len(r.remTokens) > 0
}

const editValidFlags = "--status, --assignee, --unassign, --priority, --name, --due, --body, --append-body, --add-tag, --remove-tag"

func cmdTaskEdit(args []string, c *clickup.Client, out io.Writer) int {
	var r editRequest
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			i++
			if i >= len(args) {
				output.WriteError(out, "--status needs a value", "Run `clickup-axi tasks edit <id> --status \"in review\"`")
				return 2
			}
			r.status = args[i]
			r.statusSet = true
		case "--assignee":
			i++
			if i >= len(args) {
				output.WriteError(out, "--assignee needs a value", "Run `clickup-axi tasks edit <id> --assignee <who>`")
				return 2
			}
			r.addTokens = append(r.addTokens, splitTokens(args[i])...)
		case "--unassign":
			i++
			if i >= len(args) {
				output.WriteError(out, "--unassign needs a value", "Run `clickup-axi tasks edit <id> --unassign <who>`")
				return 2
			}
			r.remTokens = append(r.remTokens, splitTokens(args[i])...)
		case "--priority":
			i++
			if i >= len(args) {
				output.WriteError(out, "--priority needs a value", "Run `clickup-axi tasks edit <id> --priority <urgent|high|normal|low|none>`")
				return 2
			}
			r.priority = args[i]
			r.prioritySet = true
		case "--name":
			i++
			if i >= len(args) {
				output.WriteError(out, "--name needs a value", "Run `clickup-axi tasks edit <id> --name \"<title>\"`")
				return 2
			}
			r.name = args[i]
			r.nameSet = true
		case "--due":
			i++
			if i >= len(args) {
				output.WriteError(out, "--due needs a value", "Run `clickup-axi tasks edit <id> --due <YYYY-MM-DD|none>`")
				return 2
			}
			r.due = args[i]
			r.dueSet = true
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks edit\n  valid: %s", args[i], editValidFlags))
				return 2
			}
			if r.id != "" {
				output.WriteError(out, "tasks edit takes exactly one task id")
				return 2
			}
			r.id = args[i]
		}
	}
	if r.id == "" {
		output.WriteError(out, "tasks edit needs a task id", "Run `clickup-axi tasks edit <id> --status \"<status>\"`")
		return 2
	}
	if r.nameSet && strings.TrimSpace(r.name) == "" {
		output.WriteError(out, "--name must not be empty",
			fmt.Sprintf("Run `clickup-axi tasks edit %s --name \"<title>\"`", r.id))
		return 2
	}
	if !r.hasChange() {
		output.WriteError(out, "tasks edit needs a change\n  valid: "+editValidFlags,
			fmt.Sprintf("Run `clickup-axi tasks edit %s --status \"<status>\"` or any change flag", r.id))
		return 2
	}

	t, err := c.GetTaskByID(r.id)
	if err != nil {
		return renderAPIError(out, err)
	}

	// Pre-flight validation: every field is checked before anything is
	// written, so a single bad field is reported alongside the others
	// (not one at a time) and never leaves a half-applied task. Assignee
	// names resolve against the workspace; the status is checked against
	// the list; priority and due date parse locally. Only once all
	// fields are known-good does the atomic PUT run. When the edit grows
	// more fields, each adds its check here.
	var fieldErrs []string

	var addUsers, remUsers []clickup.User
	if len(r.addTokens) > 0 || len(r.remTokens) > 0 {
		team, terr := c.SelectTeam()
		if terr != nil {
			return renderAPIError(out, terr)
		}
		var tokErrs []string
		if addUsers, tokErrs = resolveAssignees(r.addTokens, team, c); len(tokErrs) > 0 {
			fieldErrs = append(fieldErrs, tokErrs...)
		}
		if remUsers, tokErrs = resolveAssignees(r.remTokens, team, c); len(tokErrs) > 0 {
			fieldErrs = append(fieldErrs, tokErrs...)
		}
	}

	statusChanges := r.statusSet && !strings.EqualFold(t.Status.Status, r.status)
	if statusChanges {
		// A wrong status is caught here rather than after a failed PUT, so
		// it no longer hides behind (or gets hidden by) another field error.
		if valid := listStatuses(c, t.List.ID); len(valid) > 0 && !containsFold(valid, r.status) {
			fieldErrs = append(fieldErrs, fmt.Sprintf("status %q not accepted in list %s\n  valid: %s",
				r.status, t.List.Name, strings.Join(valid, ", ")))
		}
	}

	var prio *int
	if r.prioritySet {
		if p, ok := parsePriority(r.priority); ok {
			prio = &p
		} else {
			fieldErrs = append(fieldErrs, fmt.Sprintf("priority %q not accepted\n  valid: urgent, high, normal, low, none", r.priority))
		}
	}

	var due *int64
	if r.dueSet {
		if d, ok := parseDue(r.due); ok {
			due = &d
		} else {
			fieldErrs = append(fieldErrs, fmt.Sprintf("due %q is not a date\n  valid: YYYY-MM-DD (e.g. 2026-08-01) or none to clear", r.due))
		}
	}

	if len(fieldErrs) > 0 {
		return renderFieldErrors(out, displayID(t), fieldErrs)
	}

	// A person named in both lists is a self-contradictory request.
	for _, a := range addUsers {
		for _, rm := range remUsers {
			if a.ID == rm.ID {
				output.WriteError(out, fmt.Sprintf("%q is in both --assignee and --unassign", assigneeName(a)),
					"Name each person in only one of --assignee / --unassign")
				return 2
			}
		}
	}

	// Idempotency: drop adds already assigned and removes not assigned,
	// deduping within each set. A change that collapses to nothing is a
	// stated no-op, never a wasted PUT.
	assigned := make(map[int64]bool, len(t.Assignees))
	for _, u := range t.Assignees {
		assigned[u.ID] = true
	}
	var add, rem []int64
	var addNames, remNames []string
	seen := make(map[int64]bool)
	for _, u := range addUsers {
		if assigned[u.ID] || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		add = append(add, u.ID)
		addNames = append(addNames, assigneeName(u))
	}
	seen = make(map[int64]bool)
	for _, u := range remUsers {
		if !assigned[u.ID] || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		rem = append(rem, u.ID)
		remNames = append(remNames, assigneeName(u))
	}

	assigneeChanges := len(add) > 0 || len(rem) > 0
	priorityChanges := prio != nil && !strings.EqualFold(currentPriority(t), priorityLabel(*prio))
	dueChanges := due != nil && t.DueDate.Date() != dueLabel(*due)
	nameChanges := r.nameSet && t.Name != r.name

	if !statusChanges && !assigneeChanges && !priorityChanges && !dueChanges && !nameChanges {
		var reasons []string
		if r.statusSet {
			reasons = append(reasons, fmt.Sprintf("already has status %q", t.Status.Status))
		}
		if len(r.addTokens) > 0 || len(r.remTokens) > 0 {
			reasons = append(reasons, "assignees already as requested")
		}
		if r.prioritySet {
			reasons = append(reasons, "priority already "+currentPriority(t))
		}
		if r.dueSet {
			if d := t.DueDate.Date(); d == "" {
				reasons = append(reasons, "already has no due date")
			} else {
				reasons = append(reasons, "due already "+d)
			}
		}
		if r.nameSet {
			reasons = append(reasons, fmt.Sprintf("name already %q", t.Name))
		}
		fmt.Fprintf(out, "task: %s no changes (%s)\n", displayID(t), strings.Join(reasons, ", "))
		return 0
	}

	// The fetch above resolved any custom id; mutate via internal id.
	// All field changes go out in one PUT so they commit atomically -
	// no partial-mutation window. Every field was validated pre-flight,
	// so a failure here is a workflow restriction (e.g. a forbidden
	// status transition), which gets the raw translated error.
	edit := clickup.TaskEdit{}
	if statusChanges {
		edit.Status = r.status
	}
	if assigneeChanges {
		edit.AddAssignees = add
		edit.RemAssignees = rem
	}
	if priorityChanges {
		edit.Priority = prio
	}
	if dueChanges {
		edit.DueDate = due
	}
	if nameChanges {
		edit.Name = r.name
	}
	if err := c.UpdateTask(t.ID, edit); err != nil {
		return renderAPIError(out, err)
	}
	if statusChanges {
		fmt.Fprintf(out, "task: %s status changed: %s -> %s\n", displayID(t), t.Status.Status, r.status)
	}
	if assigneeChanges {
		fmt.Fprintf(out, "task: %s assignees%s%s\n", displayID(t), signedList("+", addNames), signedList("-", remNames))
	}
	if priorityChanges {
		fmt.Fprintf(out, "task: %s priority: %s -> %s\n", displayID(t), currentPriority(t), priorityLabel(*prio))
	}
	if dueChanges {
		fmt.Fprintf(out, "task: %s due: %s -> %s\n", displayID(t), orNone(t.DueDate.Date()), orNone(dueLabel(*due)))
	}
	if nameChanges {
		fmt.Fprintf(out, "task: %s renamed: %q -> %q\n", displayID(t), t.Name, r.name)
	}
	output.WriteHelp(out, fmt.Sprintf("Run `clickup-axi tasks %s` to see the task", displayID(t)))
	return 0
}
```

Rename `splitAssignees` to `splitTokens` (it now serves assignees and, in Task 3, tags) and update its comment:

```go
// splitTokens turns a repeatable flag value into tokens: a comma is
// always a separator, so "ting, me" is two people and "api, bug" two
// tags. Empty tokens (trailing commas, stray spaces) are dropped.
func splitTokens(v string) []string {
	parts := strings.Split(v, ",")
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			tokens = append(tokens, p)
		}
	}
	return tokens
}
```

Add the field helpers after `signedList`:

```go
// parsePriority maps the human name to ClickUp's 1-4 scale; "none"
// maps to 0, which clears the priority.
func parsePriority(s string) (int, bool) {
	switch strings.ToLower(s) {
	case "urgent":
		return 1, true
	case "high":
		return 2, true
	case "normal":
		return 3, true
	case "low":
		return 4, true
	case "none":
		return 0, true
	}
	return 0, false
}

// priorityLabel is the inverse of parsePriority, for no-op comparison
// and the change summary.
func priorityLabel(rank int) string {
	switch rank {
	case 1:
		return "urgent"
	case 2:
		return "high"
	case 3:
		return "normal"
	case 4:
		return "low"
	}
	return "none"
}

// currentPriority names the task's priority for comparison and output;
// an unset priority reads as "none".
func currentPriority(t *clickup.Task) string {
	if t.Priority == nil {
		return "none"
	}
	return t.Priority.Priority
}

// parseDue turns --due input into a millisecond epoch: "none" clears
// (0), otherwise the date is read as UTC midnight so the view's UTC
// rendering round-trips exactly.
func parseDue(s string) (int64, bool) {
	if strings.EqualFold(s, "none") {
		return 0, true
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0, false
	}
	return d.UnixMilli(), true
}

// dueLabel renders a due edit the way the task view renders due dates
// ("" when cleared), keeping no-op comparison and output consistent.
func dueLabel(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}

// orNone renders an absent value ("") as the word agents pass to clear
// it, keeping set and clear summaries symmetric.
func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
```

Add `"time"` to the import block of `internal/cli/task.go`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run TestTaskEdit -v`
Expected: PASS for all `TestTaskEdit*`, including every pre-existing status/assignee test.

- [ ] **Step 7: Full verify**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: gofmt prints nothing; vet clean; all tests pass (surface.go and the skill are untouched so far, so the skill-freshness test stays green).

- [ ] **Step 8: Commit**

```bash
git add internal/clickup/api.go internal/cli/task.go internal/cli/task_test.go
git commit -m "feat(tasks): edit priority, name, and due date with pre-flight validation" -m "--priority urgent|high|normal|low|none (none clears via JSON null),
--due YYYY-MM-DD|none (UTC midnight, date-only), and --name join the
validate-all-then-write pass: bad values aggregate into one report and
nothing is written until every field is known-good. All fields ride
the existing single atomic PUT."
```

---

### Task 2: `--body` (replace) and `--append-body` (append)

**Files:**
- Modify: `internal/clickup/types.go` (Task gains `MarkdownDescription`)
- Modify: `internal/clickup/resolve.go` (`getTask` requests the markdown source, lines 67-74)
- Modify: `internal/clickup/api.go` (TaskEdit gains `Body *string`; UpdateTask maps `markdown_content`)
- Modify: `internal/cli/task.go` (parse cases, usage checks, apply + summary)
- Test: `internal/cli/task_test.go`

**Interfaces:**
- Consumes: `editRequest`, `cmdTaskEdit`, and `clickup.TaskEdit` exactly as Task 1 left them; `taskDescription` stays untouched (the view still prefers `text_content`).
- Produces: `clickup.Task.MarkdownDescription string`; `clickup.TaskEdit.Body *string` (nil = unchanged, non-nil = full replacement sent as `markdown_content`); cli helper `taskMarkdown(*clickup.Task) string`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/task_test.go`:

```go
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
```

("Repro: safari only." is 19 runes; "New **desc**" is 12.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run TestTaskEdit -v`
Expected: FAIL - `--body`/`--append-body` are unknown flags (exit 2 instead of 0).

- [ ] **Step 3: Fetch and carry the markdown source**

In `internal/clickup/types.go`, add to the `Task` struct after `TextContent`:

```go
	// MarkdownDescription is the markdown source of the description,
	// present because getTask always requests it; edits that append to
	// the body build on it.
	MarkdownDescription string `json:"markdown_description"`
```

In `internal/clickup/resolve.go`, replace the start of `getTask` (lines 67-74) with:

```go
func (c *Client) getTask(ref taskRef) (*Task, *APIError) {
	q := url.Values{}
	// The markdown source of the description only comes along on
	// request; edits that append to the body need it.
	q.Set("include_markdown_description", "true")
	if ref.custom {
		q.Set("custom_task_ids", "true")
		q.Set("team_id", ref.teamID)
	}
	path := "/task/" + url.PathEscape(ref.id) + "?" + q.Encode()
```

In `internal/clickup/api.go`, add to `TaskEdit` after `DueDate`:

```go
	Body         *string // nil = unchanged, else full markdown replacement
```

and in `UpdateTask`, after the `DueDate` block:

```go
	if edit.Body != nil {
		body["markdown_content"] = *edit.Body
	}
```

- [ ] **Step 4: Parse, validate, and apply in the CLI**

In `internal/cli/task.go`:

Add to `editRequest`:

```go
	body                 string
	bodySet              bool
	appendBody           string
	appendSet            bool
```

Extend `hasChange`:

```go
func (r *editRequest) hasChange() bool {
	return r.statusSet || r.prioritySet || r.nameSet || r.dueSet ||
		r.bodySet || r.appendSet ||
		len(r.addTokens) > 0 || len(r.remTokens) > 0
}
```

Add parse cases after `case "--due":`:

```go
		case "--body":
			i++
			if i >= len(args) {
				output.WriteError(out, "--body needs a value", "Run `clickup-axi tasks edit <id> --body \"<markdown>\"`")
				return 2
			}
			r.body = args[i]
			r.bodySet = true
		case "--append-body":
			i++
			if i >= len(args) {
				output.WriteError(out, "--append-body needs a value", "Run `clickup-axi tasks edit <id> --append-body \"<markdown>\"`")
				return 2
			}
			r.appendBody = args[i]
			r.appendSet = true
```

Add usage checks right after the empty `--name` check:

```go
	if r.bodySet && r.appendSet {
		output.WriteError(out, "--body and --append-body cannot be combined",
			"Use --body to replace the description or --append-body to add to it")
		return 2
	}
	if r.bodySet && strings.TrimSpace(r.body) == "" {
		output.WriteError(out, "--body must not be empty",
			fmt.Sprintf("Run `clickup-axi tasks edit %s --body \"<markdown>\"`", r.id))
		return 2
	}
	if r.appendSet && strings.TrimSpace(r.appendBody) == "" {
		output.WriteError(out, "--append-body must not be empty",
			fmt.Sprintf("Run `clickup-axi tasks edit %s --append-body \"<markdown>\"`", r.id))
		return 2
	}
```

A body edit is always a change (comparing markdown to the stored source is unreliable, and re-sending the same content is harmless). Add alongside the other `*Changes` variables:

```go
	bodyChanges := r.bodySet || r.appendSet
```

include it in the no-op guard:

```go
	if !statusChanges && !assigneeChanges && !priorityChanges && !dueChanges && !nameChanges && !bodyChanges {
```

Add the mapping in the `TaskEdit` build, after the `nameChanges` block:

```go
	if bodyChanges {
		content := r.body
		if r.appendSet {
			content = r.appendBody
			if base := taskMarkdown(t); base != "" {
				content = base + "\n\n" + r.appendBody
			}
		}
		edit.Body = &content
	}
```

Add the summary line after the `nameChanges` output block:

```go
	if bodyChanges {
		if r.appendSet {
			fmt.Fprintf(out, "task: %s description appended (+%d chars)\n", displayID(t), len([]rune(r.appendBody)))
		} else {
			fmt.Fprintf(out, "task: %s description replaced (%d chars)\n", displayID(t), len([]rune(r.body)))
		}
	}
```

Add the helper next to `taskDescription`:

```go
// taskMarkdown is the markdown source of the description, for edits
// that build on it; tasks fetched without one fall back to the raw
// description.
func taskMarkdown(t *clickup.Task) string {
	if t.MarkdownDescription != "" {
		return t.MarkdownDescription
	}
	return t.Description
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run TestTaskEdit -v`
Expected: PASS, including all Task 1 tests.

- [ ] **Step 6: Full verify**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/clickup/types.go internal/clickup/resolve.go internal/clickup/api.go internal/cli/task.go internal/cli/task_test.go
git commit -m "feat(tasks): replace or append the task description in tasks edit" -m "--body replaces, --append-body appends below a blank line - append
exists so agents can add notes without clobbering human-written
content. The task fetch now always requests the markdown source
(include_markdown_description) so appends build on markdown, and the
edit sends markdown_content on the same atomic PUT."
```

---

### Task 3: `--add-tag` / `--remove-tag` and tags in the task view

**Files:**
- Modify: `internal/clickup/types.go` (`Tag` type; Task gains `Tags`, `Space`)
- Modify: `internal/clickup/api.go` (`AddTag`, `RemoveTag`)
- Modify: `internal/clickup/space.go` (`GetSpaceTags`, `ResolveSpaceTags`, `tagList`)
- Modify: `internal/cli/task.go` (parse, pre-flight, apply after the PUT, view line, `renderTagFailure`)
- Test: `internal/cli/task_test.go` (fake tag handlers + tests)

**Interfaces:**
- Consumes: `editRequest`/`cmdTaskEdit` as Task 2 left them; `splitTokens`; `signedList`; `resolveListCap` (clickup-internal, used by `tagList`); `clickup.Task.Space.ID` (new), `clickup.Task.Tags` (new).
- Produces: `clickup.Tag{Name string}`; `(*clickup.Client).GetSpaceTags(spaceID string) ([]Tag, *APIError)`; `(*clickup.Client).ResolveSpaceTags(spaceID string, names []string) ([]string, *APIError)` (per-unknown-name messages, transport error separate); `(*clickup.Client).AddTag(taskID, tag string) *APIError`; `(*clickup.Client).RemoveTag(taskID, tag string) *APIError`; cli helpers `rollbackTags` (inverts applied tag ops, returns what could not be reverted) and `renderWriteFailure`.

- [ ] **Step 1: Extend the fake with tag handlers**

In `internal/cli/task_test.go`, add fields to `fakeClickUp` (lines 18-24):

```go
	tagAdds    []string
	tagRems    []string
```

and helpers after `list`:

```go
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
```

- [ ] **Step 2: Write the failing tests**

```go
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
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestTaskEdit|TestTaskView' -v`
Expected: FAIL - `--add-tag`/`--remove-tag` are unknown flags; the view has no tags line.

- [ ] **Step 4: clickup adapter - types and endpoints**

In `internal/clickup/types.go`, add after the `Comment` type:

```go
// Tag is a task tag; only the name matters to the CLI.
type Tag struct {
	Name string `json:"name"`
}
```

and add to the `Task` struct after `List`:

```go
	Tags  []Tag `json:"tags"`
	Space struct {
		ID string `json:"id"`
	} `json:"space"`
```

In `internal/clickup/api.go`, after `UpdateTask`:

```go
// AddTag and RemoveTag attach and detach one tag; the API has no batch
// form, so callers loop. Adding an unknown name would create the tag,
// which is why the CLI validates tags pre-flight (ResolveSpaceTags).
func (c *Client) AddTag(taskID, tag string) *APIError {
	return c.do(http.MethodPost, "/task/"+taskID+"/tag/"+url.PathEscape(tag), nil, nil)
}

func (c *Client) RemoveTag(taskID, tag string) *APIError {
	return c.do(http.MethodDelete, "/task/"+taskID+"/tag/"+url.PathEscape(tag), nil, nil)
}
```

In `internal/clickup/space.go`, after `ResolveSpace`:

```go
func (c *Client) GetSpaceTags(spaceID string) ([]Tag, *APIError) {
	var out struct {
		Tags []Tag `json:"tags"`
	}
	if err := c.do(http.MethodGet, "/space/"+spaceID+"/tag", nil, &out); err != nil {
		return nil, err
	}
	return out.Tags, nil
}

// ResolveSpaceTags checks the given names against the space's existing
// tags (case-insensitive). It returns one message per unknown name,
// each inlining the existing tags - the same recovery pattern as
// ResolveSpace and ResolveMember, but aggregated so the edit's
// pre-flight can report every bad tag at once. The *APIError is
// transport-level only.
func (c *Client) ResolveSpaceTags(spaceID string, names []string) ([]string, *APIError) {
	tags, err := c.GetSpaceTags(spaceID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(tags))
	for _, t := range tags {
		known[strings.ToLower(t.Name)] = true
	}
	var bad []string
	for _, n := range names {
		if !known[strings.ToLower(n)] {
			bad = append(bad, fmt.Sprintf("tag %q does not exist in the space\n  existing: %s", n, tagList(tags)))
		}
	}
	return bad, nil
}

// tagList renders tag names for inlining into an error message, capped
// like the other resolvers' candidate lists.
func tagList(tags []Tag) string {
	if len(tags) == 0 {
		return "none (the space has no tags yet)"
	}
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = t.Name
	}
	var more int
	if len(names) > resolveListCap {
		more = len(names) - resolveListCap
		names = names[:resolveListCap]
	}
	out := strings.Join(names, ", ")
	if more > 0 {
		out += fmt.Sprintf(", and %d more", more)
	}
	return out
}
```

- [ ] **Step 5: CLI - parse, pre-flight, apply, view**

In `internal/cli/task.go`:

Add to `editRequest`:

```go
	addTags, remTags     []string
```

Extend `hasChange` with `|| len(r.addTags) > 0 || len(r.remTags) > 0`.

Add parse cases after `case "--append-body":`:

```go
		case "--add-tag":
			i++
			if i >= len(args) {
				output.WriteError(out, "--add-tag needs a value", "Run `clickup-axi tasks edit <id> --add-tag <tag>`")
				return 2
			}
			r.addTags = append(r.addTags, splitTokens(args[i])...)
		case "--remove-tag":
			i++
			if i >= len(args) {
				output.WriteError(out, "--remove-tag needs a value", "Run `clickup-axi tasks edit <id> --remove-tag <tag>`")
				return 2
			}
			r.remTags = append(r.remTags, splitTokens(args[i])...)
```

Add the conflict check after the empty `--append-body` usage check (pure string work, so it runs before any fetch):

```go
	for _, a := range r.addTags {
		for _, rm := range r.remTags {
			if strings.EqualFold(a, rm) {
				output.WriteError(out, fmt.Sprintf("%q is in both --add-tag and --remove-tag", a),
					"Name each tag in only one of --add-tag / --remove-tag")
				return 2
			}
		}
	}
```

Add the tag pre-flight in the validation pass, after the due-date check (before `if len(fieldErrs) > 0`):

```go
	// Tags to add must already exist in the space - a typo must not
	// mint a new tag from an agent path. One GET covers all of them.
	if len(r.addTags) > 0 {
		bad, terr := c.ResolveSpaceTags(t.Space.ID, r.addTags)
		if terr != nil {
			return renderAPIError(out, terr)
		}
		fieldErrs = append(fieldErrs, bad...)
	}
```

Add idempotency filtering alongside the other `*Changes` variables:

```go
	onTask := make(map[string]bool, len(t.Tags))
	for _, tg := range t.Tags {
		onTask[strings.ToLower(tg.Name)] = true
	}
	var addTags, remTags []string
	seenTag := make(map[string]bool)
	for _, tg := range r.addTags {
		k := strings.ToLower(tg)
		if onTask[k] || seenTag[k] {
			continue
		}
		seenTag[k] = true
		addTags = append(addTags, tg)
	}
	seenTag = make(map[string]bool)
	for _, tg := range r.remTags {
		k := strings.ToLower(tg)
		if !onTask[k] || seenTag[k] {
			continue
		}
		seenTag[k] = true
		remTags = append(remTags, tg)
	}
	tagChanges := len(addTags) > 0 || len(remTags) > 0
```

Extend the no-op guard with `&& !tagChanges` and add the reason:

```go
		if len(r.addTags) > 0 || len(r.remTags) > 0 {
			reasons = append(reasons, "tags already as requested")
		}
```

Replace the write phase (the unconditional `if err := c.UpdateTask(...)`) so tags apply first, the PUT last, and any mid-write failure rolls the applied tag ops back. Insert directly after the `TaskEdit` build:

```go
	// The write phase: tags first (one call per tag - the API has no
	// batch form), then the atomic PUT. POST and DELETE invert exactly,
	// so if a later tag call or the PUT fails, the applied tag ops roll
	// back and the edit stays all-or-nothing; everything was validated
	// pre-flight, so a failure here is exceptional (outage, rate limit)
	// or a workflow-restricted status transition.
	var appliedAdds, appliedRems []string
	for _, tg := range addTags {
		if terr := c.AddTag(t.ID, tg); terr != nil {
			return renderWriteFailure(out, c, t, appliedAdds, appliedRems,
				fmt.Sprintf("tag %q could not be applied: %s", tg, terr.Message))
		}
		appliedAdds = append(appliedAdds, tg)
	}
	for _, tg := range remTags {
		if terr := c.RemoveTag(t.ID, tg); terr != nil {
			return renderWriteFailure(out, c, t, appliedAdds, appliedRems,
				fmt.Sprintf("tag %q could not be removed: %s", tg, terr.Message))
		}
		appliedRems = append(appliedRems, tg)
	}
	putNeeded := statusChanges || assigneeChanges || priorityChanges || dueChanges || nameChanges || bodyChanges
	if putNeeded {
		if err := c.UpdateTask(t.ID, edit); err != nil {
			return renderWriteFailure(out, c, t, appliedAdds, appliedRems,
				fmt.Sprintf("the field changes could not be applied: %s", err.Message))
		}
	}
```

The tags summary line stays with the other field summaries (after the `bodyChanges` output block):

```go
	if tagChanges {
		fmt.Fprintf(out, "task: %s tags%s%s\n", displayID(t), signedList("+", addTags), signedList("-", remTags))
	}
```

Add the rollback helpers after `renderFieldErrors`:

```go
// rollbackTags inverts already-applied tag ops (adds are deleted,
// removes re-added) after a mid-write failure. It returns the ops that
// could not be reverted, signed the way they were requested.
func rollbackTags(c *clickup.Client, taskID string, adds, rems []string) []string {
	var stuck []string
	for _, tg := range adds {
		if err := c.RemoveTag(taskID, tg); err != nil {
			stuck = append(stuck, "+"+tg)
		}
	}
	for _, tg := range rems {
		if err := c.AddTag(taskID, tg); err != nil {
			stuck = append(stuck, "-"+tg)
		}
	}
	return stuck
}

// renderWriteFailure reports a write call that failed after pre-flight
// passed. Applied tag ops are rolled back so the edit stays
// all-or-nothing; whatever cannot be reverted is disclosed so the
// stated task state stays truthful.
func renderWriteFailure(out io.Writer, c *clickup.Client, t *clickup.Task, adds, rems []string, msg string) int {
	id := displayID(t)
	if stuck := rollbackTags(c, t.ID, adds, rems); len(stuck) > 0 {
		fmt.Fprintf(out, "task: %s tags NOT rolled back: %s\n", id, strings.Join(stuck, " "))
	} else if len(adds)+len(rems) > 0 {
		fmt.Fprintf(out, "task: %s tag changes rolled back, nothing applied\n", id)
	}
	output.WriteError(out, msg,
		fmt.Sprintf("Run `clickup-axi tasks %s` to see the task's current state, then retry", id))
	return 1
}
```

In `renderTask`, add the tags line between the `due` and `url` lines:

```go
	if len(t.Tags) > 0 {
		names := make([]string, len(t.Tags))
		for i, tg := range t.Tags {
			names[i] = tg.Name
		}
		fmt.Fprintf(out, "  tags: %s\n", strings.Join(names, ", "))
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestTaskEdit|TestTaskView' -v`
Expected: PASS.

- [ ] **Step 7: Full verify**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add internal/clickup/types.go internal/clickup/api.go internal/clickup/space.go internal/cli/task.go internal/cli/task_test.go
git commit -m "feat(tasks): add and remove existing space tags in tasks edit" -m "Tags are separate ClickUp endpoints, so they cannot ride the atomic
PUT: instead they join the same pre-flight pass - an unknown tag is
refused with the space's tags inlined (a typo must not mint a tag)
and nothing at all is written while any field is invalid. Validated
tags apply one call each before the PUT; if a later call or the PUT
fails, the applied tag ops roll back (POST/DELETE invert exactly) so
the edit stays all-or-nothing, and a failed rollback is disclosed.
The task view now shows tags."
```

---

### Task 4: surface table, skill, help, and README

**Files:**
- Modify: `internal/cli/surface.go` (the `tasks edit` entries, lines 53-61)
- Modify: `internal/cli/skill_template.md` (edit prose, after the assignee paragraph at lines 74-78)
- Modify: `internal/cli/tasks.go` (`tasksHelp` edit section lines 35-40 and examples lines 45-53)
- Regenerate: `skills/clickup-axi/SKILL.md` (via `skill --write`)
- Modify: `README.md` (edit examples)

**Interfaces:**
- Consumes: the `command` struct fields (`usage`, `summary`, `skill`, `comment`) in `internal/cli/surface.go:8-14`; the `{{COMMANDS}}` placeholder mechanism in `skill_template.md`.
- Produces: a non-stale skill so `skill --check` and `go test ./...` pass.

- [ ] **Step 1: Update the surface table**

In `internal/cli/surface.go`, replace the two `tasks edit` entries (lines 53-61) with:

```go
	{
		usage:   "tasks edit <id>",
		summary: "Change status, assignees, priority, name, due date, description, tags",
		skill:   `clickup-axi tasks edit <id> --status "<status>"`,
	},
	{
		skill:   `clickup-axi tasks edit <id> --assignee <who> --unassign <who>`,
		comment: "reassign; --assignee/--unassign are repeatable and comma-separated; who = me | name | id",
	},
	{
		skill:   `clickup-axi tasks edit <id> --priority <p> --due <date> --name "<title>"`,
		comment: "priority: urgent|high|normal|low|none; due: YYYY-MM-DD or none; fields combine in one call",
	},
	{
		skill:   `clickup-axi tasks edit <id> --append-body "<markdown>" --add-tag <tag>`,
		comment: "--body replaces the description, --append-body adds below it; tags must already exist in the space",
	},
```

- [ ] **Step 2: Update the skill template prose**

In `internal/cli/skill_template.md`, replace the assignee paragraph (lines 74-78) with:

```
`tasks edit` changes any field and they combine freely in one call:
`--assignee <who>` adds and `--unassign <who>` removes people (both
repeatable and comma-separated; `<who>` is `me`, a member name, or an
id), `--priority urgent|high|normal|low|none` (none clears),
`--due YYYY-MM-DD` or `--due none`, `--name "<title>"`, and
`--body "<markdown>"` replaces the description while
`--append-body "<markdown>"` adds below it (prefer append when the
existing description should survive). `--add-tag`/`--remove-tag` take
existing space tags only; an unknown tag fails with the space's tags
inlined. Every invalid field is reported together with the others
before anything is written - fix them all and retry once. Re-applying
the current state (same status, same assignees, existing tag) is a
stated no-op.
```

- [ ] **Step 3: Update `tasksHelp`**

In `internal/cli/tasks.go`, replace the edit section (lines 35-40) with:

```
edit <id> (mutations; "edit" is a reserved word, not an id):
  --status "<status>"    change status; valid statuses are echoed
                         when the status does not match
  --assignee <who>       add an assignee (repeatable, comma-separated);
                         who = me | member name | id
  --unassign <who>       remove an assignee (repeatable, comma-separated)
  --priority <p>         urgent | high | normal | low | none (= clear)
  --name "<title>"       rename the task
  --due <date>           set the due date (YYYY-MM-DD) or none (= clear)
  --body "<markdown>"    replace the description
  --append-body "<md>"   append to the description instead
  --add-tag <tag>        add an existing space tag (repeatable, comma-separated)
  --remove-tag <tag>     remove a tag (repeatable, comma-separated)
```

and add to the examples block (after the `--assignee ting --unassign me` line):

```
  clickup-axi tasks edit HGAI-2316 --priority high --due 2026-08-01
  clickup-axi tasks edit HGAI-2316 --append-body "QA: repro steps ..." --add-tag qa
```

- [ ] **Step 4: Regenerate the skill**

Run: `go run ./cmd/clickup-axi skill --write`
Expected: `skills/clickup-axi/SKILL.md` rewritten; `git status` shows it modified.

- [ ] **Step 5: Update the README**

In `README.md`, find the `tasks edit` examples and extend them to cover the new fields, matching the existing formatting, e.g.:

```sh
clickup-axi tasks edit HGAI-2316 --priority high --due 2026-08-01   # multi-field edit, one atomic call
clickup-axi tasks edit HGAI-2316 --append-body "QA notes ..."       # add to the description
clickup-axi tasks edit HGAI-2316 --add-tag qa --remove-tag wip      # existing space tags only
```

- [ ] **Step 6: Verify the skill is fresh and everything passes**

Run: `go run ./cmd/clickup-axi skill --check && gofmt -l . && go vet ./... && go test ./...`
Expected: skill in sync; gofmt empty; vet clean; all tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/surface.go internal/cli/skill_template.md internal/cli/tasks.go skills/clickup-axi/SKILL.md README.md
git commit -m "docs(tasks): document the full edit field set across surface, skill, help, README"
```

---

### Task 5: end-to-end verification against the real API

**Files:** none (verification only; fixes fold into the task they belong to)

- [ ] **Step 1: Build**

Run: `go build -o clickup-axi ./cmd/clickup-axi`
Expected: builds clean.

- [ ] **Step 2: Record the scratch task's state**

Use the stored token (`~/.config/clickup-axi/token`) or `CLICKUP_TOKEN`; the scratch task is `HGAI-2378`. NEVER echo the token.

Run `./clickup-axi tasks HGAI-2378` and note name, priority, due, tags, and description so everything can be restored.

- [ ] **Step 3: Exercise every field, set -> no-op -> clear**

```sh
./clickup-axi tasks edit HGAI-2378 --priority urgent          # set
./clickup-axi tasks edit HGAI-2378 --priority urgent          # expect: no changes
./clickup-axi tasks edit HGAI-2378 --priority none            # clear
./clickup-axi tasks HGAI-2378                                 # CONFIRM the priority line is gone
./clickup-axi tasks edit HGAI-2378 --due 2026-08-01
./clickup-axi tasks edit HGAI-2378 --due 2026-08-01           # expect: no changes
./clickup-axi tasks edit HGAI-2378 --due none
./clickup-axi tasks HGAI-2378                                 # CONFIRM the due line is gone
./clickup-axi tasks edit HGAI-2378 --name "<original name> (e2e)"
./clickup-axi tasks edit HGAI-2378 --name "<original name>"   # rename back
./clickup-axi tasks edit HGAI-2378 --append-body "e2e marker $(date -u +%Y-%m-%dT%H:%M)"
./clickup-axi tasks HGAI-2378 --full                          # CONFIRM the append landed below the old body
./clickup-axi tasks edit HGAI-2378 --add-tag <existing tag>   # pick one from the space
./clickup-axi tasks edit HGAI-2378 --add-tag <existing tag>   # expect: no changes
./clickup-axi tasks edit HGAI-2378 --remove-tag <existing tag>
./clickup-axi tasks edit HGAI-2378 --add-tag definitely-not-a-tag   # expect: refused, existing tags inlined, exit 1
./clickup-axi tasks edit HGAI-2378 --priority blocker --due tomorrow # expect: 2 fields aggregated, exit 1
./clickup-axi tasks edit HGAI-2378 --status "in progress" --priority high --due 2026-08-01 --append-body "combined e2e"  # multi-field
```

Expected: every mutation prints its summary line and exits 0; no-ops print `no changes (...)` and exit 0; failures aggregate, inline valid values, never leak raw ClickUp errors, and exit 1; every output ends with `help[]`.

(The mid-write rollback path cannot be exercised safely against the real API - it needs an induced failure - so it is covered by the fake-backed tests only.)

**Contingency (priority/due clearing):** if `--priority none` or `--due none` reports success but the follow-up view still shows the old value, the API ignored the JSON null. Then: try `"priority": 0` / `"due_date": 0`, or as a last resort omit-and-verify against ClickUp support docs; adjust `UpdateTask` in `internal/clickup/api.go` (Task 1) plus its test expectations, and note the actual encoding in a code comment. Re-run this step.

- [ ] **Step 4: Restore and report**

Restore the task's original status, priority, due, tags, and name (the appended e2e markers may stay - HGAI-2378 is the designated scratch task). Report the observed outputs and exit codes. If anything diverges from the plan (wording, exit code, error leakage), stop and fix in the owning task before marking the branch done.
