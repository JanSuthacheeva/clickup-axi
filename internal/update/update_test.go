package update

import "testing"

// The passive notice must fire for published versions - including
// release candidates, whose users otherwise never learn the final
// release exists - and stay silent for dev and pseudo-version builds.
func TestIsReleaseVersion(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"1.0.0", true},
		{"0.6.0", true},
		{"1.0.0-rc.1", true},
		{"1.0.0-rc.12", true},
		{"dev", false},
		{"1.0", false},
		{"1.0.0.1", false},
		{"1.0.0-rc.", false},
		{"1.0.0-rc.x", false},
		{"1.0.0-beta.1", false},
		{"0.0.0-20260712123456-abcdef123456", false}, // VCS pseudo-version
		{"1.0.0-", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isReleaseVersion(tc.version); got != tc.want {
			t.Errorf("isReleaseVersion(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

// The passive notice must fire only on a genuine upgrade. In
// particular an RC build (a GitHub prerelease) must not be nagged to
// "update" to the older stable release that /releases/latest resolves
// to, and a final release must supersede its own release candidates.
func TestNewer(t *testing.T) {
	cases := []struct {
		latest  string
		running string
		want    bool
	}{
		{"1.0.0", "0.6.0", true},
		{"0.6.0", "1.0.0-rc.1", false}, // the downgrade nag this guards
		{"1.0.0", "1.0.0-rc.1", true},  // final supersedes its RC
		{"1.0.0-rc.2", "1.0.0-rc.1", true},
		{"1.0.0-rc.1", "1.0.0-rc.2", false},
		{"1.0.0-rc.1", "1.0.0", false},
		{"1.0.0", "1.0.0", false},
		{"0.6.0", "0.6.0", false},
		{"1.2.0", "1.1.9", true},
		{"1.1.9", "1.2.0", false},
		{"", "1.0.0", false},        // unparseable latest
		{"1.0.0", "dev", false},     // unparseable running
		{"garbage", "1.0.0", false}, // unparseable latest
	}
	for _, tc := range cases {
		if got := newer(tc.latest, tc.running); got != tc.want {
			t.Errorf("newer(%q, %q) = %v, want %v", tc.latest, tc.running, got, tc.want)
		}
	}
}
