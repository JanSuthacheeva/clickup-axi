# tasks edit assignee mutation - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--assignee` (add) and `--unassign` (remove) to `tasks edit`, reusing the existing name resolvers, so an agent can reassign a task.

**Architecture:** Extend `cmdTaskEdit` in `internal/cli/task.go` to parse repeatable, comma-splittable `--assignee`/`--unassign` flags alongside the existing `--status`, resolve each person via the shared `resolveAssignee` helper, filter for idempotency against the task's current assignees, and apply through a new `clickup.Client.UpdateTaskAssignees` (`PUT /task/{id}` with an `assignees.add/rem` body). Status keeps its own `SetTaskStatus` call; the two compose in one invocation. Name resolution stays in the `cli` package (matching today's split); only the raw HTTP method is added to `clickup`.

**Tech Stack:** Go stdlib + `golang.org/x/term`; `httptest`-backed fakes for tests; the vendored AXI output conventions in `internal/output`.

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
- Verify gate before done: `gofmt -l .` (empty), `go vet ./...`, `go test ./...`, `go build`.

---

### Task 1: assignee add/remove on `tasks edit`

**Files:**
- Modify: `internal/clickup/api.go` (add `UpdateTaskAssignees` beside `SetTaskStatus` at line 24-27)
- Modify: `internal/cli/task.go` (rewrite `cmdTaskEdit` at lines 159-219; add helpers near it)
- Modify: `internal/cli/task_test.go` (generalize the `put` fake + `putBodies` field, lines 16-20 and 65-76; keep the existing status assertion working)
- Test: `internal/cli/task_test.go` (new `TestTaskEdit*Assignee*` functions)

**Interfaces:**
- Consumes: `resolveAssignee(assignee string, team *clickup.Team, c *clickup.Client) (*clickup.User, *clickup.APIError)` (`internal/cli/search.go:226`); `(*clickup.Client).SelectTeam() (*clickup.Team, *clickup.APIError)`; `(*clickup.Client).GetTaskByID(id string) (*clickup.Task, *clickup.APIError)`; `displayID`, `renderAPIError`, `validStatuses`, `output.WriteError`, `output.WriteHelp`; `clickup.User{ID int64; Username string}`, `clickup.Task{ID string; Status.Status; List.ID; List.Name; Assignees []User}`.
- Produces: `(*clickup.Client).UpdateTaskAssignees(taskID string, add, rem []int64) *clickup.APIError`; unexported helpers `splitAssignees(string) []string`, `resolveAssignees([]string, *clickup.Team, *clickup.Client) ([]clickup.User, *clickup.APIError)`, `assigneeName(clickup.User) string`, `signedList(sign string, names []string) string`.

- [ ] **Step 1: Generalize the PUT fake to capture nested + raw bodies**

The assignees body is nested (`{"assignees":{"add":[...],"rem":[...]}}`); the current fake decodes into `map[string]string` and would fail. Change the field type and record the raw JSON too.

In `internal/cli/task_test.go`, change the struct (lines 16-20):

```go
type fakeClickUp struct {
	mux        *http.ServeMux
	putBodies  []map[string]any
	putRaw     []string
	postBodies []map[string]string
	commentGET int
}
```

Replace the `put` helper (lines 65-76):

```go
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
```

Add `"io"` to the import block (lines 3-14). The existing `TestTaskEditChangesStatus` assertion `f.putBodies[0]["status"] != "in review"` still holds: a `map[string]any` value holding the string `"in review"` compares equal to the untyped constant.

- [ ] **Step 2: Write the failing tests**

Add these fixtures and tests to `internal/cli/task_test.go`. `editTaskJSON` gives the task a current assignee (`jan`, id 42) that lines up with `membersTeamJSON` in `tasks_test.go` (jan 42, Ting Nguyen 189, Tinh Tran 190), so add/remove/idempotency all resolve consistently.

```go
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
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run TestTaskEdit -v`
Expected: FAIL - `TestTaskEditChanges*/UnknownFlag` may pass, but the new assignee tests fail to compile/pass because `--assignee` is an unknown flag and `UpdateTaskAssignees` does not exist.

