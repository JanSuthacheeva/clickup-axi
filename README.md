# clickup-axi

> [!IMPORTANT]
> **Still in active development.** Stable to use right now, but the
> command surface keeps evolving - always update to the newest version
> (`clickup-axi update`, or agree when the update notice appears).
> Feature requests are very welcome in the
> [issues](https://github.com/JanSuthacheeva/clickup-axi/issues).

A minimal ClickUp CLI for AI agents, following the [AXI](https://axi.md)
design principles: token-efficient output, structured errors, and
next-step hints on every command. It covers the flows agents need most:
listing your open tasks, viewing one task with its comments, changing
its status, and commenting on it.

## Installation

Install the agent skill - that is all. Any agent that supports Agent
Skills (Claude Code, Codex, opencode, ...) downloads the binary itself
on first use:

```sh
npx skills add JanSuthacheeva/clickup-axi --skill clickup-axi -g
```

Then authenticate once in your own terminal (the binary appears in
`~/.local/bin` after the agent's first use):

```sh
clickup-axi auth login     # guides you to a token, hidden paste
```

## Quickstart

Mention a task in a conversation ("what's on my plate?", "summarize
HGAI-2316") - or run the CLI directly:

```sh
clickup-axi                          # who am I + workspaces
clickup-axi tasks                    # your open tasks
clickup-axi tasks HGAI-2316          # one task with newest comments
clickup-axi tasks edit HGAI-2316 --status "in review"
clickup-axi tasks comment HGAI-2316 --text "Deployed to staging"
```

Task ids can be internal (`86ey3tx8m`) or custom (`HGAI-2316`).
`clickup-axi tasks --help` has flags and examples.

## Environment variables

| Variable | Effect | Default |
|---|---|---|
| `CLICKUP_TOKEN` | API token to use; takes precedence over the token stored by `auth login`. | unset - the stored token is used |
| `CLICKUP_AXI_WORKSPACE` | Pin the workspace (id from `clickup-axi`) that `tasks` and custom-id resolution operate in. Required once the token sees more than one workspace; a pin the token cannot see is an error listing the visible ones. | unset - the single visible workspace is used |
| `CLICKUP_AXI_CUSTOM_IDS` | Resolve task ids as custom ids (`HGAI-2316`) only, skipping the internal-id attempt, and display custom ids everywhere. Any value except `0` or `false` enables it. | unset - internal ids tried first, custom fallback |
| `CLICKUP_AXI_NO_UPDATE_CHECK` | Disable everything update-related: the passive daily check, the `update:` notice, and skill-copy healing. Any non-empty value. | unset - update checks enabled |

## Auto updates

`clickup-axi update` replaces the binary in place with the latest
release (checksum-verified, atomic). Commands also check for a newer
release at most once per day and append a one-line
`update: vX.Y.Z available` notice; agents relay it as a question
instead of updating on their own. Installed skill copies heal from the
binary automatically, so binary and skill never skew.
`CLICKUP_AXI_NO_UPDATE_CHECK=1` disables all of this.
