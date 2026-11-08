package supervisor

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/nathan818fr/unifi-network-standalone/image/internal/store"
)

// So that an unofficial setup adds nothing to Ubiquiti's data, whatever the settings: a restore can
// turn analytics on until the next start, and Network sends stored crash logs regardless.
const telemetryBlock = "0.0.0.0 trace.svc.ui.com\n0.0.0.0 crash-report-service.svc.ui.com\n"

// prepareAsRoot does what needs root. /data may be a fresh volume, or files put back by root, and
// /run is not persisted.
func prepareAsRoot() error {
	uid, gid, err := imageUserIDs()
	if err != nil {
		return err
	}
	// Private: it holds passwords and keys.
	if err := os.MkdirAll(store.DataDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(store.RunDir, 0o700); err != nil {
		return err
	}
	if err := giveTree(store.DataDir, uid, gid); err != nil {
		return err
	}
	if err := os.Chown(store.RunDir, uid, gid); err != nil {
		return err
	}
	if err := appendTelemetryBlock(); err != nil {
		log.Printf("error: could not block the telemetry hosts: %v", err)
	}
	return nil
}

// giveTree stays on one filesystem, and skips what's right already: on a large tree, most is.
func giveTree(root string, uid, gid int) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	rootDevice := rootInfo.Sys().(*syscall.Stat_t).Dev
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		if stat.Dev != rootDevice {
			return fs.SkipDir
		}
		if int(stat.Uid) == uid {
			return nil
		}
		if err := os.Lchown(path, uid, gid); err != nil {
			return fmt.Errorf("giving %s to the image's user: %w", path, err)
		}
		return nil
	})
}

func appendTelemetryBlock() error {
	file, err := os.OpenFile("/etc/hosts", os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = file.WriteString(telemetryBlock)
	return errors.Join(err, file.Close())
}

// prepare makes the folders Network expects.
func prepare() error {
	for _, dir := range []string{store.UnifiDataDir, store.UnifiLogsDir, store.RunDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := os.Remove(store.RunDir + "/mongod.pid"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// imageUser owns /data.
const imageUser = "unifi"

func imageUserIDs() (uid, gid int, err error) {
	found, err := user.Lookup(imageUser)
	if err != nil {
		return 0, 0, err
	}
	if uid, err = strconv.Atoi(found.Uid); err != nil {
		return 0, 0, err
	}
	if gid, err = strconv.Atoi(found.Gid); err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

// DropRoot makes this process the image's user, when it runs as root.
func DropRoot() error {
	if os.Getuid() != 0 {
		return nil
	}
	uid, gid, err := imageUserIDs()
	if err != nil {
		return err
	}
	if err := syscall.Setgroups(nil); err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	return syscall.Setuid(uid)
}
