package supervisor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nathan818fr/unifi-network-standalone/image/internal/store"
)

const statusURL = "https://127.0.0.1:8443/status"

// startApp starts Network. Its memory is mostly outside its heap: these options save 270 MB idle.
func startApp() (*process, error) {
	heap := os.Getenv("UNIFI_JAVA_HEAP")
	if heap == "" {
		heap = "384M"
	}
	command := exec.Command("java",
		"-Xmx"+heap,
		"-XX:+UseSerialGC",
		"-XX:TieredStopAtLevel=1",
		"-XX:ReservedCodeCacheSize=64M",
		"-XX:+ExitOnOutOfMemoryError",
		"-XX:+PerfDisableSharedMem",
		"--enable-native-access=ALL-UNNAMED",
		"-Dfile.encoding=UTF-8",
		"-Djava.awt.headless=true",
		"-Dlogging.config=/opt/unifi-standalone/logback.xml",
		"-Dunifi.core.enabled=false",
		"-Dunifi.rmq.enabled=false",
		"-Dunifi.mongodb.service.enabled=true",
		"-Dunifi.active.db.mode=postgresql",
		"-Dspring.profiles.active=unifi-os",
		"-Dunifi-os.server=true",
		"--add-opens=java.base/java.lang=ALL-UNNAMED",
		"--add-opens=java.base/java.time=ALL-UNNAMED",
		"--add-opens=java.base/sun.security.util=ALL-UNNAMED",
		"--add-opens=java.base/java.io=ALL-UNNAMED",
		"--add-opens=java.rmi/sun.rmi.transport=ALL-UNNAMED",
		"-jar", "lib/ace.jar", "start",
	)
	command.Dir = appDir
	command.Env = appEnvironment(os.Environ())
	return startProcess("the app", command)
}

// appEnvironment keeps Home Assistant's token away from the app, which has no use for it.
func appEnvironment(environment []string) []string {
	kept := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "SUPERVISOR_TOKEN=") {
			kept = append(kept, entry)
		}
	}
	return append(kept, "MALLOC_ARENA_MAX=2")
}

// beforeAppStart makes sure of what must hold before each start of the app, whatever a restore
// just changed. It tells whether a restore is about to run.
func beforeAppStart(ctx context.Context) (restoring bool, err error) {
	properties := store.UnifiDataDir + "/system.properties"
	if err := setProperty(properties, "analytics.enabled", "false"); err != nil {
		return false, err
	}
	hasAdmins, err := databaseHasAdmins(ctx)
	if err != nil {
		return false, err
	}
	if !hasAdmins {
		return false, nil
	}
	if err := keepMigratorVersion(); err != nil {
		return false, err
	}
	// With this file lost, is_default defaults to true: anyone gets in as the first admin.
	if err := setProperty(properties, "is_default", "false"); err != nil {
		return false, err
	}
	// Network's tracer stays on if either this or analytics.enabled says so.
	changed, err := store.Query(ctx, store.NetworkDatabase, `UPDATE config.setting
		SET dynamic_part = jsonb_set(dynamic_part, '{enable_analytics}', 'false')
		WHERE key = 'super_mgmt' AND dynamic_part ->> 'enable_analytics' = 'true'`, nil)
	switch {
	case err != nil:
		log.Printf("error: could not turn the analytics setting off: %v", err)
	case changed != "UPDATE 0":
		log.Print("unifi-standalone: turned Network's analytics setting off")
	}
	// With core turned off, Inform Host Override sends devices super_identity's hostname, while the
	// settings page writes override_inform_host_location: devices would get example-domain.ui.com,
	// or UniFi OS's container name after a restore. A change there applies at the next start.
	host, err := store.Query(ctx, store.NetworkDatabase, `WITH changed AS (
		UPDATE config.setting AS identity
		SET dynamic_part = jsonb_set(identity.dynamic_part, '{hostname}',
			mgmt.dynamic_part -> 'override_inform_host_location')
		FROM config.setting AS mgmt
		WHERE identity.key = 'super_identity' AND mgmt.key = 'super_mgmt'
			AND mgmt.dynamic_part ->> 'override_inform_host' = 'true'
			AND mgmt.dynamic_part ->> 'override_inform_host_location' <> ''
			AND identity.dynamic_part ->> 'hostname'
				IS DISTINCT FROM mgmt.dynamic_part ->> 'override_inform_host_location'
		RETURNING identity.dynamic_part ->> 'hostname' AS hostname)
		SELECT hostname FROM changed`, nil)
	switch {
	case err != nil:
		log.Printf("error: could not apply Inform Host Override: %v", err)
	case host != "":
		log.Printf("unifi-standalone: Inform Host Override now gives devices %s", host)
	}

	pending, err := os.Stat(store.RestorePending)
	if err != nil {
		if err := store.RepairUsers(ctx, os.Stdout); err != nil {
			log.Printf("error: could not check the users: %v", err)
		}
		if err := store.ApplyResetAdmin(ctx, os.Stdout); err != nil {
			log.Printf("error: %v", err)
		}
		return false, nil
	}
	// A restore of an old backup can't be undone once the app has started it.
	saved, err := os.Stat(store.PreRestoreDump)
	if err != nil || saved.ModTime().Before(pending.ModTime()) {
		if err := store.DumpBeforeRestore(ctx, os.Stderr); err != nil {
			log.Printf("error: no safety copy before the restore: %v", err)
		}
	}
	return true, nil
}

