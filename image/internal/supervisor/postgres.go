package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/nathan818fr/unifi-network-standalone/image/internal/store"
)

const (
	pgData = store.DataDir + "/postgres"
	// Exists while a new cluster is being set up: left behind, the next start begins again.
	pgIncomplete = store.DataDir + "/postgres.incomplete"
)

// Few resources, few disk writes. Losing the last seconds of commits in a power cut is accepted
// (synchronous_commit=off), the database itself stays consistent.
var pgSettings = []string{
	"port=" + store.PostgresPort,
	"listen_addresses=localhost",
	"unix_socket_directories=",
	"shared_buffers=32MB",
	"work_mem=4MB",
	"maintenance_work_mem=16MB",
	"synchronous_commit=off",
	"checkpoint_timeout=15min",
	"max_wal_size=128MB",
	"min_wal_size=32MB",
	"wal_compression=on",
	"wal_level=minimal",
	"max_wal_senders=0",
	"jit=off",
	"io_method=sync",
	"log_min_messages=warning",
}

func startPostgres(ctx context.Context) (*process, error) {
	isNew, err := createClusterIfNeeded(ctx)
	if err != nil {
		return nil, err
	}
	args := []string{"-D", pgData}
	for _, setting := range pgSettings {
		args = append(args, "-c", setting)
	}
	// Not tied to ctx: Postgres stops last, after everything using it.
	database, err := startProcess("postgres", exec.Command("postgres", args...))
	if err != nil {
		return nil, err
	}
	if err := waitForPostgres(ctx, database); err != nil {
		stopPostgres(database)
		return nil, err
	}
	if isNew {
		if err := setUpNewCluster(ctx); err != nil {
			stopPostgres(database)
			// So that the next start tries again, rather than starting empty.
			return nil, errors.Join(err, os.RemoveAll(pgData))
		}
	}
	return database, nil
}

// SIGINT is Postgres's fast shutdown.
func stopPostgres(database *process) {
	database.stop(syscall.SIGINT)
}

func createClusterIfNeeded(ctx context.Context) (bool, error) {
	_, incompleteErr := os.Stat(pgIncomplete)
	version, versionErr := os.ReadFile(pgData + "/PG_VERSION")
	if incompleteErr != nil && versionErr == nil && len(version) > 0 {
		return false, nil
	}
	log.Print("unifi-standalone: creating the database cluster")
	if err := writeDurably(pgIncomplete); err != nil {
		return false, err
	}
	if err := os.RemoveAll(pgData); err != nil {
		return false, err
	}
	output, err := exec.CommandContext(ctx, "initdb", "-D", pgData, "-U", "postgres",
		"--auth=trust", "--encoding=UTF8", "--locale=C").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("initdb: %w: %s", err, output)
	}
	return true, nil
}

func writeDurably(name string) error {
	file, err := os.Create(name)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	return file.Close()
}

func waitForPostgres(ctx context.Context, database *process) error {
	for {
		if exec.CommandContext(ctx, "pg_isready", "-q", "-h", "127.0.0.1", "-p", store.PostgresPort).Run() == nil {
			return nil
		}
		select {
		case <-database.done:
			return fmt.Errorf("postgres did not start: %w", database.err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func setUpNewCluster(ctx context.Context) error {
	if _, err := store.Query(ctx, "postgres", "CREATE ROLE unifi_network_admin LOGIN CREATEDB;", nil); err != nil {
		return err
	}
	// A new cluster next to a dump: the database files were left out of a backup being restored.
	if info, err := os.Stat(store.SettingsDump); err == nil && info.Size() > 0 {
		log.Printf("unifi-standalone: loading the settings from %s", store.SettingsDump)
		output, err := exec.CommandContext(ctx, "pg_restore", "-h", "127.0.0.1", "-p", store.PostgresPort, "-U", "postgres",
			"-d", "postgres", "--create", "--exit-on-error", store.SettingsDump).CombinedOutput()
		if err != nil {
			return fmt.Errorf("could not load the settings: %w: %s", err, output)
		}
	}
	// synchronous_commit=off: make the load durable before trusting the cluster.
	if _, err := store.Query(ctx, "postgres", "CHECKPOINT;", nil); err != nil {
		return err
	}
	return os.Remove(pgIncomplete)
}
