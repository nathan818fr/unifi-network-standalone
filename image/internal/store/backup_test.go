package store

import "testing"

func TestHistoryWantedFromTheEnvironment(t *testing.T) {
	t.Setenv("UNIFI_BACKUP_HISTORY", "true")
	if !historyWanted() {
		t.Error("asked for, but not wanted")
	}
	t.Setenv("UNIFI_BACKUP_HISTORY", "false")
	if historyWanted() {
		t.Error("refused, but wanted")
	}
}
