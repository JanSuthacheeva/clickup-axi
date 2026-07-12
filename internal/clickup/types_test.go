package clickup

import (
	"testing"
	"time"
)

// TestInstantDateIgnoresHostTimezone pins the split between Date (a
// workspace-local rendering for date-only due dates) and InstantDate (a
// UTC rendering for true instants like comment timestamps): the same
// instant must render the same calendar date on every host, while the
// due-date proxy is allowed to follow the machine's zone.
func TestInstantDateIgnoresHostTimezone(t *testing.T) {
	savedLocal := time.Local
	savedDate := WorkspaceDateLocation()
	t.Cleanup(func() {
		time.Local = savedLocal
		setDateLocation(savedDate)
	})

	// 2026-07-06T22:00:00Z sits in the previous UTC day but the next
	// local day once the host is far enough east.
	const epoch = "1783375200000"

	time.Local = time.FixedZone("TEST+04", 4*60*60)
	setDateLocation(time.Local)
	if got := MsEpoch(epoch).InstantDate(); got != "2026-07-06" {
		t.Errorf("InstantDate() = %q, want 2026-07-06 (UTC-stable)", got)
	}
	if got := MsEpoch(epoch).Date(); got != "2026-07-07" {
		t.Errorf("Date() = %q, want 2026-07-07 (workspace-local)", got)
	}

	time.Local = time.UTC
	if got := MsEpoch(epoch).InstantDate(); got != "2026-07-06" {
		t.Errorf("InstantDate() under UTC = %q, want 2026-07-06", got)
	}
}

func TestDateUsesResolvedWorkspaceTimezone(t *testing.T) {
	saved := WorkspaceDateLocation()
	t.Cleanup(func() { setDateLocation(saved) })

	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	setDateLocation(loc)

	// 2026-07-06 21:00 UTC is 04:00 on July 7 in Bangkok: ClickUp's
	// date-only storage anchor must render as the workspace's July 7.
	if got := MsEpoch("1783371600000").Date(); got != "2026-07-07" {
		t.Errorf("Date() = %q, want 2026-07-07", got)
	}
}
