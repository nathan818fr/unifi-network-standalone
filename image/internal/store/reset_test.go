package store

import "testing"

// A reset must happen once: the setting stays in place across restarts.
func TestResetAdminNeeded(t *testing.T) {
	tests := []struct {
		wanted, done string
		want         bool
	}{
		{"", "", false},
		{"", "alex\n", false},
		{"alex", "", true},
		{"alex", "alex\n", false},
		{"other", "alex\n", true},
	}
	for _, test := range tests {
		if got := resetAdminNeeded(test.wanted, test.done); got != test.want {
			t.Errorf("resetAdminNeeded(%q, %q) = %v", test.wanted, test.done, got)
		}
	}
}
