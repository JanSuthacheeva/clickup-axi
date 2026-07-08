package clickup

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveToken prefers the CLICKUP_TOKEN environment variable and falls
// back to the token stored by `clickup-axi auth login`.
func resolveToken() string {
	if t := os.Getenv("CLICKUP_TOKEN"); t != "" {
		return t
	}
	path, err := TokenFilePath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func TokenFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clickup-axi", "token"), nil
}
