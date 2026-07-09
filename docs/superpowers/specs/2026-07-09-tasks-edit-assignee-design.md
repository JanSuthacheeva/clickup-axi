# `tasks edit` assignee mutation - design

Step 2 of `docs/v1.0.0.md`: give the existing `tasks edit` command the
ability to add and remove assignees, reusing the name resolvers that
`search`/`tasks --assignee` already share. This closes the first write
gap beyond status and comments.

## Command surface

```
clickup-axi tasks edit <id> [--status "<s>"] [--assignee <who>]... [--unassign <who>]...
```

- `--assignee` adds people; `--unassign` removes them.
- Both flags are **repeatable** and each value accepts a
  **comma-separated list**. These reach the same result:

  ```
  tasks edit HGAI-1 --assignee ting,me --unassign bob,alice
  tasks edit HGAI-1 --assignee ting --assignee me --unassign bob --unassign alice
  ```

- `<who>` resolves as `me`, a numeric member id, or a member name
  (exact, case-insensitive -> unique substring), via the existing
  `resolveAssignee` (`search.go`) which wraps `Team.ResolveMember`.
- At least one of `--status` / `--assignee` / `--unassign` is required.
- `--status`, `--assignee`, and `--unassign` compose in a single call.

Consequence of comma-splitting: a comma is always a separator, so a
member whose username literally contains a comma must be targeted by
numeric id. This is a non-issue for real ClickUp usernames.

## Flow (`cmdTaskEdit` in `internal/cli/task.go`)

1. Parse flags into `status string`, `statusSet bool`, `add []string`,
   `rem []string`. Each `--assignee`/`--unassign` value is split on `,`,
   trimmed, empties dropped, and appended to the respective slice.
2. Validate:
   - unknown flag -> exit 2, valid set inlined
     (`--status, --assignee, --unassign`);
   - no change flags at all -> exit 2, "needs a change" (replaces
     today's "needs --status") with an example.
3. `c.GetTaskByID(id)` - resolves a custom id to the internal id, as the
   status path already does.
4. If `add` or `rem` is non-empty: `c.SelectTeam()`, then resolve every
   token via `resolveAssignee`. A miss or ambiguity returns the existing
   inline-candidates `APIError` unchanged (rendered via
   `renderAPIError`). Collect resolved adds/removes as `[]int64` id
   sets (dedup within each set).
5. Conflict check: any id present in **both** resolved add and remove
   sets -> exit 2 (usage error; the request is self-contradictory).
6. Idempotency filter against `t.Assignees`:
   - drop adds whose id is already assigned;
   - drop removes whose id is not currently assigned.
7. No-op detection: if the status is unchanged (or unset) **and** the
   filtered add/remove sets are both empty -> print a `no-op` line and
   exit 0.
8. Apply (task fetched above resolved the id; mutate by internal id):
   - status change keeps `c.SetTaskStatus` unchanged, including its
     valid-statuses enrichment on rejection;
   - assignee change calls a new
     `c.UpdateTaskAssignees(taskID string, add, rem []int64) *APIError`
     -> `PUT /task/{id}` with body
     `{"assignees":{"add":[...],"rem":[...]}}`.

   When both a status and an assignee change are requested, this is two
   PUTs. That is acceptable and keeps each mutation isolated; step 3's
   full multi-field edit can later fold both into one `UpdateTask` body.

## Output

Greppable, stable, matching the existing `status changed:` line. One
line per applied change; the `help[]` view hint is preserved.

```
task: HGAI-1 status changed: open -> in review
task: HGAI-1 assignees +Ting Nguyen -Bob Smith
```

- Names in the assignee line use the resolved username; an id-only
  resolution (numeric input) shows the id.
- Full no-op (`--status` matches current and every add/remove filtered
  away): `task: HGAI-1 no changes (already assigned / same status)`,
  exit 0.
- A partial case (e.g. status changed but every assignee add was
  already assigned) prints only the line(s) that actually changed.

## API layer (`internal/clickup/api.go`)

New method beside `SetTaskStatus`:

```go
func (c *Client) UpdateTaskAssignees(taskID string, add, rem []int64) *APIError {
    body := map[string]any{"assignees": map[string][]int64{"add": add, "rem": rem}}
    return c.do(http.MethodPut, "/task/"+taskID, body, nil)
}
```

`SetTaskStatus` is untouched. No new resolver logic in `clickup` - name
resolution stays in `cli` via `resolveAssignee`, matching the current
split.

## Surface / skill / docs (same commit set)

CI fails on a stale skill, so in the same work:

- `internal/cli/surface.go` - extend the `tasks edit` entry's summary
  and `skill` example to include `--assignee` / `--unassign`.
- `internal/cli/skill_template.md` - document the new flags and the
  comma / repeatable forms.
- `go run ./cmd/clickup-axi skill --write` - regenerate
  `skills/clickup-axi/SKILL.md` (never hand-edited).
- `internal/cli/tasks.go` - update the `tasksHelp` `edit` section.
- `README.md` - add an assignee-edit example next to the status one.

## Tests (`internal/cli/tasks_test.go`, httptest fake)

Every agent-visible behavior gets an exact-output assertion:

1. add by name -> `+Name` line, PUT body `assignees.add`.
2. add `me` -> resolves via `GetUser`.
3. add by numeric id -> id shown, no member lookup needed.
4. `--unassign` by name -> `-Name` line, PUT body `assignees.rem`.
5. comma-separated value -> two adds in one flag.
6. mixed comma + repeated flag -> same resolved set.
7. combined `--status` + `--assignee` -> both lines, both PUTs.
8. idempotent add (already assigned) -> no-op, exit 0, no PUT.
9. idempotent unassign (not assigned) -> no-op, exit 0, no PUT.
10. name miss -> inline candidates, exit 1.
11. ambiguous name -> ambiguous candidates, exit 1.
12. same person in `--assignee` and `--unassign` -> exit 2.
13. no change flags -> exit 2, "needs a change".
14. unknown flag -> exit 2, valid set inlined.

## Out of scope (later steps)

Priority, body, name, due, tags (step 3); `create` (step 6). The
general `UpdateTask` body refactor belongs to step 3, not here.
