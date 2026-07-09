package clickup

import (
	"fmt"
	"strings"
)

// resolveListCap bounds how many candidates a resolution error
// inlines, so recovery hints stay readable on large workspaces.
const resolveListCap = 20

// ResolveMember matches user input against the workspace's members:
// an exact username or email match (case-insensitive) wins, otherwise
// a unique substring match on the username ("ting" finds "Ting
// Nguyen"). Users refer to people by name, so every failure inlines
// candidates for a one-step retry - the same recovery pattern as
// SelectTeam and ResolveSpace.
func (t *Team) ResolveMember(input string) (*User, *APIError) {
	var exact, partial []User
	for _, m := range t.Members {
		u := m.User
		switch {
		case strings.EqualFold(u.Username, input),
			u.Email != "" && strings.EqualFold(u.Email, input):
			exact = append(exact, u)
		case containsFold(u.Username, input):
			partial = append(partial, u)
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
			"assignee %q matches none of the members of %s: %s", input, t.Name, memberList(allMembers(t)))}
	}
	return nil, &APIError{Message: fmt.Sprintf(
		"assignee %q is ambiguous: %s", input, memberList(candidates))}
}

// ResolveMemberID validates a numeric id against the workspace's
// members, returning the full member (username included) so a mutation
// never trusts an id that belongs to nobody. A miss inlines candidates
// for a one-step retry, the same recovery pattern as ResolveMember.
func (t *Team) ResolveMemberID(id int64) (*User, *APIError) {
	for i := range t.Members {
		if t.Members[i].User.ID == id {
			u := t.Members[i].User
			return &u, nil
		}
	}
	return nil, &APIError{Message: fmt.Sprintf(
		"assignee %d matches none of the members of %s: %s", id, t.Name, memberList(allMembers(t)))}
}

func allMembers(t *Team) []User {
	users := make([]User, len(t.Members))
	for i, m := range t.Members {
		users[i] = m.User
	}
	return users
}

// memberList renders users as `89547987 "Jan Suthacheeva", ...` for
// inlining into error messages, capped to stay readable.
func memberList(users []User) string {
	if len(users) == 0 {
		return "none"
	}
	shown := users
	var more int
	if len(shown) > resolveListCap {
		more = len(shown) - resolveListCap
		shown = shown[:resolveListCap]
	}
	parts := make([]string, len(shown))
	for i, u := range shown {
		parts[i] = fmt.Sprintf("%d %q", u.ID, u.Username)
	}
	s := strings.Join(parts, ", ")
	if more > 0 {
		s += fmt.Sprintf(", and %d more", more)
	}
	return s
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
