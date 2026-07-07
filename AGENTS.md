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

The agent skill (`skills/clickup-axi/SKILL.md`) is generated - never
edit it by hand. When the command surface changes, update `surface.go`
and `skill_template.md`, run `go run . skill --write`, and update the
README in the same commit. `go test ./...` and CI fail while the
committed skill is stale.

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

## Releasing

Pushing a `v*` tag triggers `.github/workflows/release.yml`: it re-runs
the verify gate (gofmt, vet, test, build, `skill --check`),
cross-compiles CGO-free binaries for linux/darwin (amd64+arm64) and
windows/amd64, and publishes them via `gh release create` as raw
unversioned assets (`clickup-axi_<os>_<arch>`) plus `SHA256SUMS`. Asset
names stay unversioned so the `releases/latest/download` URLs in the
README and skill never go stale. The tag (minus the `v`) is injected
into `main.version` via ldflags; source builds fall back to the module
build-info version, then `dev`.

`clickup-axi update` self-replaces the binary from the latest release
(checksum-verified, atomic rename). A passive once-per-24h check
(cache: `~/.config/clickup-axi/update-check`, hard 500ms budget,
silent on failure) appends an `update: vX.Y.Z available` notice to
command output, and installed skill copies heal from the embedded
skill after ordinary commands. All of it lives in `update.go`, is
excluded from byte-exact outputs (`skill`, `update`, `--version`,
`--help`), never nags dev/pseudo-version builds, and is disabled by
`CLICKUP_AXI_NO_UPDATE_CHECK=1` (the test fakes pass an inert updater
instead).

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
