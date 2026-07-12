package clickup

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetTaskWithSubtasksKeepsExpansionDetailOnly(t *testing.T) {
	t.Setenv("CLICKUP_AXI_CUSTOM_IDS", "")
	var queries []string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("GET /api/v2/task/parent1", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		w.Write([]byte(`{
			"id": "parent1",
			"name": "Parent",
			"status": {"status": "open"},
			"subtasks": [
				{"id": "child1", "parent": "parent1", "name": "Child", "status": {"status": "in progress"}}
			]
		}`))
	})
	c := New(srv.URL+"/api/v2", "pk_test", srv.Client())
	c.SeedDateLocation("UTC")

	detail, err := c.GetTaskWithSubtasksByID("parent1")
	if err != nil {
		t.Fatalf("GetTaskWithSubtasksByID() error = %v", err)
	}
	if len(detail.Subtasks) != 1 || detail.Subtasks[0].ID != "child1" {
		t.Fatalf("GetTaskWithSubtasksByID() subtasks = %#v, want child1", detail.Subtasks)
	}
	if _, err := c.GetTaskByID("parent1"); err != nil {
		t.Fatalf("GetTaskByID() error = %v", err)
	}

	if len(queries) != 2 {
		t.Fatalf("task requests = %d, want 2", len(queries))
	}
	if queries[0] != "include_markdown_description=true&include_subtasks=true" {
		t.Errorf("detail query = %q, want markdown and subtasks", queries[0])
	}
	if queries[1] != "include_markdown_description=true" {
		t.Errorf("ordinary query = %q, want markdown only", queries[1])
	}
}
