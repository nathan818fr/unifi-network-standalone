// Command unifi-standalone runs UniFi Network without UniFi OS. See the project's README.
//
// It is one program with several names, as links in the image: run as unifi-users, it is that
// command.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/nathan818fr/unifi-network-standalone/image/internal/supervisor"
)

const usage = `Usage: unifi-standalone <command>

Commands:
  run          Run everything: the container's main process
  healthcheck  Check that Network answers, for the container's health check
`

// errUsage asks main to print a command's help, and to exit with the status of a wrong usage.
type errUsage string

func (e errUsage) Error() string { return string(e) }

func main() {
	log.SetFlags(0) // The container's log adds its own time.
	if err := run(context.Background(), filepath.Base(os.Args[0]), os.Args[1:]); err != nil {
		if help := errUsage(""); errors.As(err, &help) {
			fmt.Fprint(os.Stderr, string(help))
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, name string, args []string) error {
	switch name {
	case "unifi-users":
		return runUsers(ctx, args)
	case "unifi-export":
		return runExport(ctx, args)
	case "unifi-backup":
		return runBackup(ctx, args)
	case "sudo":
		return runSudo(ctx, args)
	}
	switch {
	case len(args) == 1 && args[0] == "run":
		return supervisor.Run()
	case len(args) == 1 && args[0] == "healthcheck":
		return supervisor.Healthcheck(ctx)
	}
	return errUsage(usage)
}
