package clickup

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// FolderlessList is the explicit folder context rendered for a list
// directly in a space. It keeps duplicate list names distinguishable.
const FolderlessList = "(folderless)"

// ListRef is the minimum stable list identity needed for discovery and
// later name resolution. Folder is either a containing folder's name or
// FolderlessList.
type ListRef struct {
	ID     string
	Name   string
	Folder string
}

type folderRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type listRefWire struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetSpaceLists returns a complete list inventory for one space. In
// active mode it traverses active folders only. In archived mode it
// queries both active and archived folders because archive state belongs
// to the list, not its parent folder. A failure returns no partial data.
func (c *Client) GetSpaceLists(spaceID string, archived bool) ([]ListRef, *APIError) {
	folderless, err := c.getFolderlessLists(spaceID, archived)
	if err != nil {
		return nil, err
	}

	folderModes := []bool{false}
	if archived {
		folderModes = append(folderModes, true)
	}
	folders := make([]folderRef, 0)
	seenFolders := make(map[string]bool)
	for _, folderArchived := range folderModes {
		found, err := c.getFolders(spaceID, folderArchived)
		if err != nil {
			return nil, err
		}
		for _, f := range found {
			if seenFolders[f.ID] {
				continue
			}
			seenFolders[f.ID] = true
			folders = append(folders, f)
		}
	}

	refs := make([]ListRef, 0, len(folderless))
	for _, l := range folderless {
		refs = append(refs, ListRef{ID: l.ID, Name: l.Name, Folder: FolderlessList})
	}
	for _, f := range folders {
		lists, err := c.getFolderLists(f.ID, archived)
		if err != nil {
			return nil, err
		}
		for _, l := range lists {
			refs = append(refs, ListRef{ID: l.ID, Name: l.Name, Folder: f.Name})
		}
	}
	return refs, nil
}

// ResolveList turns user input into a list within one space: a numeric
// value is used as an id directly (no lookup), anything else is matched
// against the space's active lists - an exact match (case-insensitive)
// wins, otherwise a unique substring match. Every miss/ambiguity
// inlines candidate id,name pairs with folder context for a one-step
// retry - the same recovery pattern as ResolveSpace and ResolveMember.
func (c *Client) ResolveList(spaceID, input string) (*ListRef, *APIError) {
	if isDigits(input) {
		return &ListRef{ID: input}, nil
	}
	refs, err := c.GetSpaceLists(spaceID, false)
	if err != nil {
		return nil, err
	}
	var exact, partial []ListRef
	for _, l := range refs {
		switch {
		case strings.EqualFold(l.Name, input):
			exact = append(exact, l)
		case containsFold(l.Name, input):
			partial = append(partial, l)
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
			"list %q matches none of the space's %d lists: %s", input, len(refs), listRefList(refs))}
	}
	return nil, &APIError{Message: fmt.Sprintf(
		"list %q is ambiguous: %s", input, listRefList(candidates))}
}

// listRefList renders lists as `901 "Sprint 12" (folderless), 902
// "Sprint 12" (in Development)` for inlining into error messages; the
// folder context keeps duplicate names distinguishable, capped like the
// other resolvers' candidate lists.
func listRefList(refs []ListRef) string {
	if len(refs) == 0 {
		return "none (the space has no active lists)"
	}
	shown := refs
	var more int
	if len(shown) > resolveListCap {
		more = len(shown) - resolveListCap
		shown = shown[:resolveListCap]
	}
	parts := make([]string, len(shown))
	for i, l := range shown {
		folder := l.Folder
		if folder != FolderlessList {
			folder = "(in " + folder + ")"
		}
		parts[i] = fmt.Sprintf("%s %q %s", l.ID, l.Name, folder)
	}
	out := strings.Join(parts, ", ")
	if more > 0 {
		out += fmt.Sprintf(", and %d more (ask the user for the exact list name)", more)
	}
	return out
}

func (c *Client) getFolderlessLists(spaceID string, archived bool) ([]listRefWire, *APIError) {
	var out struct {
		Lists []listRefWire `json:"lists"`
	}
	if err := c.do(http.MethodGet, "/space/"+url.PathEscape(spaceID)+"/list?archived="+strconv.FormatBool(archived), nil, &out); err != nil {
		return nil, err
	}
	return out.Lists, nil
}

func (c *Client) getFolders(spaceID string, archived bool) ([]folderRef, *APIError) {
	var out struct {
		Folders []folderRef `json:"folders"`
	}
	if err := c.do(http.MethodGet, "/space/"+url.PathEscape(spaceID)+"/folder?archived="+strconv.FormatBool(archived), nil, &out); err != nil {
		return nil, err
	}
	return out.Folders, nil
}

func (c *Client) getFolderLists(folderID string, archived bool) ([]listRefWire, *APIError) {
	var out struct {
		Lists []listRefWire `json:"lists"`
	}
	if err := c.do(http.MethodGet, "/folder/"+url.PathEscape(folderID)+"/list?archived="+strconv.FormatBool(archived), nil, &out); err != nil {
		return nil, err
	}
	return out.Lists, nil
}
