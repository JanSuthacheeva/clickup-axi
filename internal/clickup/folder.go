package clickup

import (
	"net/http"
	"net/url"
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
