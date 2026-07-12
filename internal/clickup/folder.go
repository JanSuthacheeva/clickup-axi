package clickup

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Folder is one folder with its lists, from GET /folder/{id}. It backs
// the folder:<id> default-list form: a sprint folder holds one list
// per sprint, so the current list can be derived instead of being
// re-configured every cycle.
type Folder struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Lists []FolderList `json:"lists"`
}

// FolderList is a list as it appears inside its folder, carrying the
// start/due range sprint lists have. Non-sprint lists leave both unset.
type FolderList struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	StartDate MsEpoch `json:"start_date"`
	DueDate   MsEpoch `json:"due_date"`
}

func (c *Client) GetFolder(id string) (*Folder, *APIError) {
	var f Folder
	if err := c.do(http.MethodGet, "/folder/"+url.PathEscape(id), nil, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ResolveFolder turns user input into a folder within one space - the
// same policy as ResolveList: a numeric value is fetched as an id
// (which also validates it), anything else matches the space's active
// folders case-insensitively, exact first then a unique substring.
// Every miss/ambiguity inlines candidate id,name pairs for a one-step
// retry.
func (c *Client) ResolveFolder(spaceID, input string) (*Folder, *APIError) {
	if isDigits(input) {
		return c.GetFolder(input)
	}
	var out struct {
		Folders []Folder `json:"folders"`
	}
	if err := c.do(http.MethodGet, "/space/"+url.PathEscape(spaceID)+"/folder?archived=false", nil, &out); err != nil {
		return nil, err
	}
	var exact, partial []Folder
	for _, f := range out.Folders {
		switch {
		case strings.EqualFold(f.Name, input):
			exact = append(exact, f)
		case containsFold(f.Name, input):
			partial = append(partial, f)
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
			"folder %q matches none of the space's %d folders: %s", input, len(out.Folders), folderNameList(out.Folders))}
	}
	return nil, &APIError{Message: fmt.Sprintf(
		"folder %q is ambiguous: %s", input, folderNameList(candidates))}
}

// folderNameList renders folders as `9012 "Sprints", ...` for inlining
// into error messages, capped like the other resolvers' candidates.
func folderNameList(folders []Folder) string {
	if len(folders) == 0 {
		return "none (the space has no folders)"
	}
	shown := folders
	var more int
	if len(shown) > resolveListCap {
		more = len(shown) - resolveListCap
		shown = shown[:resolveListCap]
	}
	parts := make([]string, len(shown))
	for i, f := range shown {
		parts[i] = fmt.Sprintf("%s %q", f.ID, f.Name)
	}
	out := strings.Join(parts, ", ")
	if more > 0 {
		out += fmt.Sprintf(", and %d more (ask the user for the exact folder name)", more)
	}
	return out
}

// CurrentList picks the folder's current list. Preference order:
//
//  1. a list whose start/due range contains now (latest start on ties),
//  2. the most recently started list (the sprint that just ended),
//  3. the next upcoming list (everything still in the future),
//  4. the folder's last list when nothing carries dates - ClickUp
//     orders sprint lists oldest-first, so last is newest.
//
// false only when the folder has no lists at all.
func (f *Folder) CurrentList(now time.Time) (FolderList, bool) {
	if len(f.Lists) == 0 {
		return FolderList{}, false
	}
	nowMs := now.UnixMilli()
	type dated struct {
		list  FolderList
		start int64
		due   int64
	}
	var current, past, future *dated
	for _, l := range f.Lists {
		start, ok := l.StartDate.Millis()
		if !ok {
			continue
		}
		due, _ := l.DueDate.Millis()
		d := dated{list: l, start: start, due: due}
		switch {
		case start <= nowMs && due >= nowMs:
			if current == nil || d.start > current.start {
				current = &d
			}
		case start <= nowMs:
			if past == nil || d.start > past.start {
				past = &d
			}
		default:
			if future == nil || d.start < future.start {
				future = &d
			}
		}
	}
	switch {
	case current != nil:
		return current.list, true
	case past != nil:
		return past.list, true
	case future != nil:
		return future.list, true
	}
	return f.Lists[len(f.Lists)-1], true
}
