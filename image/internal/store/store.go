// Package store is what the image keeps, and who can reach it: Network's users, the settings' dump,
// and MongoDB for migrations. It knows nothing of the processes that keep them running.
package store

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Where things are, as Network expects them.
const (
	DataDir      = "/data"
	UnifiDataDir = DataDir + "/unifi/data"
	UnifiLogsDir = DataDir + "/unifi/logs"
	RunDir       = "/run/unifi"

	// RestorePending exists while Network has a restore to apply.
	RestorePending = UnifiDataDir + "/restore/restore-pending.json"
	// MigratorVersion is the version of the database's data. Without this file, Network takes the
	// database for a new install and wipes it.
	MigratorVersion = UnifiDataDir + "/db_migrator/version"

	// PostgresPort is where Network expects Postgres.
	PostgresPort = "5434"
	// NetworkDatabase is the one Network uses.
	NetworkDatabase = "unifi-network"
)

// Query runs SQL on a database through psql, and returns its output. Values fill the `:'name'`
// variables of the SQL, which psql only does for SQL read from its input: that keeps them from
// injecting SQL.
func Query(ctx context.Context, database, sql string, values map[string]string) (string, error) {
	args := []string{
		"-h", "127.0.0.1", "-p", PostgresPort, "-U", "postgres", "-d", database,
		"-At", "-v", "ON_ERROR_STOP=1",
	}
	for name, value := range values {
		args = append(args, "-v", name+"="+value)
	}
	command := exec.CommandContext(ctx, "psql", args...)
	command.Stdin = strings.NewReader(sql)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("psql: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
