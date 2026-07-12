# clickup-axi - Agent Instructions

A ClickUp CLI for AI agents following the AXI (Agent eXperience
Interface) principles. The binary is the transport of an agent
interface, not a human CLI - design decisions favor the agent reading
stdout.

## Before changing any agent-facing output

Read `.agents/skills/axi/SKILL.md` (the vendored AXI design guidelines)
first. Every command must keep the contract:

- stdout carries structured data AND errors; stderr is diagnostics only
- exit codes: 0 = success (including idempotent no-ops), 1 = error,
  2 = usage error; anything decidable locally (unknown flags, malformed
  values like a bad date or priority) is a usage error caught before any
  API call, while server-derived validation (unknown member, status,
  tag) exits 1 - both kinds aggregate all bad fields into one report
- zero results are stated explicitly ("tasks: 0 open tasks...")
- long text is truncated with a total-size hint and a `--full` escape
  hatch suggested only when actually truncated
- list, error, and mutation outputs end with parameterized `help[]`
  next-step hints; self-contained outputs (detail views, counts,
  confirmations) omit them per AXI section 9, keeping only hints that
  reveal truncated content (`--full`)
- no interactive prompts on agent paths (the only exceptions: `auth login`
  and the `setup` scope prompt, both only when stdin is a real terminal;
  agent paths get a flags-only usage error instead)
- raw ClickUp API errors never leak; translate them

The agent skill (`skills/clickup-axi/SKILL.md`) is generated - never
edit it by hand. When the command surface changes, update
`internal/cli/surface.go` and `internal/cli/skill_template.md`, run
`go run ./cmd/clickup-axi skill --write` from the repo root, and update
the README in the same commit. `go test ./...` and CI fail while the
committed skill is stale.

## Layout

The standard Go cmd/internal layout carrying a hexagonal dependency
rule: imports only point inward, no package imports its caller.

- `cmd/clickup-axi` - composition root; wires the real client and
  updater (injecting the rendered skill) and calls `cli.Run`
- `internal/cli` - driving adapter: dispatch, command handlers,
  rendering, the command surface table, skill generation (the embedded
  template lives here; `go:embed` paths are package-relative)
- `internal/clickup` - driven adapter for the ClickUp API: client,
  types, id resolution, token storage, `APIError` translation
- `internal/update` - driven adapter for GitHub releases: self-update,
  passive check, skill healing; never imports `cli`
- `internal/hostcfg` - driven adapter for agent-host configuration:
  installs, repairs, and removes the `context` session hook in Claude
  Code, Codex, and OpenCode configs; knows nothing about ClickUp, never
  imports `cli`
- `internal/output` - AXI output conventions (`help[]`, structured
  errors, TOON cells, truncation), shared by `cli` and `update`
- `internal/version` - ldflags injection target and fallback resolution

## Build, test, verify

```sh
go build -o clickup-axi ./cmd/clickup-axi
gofmt -l . && go vet ./... && go test ./...
```

- Dependencies: stdlib plus `golang.org/x/term` only. Do not add more
  without a strong reason.
- Tests use `httptest` fakes (see `newFakeClickUp`), which also isolates
  `CLICKUP_AXI_CUSTOM_IDS` from the host environment. Golden rule: every
  behavior visible to an agent has a test asserting the exact output.
- E2E verification runs the built binary against the real ClickUp API
  using the stored token (`~/.config/clickup-axi/token`) or
  `CLICKUP_TOKEN`. Prefer read-only commands and idempotent no-op edits.
  NEVER echo a token into a command, file in the repo, or transcript.

## Releasing

Pushing a `v*` tag triggers `.github/workflows/release.yml`: it re-runs
the verify gate (gofmt, vet, test, build, `skill --check`),
cross-compiles CGO-free binaries for linux/darwin (amd64+arm64) and
windows/amd64, and publishes them via `gh release create` as raw
unversioned assets (`clickup-axi_<os>_<arch>`) plus `SHA256SUMS`. Asset
names stay unversioned so the `releases/latest/download` URLs in the
skill never go stale. The tag (minus the `v`) is injected
into `internal/version.Version` via ldflags; source builds fall back
to the module build-info version, then `dev`.

`clickup-axi update` self-replaces the binary from the latest release
(checksum-verified, atomic rename). A passive once-per-24h check
(cache: `~/.config/clickup-axi/update-check`, hard 500ms budget,
silent on failure) appends an `update: vX.Y.Z available` notice to
command output, and installed skill copies heal from the embedded
skill after ordinary commands. All of it lives in `internal/update`, is
excluded from byte-exact outputs (`skill`, `update`, `--version`,
`--help`), never nags dev/pseudo-version builds, and is disabled by
`CLICKUP_AXI_NO_UPDATE_CHECK=1` (the test fakes pass an inert updater
instead).

## Domain notes

- ClickUp API v2, base `https://api.clickup.com/api/v2`, personal token
  in the `Authorization` header (no Bearer prefix).
- Task ids come in two kinds: internal (`86ey3tx8m`) and custom
  (`HGAI-2316`). Resolution policy lives in `clickup.GetTaskByID`:
  `CLICKUP_AXI_CUSTOM_IDS` set = custom-only; otherwise internal first,
  custom fallback. When forced, custom ids are also displayed everywhere.
- Workspace-scoped calls (`tasks`, `search`, `spaces`, `lists`,
  custom-id resolution) pick a team through `clickup.SelectTeam`:
  `CLICKUP_AXI_WORKSPACE` pins one
  when set (validated against the visible teams); otherwise the single
  visible team is used, and more than one is an error that inlines the
  visible `id,name` pairs so the agent can retry in one step.
- Spaces and assignees resolve by name, not just id: `ResolveSpace`
  (`clickup/space.go`) and `Team.ResolveMember` (`clickup/member.go`)
  take a numeric id directly, otherwise match an exact name/email
  (case-insensitive) then a unique substring. Every miss/ambiguity is
  an error inlining candidate `id,name` pairs (capped at
  `resolveListCap`) for a one-step retry - the same recovery pattern as
  `SelectTeam`. `tasks edit` tags resolve through `ResolveSpaceTags`
  (`clickup/space.go`): a case-insensitive exact match against the
  space's tags, canonicalized to the stored casing so a write never
  mints a case-different duplicate. Unlike the fail-fast resolvers it
  aggregates every unknown tag into one pre-flight report (space tags
  inlined via `tagList`), so a multi-field edit surfaces all bad values
  at once.
- `search` has no ClickUp text-search endpoint behind it: it filters
  tasks server-side via `GetTeamTasksPage` (paged, bounded by
  `searchMaxPages`) and ranks matches locally in `rankTasks` (title >
  id > description, AND across query words). It defaults to
  `assignee=me` and excludes the final closed status; `--assignee all`
  requires at least one bounding filter. `tasks` (the list form) shares
  `--assignee`/`--space` with the same resolvers, and its
  `--assignee all` requires `--space` as the bound.
- After a task is fetched, follow-up API calls use the internal id from
  the response.
- Rate limit is roughly 100 requests/minute; the client retries a GET
  once on 429 honoring Retry-After.
