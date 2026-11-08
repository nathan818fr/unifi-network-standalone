package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nathan818fr/unifi-network-standalone/image/internal/store"
	"github.com/nathan818fr/unifi-network-standalone/image/internal/supervisor"
)

const usersUsage = `Manages Network's users.

Usage: unifi-users list
       unifi-users set-password <user>
       unifi-users grant-admin <user> [--yes]

  list                 every user: its password, rights and last login
  set-password <user>  asks for a new password, and changes nothing else
  grant-admin <user>   gives admin rights on every site, after asking to confirm

<user> is a name, an email, or an id from the list when several users share a name.
`

const minPasswordLength = 8

func runUsers(ctx context.Context, args []string) error {
	switch {
	case len(args) == 1 && args[0] == "list":
		users, err := store.LoadUsers(ctx)
		if err != nil {
			return err
		}
		return store.WriteUsers(os.Stdout, users)
	case len(args) == 2 && args[0] == "set-password":
		return setPassword(ctx, args[1])
	case len(args) == 2 && args[0] == "grant-admin":
		return grantAdmin(ctx, args[1], false)
	case len(args) == 3 && args[0] == "grant-admin" && args[2] == "--yes":
		return grantAdmin(ctx, args[1], true)
	}
	return errUsage(usersUsage)
}

func findUser(ctx context.Context, who string) (store.User, error) {
	users, err := store.LoadUsers(ctx)
	if err != nil {
		return store.User{}, err
	}
	return store.FindUser(users, who)
}

func setPassword(ctx context.Context, who string) error {
	user, err := findUser(ctx, who)
	if err != nil {
		return err
	}
	password, err := readPassword()
	if err != nil {
		return err
	}
	if len(password) < minPasswordLength {
		return fmt.Errorf("use at least %d characters", minPasswordLength)
	}
	if err := store.SetPassword(ctx, user.ID, password); err != nil {
		return err
	}
	fmt.Printf("password changed for %s\n", user.Name)
	return nil
}

// readPassword asks twice on a terminal. Otherwise it reads one line, for scripts.
func readPassword() (string, error) {
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("reading the password: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	first, err := askHidden(os.Stdout, "New password: ")
	if err != nil {
		return "", err
	}
	second, err := askHidden(os.Stdout, "Again: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("the passwords differ")
	}
	return first, nil
}

func grantAdmin(ctx context.Context, who string, confirmed bool) error {
	user, err := findUser(ctx, who)
	if err != nil {
		return err
	}
	if !confirmed {
		answer, err := askLine(os.Stdout, fmt.Sprintf(
			"%s has admin rights on %d site(s). Give it admin rights on every site? [y/N] ",
			user.Name, user.AdminSites))
		if err != nil {
			return fmt.Errorf("%w: use --yes", err)
		}
		if !strings.HasPrefix(strings.ToLower(answer), "y") {
			return errors.New("nothing changed")
		}
	}
	if err := store.GrantAdmin(ctx, user.ID); err != nil {
		return err
	}
	fmt.Printf("admin rights granted to %s\n", user.Name)
	return nil
}

func runBackup(ctx context.Context, args []string) error {
	// Home Assistant runs its backup_pre as root: the files must stay the image's user's.
	if err := supervisor.DropRoot(); err != nil {
		return err
	}
	if len(args) != 0 {
		return errUsage("Usage: unifi-backup\n")
	}
	return store.DumpSettings(ctx, os.Stdout)
}

// runSudo stands in for sudo and systemd: Network runs `sudo systemctl start|stop|restart
// unifi-mongodb` when it needs MongoDB. Anything else it asks for is refused.
func runSudo(ctx context.Context, args []string) error {
	if len(args) == 3 && filepath.Base(args[0]) == "systemctl" && args[2] == "unifi-mongodb" {
		switch args[1] {
		case "start":
			return store.StartMongod(ctx, os.Stderr)
		case "stop":
			return store.StopMongod(os.Stderr)
		case "restart":
			return store.RestartMongod(ctx, os.Stderr)
		}
	}
	return fmt.Errorf("sudo: not allowed here: %s", strings.Join(args, " "))
}
