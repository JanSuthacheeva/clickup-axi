package clickup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A transport failure must be translated, never passed through: Go's
// raw error strings carry the full request URL and dial internals,
// which the AXI contract forbids on stdout.
func TestTransportErrorDoesNotLeakRawDetails(t *testing.T) {
	// 127.0.0.1:1 refuses connections immediately on every platform.
	c := New("http://127.0.0.1:1/api/v2", "pk_test", &http.Client{Timeout: 2 * time.Second})
	_, err := c.GetUser()
	if err == nil {
		t.Fatal("expected a transport error, got none")
	}
	for _, leak := range []string{"dial tcp", "127.0.0.1", "http://", "Get \""} {
		if strings.Contains(err.Message, leak) {
			t.Errorf("translated message leaks %q: %s", leak, err.Message)
		}
	}
	if !strings.Contains(err.Message, "could not reach the ClickUp API") {
		t.Errorf("message = %q, want it to state the API is unreachable", err.Message)
	}
	if !strings.Contains(err.Message, "retry") {
		t.Errorf("message = %q, want an actionable retry hint", err.Message)
	}
}

// A timeout is the one transport failure worth naming: the agent's
// right move (retry) differs from a config problem, and the message
// must still leak nothing.
func TestTransportTimeoutIsNamedWithoutLeaking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL+"/api/v2", "pk_test", &http.Client{Timeout: 20 * time.Millisecond})
	_, err := c.GetUser()
	if err == nil {
		t.Fatal("expected a timeout error, got none")
	}
	if !strings.Contains(err.Message, "did not respond in time") {
		t.Errorf("message = %q, want the timeout named", err.Message)
	}
	for _, leak := range []string{"context deadline", "Client.Timeout", "127.0.0.1", "http://", "Get \""} {
		if strings.Contains(err.Message, leak) {
			t.Errorf("translated message leaks %q: %s", leak, err.Message)
		}
	}
}
