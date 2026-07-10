package clickup

import (
	"fmt"
	"net/http"
	"strings"
)

// Space is a ClickUp space - the "project" level users think in.
type Space struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) GetSpaces(teamID string) ([]Space, *APIError) {
	var out struct {
		Spaces []Space `json:"spaces"`
	}
	if err := c.do(http.MethodGet, "/team/"+teamID+"/space", nil, &out); err != nil {
		return nil, err
	}
	return out.Spaces, nil
}

// ResolveSpace turns user input into a space: a numeric value is used
// as an id directly (no lookup), anything else is matched against the
// workspace's space names - an exact match (case-insensitive) wins,
// otherwise a unique substring match ("holy" finds "Holy Grail").
// Users refer to spaces by project name, so every failure inlines
// candidate id,name pairs for a one-step retry - the same recovery
// pattern as SelectTeam and ResolveMember.
func (c *Client) ResolveSpace(teamID, input string) (*Space, *APIError) {
	if isDigits(input) {
		return &Space{ID: input}, nil
	}
	spaces, err := c.GetSpaces(teamID)
	if err != nil {
		return nil, err
	}
	var exact, partial []Space
	for _, s := range spaces {
		switch {
		case strings.EqualFold(s.Name, input):
			exact = append(exact, s)
		case containsFold(s.Name, input):
			partial = append(partial, s)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = partial
	}
	switch len(candidates) {
	case 1:
		return &candidates[0], nil
	case 0:
		return nil, &APIError{Message: fmt.Sprintf(
			"space %q matches none of the workspace's %d spaces: %s", input, len(spaces), spaceList(spaces))}
	}
	return nil, &APIError{Message: fmt.Sprintf(
		"space %q is ambiguous: %s", input, spaceList(candidates))}
}

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
// tags (case-insensitive). It returns a lowercase-keyed map to each
// tag's stored casing (so writes use the canonical name and never mint
// a case-different duplicate) plus one message per unknown name, each
// inlining the existing tags - the same recovery pattern as
// ResolveSpace and ResolveMember, but aggregated so the edit's
// pre-flight can report every bad tag at once. The *APIError is
// transport-level only.
func (c *Client) ResolveSpaceTags(spaceID string, names []string) (map[string]string, []string, *APIError) {
	tags, err := c.GetSpaceTags(spaceID)
	if err != nil {
		return nil, nil, err
	}
	canonical := make(map[string]string, len(tags))
	for _, t := range tags {
		canonical[strings.ToLower(t.Name)] = t.Name
	}
	var bad []string
	for _, n := range names {
		if _, ok := canonical[strings.ToLower(n)]; !ok {
			bad = append(bad, fmt.Sprintf("tag %q does not exist in the space\n  existing: %s", n, tagList(tags)))
		}
	}
	return canonical, bad, nil
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

// spaceList renders spaces as `90121 "Holy Grail", 90122 "Webshop"`
// for inlining into error messages, capped to stay readable.
func spaceList(spaces []Space) string {
	if len(spaces) == 0 {
		return "none"
	}
	shown := spaces
	var more int
	if len(shown) > resolveListCap {
		more = len(shown) - resolveListCap
		shown = shown[:resolveListCap]
	}
	parts := make([]string, len(shown))
	for i, s := range shown {
		parts[i] = fmt.Sprintf("%s %q", s.ID, s.Name)
	}
	out := strings.Join(parts, ", ")
	if more > 0 {
		out += fmt.Sprintf(", and %d more (ask the user for the exact project name)", more)
	}
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
