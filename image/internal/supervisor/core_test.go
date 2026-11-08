package supervisor

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newStub(interfaces string) (*coreStub, *bytes.Buffer) {
	log := &bytes.Buffer{}
	stub := &coreStub{system: []byte(`{"system":true}`), log: log}
	if interfaces != "" {
		stub.interfaces = []byte(interfaces)
	}
	return stub, log
}

func request(stub *coreStub, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	stub.ServeHTTP(recorder, httptest.NewRequest(method, path, http.NoBody))
	return recorder
}

func TestAnswers(t *testing.T) {
	tests := []struct {
		method, path string
		status       int
		body         string
	}{
		{http.MethodGet, "/api/system", http.StatusOK, `{"system":true}`},
		{http.MethodGet, "/api/controllers", http.StatusOK, `[]`},
		{http.MethodGet, "/api/services?x=1", http.StatusOK, `[]`},
		{http.MethodPost, "/mb/sub", http.StatusOK, `{}`},
		{http.MethodPost, "/mb/pub", http.StatusOK, `{}`},
		{http.MethodGet, "/api/info", http.StatusOK, `{}`},
		{http.MethodGet, "/proxy/cloud/ztp/consoles/auto-adoptions", http.StatusOK,
			`{"data":{"devices":[]}}`},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			stub, log := newStub("")
			got := request(stub, test.method, test.path)
			if got.Code != test.status || got.Body.String() != test.body {
				t.Errorf("got %d %q, want %d %q", got.Code, got.Body, test.status, test.body)
			}
			if log.Len() != 0 {
				t.Errorf("logged %q", log)
			}
		})
	}
}

// Identity checks ask core about a user or an API key: the stub must never say yes by default.
func TestUnknownPathsAreRefusedAndLogged(t *testing.T) {
	for _, path := range []string{"/api/users/1", "/api/apiKey/1", "/made-up"} {
		t.Run(path, func(t *testing.T) {
			stub, log := newStub("")
			got := request(stub, http.MethodGet, path)
			if got.Code != http.StatusNotFound {
				t.Errorf("got %d, want 404", got.Code)
			}
			if !strings.Contains(log.String(), "core stub: unexpected GET "+path) {
				t.Errorf("logged %q", log)
			}
		})
	}
}

// Only the list is known: writing to the cloud's list of auto-adoptions must not look like success.
func TestAutoAdoptionsOnlyAnswersReads(t *testing.T) {
	stub, log := newStub("")
	got := request(stub, http.MethodPut, "/proxy/cloud/ztp/consoles/auto-adoptions")
	if got.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", got.Code)
	}
	if !strings.Contains(log.String(), "core stub: unexpected PUT") {
		t.Errorf("logged %q", log)
	}
}

func TestInterfaces(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		stub, _ := newStub(`{"eth0":[]}`)
		got := request(stub, http.MethodGet, "/api/network/interfaces")
		if got.Code != http.StatusOK || got.Body.String() != `{"eth0":[]}` {
			t.Errorf("got %d %q", got.Code, got.Body)
		}
	})
	// `{}` here would empty Network's own list of interfaces.
	t.Run("unknown is an error, never a success", func(t *testing.T) {
		stub, log := newStub("")
		got := request(stub, http.MethodGet, "/api/network/interfaces")
		if got.Code != http.StatusNotFound {
			t.Errorf("got %d, want 404", got.Code)
		}
		if log.Len() != 0 {
			t.Errorf("logged %q", log)
		}
	})
}

func TestULPRefusesOtherPaths(t *testing.T) {
	recorder := httptest.NewRecorder()
	answerULP(recorder, httptest.NewRequest(http.MethodGet, "/api/users", http.NoBody))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", recorder.Code)
	}
}
