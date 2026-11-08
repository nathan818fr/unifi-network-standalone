package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nathan818fr/unifi-network-standalone/image/internal/migration"
)

const exportUsage = `Gets a full Network backup that can be restored, and checks it.

Usage: unifi-export --from-console <file.unifi> [options]
       unifi-export <url> [options]
       unifi-export --check <file>

  --from-console <file.unifi>
                     convert a console backup: on UniFi OS, Control Plane > Backups > Download.
                     This works offline, and takes only Network's data out of the file.
  <url>              or ask a running controller, UniFi OS or standalone, such as
                     https://192.168.1.1 or https://192.168.1.1:8443
  --check <file>     only check a .unf file: is it complete, is it a full backup?

Options:
  -o, --output <f>   where to save it (default: network-backup-<version>.unf)
      --overwrite    replace the output file if it exists
  -u, --user <name>  with <url>: a local admin (asked if not given)
      --cacert <f>   with <url>: trust this certificate
      --insecure     with <url>: don't verify the certificate: the password could be intercepted

With <url>, the password is asked for, or read from UNIFI_PASSWORD, and a 2FA code is asked for
when needed. Treat backups as secrets: they hold your admins, device keys and Wi-Fi passwords, and
the keys that protect them are the same on every install. A console backup also holds the console's
own private keys and database.
`

type exportOptions struct {
	url, check, fromConsole, output, user, cacert string
	overwrite, insecure                           bool
}

func parseExportOptions(args []string) (exportOptions, error) {
	var options exportOptions
	flags := flag.NewFlagSet("unifi-export", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.check, "check", "", "")
	flags.StringVar(&options.fromConsole, "from-console", "", "")
	flags.StringVar(&options.output, "o", "", "")
	flags.StringVar(&options.output, "output", "", "")
	flags.StringVar(&options.user, "u", "", "")
	flags.StringVar(&options.user, "user", "", "")
	flags.StringVar(&options.cacert, "cacert", "", "")
	flags.BoolVar(&options.overwrite, "overwrite", false, "")
	flags.BoolVar(&options.insecure, "insecure", false, "")
	// The flag package stops at the first argument that isn't an option, and the url comes first.
	for {
		if err := flags.Parse(args); err != nil {
			return options, errUsage(exportUsage)
		}
		if flags.NArg() == 0 {
			break
		}
		if options.url != "" {
			return options, errUsage(exportUsage)
		}
		options.url = flags.Arg(0)
		args = flags.Args()[1:]
	}
	if options.url == "" && options.check == "" && options.fromConsole == "" {
		return options, errUsage(exportUsage)
	}
	return options, nil
}

func runExport(ctx context.Context, args []string) error {
	options, err := parseExportOptions(args)
	if err != nil {
		return err
	}
	switch {
	case options.check != "":
		_, err := checkFile(options.check)
		return err
	case options.fromConsole != "":
		data, err := readBackup(options.fromConsole)
		if err != nil {
			return err
		}
		converted, err := migration.FromConsole(data)
		if err != nil {
			return describe(options.fromConsole, err)
		}
		return saveBackup(converted, options)
	default:
		return exportFromController(ctx, options)
	}
}

func exportFromController(ctx context.Context, options exportOptions) error {
	user := options.user
	if user == "" {
		var err error
		if user, err = askLine(os.Stderr, "Local admin: "); err != nil {
			return err
		}
	}
	password := os.Getenv("UNIFI_PASSWORD")
	if password == "" {
		var err error
		if password, err = askHidden(os.Stderr, "Password: "); err != nil {
			return fmt.Errorf("%w: set UNIFI_PASSWORD", err)
		}
	}
	data, err := migration.Download(ctx, migration.DownloadOptions{
		URL: options.url, User: user, Password: password,
		Insecure: options.insecure, CACert: options.cacert,
		AskCode: func() (string, error) { return askLine(os.Stderr, "Code from your 2FA app: ") },
		Log:     logExport,
	})
	if err != nil {
		return err
	}
	return saveBackup(data, options)
}

// saveBackup checks a backup before it lands where it's asked to, so that a bad one never does.
func saveBackup(data []byte, options exportOptions) error {
	version, err := migration.CheckBackup(data)
	if err != nil {
		return describe("the new backup", err)
	}
	logExport("the backup is complete: a full backup of Network " + version)
	output := options.output
	if output == "" {
		output = "network-backup-" + version + ".unf"
	}
	if _, err := os.Stat(output); err == nil && !options.overwrite {
		return fmt.Errorf("%s exists: choose another name with --output, or use --overwrite", output)
	}
	// In the same folder, for the rename: a new file is only readable by its owner.
	temporary, err := os.CreateTemp(filepath.Dir(output), ".unifi-export-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if _, err := temporary.Write(data); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), output); err != nil {
		return err
	}
	logExport("saved to " + output + ". Treat it as a secret: it holds your admins and Wi-Fi passwords.")
	return checkAcceptedHere(output, version)
}

func checkFile(name string) (string, error) {
	data, err := readBackup(name)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(name, ".unifi") {
		return "", fmt.Errorf("%s is a console backup, which Network refuses. Convert it first: unifi-export --from-console %s",
			filepath.Base(name), filepath.Base(name))
	}
	version, err := migration.CheckBackup(data)
	if err != nil {
		return "", describe(name, err)
	}
	logExport("the backup is complete: a full backup of Network " + version)
	return version, checkAcceptedHere(name, version)
}

// checkAcceptedHere needs Network itself, so only says more inside the image.
func checkAcceptedHere(name, version string) error {
	if _, err := os.Stat(migration.NetworkJar); err != nil {
		logExport("run this inside the image to also know whether its Network accepts this version")
		return nil
	}
	if err := migration.CheckAccepted(migration.NetworkJar, version); err != nil {
		return describe(name, err)
	}
	logExport("this image's Network accepts this version")
	return nil
}

func readBackup(name string) ([]byte, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s is empty", name)
	}
	return data, nil
}

func describe(name string, err error) error {
	return fmt.Errorf("%s: %w", filepath.Base(name), err)
}

func logExport(message string) {
	fmt.Fprintln(os.Stderr, "unifi-export: "+message)
}
