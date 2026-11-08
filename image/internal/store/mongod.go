package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Network only uses MongoDB to migrate data into Postgres. Its files are deleted once it's stopped,
// unless a restore is still pending: they are large, and never read again.
const (
	mongodBinary      = "/opt/mongodb/bin/mongod"
	mongodDataDir     = UnifiDataDir + "/db"
	mongodPidFile     = RunDir + "/mongod.pid"
	mongodLog         = UnifiLogsDir + "/mongod.log"
	mongodStopTimeout = time.Minute
)

// StartMongod starts MongoDB in the background, as Network expects from `systemctl start`.
func StartMongod(ctx context.Context, out io.Writer) error {
	if mongodIsRunning() {
		return nil
	}
	if err := os.MkdirAll(mongodDataDir, 0o700); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "unifi-standalone: starting MongoDB for a migration"); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, mongodBinary,
		"--dbpath", mongodDataDir,
		"--port", "27117",
		"--bind_ip", "127.0.0.1",
		"--nounixsocket",
		"--pidfilepath", mongodPidFile,
		"--logpath", mongodLog,
		// No journal: this data can always be migrated again, and it spares 300 MB of disk writes.
		"--nojournal",
		"--wiredTigerCacheSizeGB", "0.25",
		"--fork",
	)
	// No pipes for its output: the daemon would inherit them, and waiting for them would never end.
	if err := command.Run(); err != nil {
		return fmt.Errorf("mongod: %w: see %s", err, mongodLog)
	}
	return nil
}

// StopMongod stops MongoDB and deletes its files, unless a restore still needs them.
func StopMongod(out io.Writer) error {
	if err := stopMongodProcess(out); err != nil {
		return err
	}
	if _, err := os.Stat(RestorePending); err == nil {
		return nil
	}
	return os.RemoveAll(mongodDataDir)
}

// RestartMongod keeps its files.
func RestartMongod(ctx context.Context, out io.Writer) error {
	if err := stopMongodProcess(out); err != nil {
		return err
	}
	return StartMongod(ctx, out)
}

func stopMongodProcess(out io.Writer) error {
	pid, running := mongodPid()
	if !running {
		return removeMongodPidFile()
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(mongodStopTimeout)
	for processIsAlive(pid) {
		if time.Now().After(deadline) {
			return errors.New("MongoDB did not stop")
		}
		time.Sleep(time.Second)
	}
	if _, err := fmt.Fprintln(out, "unifi-standalone: MongoDB stopped"); err != nil {
		return err
	}
	return removeMongodPidFile()
}

func mongodIsRunning() bool {
	_, running := mongodPid()
	return running
}

func mongodPid() (int, bool) {
	content, err := os.ReadFile(mongodPidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || !processIsAlive(pid) {
		return 0, false
	}
	return pid, true
}

func processIsAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func removeMongodPidFile() error {
	if err := os.Remove(mongodPidFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
