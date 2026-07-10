package clickup

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGetSpaceListsArchivedTraversesBothFolderStates(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/api/v2", "pk_test", srv.Client())

	mux.HandleFunc("GET /api/v2/space/90121/list", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("archived"); got != "true" {
			t.Errorf("folderless archived = %q, want true", got)
		}
		w.Write([]byte(`{"lists": [{"id": "11", "name": "Old Inbox"}]}`))
	})
	folderCalls := map[string]int{}
	mux.HandleFunc("GET /api/v2/space/90121/folder", func(w http.ResponseWriter, r *http.Request) {
		archived := r.URL.Query().Get("archived")
		folderCalls[archived]++
		switch archived {
		case "false":
			w.Write([]byte(`{"folders": [{"id": "f1", "name": "Current"}]}`))
		case "true":
			w.Write([]byte(`{"folders": [{"id": "f1", "name": "Current"}, {"id": "f2", "name": "Old"}]}`))
		default:
			t.Errorf("unexpected archived query %q", archived)
		}
	})
	folderListCalls := map[string]int{}
	for _, id := range []string{"f1", "f2"} {
		id := id
		mux.HandleFunc("GET /api/v2/folder/"+id+"/list", func(w http.ResponseWriter, r *http.Request) {
			folderListCalls[id]++
			if got := r.URL.Query().Get("archived"); got != "true" {
				t.Errorf("%s archived = %q, want true", id, got)
			}
			if id == "f1" {
				w.Write([]byte(`{"lists": [{"id": "12", "name": "Current archived"}]}`))
				return
			}
			w.Write([]byte(`{"lists": [{"id": "13", "name": "Old archived"}]}`))
		})
	}

	got, err := c.GetSpaceLists("90121", true)
	if err != nil {
		t.Fatalf("GetSpaceLists() error = %v", err)
	}
	want := []ListRef{
		{ID: "11", Name: "Old Inbox", Folder: FolderlessList},
		{ID: "12", Name: "Current archived", Folder: "Current"},
		{ID: "13", Name: "Old archived", Folder: "Old"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetSpaceLists() = %#v, want %#v", got, want)
	}
	if folderCalls["false"] != 1 || folderCalls["true"] != 1 {
		t.Errorf("folder traversal = %#v, want one request for each archive state", folderCalls)
	}
	if folderListCalls["f1"] != 1 || folderListCalls["f2"] != 1 {
		t.Errorf("folder list traversal = %#v, want each unique folder once", folderListCalls)
	}
}

func TestGetSpaceListsFailureReturnsNoPartialInventory(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/api/v2", "pk_test", srv.Client())

	mux.HandleFunc("GET /api/v2/space/90121/list", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"lists": [{"id": "11", "name": "Would be partial"}]}`))
	})
	mux.HandleFunc("GET /api/v2/space/90121/folder", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"folders": [{"id": "f1", "name": "Folder"}]}`))
	})
	mux.HandleFunc("GET /api/v2/folder/f1/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"err": "broken upstream"}`))
	})

	got, err := c.GetSpaceLists("90121", false)
	if got != nil {
		t.Errorf("GetSpaceLists() returned partial inventory %#v, want nil", got)
	}
	if err == nil || err.Message != "ClickUp rejected the request: broken upstream (HTTP 500)" {
		t.Errorf("GetSpaceLists() error = %#v, want translated HTTP 500", err)
	}
}