- [ ] **Step 4: Add the API method**

In `internal/clickup/api.go`, immediately after `SetTaskStatus` (line 27):

```go
func (c *Client) UpdateTaskAssignees(taskID string, add, rem []int64) *APIError {
	if add == nil {
		add = []int64{}
	}
	if rem == nil {
		rem = []int64{}
	}
	body := map[string]any{"assignees": map[string][]int64{"add": add, "rem": rem}}
	return c.do(http.MethodPut, "/task/"+taskID, body, nil)
}
```

- [ ] **Step 5: Rewrite `cmdTaskEdit` and add helpers**

Replace `cmdTaskEdit` (`internal/cli/task.go:159-219`) with:

```go
func cmdTaskEdit(args []string, c *clickup.Client, out io.Writer) int {
	var id, status string
	statusSet := false
	var addTokens, remTokens []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			i++
			if i >= len(args) {
				output.WriteError(out, "--status needs a value", "Run `clickup-axi tasks edit <id> --status \"in review\"`")
				return 2
			}
			status = args[i]
			statusSet = true
		case "--assignee":
			i++
			if i >= len(args) {
				output.WriteError(out, "--assignee needs a value", "Run `clickup-axi tasks edit <id> --assignee <who>`")
				return 2
			}
			addTokens = append(addTokens, splitAssignees(args[i])...)
		case "--unassign":
			i++
			if i >= len(args) {
				output.WriteError(out, "--unassign needs a value", "Run `clickup-axi tasks edit <id> --unassign <who>`")
				return 2
			}
			remTokens = append(remTokens, splitAssignees(args[i])...)
		case "--help", "-h":
			fmt.Fprintln(out, tasksHelp)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				output.WriteError(out, fmt.Sprintf("unknown flag %q for tasks edit\n  valid: --status, --assignee, --unassign", args[i]))
				return 2
			}
			if id != "" {
				output.WriteError(out, "tasks edit takes exactly one task id")
				return 2
			}
			id = args[i]
		}
	}
	if id == "" {
		output.WriteError(out, "tasks edit needs a task id", "Run `clickup-axi tasks edit <id> --status \"<status>\"`")
		return 2
	}
	if !statusSet && len(addTokens) == 0 && len(remTokens) == 0 {
		output.WriteError(out, "tasks edit needs a change (--status, --assignee, or --unassign)",
			fmt.Sprintf("Run `clickup-axi tasks edit %s --status \"<status>\"` or `--assignee <who>`", id))
		return 2
	}

	t, err := c.GetTaskByID(id)
	if err != nil {
		return renderAPIError(out, err)
	}

	// Resolve assignee tokens against the workspace members. Name
	// resolution reuses search's resolveAssignee (me / id / member name),
	// so a miss or ambiguity returns the inline-candidates error.
	var addUsers, remUsers []clickup.User
	if len(addTokens) > 0 || len(remTokens) > 0 {
		team, terr := c.SelectTeam()
		if terr != nil {
			return renderAPIError(out, terr)
		}
		if addUsers, err = resolveAssignees(addTokens, team, c); err != nil {
			return renderAPIError(out, err)
		}
		if remUsers, err = resolveAssignees(remTokens, team, c); err != nil {
			return renderAPIError(out, err)
		}
	}

	// A person named in both lists is a self-contradictory request.
	for _, a := range addUsers {
		for _, r := range remUsers {
			if a.ID == r.ID {
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

	statusChanges := statusSet && !strings.EqualFold(t.Status.Status, status)
	assigneeChanges := len(add) > 0 || len(rem) > 0
	if !statusChanges && !assigneeChanges {
		fmt.Fprintf(out, "task: %s no changes (already assigned / same status)\n", displayID(t))
		return 0
	}

	// The fetch above resolved any custom id; mutate via internal id.
	if statusChanges {
		if serr := c.SetTaskStatus(t.ID, status); serr != nil {
			if valid := validStatuses(c, t.List.ID); valid != "" {
				output.WriteError(out, fmt.Sprintf("status %q not accepted for task %s in list %s\n  valid: %s",
					status, displayID(t), t.List.Name, valid),
					fmt.Sprintf("Run `clickup-axi tasks edit %s --status \"<status>\"` with one of the valid statuses", displayID(t)))
				return 1
			}
			return renderAPIError(out, serr)
		}
		fmt.Fprintf(out, "task: %s status changed: %s -> %s\n", displayID(t), t.Status.Status, status)
	}
	if assigneeChanges {
		if aerr := c.UpdateTaskAssignees(t.ID, add, rem); aerr != nil {
			return renderAPIError(out, aerr)
		}
		fmt.Fprintf(out, "task: %s assignees%s%s\n", displayID(t), signedList("+", addNames), signedList("-", remNames))
	}
	output.WriteHelp(out, fmt.Sprintf("Run `clickup-axi tasks %s` to see the task", displayID(t)))
	return 0
}

// splitAssignees turns a --assignee/--unassign value into tokens: a
// comma is always a separator, so "ting, me" is two people. Empty
// tokens (trailing commas, stray spaces) are dropped.
func splitAssignees(v string) []string {
	parts := strings.Split(v, ",")
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			tokens = append(tokens, p)
		}
	}
	return tokens
}

