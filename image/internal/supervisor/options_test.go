package supervisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEmptyResetAdmin(t *testing.T) {
	file := filepath.Join(t.TempDir(), "options.json")
	if err := os.WriteFile(file, []byte(`{"backup_history":true,"reset_admin":"someone"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var sent map[string]map[string]any
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(`{"result":"ok","data":{}}`))
	}))
	defer server.Close()

	if err := emptyResetAdmin(context.Background(), server.URL, "the-token", file); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer the-token" {
		t.Errorf("sent %q", auth)
	}
	// All the settings: the others must keep their values.
	want := map[string]map[string]any{"options": {"backup_history": true, "reset_admin": ""}}
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("sent %v, want %v", sent, want)
	}
}

func TestEmptyResetAdminRefused(t *testing.T) {
	file := filepath.Join(t.TempDir(), "options.json")
	if err := os.WriteFile(file, []byte(`{"reset_admin":"someone"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	if err := emptyResetAdmin(context.Background(), server.URL, "the-token", file); err == nil {
		t.Error("a refusal was taken for success")
	}
}
