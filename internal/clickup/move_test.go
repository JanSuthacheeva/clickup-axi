package clickup

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMoveTaskHitsV3WithMappings pins the v3 wire contract: the path is
// derived from the v2 base by swapping the version suffix, an empty
// mapping set sends an empty object, and mappings serialize as the
// documented id pairs.
func TestMoveTaskHitsV3WithMappings(t *testing.T) {
	var bodies []string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("PUT /api/v3/workspaces/9018/tasks/abc123/home_list/905678",
		func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(raw))
			w.Write([]byte(`{"data": {"task_id": "abc123", "new_list_id": "905678"}}`))
		})
	c := New(srv.URL+"/api/v2", "pk_test", srv.Client())

	if err := c.MoveTask("9018", "abc123", "905678", nil); err != nil {
		t.Fatalf("MoveTask without mappings: %v", err)
	}
	if err := c.MoveTask("9018", "abc123", "905678", []StatusMapping{
		{SourceStatus: "st_src", DestinationStatus: "st_dst"},
	}); err != nil {
		t.Fatalf("MoveTask with mapping: %v", err)
	}

	if len(bodies) != 2 {
		t.Fatalf("v3 PUTs = %d, want 2", len(bodies))
	}
	if bodies[0] != "{}" {
		t.Errorf("body without mappings = %s, want {}", bodies[0])
	}
	want := `{"status_mappings":[{"source_status":"st_src","destination_status":"st_dst"}]}`
	if bodies[1] != want {
		t.Errorf("body with mapping = %s\nwant %s", bodies[1], want)
	}
}

// TestMoveTaskTranslatesV3Error pins the v3 error shape ({message},
// not v2's {err}) through the shared translator.
func TestMoveTaskTranslatesV3Error(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("PUT /api/v3/workspaces/9018/tasks/abc123/home_list/905678",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status": 400, "message": "Invalid status mappings"}`))
		})
	c := New(srv.URL+"/api/v2", "pk_test", srv.Client())

	err := c.MoveTask("9018", "abc123", "905678", nil)
	if err == nil {
		t.Fatal("MoveTask succeeded, want translated error")
	}
	if want := "ClickUp rejected the request: Invalid status mappings (HTTP 400)"; err.Message != want {
		t.Errorf("error = %q, want %q", err.Message, want)
	}
}
