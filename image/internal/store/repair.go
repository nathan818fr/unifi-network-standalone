package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// repairLog keeps what a repair did, for review. It never holds a password.
const repairLog = UnifiLogsDir + "/access-repair.log"

// repairKind is what a repair has to do.
type repairKind int

const (
	// Nobody to repair, or someone can already log in as an admin.
	repairNotNeeded repairKind = iota
	// Same person: this user has the password, a user of the same name or email the rights.
	repairGrantRights
	// A real admin with no password here.
	repairTemporaryPassword
	// Nothing tells which user is the real admin.
	repairUnclear
)

// planRepair decides what to do when a restore from UniFi OS left password and rights on different
// users, as core handled logins there. It gives a way in to a real admin, and only to one: a user
// who isn't an admin gains rights only when it's the same person as one.
func planRepair(users []User) (repairKind, User) {
	for _, user := range users {
		if user.HasPassword && user.IsAdmin() {
			return repairNotNeeded, User{}
		}
	}

	var samePerson []User
	for _, user := range users {
		if user.HasPassword && sameAsAnAdmin(user, users) {
			samePerson = append(samePerson, user)
		}
	}
	if len(samePerson) == 1 {
		return repairGrantRights, samePerson[0]
	}

	var admins, owners []User
	for _, user := range users {
		if !user.IsAdmin() {
			continue
		}
		admins = append(admins, user)
		if user.Owner {
			owners = append(owners, user)
		}
	}
	switch {
	case len(owners) == 1:
		return repairTemporaryPassword, owners[0]
	case len(admins) == 1:
		return repairTemporaryPassword, admins[0]
	case len(users) == 0:
		return repairNotNeeded, User{}
	default:
		return repairUnclear, User{}
	}
}

func sameAsAnAdmin(user User, users []User) bool {
	for _, other := range users {
		if other.ID == user.ID || !other.IsAdmin() {
			continue
		}
		sameEmail := user.Email != "" && strings.EqualFold(user.Email, other.Email)
		if strings.EqualFold(user.Name, other.Name) || sameEmail {
			return true
		}
	}
	return false
}

// RepairUsers applies planRepair, and says what it did: on out, and in repairLog apart from a
// password.
func RepairUsers(ctx context.Context, out io.Writer) error {
	users, err := LoadUsers(ctx)
	if err != nil {
		return err
	}
	kind, user := planRepair(users)
	if kind == repairNotNeeded {
		return nil
	}

	record := newRecorder(out)
	defer record.close()
	record.say("no user has both a password and admin rights, as after a restore from UniFi OS")
	record.table(users)
	switch kind {
	case repairGrantRights:
		if err := GrantAdmin(ctx, user.ID); err != nil {
			return err
		}
		record.say("gave admin rights to %q (%s): it shares its name or email with an admin, and has a password. Log in with that password.",
			user.Name, user.ID)
		record.say("no other user was changed")
	case repairTemporaryPassword:
		password, err := temporaryPassword()
		if err != nil {
			return err
		}
		if err := SetPassword(ctx, user.ID, password); err != nil {
			return err
		}
		record.say("gave a temporary password to %q (%s): it is a real admin, and had no password here. No other user was changed.",
			user.Name, user.ID)
		// Only on out, not in the permanent record.
		_, err = fmt.Fprintf(out, "unifi-standalone: log in as %q with the temporary password %q, then change it\n",
			user.Name, password)
		return err
	case repairUnclear:
		record.say("changed nothing: which user is the real admin is not clear. Choose with unifi-users list, set-password and grant-admin.")
	}
	return nil
}

// temporaryPassword is only hexadecimal digits: easy to copy from a log.
func temporaryPassword() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

// recorder writes to the container's log and, best effort, to repairLog.
type recorder struct {
	out  io.Writer
	file *os.File
}

func newRecorder(out io.Writer) *recorder {
	file, err := os.OpenFile(repairLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		_, _ = fmt.Fprintf(out, "unifi-standalone: could not keep a record in %s: %v\n", repairLog, err)
		return &recorder{out: out}
	}
	return &recorder{out: out, file: file}
}

func (r *recorder) close() {
	if r.file != nil {
		_ = r.file.Close()
	}
}

func (r *recorder) say(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(r.out, "unifi-standalone: %s\n", message)
	if r.file != nil {
		_, _ = fmt.Fprintf(r.file, "%s %s\n", time.Now().UTC().Format(time.RFC3339), message)
	}
}

func (r *recorder) table(users []User) {
	var text strings.Builder
	_ = WriteUsers(&text, users)
	r.say("the users before this repair:\n%s", strings.TrimRight(text.String(), "\n"))
}
