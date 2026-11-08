package supervisor

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestWithProperty(t *testing.T) {
	tests := []struct {
		name, content, want string
		changed             bool
	}{
		{"an empty file", "", "a=b\n", true},
		{"already right", "x=y\na=b\n", "x=y\na=b\n", false},
		{"replaces only its key", "#c\na=old\nab=keep\n", "#c\nab=keep\na=b\n", true},
		{"no final newline", "x=y", "x=y\na=b\n", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, changed := withProperty(test.content, "a", "b")
			if got != test.want || changed != test.changed {
				t.Errorf("got %q %v, want %q %v", got, changed, test.want, test.changed)
			}
		})
	}
}

func TestMigrationHalted(t *testing.T) {
	dir := t.TempDir()
	for name, test := range map[string]struct {
		log  string
		want bool
	}{
		"halted":  {"line\nplease factory default the console and restore\n", true},
		"stopped": {"halting every boot until the operator intervenes\n", true},
		"healthy": {"Embedded db-migrator finished\n", false},
		"old news, far before the end": {
			"factory default the console\n" + strings.Repeat("later lines\n", 1000), false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(dir, "log")
			if err := os.WriteFile(file, []byte(test.log), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := migrationHalted(file); got != test.want {
				t.Errorf("got %v", got)
			}
		})
	}
	if migrationHalted(filepath.Join(dir, "missing")) {
		t.Error("a missing log is not a halt")
	}
}

func TestParseNetworkInfo(t *testing.T) {
	tests := []struct {
		name, body, want string
		fails            bool
	}{
		{
			name: "the primary interface",
			body: `{"data":{"interfaces":[
				{"primary":false,"ipv4":{"address":["192.0.2.99/24"]}},
				{"primary":true,"ipv4":{"address":["192.0.2.20/24"]}}]}}`,
			want: "192.0.2.20",
		},
		{name: "no address", body: `{"data":{"interfaces":[{"primary":true,"ipv4":{"address":[]}}]}}`},
		{name: "no primary", body: `{"data":{"interfaces":[{"primary":false}]}}`},
		{name: "not JSON", body: `<html>`, fails: true},
		{name: "IPv6", body: `{"data":{"interfaces":[{"primary":true,"ipv4":{"address":["::1/64"]}}]}}`, fails: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseNetworkInfo(strings.NewReader(test.body))
			if (err != nil) != test.fails || got != test.want {
				t.Errorf("got %q, %v", got, err)
			}
		})
	}
}

// Network refuses to start with anything but a version 5 uuid.
func TestNewInstallID(t *testing.T) {
	id, err := newInstallID()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(id) {
		t.Errorf("got %q", id)
	}
}

func TestInstallIDIsKept(t *testing.T) {
	file := filepath.Join(t.TempDir(), "id")
	first, err := loadInstallID(file)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadInstallID(file)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("%q became %q", first, second)
	}
}

func TestAppDoesNotGetTheToken(t *testing.T) {
	got := appEnvironment([]string{"PATH=/bin", "SUPERVISOR_TOKEN=secret"})
	if slices.Contains(got, "SUPERVISOR_TOKEN=secret") || !slices.Contains(got, "PATH=/bin") ||
		!slices.Contains(got, "MALLOC_ARENA_MAX=2") {
		t.Errorf("got %v", got)
	}
}
