package migration

import (
	"strings"
	"testing"
)

func TestCheckAccepted(t *testing.T) {
	const current, list = "11.0.99", "7.0.[0-26] 8.1.8 8.1.[10-14] 10.6.[108-109] 11.0.[73-76]"
	tests := map[string]bool{
		"7.0.0":    true,
		"7.0.26":   true,
		"8.1.8":    true,
		"8.1.12":   true,
		"10.6.109": true,
		"11.0.76":  true,
		"11.0.80":  true, // After the list's end, up to its own version
		"11.0.99":  true,
		"6.5.55":   false,
		"7.0.27":   false,
		"8.1.9":    false, // A gap
		"10.6.107": false,
		"11.0.100": false, // Newer
		"12.0.0":   false,
	}
	for version, want := range tests {
		t.Run(version, func(t *testing.T) {
			err := checkAccepted(current, list, version)
			if (err == nil) != want {
				t.Errorf("got %v, want accepted: %v", err, want)
			}
		})
	}
}

func TestParseProperties(t *testing.T) {
	current, list, err := parseProperties(strings.NewReader("version=11.0.99\nother=x\nmigratable.versions=7.0.[0-26]\n"))
	if err != nil || current != "11.0.99" || list != "7.0.[0-26]" {
		t.Errorf("got %q %q, %v", current, list, err)
	}
	if _, _, err := parseProperties(strings.NewReader("version=11.0.99\n")); err == nil {
		t.Error("a missing list must be an error: Network has changed")
	}
}
