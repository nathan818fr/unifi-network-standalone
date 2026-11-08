package migration

import (
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
			if string(got) != "the backup" {
				t.Errorf("got %q", got)
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
