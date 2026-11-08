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
// there, as running unifi-users takes an SSH app without its protection.
func ApplyResetAdmin(ctx context.Context, out io.Writer) error {
	wanted := readHomeAssistantOptions().ResetAdmin
	done, err := os.ReadFile(resetAdminDone)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !resetAdminNeeded(wanted, string(done)) {
		if wanted == "" {
			// So that setting the same user again later applies again.
			if err := os.Remove(resetAdminDone); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return nil
	}

	users, err := LoadUsers(ctx)
	if err != nil {
		return err
	}
	user, err := FindUser(users, wanted)
	if err != nil {
		return fmt.Errorf("the reset_admin setting: %w", err)
	}
	password, err := temporaryPassword()
	if err != nil {
		return err
	}
	if err := SetPassword(ctx, user.ID, password); err != nil {
		return err
	}
	if err := GrantAdmin(ctx, user.ID); err != nil {
		return err
	}
	if err := os.WriteFile(resetAdminDone, []byte(wanted+"\n"), 0o600); err != nil {
		return err
	}

	record := newRecorder(out)
	defer record.close()
	record.say("as the reset_admin setting asked, gave a temporary password and admin rights to %q (%s). Empty the setting once logged in.",
		user.Name, user.ID)
	// Only on out, not in the permanent record.
	_, err = fmt.Fprintf(out, "unifi-standalone: log in as %q with the temporary password %q, then change it\n",
		user.Name, password)
	return err
}

func resetAdminNeeded(wanted, done string) bool {
	return wanted != "" && wanted != strings.TrimSpace(done)
}
