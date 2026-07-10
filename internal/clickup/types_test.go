package clickup

import (
	"testing"
	"time"
)

// TestInstantDateIgnoresHostTimezone pins the split between Date (a
// workspace-local proxy for date-only due dates) and InstantDate (a
// UTC rendering for true instants like comment timestamps): the same
// instant must render the same calendar date on every host, while the
// due-date proxy is allowed to follow the machine's zone.
func TestInstantDateIgnoresHostTimezone(t *testing.T) {
	saved := time.Local
	t.Cleanup(func() { time.Local = saved })

	// 2026-07-06T22:00:00Z sits in the previous UTC day but the next
	// local day once the host is far enough east.
	const epoch = "1783375200000"

	time.Local = time.FixedZone("TEST+04", 4*60*60)
	if got := MsEpoch(epoch).InstantDate(); got != "2026-07-06" {
		t.Errorf("InstantDate() = %q, want 2026-07-06 (UTC-stable)", got)
	}
	if got := MsEpoch(epoch).Date(); got != "2026-07-07" {
		t.Errorf("Date() = %q, want 2026-07-07 (host-local proxy)", got)
	}

	time.Local = time.UTC
	if got := MsEpoch(epoch).InstantDate(); got != "2026-07-06" {
		t.Errorf("InstantDate() under UTC = %q, want 2026-07-06", got)
	}
}
