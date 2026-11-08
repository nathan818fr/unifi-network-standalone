package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"
)

// User is one row of config.admin: admins or not, this is who can log in. (In Network's API, "user"
// means a Wi-Fi client instead.) Rights come from config.privilege, one row per site:
// is_super_admin grants nothing by itself, and without those rows the web interface shows a black
// page after login.
type User struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	HasPassword bool   `json:"has_password"`
	Owner       bool   `json:"owner"`
	AdminSites  int    `json:"admin_sites"`
	LastLogin   *int64 `json:"last_login"`
}

func (u User) IsAdmin() bool {
	return u.AdminSites > 0
}

const loadUsersSQL = `
SELECT coalesce(json_agg(row_to_json(users) ORDER BY users.name, users.id), '[]')
FROM (
  SELECT a.id, a.name, coalesce(a.email, '') AS email,
    coalesce(a.shadow, '') <> '' AS has_password,
    coalesce(a.is_owner, false) AS owner,
    (SELECT count(*) FROM config.privilege p WHERE p.admin_id = a.id AND p.role = 'admin') AS admin_sites,
    a.last_login_timestamp AS last_login
  FROM config.admin a
) users`

func LoadUsers(ctx context.Context) ([]User, error) {
	output, err := Query(ctx, NetworkDatabase, loadUsersSQL, nil)
	if err != nil {
		return nil, err
	}
	var loaded []User
	if err := json.Unmarshal([]byte(output), &loaded); err != nil {
		return nil, fmt.Errorf("reading the users: %w", err)
	}
	return loaded, nil
}

// FindUser returns the one user with this name, email or id.
func FindUser(users []User, who string) (User, error) {
	var found []User
	for _, user := range users {
		if user.ID == who || strings.EqualFold(user.Name, who) ||
			(user.Email != "" && strings.EqualFold(user.Email, who)) {
			found = append(found, user)
		}
	}
	switch len(found) {
	case 0:
		return User{}, fmt.Errorf("no user %q: see unifi-users list", who)
	case 1:
		return found[0], nil
	default:
		return User{}, fmt.Errorf("several users match %q: use an id from unifi-users list", who)
	}
}

// GrantAdmin gives a user the admin role on every site.
func GrantAdmin(ctx context.Context, id string) error {
	_, err := Query(ctx, NetworkDatabase, `
BEGIN;
UPDATE config.privilege SET role = 'admin' WHERE admin_id = :'id'::uuid;
INSERT INTO config.privilege (admin_id, site_id, role)
  SELECT :'id'::uuid, s.id, 'admin' FROM config.site s
  WHERE NOT EXISTS (
    SELECT 1 FROM config.privilege p WHERE p.admin_id = :'id'::uuid AND p.site_id = s.id);
COMMIT;`, map[string]string{"id": id})
	return err
}

// SetPassword replaces a user's password.
func SetPassword(ctx context.Context, id, password string) error {
	hash, err := hashPassword(ctx, password)
	if err != nil {
		return err
	}
	_, err = Query(ctx, NetworkDatabase,
		`UPDATE config.admin SET shadow = :'hash' WHERE id = :'id'::uuid`,
		map[string]string{"id": id, "hash": hash})
	return err
}

// hashPassword makes the format of the users Network creates itself: SHA-512 crypt, which Go's
// standard library doesn't have.
func hashPassword(ctx context.Context, password string) (string, error) {
	command := exec.CommandContext(ctx, "openssl", "passwd", "-6", "-stdin")
	command.Stdin = strings.NewReader(password)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("hashing the password: %w", err)
	}
	hash := strings.TrimSpace(string(output))
	if !strings.HasPrefix(hash, "$6$") {
		return "", errors.New("hashing the password: unexpected format")
	}
	return hash, nil
}

// WriteUsers prints users for a person to choose from.
func WriteUsers(out io.Writer, users []User) error {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "ID\tNAME\tEMAIL\tPASSWORD\tOWNER\tADMIN SITES\tLAST LOGIN"); err != nil {
		return err
	}
	for _, user := range users {
		lastLogin := "never"
		if user.LastLogin != nil {
			lastLogin = time.Unix(*user.LastLogin, 0).UTC().Format("2006-01-02")
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", user.ID, user.Name, user.Email,
			yesNo(user.HasPassword), yesNo(user.Owner), user.AdminSites, lastLogin); err != nil {
			return err
		}
	}
	return table.Flush()
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
