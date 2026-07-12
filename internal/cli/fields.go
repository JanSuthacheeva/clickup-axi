package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

// taskField is one column an agent can request via --fields on the
// tasks listing, search, or the task detail view. Every field renders
// from the Task the API already returned, so a requested column never
// costs an extra request.
type taskField struct {
	name   string
	render func(*clickup.Task) string
}

// taskFields is the --fields vocabulary in its canonical (help and
// error) order. Output columns follow the request order instead.
// Deliberately absent: description (long-form content belongs in the
// detail view, AXI section 2) and space (the list response carries only
// a space id, usually the filter the agent just passed in).
var taskFields = []taskField{
	{"assignees", func(t *clickup.Task) string { return usernames(t.Assignees) }},
	{"priority", func(t *clickup.Task) string {
		if t.Priority == nil {
			return ""
		}
		return t.Priority.Priority
	}},
	{"tags", func(t *clickup.Task) string { return tagNames(t.Tags) }},
	{"list", func(t *clickup.Task) string {
		if t.List.Name == "" {
			return t.List.ID
		}
		return fmt.Sprintf("%s (%s)", t.List.Name, t.List.ID)
	}},
	{"url", func(t *clickup.Task) string { return t.URL }},
}

// fieldAliases forgives the singular forms the flags themselves use
// (--assignee, --add-tag), so typing the flag's word lands on the column.
var fieldAliases = map[string]string{"assignee": "assignees", "tag": "tags"}

// taskFieldNames renders the vocabulary for help text and errors.
func taskFieldNames() string {
	names := make([]string, len(taskFields))
	for i, f := range taskFields {
		names[i] = f.name
	}
	return strings.Join(names, ", ")
}

// resolveTaskFields canonicalizes the requested names (case-insensitive,
// aliases applied) into extra columns in request order, deduped against
// the defaults and each other - naming a column the schema already
// carries is a silent no-op, like re-applying a task's current state in
// an edit. Unknown names come back separately: they are decidable
// locally, so callers report them aggregated as one exit-2 usage error
// before any API call.
func resolveTaskFields(tokens, defaults []string) ([]taskField, []string) {
	seen := make(map[string]bool, len(defaults))
	for _, d := range defaults {
		seen[d] = true
	}
	var fields []taskField
	var unknown []string
	for _, tok := range tokens {
		name := strings.ToLower(tok)
		if canon, ok := fieldAliases[name]; ok {
			name = canon
		}
		// A default column (e.g. due) is a known name even though it is
		// not a vocabulary entry, so absorb it before the lookup.
		if seen[name] {
			continue
		}
		f, ok := lookupTaskField(name)
		if !ok {
			unknown = append(unknown, tok)
			continue
		}
		seen[name] = true
		fields = append(fields, f)
	}
	return fields, unknown
}

func lookupTaskField(name string) (taskField, bool) {
	for _, f := range taskFields {
		if f.name == name {
			return f, true
		}
	}
	return taskField{}, false
}

// renderUnknownFields reports every unknown --fields name in one usage
// error with the vocabulary inlined, so the agent fixes them all before
// a single retry. retry is the command-shaped recovery hint.
func renderUnknownFields(out io.Writer, unknown []string, retry string) int {
	if len(unknown) == 1 {
		output.WriteError(out, fmt.Sprintf("--fields: %q is not a task field\n  valid: %s", unknown[0], taskFieldNames()), retry)
		return 2
	}
	items := make([]string, len(unknown))
	for i, u := range unknown {
		items[i] = fmt.Sprintf("%q", u)
	}
	output.WriteError(out, fmt.Sprintf("--fields: %d names are not task fields: %s\n  valid: %s",
		len(items), strings.Join(items, ", "), taskFieldNames()), retry)
	return 2
}

// fieldNamesOf joins resolved columns for reconstructing the --fields
// value in carried-forward hints.
func fieldNamesOf(fields []taskField) string {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.name
	}
	return strings.Join(names, ",")
}

// fieldsHeader appends the extra column names to a TOON array header.
func fieldsHeader(defaults string, fields []taskField) string {
	for _, f := range fields {
		defaults += "," + f.name
	}
	return defaults
}

// fieldsCells renders the extra cells for one task, each prefixed with
// the cell separator so callers append it to a default row.
func fieldsCells(t *clickup.Task, fields []taskField) string {
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(",")
		b.WriteString(output.ToonCell(f.render(t)))
	}
	return b.String()
}

// tagNames joins tag names for a listing cell or the detail view line.
func tagNames(tags []clickup.Tag) string {
	names := make([]string, len(tags))
	for i, tg := range tags {
		names[i] = tg.Name
	}
	return strings.Join(names, ", ")
}