// resolveAssignees resolves each token to a member, propagating the
// first miss/ambiguity error (with its inline candidates) unchanged.
func resolveAssignees(tokens []string, team *clickup.Team, c *clickup.Client) ([]clickup.User, *clickup.APIError) {
	users := make([]clickup.User, 0, len(tokens))
	for _, tok := range tokens {
		u, err := resolveAssignee(tok, team, c)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, nil
}

// assigneeName prefers the resolved username; a numeric-id input
// resolves to an id-only user, so fall back to the id.
func assigneeName(u clickup.User) string {
	if u.Username != "" {
		return u.Username
	}
	return strconv.FormatInt(u.ID, 10)
}

// signedList renders " +Name +Name" (or "-") for the change summary.
func signedList(sign string, names []string) string {
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, " %s%s", sign, n)
	}
	return b.String()
}
```

`internal/cli/task.go` already imports `strconv` (used elsewhere in the file), so no import change is needed - `assigneeName` uses the existing import.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run TestTaskEdit -v`
Expected: PASS for all `TestTaskEdit*`, including the pre-existing status tests.

- [ ] **Step 7: Full verify**

Run: `gofmt -l internal/ && go vet ./... && go test ./...`
Expected: `gofmt` prints nothing; vet clean; all tests pass.

Note: `internal/cli/skill_test.go` may now fail because the surface/skill is stale - that is expected and fixed in Task 2. If it fails only on skill staleness, proceed; if any `cli` logic test fails, fix before committing.

- [ ] **Step 8: Commit**

```bash
git add internal/clickup/api.go internal/cli/task.go internal/cli/task_test.go
git commit -m "feat(tasks): add and remove assignees in tasks edit" -m "--assignee and --unassign are repeatable and comma-splittable, resolve
me/id/name via the shared resolver, and compose with --status. Adds
already assigned or removes not assigned collapse to a stated no-op.
New UpdateTaskAssignees issues PUT /task/{id} with assignees.add/rem."
```

---

### Task 2: surface table, skill, help, and README

**Files:**
- Modify: `internal/cli/surface.go` (the `tasks edit` entry, lines 53-57)
- Modify: `internal/cli/skill_template.md` (the tasks-edit documentation)
- Modify: `internal/cli/tasks.go` (`tasksHelp` edit section, lines 35-37, and examples, lines 42-49)
- Regenerate: `skills/clickup-axi/SKILL.md` (via `skill --write`)
- Modify: `README.md` (edit example)

**Interfaces:**
- Consumes: the `surface` entry struct (`usage`, `summary`, `skill`, `comment` fields) as used at `internal/cli/surface.go:53-62`.
- Produces: a non-stale skill so `skill --check` and `go test ./...` pass.

- [ ] **Step 1: Update the surface table entry**

In `internal/cli/surface.go`, replace the `tasks edit` entry (lines 53-57):

