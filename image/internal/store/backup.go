package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	// SettingsDump holds Network's settings, which the image loads when it finds no database.
	SettingsDump = DataDir + "/backup/settings.dump"
	// PreRestoreDump is the safety copy from before a restore.
	PreRestoreDump       = DataDir + "/backup/settings.pre-restore.dump"
	dumpLock             = DataDir + "/backup/settings.lock"
	HomeAssistantOptions = DataDir + "/options.json"
)

// By default history is left out, only the structure of those tables is kept: that keeps backups
// small.
var historyTables = []string{"stats.*", "logs.*", "audit.*"}

// DumpSettings refreshes the settings' dump. It fails rather than replace a good dump with a bad
// one.
func DumpSettings(ctx context.Context, out io.Writer) error {
	// Mid-restore the database is incomplete.
	if _, err := os.Stat(RestorePending); err == nil {
		return errors.New("a restore is pending, the database is not complete")
	}
	return dump(ctx, SettingsDump, out)
}

// DumpBeforeRestore makes the safety copy: a restore of an old backup can't be undone otherwise.
func DumpBeforeRestore(ctx context.Context, out io.Writer) error {
	return dump(ctx, PreRestoreDump, out)
}

func dump(ctx context.Context, file string, out io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	unlock, err := lockDump()
	if err != nil {
		return err
	}
	defer unlock()

	temporary := file + ".tmp"
	defer func() { _ = os.Remove(temporary) }()
	args := []string{
		"-h", "127.0.0.1", "-p", PostgresPort, "-U", "postgres",
		"--format=custom", "--compress=9", "--file=" + temporary,
	}
	if !historyWanted() {
		for _, tables := range historyTables {
			args = append(args, "--exclude-table-data="+tables)
		}
	}
	if err := runCommand(ctx, "pg_dump", append(args, NetworkDatabase)...); err != nil {
		return err
	}
	// A dump that can't be read back must not replace a good one.
	if err := runCommand(ctx, "pg_restore", "--list", temporary); err != nil {
		return err
	}
	if err := syncFile(temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, file); err != nil {
		return err
	}
	if err := saveVersion(file); err != nil {
		return err
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "unifi-backup: settings dumped to %s (%d bytes)\n", file, info.Size())
	return err
}

// saveVersion keeps the version of the database's data with the dump: Network wipes a database it
// finds without one.
func saveVersion(file string) error {
	version, err := os.ReadFile(MigratorVersion)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	temporary := file + ".version.tmp"
	if err := os.WriteFile(temporary, version, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, file+".version")
}

func historyWanted() bool {
	if wanted := os.Getenv("UNIFI_BACKUP_HISTORY"); wanted != "" {
		return wanted == "true"
	}
	return readHomeAssistantOptions().BackupHistory
}

// homeAssistantSettings are the app's settings. Elsewhere, there are none.
type homeAssistantSettings struct {
	BackupHistory bool   `json:"backup_history"`
	ResetAdmin    string `json:"reset_admin"`
}

func readHomeAssistantOptions() homeAssistantSettings {
	var settings homeAssistantSettings
	content, err := os.ReadFile(HomeAssistantOptions)
	if err == nil {
		_ = json.Unmarshal(content, &settings) // Unreadable means the same as none.
	}
	return settings
}

func lockDump() (unlock func(), err error) {
	file, err := os.OpenFile(dumpLock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() { _ = file.Close() }, nil
}

func syncFile(name string) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Sync()
}

func runCommand(ctx context.Context, name string, args ...string) error {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}
