package supervisor

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// coreAddr is where Network expects unifi-core.
const coreAddr = "127.0.0.1:11081"

const coreReadTimeout = 10 * time.Second

// ulpAddr is where Network expects ulp-go, UniFi OS's service for users.
const ulpAddr = "127.0.0.1:9080"

// coreStub stands in for unifi-core, which Network's unifi-os profile asks about itself. Anything
// it doesn't know answers 404 and is logged: it must never look like an approval, and after a
// Network update the log shows what Network newly expects.
type coreStub struct {
	// system is the reply of /api/system.
	system []byte
	// interfaces is the reply of /api/network/interfaces, when the host's address is known.
	interfaces []byte
	// log receives what the stub doesn't know.
	log io.Writer
}

func (s *coreStub) serve() error {
	server := &http.Server{
		Addr:              coreAddr,
		Handler:           s,
		ReadHeaderTimeout: coreReadTimeout,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	return server.ListenAndServe()
}

func (s *coreStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status, body := s.answer(r.Method, r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body) // Only fails when Network has gone, and it retries.
}

func (s *coreStub) answer(method, path string) (int, []byte) {
	switch path {
	case "/api/system":
		return http.StatusOK, s.system
	case "/api/network/interfaces":
		// Network keeps its own list on an error, while `{}` would empty that list.
		if s.interfaces == nil {
			return http.StatusNotFound, []byte("{}")
		}
		return http.StatusOK, s.interfaces
	case "/api/controllers", "/api/services", "/api/ups/consoles":
		// Network parses these as lists. There's no UPS to pair a console with.
		return http.StatusOK, []byte("[]")
	case "/mb/sub", "/mb/pub", "/api/info":
		return http.StatusOK, []byte("{}")
	case "/proxy/cloud/ztp/consoles/auto-adoptions":
		// Devices waiting to be adopted through Ubiquiti's cloud: there is none without a cloud
		// account. An error would log one at each start.
		if method == http.MethodGet {
			return http.StatusOK, []byte(`{"data":{"devices":[]}}`)
		}
	}
	_, _ = fmt.Fprintf(s.log, "core stub: unexpected %s %s\n", method, path)
	return http.StatusNotFound, []byte("{}")
}

// serveULP answers the one thing Network asks ulp-go without being told to: which applications it
// has. None is a true answer, and spares a stack trace in the log every minute, 9 MB a day.
func serveULP() error {
	server := &http.Server{
		Addr:              ulpAddr,
		Handler:           http.HandlerFunc(answerULP),
		ReadHeaderTimeout: coreReadTimeout,
		ReadTimeout:       coreReadTimeout,
		WriteTimeout:      coreReadTimeout,
	}
	return server.ListenAndServe()
}

func answerULP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/ucore/manifest" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"applications":[]}`)
}