```go
	{
		usage:   "tasks edit <id>",
		summary: `Change status, add/remove assignees (--status, --assignee, --unassign)`,
		skill:   `clickup-axi tasks edit <id> --status "<status>"`,
	},
	{
		skill:   `clickup-axi tasks edit <id> --assignee <who> --unassign <who>`,
		comment: "reassign; --assignee/--unassign are repeatable and comma-separated; who = me | name | id",
	},
```

- [ ] **Step 2: Update the skill template prose**

The command list under `## Commands` is generated from the surface table via the `{{COMMANDS}}` placeholder (`internal/cli/skill_template.md:66`), so Step 1 already feeds the new `tasks edit` lines into the skill. Only add a prose sentence so the behavior is explained. In `internal/cli/skill_template.md`, immediately after the invalid-status sentence (the paragraph ending "...pick one and retry once." at line 70-71), append:

```
`tasks edit` also sets assignees: `--assignee <who>` adds and
`--unassign <who>` removes, both repeatable and comma-separated
(`--assignee ting,me`); `<who>` is `me`, a member name, or an id, and
they combine with `--status` in one call. Re-adding an existing
assignee or removing an absent one is a stated no-op.
```

(Read the file first and match its existing sentence style and line wrapping.)

- [ ] **Step 3: Update `tasksHelp`**

In `internal/cli/tasks.go`, extend the edit section (lines 35-37):

```go
edit <id> (mutations; "edit" is a reserved word, not an id):
  --status "<status>"  change status; valid statuses are echoed
                       when the status does not match
  --assignee <who>     add an assignee (repeatable, comma-separated);
                       who = me | member name | id
  --unassign <who>     remove an assignee (repeatable, comma-separated)
```

And add an example (after line 48):

```go
  clickup-axi tasks edit HGAI-2316 --assignee ting --unassign me
```

- [ ] **Step 4: Regenerate the skill**

Run: `go run ./cmd/clickup-axi skill --write`
Expected: `skills/clickup-axi/SKILL.md` is rewritten; `git status` shows it modified.

- [ ] **Step 5: Update the README**

In `README.md`, find the status-edit example and add an assignee example beside it, matching the existing formatting, e.g.:

```sh
clickup-axi tasks edit HGAI-2316 --assignee ting        # add an assignee
clickup-axi tasks edit HGAI-2316 --unassign me          # remove yourself
```

- [ ] **Step 6: Verify the skill is fresh and everything passes**

Run: `go run ./cmd/clickup-axi skill --check && gofmt -l internal/ && go vet ./... && go test ./...`
Expected: `skill --check` reports the skill in sync; `gofmt` prints nothing; vet clean; all tests pass (including `skill_test.go`).

- [ ] **Step 7: Commit**

```bash
git add internal/cli/surface.go internal/cli/skill_template.md internal/cli/tasks.go skills/clickup-axi/SKILL.md README.md
git commit -m "docs(tasks): document assignee edit across surface, skill, help, README"
```

---

### Task 3: end-to-end verification against the real API

**Files:** none (verification only)

- [ ] **Step 1: Build**

Run: `go build -o clickup-axi ./cmd/clickup-axi`
Expected: builds clean.

- [ ] **Step 2: Exercise on the scratch task**

Use the stored token (`~/.config/clickup-axi/token`) or `CLICKUP_TOKEN`. The scratch task is `HGAI-2378` (per the roadmap notes). NEVER echo the token.

Run an idempotent-friendly sequence and observe the exact stdout:

```sh
./clickup-axi tasks edit HGAI-2378 --assignee me      # add self
./clickup-axi tasks edit HGAI-2378 --assignee me      # expect: no changes ...
./clickup-axi tasks edit HGAI-2378 --unassign me      # remove self
./clickup-axi tasks edit HGAI-2378 --unassign me      # expect: no changes ...
```

Expected: first add prints `assignees +<you>` and exits 0; the repeat prints `no changes` and exits 0; unassign prints `assignees -<you>`; the repeat is a no-op. Confirm no raw ClickUp error text appears and each output ends with a `help[]` hint. Leave the task's assignee state as you found it.

- [ ] **Step 3: Confirm and report**

Report the observed outputs and exit codes. If anything diverges from the plan (output wording, exit code, error leakage), stop and fix in the relevant task before marking the branch done.
