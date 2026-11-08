package migration

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// NetworkJar is where the image holds Network.
const NetworkJar = "/opt/unifi/lib/ace.jar"

const productProperties = "BOOT-INF/classes/product.properties"

// CheckAccepted tells whether this Network would accept a backup of a version. It keeps a list of
// versions it can migrate from, with gaps that change at each release: only it can tell.
func CheckAccepted(jar, version string) error {
	current, list, err := readVersions(jar)
	if err != nil {
		return err
	}
	return checkAccepted(current, list, version)
}

// checkAccepted follows Network's rule: its own version, one on its list, or one between the end of
// its list and its own version.
func checkAccepted(current, list, version string) error {
	parsed, err := parseVersion(version)
	if err != nil {
		return err
	}
	running, err := parseVersion(current)
	if err != nil {
		return err
	}
	if compareVersions(parsed, running) > 0 {
		return fmt.Errorf("it is from Network %s, newer than this Network %s: update this image first", version, current)
	}
	if parsed == running {
		return nil
	}
	var last [3]int
	for entry := range strings.FieldsSeq(list) {
		from, to, err := parseEntry(entry)
		if err != nil {
			return err
		}
		if compareVersions(parsed, from) >= 0 && compareVersions(parsed, to) <= 0 {
			return nil
		}
		last = to
	}
	if compareVersions(parsed, last) > 0 {
		return nil
	}
	return fmt.Errorf("this Network %s doesn't accept backups of exactly Network %s: upgrade the old controller to another version first",
		current, version)
}

func readVersions(jar string) (current, list string, err error) {
	archive, err := zip.OpenReader(jar)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = archive.Close() }()
	file, err := archive.Open(productProperties)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = file.Close() }()
	return parseProperties(file)
}

func parseProperties(properties io.Reader) (current, list string, err error) {
	lines := bufio.NewScanner(properties)
	for lines.Scan() {
		key, value, _ := strings.Cut(lines.Text(), "=")
		switch key {
		case "version":
			current = value
		case "migratable.versions":
			list = value
		}
	}
	if err := lines.Err(); err != nil {
		return "", "", err
	}
	if current == "" || list == "" {
		return "", "", errors.New("no list of versions found in Network: this tool needs an update")
	}
	return current, list, nil
}

// parseEntry reads `8.1.8` or `8.1.[10-14]`.
func parseEntry(entry string) (from, to [3]int, err error) {
	prefix, last, found := strings.Cut(entry, "[")
	if !found {
		from, err = parseVersion(entry)
		return from, from, err
	}
	first, final, found := strings.Cut(strings.TrimSuffix(last, "]"), "-")
	if !found {
		return from, to, fmt.Errorf("unexpected entry in Network's list of versions: %q", entry)
	}
	if from, err = parseVersion(prefix + first); err != nil {
		return from, to, err
	}
	to, err = parseVersion(prefix + final)
	return from, to, err
}

func parseVersion(version string) ([3]int, error) {
	var parsed [3]int
	parts := strings.Split(version, ".")
	if len(parts) != len(parsed) {
		return parsed, fmt.Errorf("unreadable version: %q", version)
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return parsed, fmt.Errorf("unreadable version: %q", version)
		}
		parsed[index] = number
	}
	return parsed, nil
}

func compareVersions(a, b [3]int) int {
	for index := range a {
		if a[index] != b[index] {
			return a[index] - b[index]
		}
	}
	return 0
}
