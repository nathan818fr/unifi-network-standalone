package migration

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// controller is a fake of what this package uses, on UniFi OS or standalone.
type controller struct {
	unifiOS    bool
	needsCode  bool
	sawCSRF    bool
	sawCode    string
	backupBody string
}

func (c *controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prefix, loginPath := "", "/api/login"
	if c.unifiOS {
		prefix, loginPath = networkPrefix, "/api/auth/login"
	}
	switch r.URL.Path {
	case "/api/system":
		if c.unifiOS {
			_, _ = io.WriteString(w, `{"hardware":{"shortname":"UOSSERVER"}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case loginPath:
		var login map[string]any
		_ = json.NewDecoder(r.Body).Decode(&login)
		code, _ := login["token"].(string)
		switch {
		case c.needsCode && code == "":
			w.WriteHeader(499)
		default:
			c.sawCode = code
			http.SetCookie(w, &http.Cookie{Name: "TOKEN", Value: "session", Path: "/"})
			w.Header().Set("X-Csrf-Token", "the-token")
		}
	case prefix + "/api/s/default/cmd/backup":
		if _, err := r.Cookie("TOKEN"); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		c.sawCSRF = r.Header.Get("X-Csrf-Token") == "the-token"
		_, _ = io.WriteString(w, `{"meta":{"rc":"ok"},"data":[{"url":"/dl/backup/x.unf"}]}`)
	case prefix + "/dl/backup/x.unf":
		_, _ = io.WriteString(w, c.backupBody)
	default:
		t := "unexpected " + r.URL.Path
		http.Error(w, t, http.StatusTeapot)
	}
}

func options(url string) DownloadOptions {
	return DownloadOptions{
		URL: url, User: "admin", Password: "right", Insecure: true,
		AskCode: func() (string, error) { return "123456", nil },
		Log:     func(string) {},
	}
}

func TestDownload(t *testing.T) {
	for name, unifiOS := range map[string]bool{"standalone": false, "UniFi OS": true} {
		t.Run(name, func(t *testing.T) {
			fake := &controller{unifiOS: unifiOS, backupBody: "the backup"}
			server := httptest.NewTLSServer(fake)
			defer server.Close()
			got, err := Download(context.Background(), options(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Backup) != "the backup" || got.InformOverrideOff {
				t.Errorf("got %q, %+v", got.Backup, got)
			}
			if !fake.sawCSRF {
				t.Error("the backup request had no CSRF token")
			}
		})
	}
}

func TestDownloadAsksForTheCode(t *testing.T) {
	fake := &controller{unifiOS: true, needsCode: true, backupBody: "x"}
	server := httptest.NewTLSServer(fake)
	defer server.Close()
	if _, err := Download(context.Background(), options(server.URL)); err != nil {
		t.Fatal(err)
	}
	if fake.sawCode != "123456" {
		t.Errorf("sent the code %q", fake.sawCode)
	}
}

// A self-signed certificate is the norm here, and must never be accepted silently.
func TestDownloadVerifiesTheCertificate(t *testing.T) {
	server := httptest.NewTLSServer(&controller{})
	defer server.Close()
	opts := options(server.URL)
	opts.Insecure = false
	_, err := Download(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Errorf("got %v", err)
	}
}

// backupWithDatabase makes a Network backup whose database dump is this one.
func backupWithDatabase(t *testing.T, database []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(database); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return zipOf(t, map[string]string{"version": "10.6.109\n", "db.gz": compressed.String()})
}

// Only UniFi OS keeps the override where Network here doesn't read it.
func TestDownloadTurnsOffUniFiOSInformOverride(t *testing.T) {
	database := bsonDocument(t, "key", "super_mgmt", "override_inform_host", true,
		"override_inform_host_location", "192.0.2.11")
	backup := backupWithDatabase(t, database)
	for _, unifiOS := range []bool{true, false} {
		t.Run(map[bool]string{true: "UniFi OS", false: "standalone"}[unifiOS], func(t *testing.T) {
			server := httptest.NewTLSServer(&controller{unifiOS: unifiOS, backupBody: string(backup)})
			defer server.Close()
			got, err := Download(context.Background(), options(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if !unifiOS {
				if got.InformOverrideOff || !bytes.Equal(got.Backup, backup) {
					t.Errorf("a standalone controller's backup changed: %+v", got)
				}
				return
			}
			if !got.InformOverrideOff || got.InformOverrideWas != "192.0.2.11" {
				t.Errorf("got %+v", got)
			}
			if !bytes.Equal(databaseOf(t, got.Backup),
				bytes.ReplaceAll(database, informOverrideOn, informOverrideOff)) {
				t.Error("the database is not the same with only the override turned off")
			}
		})
	}
}