// databaseHasAdmins looks step by step: a new install has no database, then no such table.
func databaseHasAdmins(ctx context.Context) (bool, error) {
	found, err := store.Query(ctx, "postgres",
		"SELECT count(*) FROM pg_database WHERE datname = '"+store.NetworkDatabase+"'", nil)
	if err != nil || found != "1" {
		return false, err
	}
	found, err = store.Query(ctx, store.NetworkDatabase, "SELECT to_regclass('config.admin') IS NOT NULL", nil)
	if err != nil || found != "t" {
		return false, err
	}
	found, err = store.Query(ctx, store.NetworkDatabase, "SELECT count(*) > 0 FROM config.admin", nil)
	return found == "t", err
}

// keepMigratorVersion puts back the version saved with the dump: without this file, Network takes
// the database for a new install and wipes it. It would rather refuse to start.
func keepMigratorVersion() error {
	if info, err := os.Stat(store.MigratorVersion); err == nil && info.Size() > 0 {
		return nil
	}
	saved, err := os.ReadFile(store.SettingsDump + ".version")
	if err != nil || len(saved) == 0 {
		return fmt.Errorf("the database has admins, but %s is missing: the app would wipe it", store.MigratorVersion)
	}
	if err := os.MkdirAll(filepath.Dir(store.MigratorVersion), 0o700); err != nil {
		return err
	}
	return os.WriteFile(store.MigratorVersion, saved, 0o600)
}

// setProperty sets one key of the app's settings file, leaving the others alone: the app owns it.
func setProperty(file, key, value string) error {
	content, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	updated, changed := withProperty(string(content), key, value)
	if !changed {
		return nil
	}
	temporary := file + ".standalone"
	if err := os.WriteFile(temporary, []byte(updated), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, file)
}

func withProperty(content, key, value string) (string, bool) {
	line := key + "=" + value
	var kept []string
	for existing := range strings.Lines(content) {
		existing = strings.TrimRight(existing, "\n")
		if existing == line {
			return content, false
		}
		if !strings.HasPrefix(existing, key+"=") {
			kept = append(kept, existing)
		}
	}
	return strings.Join(append(kept, line), "\n") + "\n", true
}

// migrationHalted tells whether the migrator's last words are that it won't go on by itself.
func migrationHalted(logFile string) bool {
	file, err := os.Open(logFile)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	const tailBytes = 4096
	if info, err := file.Stat(); err == nil && info.Size() > tailBytes {
		if _, err := file.Seek(-tailBytes, io.SeekEnd); err != nil {
			return false
		}
	}
	end, err := io.ReadAll(file)
	if err != nil {
		return false
	}
	return strings.Contains(string(end), "factory default the console") ||
		strings.Contains(string(end), "until the operator intervenes")
}

// appIsUp asks the app. While starting, /status answers 200 with an HTML page.
func appIsUp(ctx context.Context) bool {
	client := http.Client{
		Timeout: 5 * time.Second,
		// It's called in a loop, so no connections left open. The certificate is its own, self-signed.
		Transport: &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, http.NoBody)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	return err == nil && strings.Contains(string(body), `"up":true`)
}

func Healthcheck(ctx context.Context) error {
	if !appIsUp(ctx) {
		return errors.New("the app is not up")
	}
	return nil
}
