package clickup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTokenPrefersEnvOverFile(t *testing.T) {
	// Point the user config directory at a temp dir and clear
	// CLICKUP_TOKEN so the test never touches the real environment.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("CLICKUP_TOKEN", "")
	tokenPath := filepath.Join(dir, "clickup-axi", "token")

	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("pk_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := resolveToken(); got != "pk_file" {
		t.Errorf("resolveToken() = %q, want stored token %q", got, "pk_file")
	}

	t.Setenv("CLICKUP_TOKEN", "pk_env")
	if got := resolveToken(); got != "pk_env" {
		t.Errorf("resolveToken() = %q, want env token %q", got, "pk_env")
	}
}
