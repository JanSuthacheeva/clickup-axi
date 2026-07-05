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
  2 = usage error (unknown flags rejected with valid ones listed inline)
- zero results are stated explicitly ("tasks: 0 open tasks...")
- long text is truncated with a total-size hint and a `--full` escape
  hatch suggested only when actually truncated
- every output ends with parameterized `help[]` next-step hints
- no interactive prompts on agent paths (the only exception: `auth login`
  prompts when stdin is a real terminal)
- raw ClickUp API errors never leak; translate them

When the command surface changes, update `skills/clickup-axi/SKILL.md`
and the README in the same commit.

## Build, test, verify

```sh
go build -o clickup-axi .
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

## Domain notes

- ClickUp API v2, base `https://api.clickup.com/api/v2`, personal token
  in the `Authorization` header (no Bearer prefix).
- Task ids come in two kinds: internal (`86ey3tx8m`) and custom
  (`HGAI-2316`). Resolution policy lives in `getTaskByID`:
  `CLICKUP_AXI_CUSTOM_IDS` set = custom-only; otherwise internal first,
  custom fallback. When forced, custom ids are also displayed everywhere.
- After a task is fetched, follow-up API calls use the internal id from
  the response.
- Rate limit is roughly 100 requests/minute; the client retries a GET
  once on 429 honoring Retry-After.
