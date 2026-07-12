package clickup

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestGetFolder(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/api/v2", "pk_test", srv.Client())

	mux.HandleFunc("GET /api/v2/folder/90123456", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id": "90123456", "name": "Sprints", "lists": [
			{"id": "901", "name": "Sprint 1 (6/23 - 7/6)", "start_date": "1750636800000", "due_date": "1751846400000"},
			{"id": "902", "name": "Sprint 2 (7/7 - 7/20)", "start_date": 1751846400000, "due_date": null}
		]}`))
	})

	f, err := c.GetFolder("90123456")
	if err != nil {
		t.Fatalf("GetFolder: %v", err)
	}
	if f.Name != "Sprints" || len(f.Lists) != 2 {
		t.Fatalf("folder = %+v", f)
	}
	if s, ok := f.Lists[0].StartDate.Millis(); !ok || s != 1750636800000 {
		t.Fatalf("string start_date = %v, %v", s, ok)
	}
	if s, ok := f.Lists[1].StartDate.Millis(); !ok || s != 1751846400000 {
		t.Fatalf("numeric start_date = %v, %v", s, ok)
	}
	if _, ok := f.Lists[1].DueDate.Millis(); ok {
		t.Fatal("null due_date parsed as set")
	}
}

// julyMs renders 2026-07-<day> 04:00 UTC as the millisecond-epoch
// string ClickUp uses for list start/due dates.
func julyMs(day int) MsEpoch {
	t := time.Date(2026, time.July, day, 4, 0, 0, 0, time.UTC)
	return MsEpoch(strconv.FormatInt(t.UnixMilli(), 10))
}

func TestCurrentList(t *testing.T) {
	list := func(id string, startDay, dueDay int) FolderList {
		l := FolderList{ID: id, Name: id}
		if startDay != 0 {
			l.StartDate = julyMs(startDay)
		}
		if dueDay != 0 {
			l.DueDate = julyMs(dueDay)
		}
		return l
	}
	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		lists []FolderList
		want  string
		ok    bool
	}{
		{"empty folder", nil, "", false},
		{"contains today wins", []FolderList{list("past", 1, 5), list("cur", 7, 20), list("next", 21, 31)}, "cur", true},
		{"latest past when between sprints", []FolderList{list("older", 1, 3), list("old", 4, 5), list("next", 21, 31)}, "old", true},
		{"earliest future when all upcoming", []FolderList{list("late", 25, 31), list("soon", 21, 24)}, "soon", true},
		{"open-ended due lands in past bucket", []FolderList{list("open", 7, 0), list("older", 1, 5)}, "open", true},
		{"no dates falls back to last", []FolderList{list("a", 0, 0), list("b", 0, 0)}, "b", true},
		{"dated beats undated", []FolderList{list("undated", 0, 0), list("cur", 7, 20)}, "cur", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &Folder{Lists: tc.lists}
			got, ok := f.CurrentList(now)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got.ID != tc.want {
				t.Fatalf("CurrentList = %q, want %q", got.ID, tc.want)
			}
		})
	}
}
