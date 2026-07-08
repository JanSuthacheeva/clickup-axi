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
