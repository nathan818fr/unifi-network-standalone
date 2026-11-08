// Package supervisor keeps everything running: it is the container's main process.
//
// It prepares /data, runs Postgres and the stand-in for unifi-core, and restarts Network when it
// exits on its own, as the package's systemd unit does (a restore relies on it). Network starts as
// on UniFi OS Server, with core's connection off.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nathan818fr/unifi-network-standalone/image/internal/store"
)

const (
	appDir = "/opt/unifi"

	// Exit status with which the app says it must not be restarted.
	appNoRestartStatus = 2
	appRestartDelay    = 5 * time.Second
	// The app refuses to start until someone steps in: no need to retry often.
	appHaltedRestartDelay = time.Minute
)

// Run runs everything until asked to stop, and returns what ended it.
func Run() error {
	if os.Getuid() == 0 {
		if err := prepareAsRoot(); err != nil {
			return err
		}
		if err := DropRoot(); err != nil {
			return err
		}
	}
	if err := prepare(); err != nil {
		return err
	}
	// Signals first: a stop can come at any time, and Postgres must then shut down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	database, err := startPostgres(ctx)
	if err != nil {
		return err
	}
	err = runWithPostgres(ctx, database)
	stopPostgres(database)
	return err
}

func runWithPostgres(ctx context.Context, database *process) error {
	stub, err := prepareCoreStub(ctx)
	if err != nil {
		return err
	}
	coreFailed := make(chan error, 3)
	go func() { coreFailed <- stub.serve() }()
	grpc := &coreGRPC{log: os.Stdout}
	go func() { coreFailed <- grpc.serve() }()
	go func() { coreFailed <- serveULP() }()

	runErr := runApp(ctx, database, coreFailed)

	stopErr := store.StopMongod(os.Stderr)
	// A stopped app gets no backup_pre from Home Assistant: its backup holds this dump.
	if database.running() {
		if err := store.DumpSettings(context.WithoutCancel(ctx), os.Stderr); err != nil {
			log.Printf("error: could not dump the settings: %v", err)
		}
	}
	return errors.Join(runErr, stopErr)
}

// runApp starts the app again each time it exits on its own, until asked to stop.
func runApp(ctx context.Context, database *process, coreFailed <-chan error) error {
	for ctx.Err() == nil {
		status, err := runAppOnce(ctx, database, coreFailed)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		// The app starts it again if it still needs it.
		if err := store.StopMongod(os.Stderr); err != nil {
			log.Printf("error: %v", err)
		}
		if exitCode(status) == appNoRestartStatus {
			return errors.New("the app exited, asking not to be restarted")
		}
		log.Printf("unifi-standalone: the app exited (%v), restarting", status)
		select {
		case <-ctx.Done():
		case <-time.After(restartDelay()):
		}
	}
	return nil
}

// runAppOnce runs the app until it exits, or something it needs does. It returns how the app ended.
func runAppOnce(ctx context.Context, database *process, coreFailed <-chan error) (status, err error) {
	restoring, err := beforeAppStart(ctx)
	if err != nil {
		return nil, err
	}
	app, err := startApp()
	if err != nil {
		return nil, err
	}
	// Only for this run of the app.
	repairCtx, cancelRepair := context.WithCancel(ctx)
	defer cancelRepair()
	if restoring {
		go repairAfterRestore(repairCtx)
	}

	select {
	case <-app.done:
		return app.err, nil
	case <-ctx.Done():
		err = nil
	case <-database.done:
		err = fmt.Errorf("postgres exited: %w", database.err)
	case failure := <-coreFailed:
		err = fmt.Errorf("the core stand-in exited: %w", failure)
	}
	app.stop(syscall.SIGTERM)
	return nil, err
}

func restartDelay() time.Duration {
	migrationLog := store.UnifiLogsDir + "/migration.log"
	if !migrationHalted(migrationLog) {
		return appRestartDelay
	}
	log.Printf("error: the data migration failed, the app refuses to start: see %s and docs/moving.md",
		migrationLog)
	return appHaltedRestartDelay
}

func exitCode(err error) int {
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return exited.ExitCode()
	}
	return 0
}

// repairAfterRestore waits for a restore to finish: the app applies it without restarting.
func repairAfterRestore(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
		if _, err := os.Stat(store.RestorePending); err == nil || !appIsUp(ctx) {
			continue
		}
		if err := store.RepairUsers(ctx, os.Stdout); err != nil {
			log.Printf("error: could not check the users: %v", err)
		}
		return
	}
}

// prepareCoreStub makes what the stand-in answers.
func prepareCoreStub(ctx context.Context) (*coreStub, error) {
	id, err := loadInstallID(store.UnifiDataDir + "/core-identity")
	if err != nil {
		return nil, err
	}
	version, err := os.ReadFile(coreVersionFile)
	if err != nil {
		return nil, err
	}
	stub := &coreStub{system: systemJSON(id, strings.TrimSpace(string(version))), log: os.Stdout}
	ip, err := hostIP(ctx)
	switch {
	case err != nil:
		log.Printf("unifi-standalone: could not read the host address: %v", err)
	case ip == "":
		log.Printf("unifi-standalone: the host address is not known: set UNIFI_HOST_IP, or Inform Host Override")
	default:
		stub.interfaces = interfacesJSON(ip)
	}
	return stub, nil
}
