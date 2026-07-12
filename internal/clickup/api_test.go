package clickup

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestCreateTaskMapsAllFields pins the wire mapping of a full create:
// every field present exactly once, dates flagged date-only, and the
// response's server-derived facts handed back to the caller.
func TestCreateTaskMapsAllFields(t *testing.T) {
	var body map[string]any
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("POST /api/v2/list/901234/task", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("POST body did not decode: %v", err)
		}
		w.Write([]byte(`{"id": "new1", "name": "Fix login", "status": {"status": "to do"}, "url": "https://app.clickup.com/t/new1"}`))
	})
	c := New(srv.URL+"/api/v2", "pk_test", srv.Client())

	task, err := c.CreateTask("901234", TaskCreate{
		Name:      "Fix login",
		Body:      "## Steps",
		Status:    "to do",
		Priority:  2,
		DueDate:   1783339200000,
		Assignees: []int64{1, 2},
		Tags:      []string{"api"},
		Parent:    "abc123",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	want := map[string]any{
		"name":             "Fix login",
		"markdown_content": "## Steps",
		"status":           "to do",
		"priority":         float64(2),
		"due_date":         float64(1783339200000),
		"due_date_time":    false,
		"assignees":        []any{float64(1), float64(2)},
		"tags":             []any{"api"},
		"parent":           "abc123",
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("POST body = %#v, want %#v", body, want)
	}
	if task.ID != "new1" || task.Status.Status != "to do" || task.URL != "https://app.clickup.com/t/new1" {
		t.Errorf("CreateTask() returned %#v, want the server's id/status/url", task)
	}
}

// TestCreateTaskOmitsUnsetFields pins that zero values stay off the
// wire, so ClickUp applies its own defaults instead of explicit nulls.
func TestCreateTaskOmitsUnsetFields(t *testing.T) {
	var body map[string]any
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("POST /api/v2/list/901234/task", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("POST body did not decode: %v", err)
		}
		w.Write([]byte(`{"id": "new1", "name": "Bare", "status": {"status": "open"}}`))
	})
	c := New(srv.URL+"/api/v2", "pk_test", srv.Client())

	if _, err := c.CreateTask("901234", TaskCreate{Name: "Bare"}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	want := map[string]any{"name": "Bare"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("POST body = %#v, want only the name", body)
	}
}
