package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateConfig points the user config directory at a temp dir and clears
// CLICKUP_TOKEN so auth tests never touch the real environment.
func isolateConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("CLICKUP_TOKEN", "")
	return filepath.Join(dir, "clickup-axi", "token")
}

func TestAuthLoginStoresValidatedToken(t *testing.T) {
	tokenPath := isolateConfig(t)
	f, c := newFakeClickUp(t)
	f.mux.HandleFunc("GET /api/v2/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "pk_fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{}`))
			return
		}
		w.Write([]byte(`{"user": {"id": 42, "username": "jan"}}`))
	})

	out, code := runCLIWithStdin(t, c, "pk_fresh\n", "auth", "login")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "auth: logged in as jan (id: 42)") {
		t.Errorf("output missing login confirmation\noutput:\n%s", out)
	}

	b, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("token file not written: %v", err)
	}
	if string(b) != "pk_fresh" {
		t.Errorf("stored token = %q, want %q (trimmed)", b, "pk_fresh")
	}
	info, _ := os.Stat(tokenPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestAuthLoginRejectsInvalidToken(t *testing.T) {
	tokenPath := isolateConfig(t)
	f, c := newFakeClickUp(t)
	f.mux.HandleFunc("GET /api/v2/user", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{}`))
	})

	out, code := runCLIWithStdin(t, c, "pk_bad", "auth", "login")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "rejected the token") {
		t.Errorf("output missing rejection message\noutput:\n%s", out)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Errorf("a rejected token must not be stored")
	}
}

func TestAuthLoginEmptyStdinIsUsageError(t *testing.T) {
	isolateConfig(t)
	_, c := newFakeClickUp(t)

	out, code := runCLIWithStdin(t, c, "  \n", "auth", "login")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "clickup-axi auth login < tokenfile") {
		t.Errorf("output missing by-reference stdin example\noutput:\n%s", out)
	}
	if strings.Contains(out, "echo -n pk_") || strings.Contains(out, "echo pk_") {
		t.Errorf("usage help must not model echoing a literal token\noutput:\n%s", out)
	}
}

func TestAuthLogoutIsIdempotent(t *testing.T) {
	tokenPath := isolateConfig(t)
	_, c := newFakeClickUp(t)

	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("pk_x"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, code := runCLI(t, c, "auth", "logout")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "auth: logged out") {
		t.Errorf("output missing logout confirmation\noutput:\n%s", out)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Errorf("token file still exists after logout")
	}

	out, code = runCLI(t, c, "auth", "logout")
	if code != 0 {
		t.Fatalf("second logout exit code = %d, want 0 (no-op)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "already logged out (no-op)") {
		t.Errorf("output missing no-op acknowledgement\noutput:\n%s", out)
	}
}
