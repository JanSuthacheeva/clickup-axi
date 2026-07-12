package output

import "testing"

func TestToonCellEscaping(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a, b", `"a, b"`},
		{`say "hi"`, `"say \"hi\""`},
		{"line\nbreak", "line break"},
		// Backslashes escape before quotes, so a value containing \" is
		// not misread as an escaped quote followed by a terminator.
		{`path\"x`, `"path\\\"x"`},
		{`c:\temp`, `c:\temp`}, // no quoting trigger: backslash stays literal
		// Strings a strict TOON parser would type-coerce stay strings.
		{"123", `"123"`},
		{"-4.5e3", `"-4.5e3"`},
		{"true", `"true"`},
		{"False", "False"}, // only exact scalar literals coerce
		{"null", `"null"`},
		{"2026-07-06", "2026-07-06"}, // not a valid number: no quoting
		{"86ey3tx8m", "86ey3tx8m"},
	}
	for _, tc := range cases {
		if got := ToonCell(tc.in); got != tc.want {
			t.Errorf("ToonCell(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
