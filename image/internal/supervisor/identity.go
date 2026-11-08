package supervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"
)

const (
	// coreVersionFile holds what the stand-in reports as its version: the one this Network asks for.
	// A higher one could switch on calls nothing has tested.
	coreVersionFile = "/opt/unifi-standalone/core-version"
	// coreSysID is the device type of UniFi OS Server. Network requires one, and this mode ignores it.
	coreSysID = 44545

	supervisorNetworkInfo = "http://supervisor/network/info"
)

// loadInstallID returns the uuid the stand-in reports. It's made once per install, and kept with
// its data.
func loadInstallID(file string) (string, error) {
	content, err := os.ReadFile(file)
	if err == nil && len(content) > 0 {
		id, _, _ := strings.Cut(string(content), "\n")
		return id, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	id, err := newInstallID()
	if err != nil {
		return "", err
	}
	temporary := file + ".tmp"
	if err := os.WriteFile(temporary, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, os.Rename(temporary, file)
}

// newInstallID makes a version 5 uuid, as Network requires: only that it reads as one matters.
func newInstallID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	text := hex.EncodeToString(random)
	return text[:8] + "-" + text[8:12] + "-5" + text[13:16] + "-" + text[16:20] + "-" + text[20:], nil
}

// systemJSON has what Network requires: it refuses to start otherwise.
func systemJSON(id, version string) []byte {
	return mustJSON(map[string]any{
		"anonymousDeviceId": id,
		"ucore_version":     version,
		"isSetup":           true,
		"hardware": map[string]any{
			"firmwareVersion": version,
			"shortname":       "UOSSERVER",
			"uuid":            id,
			"sysid":           coreSysID,
		},
	})
}

// interfacesJSON tells Network the address devices should use: the host's, which the container
// can't see by itself. UniFi OS Server does the same from its own container. Network requires all
// these fields, but only reads the address and the family.
func interfacesJSON(ip string) []byte {
	return mustJSON(map[string]any{
		"eth0": []map[string]any{{
			"address": ip, "netmask": "255.255.255.0", "family": "IPv4",
			"mac": "02:00:00:00:00:01", "internal": false, "cidr": ip + "/24",
		}},
	})
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err) // Only maps of plain values reach here.
	}
	return encoded
}

// hostIP returns the host's address, or nothing when it isn't known. It's read once per start.
func hostIP(ctx context.Context) (string, error) {
	if ip := os.Getenv("UNIFI_HOST_IP"); ip != "" {
		return validIPv4(ip)
	}
	token := os.Getenv("SUPERVISOR_TOKEN")
	if token == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, supervisorNetworkInfo, http.NoBody)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the answer from Home Assistant: status %d", response.StatusCode)
	}
	return parseNetworkInfo(response.Body)
}

// parseNetworkInfo finds the address of the primary interface in Home Assistant's /network/info.
func parseNetworkInfo(body io.Reader) (string, error) {
	var info struct {
		Data struct {
			Interfaces []struct {
				Primary bool `json:"primary"`
				IPv4    struct {
					Address []string `json:"address"`
				} `json:"ipv4"`
			} `json:"interfaces"`
		} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&info); err != nil {
		return "", fmt.Errorf("reading Home Assistant's answer: %w", err)
	}
	for _, network := range info.Data.Interfaces {
		if network.Primary && len(network.IPv4.Address) > 0 {
			ip, _, _ := strings.Cut(network.IPv4.Address[0], "/")
			return validIPv4(ip)
		}
	}
	return "", nil
}

// validIPv4 keeps what Network can use: it compares IPv4 addresses, and takes no host name.
func validIPv4(ip string) (string, error) {
	parsed, err := netip.ParseAddr(ip)
	if err != nil || !parsed.Is4() {
		return "", fmt.Errorf("%q is not an IPv4 address", ip)
	}
	return ip, nil
}
