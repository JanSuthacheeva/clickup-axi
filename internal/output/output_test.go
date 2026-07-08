package output

import "testing"

func TestToonCellEscaping(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a, b", `"a, b"`},
		{`say "hi"`, `"say \"hi\""`},
		{"line\nbreak", "line break"},
	}
	for _, tc := range cases {
		if got := ToonCell(tc.in); got != tc.want {
			t.Errorf("ToonCell(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
