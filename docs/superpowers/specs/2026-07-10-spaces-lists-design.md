# Step 5: spaces and lists discovery - design

This is the implementation plan for Step 5 of `docs/v1.0.0.md`. It
exposes ClickUp's space/list hierarchy without adding configuration or
mutations, so agents can discover a list id before Step 6 creates or
moves a task.

## Commands and output

```
clickup-axi spaces
clickup-axi lists --space <name|id> [--archived]
```

- `spaces` selects the workspace through `SelectTeam`, fetches active
  spaces, sorts by case-insensitive name then id, and renders
  `spaces[N]{id,name}`.
- `lists` requires exactly one `--space`, resolved through
  `ResolveSpace`. It renders `lists[N]{id,name,folder}`. Folderless
  lists use the literal `(folderless)` folder value.
- Active and archived lists each sort folderless-first, then by
  case-insensitive folder, name, and id.
- Both commands state zero results explicitly and end every successful
  response with contextual `help[]`. Missing/repeated `--space`, a
  missing value, positional arguments, and unknown flags are exit-2
  usage errors with an inline corrective command.

## ClickUp adapter

Add `clickup.ListRef` with `ID`, `Name`, and `Folder`, and expose
`GetSpaceLists(spaceID string, archived bool)`.

- Active discovery fetches active folderless lists, active folders, and
  active lists from every active folder.
- `--archived` filters on the **list's** archived state. It fetches
  archived folderless lists, both active and archived folders,
  deduplicates folder ids, then fetches archived lists from every
  discovered folder.
- Accumulate the complete inventory before rendering. On any required
  API failure, return the translated error and print no partial list
  rows.

Step 6 will use `ListRef` to resolve a target list: exact id first,
then exact case-insensitive name, then a unique case-insensitive
substring. Ambiguity candidates will render `id,name,folder`; creation
and move will resolve active lists only.

## Tests and handoff

Cover exact output and exit codes for populated/empty spaces, active
and archived folderless/folder-contained lists, ordering, every flag
error, space-resolution failures, and translated API failures. Assert
that archived discovery visits both active and archived folders and
that a late folder/list failure produces no partial inventory. Update
the surface, README, skill template, regenerated skill, then run
`gofmt -l .`, `go vet ./...`, `go test ./...`, and
`go run ./cmd/clickup-axi skill --check`.
