package clickup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDateLocationFetchesAndCachesTimezone(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"user":{"timezone":"Asia/Bangkok"}}`)
	}))
	t.Cleanup(srv.Close)

	cache := filepath.Join(t.TempDir(), "timezone")
	c := New(srv.URL, "pk_test", srv.Client())
	c.dateCachePath = cache
	if got := c.DateLocation().String(); got != "Asia/Bangkok" {
		t.Fatalf("DateLocation() = %q, want Asia/Bangkok", got)
	}
	if hits != 1 {
		t.Fatalf("GET /user hits = %d, want 1", hits)
	}

	// A new process/client reads the persisted cache without another API call.
	cached := New(srv.URL, "pk_test", srv.Client())
	cached.dateCachePath = cache
	if got := cached.DateLocation().String(); got != "Asia/Bangkok" {
		t.Fatalf("cached DateLocation() = %q, want Asia/Bangkok", got)
	}
	if hits != 1 {
		t.Fatalf("GET /user hits after cache read = %d, want 1", hits)
	}
}

func TestDateLocationRefetchesStaleAndCorruptCaches(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "stale", body: time.Now().Add(-25*time.Hour).UTC().Format(time.RFC3339) + " UTC\n"},
		{name: "corrupt", body: "not a timezone cache\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				fmt.Fprint(w, `{"user":{"timezone":"Asia/Bangkok"}}`)
			}))
			t.Cleanup(srv.Close)

			cache := filepath.Join(t.TempDir(), "timezone")
			if err := os.WriteFile(cache, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			c := New(srv.URL, "pk_test", srv.Client())
			c.dateCachePath = cache
			if got := c.DateLocation().String(); got != "Asia/Bangkok" {
				t.Fatalf("DateLocation() = %q, want Asia/Bangkok", got)
			}
			if hits != 1 {
				t.Fatalf("GET /user hits = %d, want 1", hits)
			}
		})
	}
}

func TestDateLocationFallsBackToLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "pk_test", srv.Client())
	if got := c.DateLocation(); got != time.Local {
		t.Fatalf("DateLocation() = %v, want time.Local %v", got, time.Local)
	}
}
