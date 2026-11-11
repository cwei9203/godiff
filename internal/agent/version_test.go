package agent

import "testing"

// TestVersionLine checks the version read out of what an agent's --version
// printed, and the lines that say none.
func TestVersionLine(t *testing.T) {
	for _, c := range []struct{ out, want string }{
		{"1.0.42\n", "1.0.42"},
		{"claude 2.1.3\n", "2.1.3"},
		{"codex-cli v0.44.0\n", "0.44.0"},
		{"opencode 2.0.4-beta.2\n", "2.0.4-beta.2"},
		{"pi v1.2.3 (build 8)\n", ""},
		{"", ""},
		{"\n\n1.2.3", ""},
		{"a version longer than anyone would write in one word . . . . . . . .", ""},
	} {
		if got := versionLine(c.out); got != c.want {
			t.Errorf("versionLine(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}
