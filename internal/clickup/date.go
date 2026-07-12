package clickup

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const dateCacheTTL = 24 * time.Hour

var (
	dateLocationMu sync.RWMutex
	dateLocation   = time.Local
)

// DateLocation returns the ClickUp user's IANA timezone. It is resolved
// once per client and cached on disk for 24 hours so ordinary commands
// do not add an API call. A missing/invalid timezone or failed request
// silently preserves the historical time.Local behavior.
func (c *Client) DateLocation() *time.Location {
	c.dateOnce.Do(func() {
		loc := time.Local
		if cached, ok := readDateLocationCache(c.dateCachePath); ok {
			loc = cached
		} else if user, err := c.GetUser(); err == nil {
			if fetched, loadErr := time.LoadLocation(user.Timezone); loadErr == nil && user.Timezone != "" {
				loc = fetched
				writeDateLocationCache(c.dateCachePath, user.Timezone)
			}
		}
		c.dateLocation = loc
		setDateLocation(loc)
	})
	return c.dateLocation
}

// WorkspaceDateLocation is the location used to render ClickUp's
// workspace-local date-only epochs. It defaults to time.Local until a
// client resolves the user's ClickUp timezone.
func WorkspaceDateLocation() *time.Location {
	dateLocationMu.RLock()
	defer dateLocationMu.RUnlock()
	return dateLocation
}

func setDateLocation(loc *time.Location) {
	if loc == nil {
		loc = time.Local
	}
	dateLocationMu.Lock()
	dateLocation = loc
	dateLocationMu.Unlock()
}

func readDateLocationCache(path string) (*time.Location, bool) {
	if path == "" {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		return nil, false
	}
	stamp, err := time.Parse(time.RFC3339, fields[0])
	if err != nil || time.Since(stamp) > dateCacheTTL {
		return nil, false
	}
	loc, err := time.LoadLocation(fields[1])
	return loc, err == nil
}

func writeDateLocationCache(path, name string) {
	if path == "" || name == "" || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	line := time.Now().UTC().Format(time.RFC3339) + " " + name + "\n"
	_ = os.WriteFile(path, []byte(line), 0o600)
}
