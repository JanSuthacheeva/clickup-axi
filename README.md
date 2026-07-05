# clickup-axi

> [!WARNING]
> **STILL IN HEAVY DEVELOPMENT.** The command surface, output format, and
> flags change without notice, and there are no releases yet. Use at your
> own risk - especially anything that mutates tasks.

A minimal ClickUp CLI for AI agents, following the [AXI](https://axi.md) design
principles: token-efficient output, combined operations, structured errors, and
contextual next-step hints.

Deliberately small for now - it covers the two flows agents need most:

```sh
# your open tasks, then a task by id with its newest comments inline
clickup-axi tasks
clickup-axi tasks 86c2x1a

# change a task's status; an invalid status echoes the list's valid ones
clickup-axi tasks edit 86c2x1a --status "in review"
```

## Setup

```sh
go build -o clickup-axi .
./clickup-axi auth login   # guides you to create a token, hidden paste
```

`auth login` validates the token against the API and stores it at
`~/.config/clickup-axi/token` (mode 600). In a terminal it prompts for a
hidden paste; for scripted use pipe the token by reference
(`clickup-axi auth login < tokenfile`, or from a secret manager) - never
echo a literal token, as the command line lands in shell history and agent
transcripts. `auth logout` removes the stored token and is a no-op when
already logged out. A `CLICKUP_TOKEN` environment variable, when set, takes
precedence over the stored token.

Running `clickup-axi` with no arguments shows live state (user, workspaces) instead
of help text. `clickup-axi tasks --help` has flags and examples.

Task ids can be internal (`86ey3tx8m`) or custom (`HGAI-2316`); an id is
tried as internal first with a custom-id fallback. Set
`CLICKUP_AXI_CUSTOM_IDS=1` to always resolve custom ids directly and skip
the internal attempt.

## Behavior contract (AXI)

- stdout carries structured data and errors; stderr is for diagnostics only
- exit codes: 0 success (including idempotent no-ops), 1 error, 2 usage error
- long descriptions are truncated with a total size hint; `--full` lifts it
- zero results are stated explicitly, never silent
- no interactive prompts on agent paths; `auth login` prompts for a paste
  only when stdin is a real terminal, and reads piped stdin otherwise

## Claude Code integration

The Agent Skill in `skills/clickup-axi/` teaches agents when and how to
use this CLI. Install it with the
[skills](https://skills.sh) installer:

```sh
npx skills add JanSuthacheeva/clickup-axi --skill clickup-axi -g
```

or from a local checkout, symlink it and put the binary on PATH:

```sh
go build -o ~/.local/bin/clickup-axi .
ln -sfn "$(pwd)/skills/clickup-axi" ~/.claude/skills/clickup-axi
```

Agents then load the skill on their own when a conversation touches
ClickUp tasks.

## Tests

```sh
go test ./...
```
