package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// resetAdminDone remembers which user the reset_admin setting was applied to, as the setting stays
// in place across restarts, while a reset must only happen once.
const resetAdminDone = UnifiDataDir + "/reset-admin-done"

// ApplyResetAdmin applies Home Assistant's reset_admin setting: the way out of a lost password
// there, as running unifi-users takes an SSH app without its protection. It tells whether it did.
func ApplyResetAdmin(ctx context.Context, out io.Writer) (applied bool, err error) {
	wanted := readHomeAssistantOptions().ResetAdmin
	done, err := os.ReadFile(resetAdminDone)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if !resetAdminNeeded(wanted, string(done)) {
		if wanted == "" {
			// So that setting the same user again later applies again.
			if err := os.Remove(resetAdminDone); err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
		return false, nil
	}

	users, err := LoadUsers(ctx)
	if err != nil {
		return false, err
	}
	user, err := FindUser(users, wanted)
	if err != nil {
		return false, fmt.Errorf("the reset_admin setting: %w", err)
	}
	password, err := temporaryPassword()
	if err != nil {
		return false, err
	}
	if err := SetPassword(ctx, user.ID, password); err != nil {
		return false, err
	}
	if err := GrantAdmin(ctx, user.ID); err != nil {
		return false, err
	}
	if err := os.WriteFile(resetAdminDone, []byte(wanted+"\n"), 0o600); err != nil {
		return false, err
	}

	record := newRecorder(out)
	defer record.close()
	record.say("as the reset_admin setting asked, gave a temporary password and admin rights to %q (%s)",
		user.Name, user.ID)
	// Only on out, not in the permanent record.
	_, err = fmt.Fprintf(out, "unifi-standalone: log in as %q with the temporary password %q, then change it\n",
		user.Name, password)
	return true, err
}

func resetAdminNeeded(wanted, done string) bool {
	return wanted != "" && wanted != strings.TrimSpace(done)
}
