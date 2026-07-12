package clickup

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const dateCacheTTL = 24 * time.Hour

// dateFallbackMarker is the cache token for a profile whose timezone is
// unset or unparseable: a stable condition, so it is memoized for the
// TTL to avoid a GetUser refetch on every later invocation. A failed
// request is never cached, so a transient outage still retries.
const dateFallbackMarker = "-"

var (
	dateLocationMu sync.RWMutex
	dateLocation   = time.Local
)

// DateLocation returns the ClickUp user's IANA timezone. It is resolved
// once per client and cached on disk for 24 hours so ordinary commands
// do not add an API call. A missing/invalid timezone or failed request
// silently preserves the historical time.Local behavior.
func (c *Client) DateLocation() *time.Location {
	c.resolveDateLocation(func() (string, bool) {
		user, err := c.GetUser()
		if err != nil {
			return "", false
		}
		return user.Timezone, true
	})
	return c.dateLocation
}

// SeedDateLocation resolves the timezone from an already-fetched user's
// profile, so a caller that has just fetched the user (the session-start
// path) primes the cache without a second GetUser. It is a no-op once
// the location has been resolved.
func (c *Client) SeedDateLocation(timezone string) {
	c.resolveDateLocation(func() (string, bool) {
		return timezone, true
	})
}

// resolveDateLocation runs the once-per-client resolution: on-disk cache
// first, then fetchTimezone. fetchTimezone reports ok=false only when the
// timezone could not be determined (a failed request), which is left
// uncached; a fetched-but-empty/invalid zone caches the fallback marker.
func (c *Client) resolveDateLocation(fetchTimezone func() (string, bool)) {
	c.dateOnce.Do(func() {
		loc := time.Local
		if cached, ok := readDateLocationCache(c.dateCachePath); ok {
			loc = cached
		} else if tz, ok := fetchTimezone(); ok {
			if fetched, loadErr := time.LoadLocation(tz); loadErr == nil && tz != "" {
				loc = fetched
				writeDateLocationCache(c.dateCachePath, tz)
			} else {
				writeDateLocationCache(c.dateCachePath, dateFallbackMarker)
			}
		}
		c.dateLocation = loc
		setDateLocation(loc)
	})
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
	if fields[1] == dateFallbackMarker {
		return time.Local, true
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
