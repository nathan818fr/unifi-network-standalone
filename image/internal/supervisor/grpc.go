package supervisor

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	// coreGRPCAddr is where Network expects unifi-core's gRPC.
	coreGRPCAddr = "127.0.0.1:11051"

	// See https://grpc.io/docs/guides/status-codes/.
	grpcOK            = 0
	grpcUnimplemented = 12
	grpcUnavailable   = 14

	grpcContentType = "application/grpc"
)

// grpcEmptyMessage is not compressed, and of length zero: for device tags, it means no tags.
var grpcEmptyMessage = make([]byte, 5)

// coreGRPC answers what Network asks of core's gRPC, on plain HTTP/2 from the standard library.
//
// Network runs without it, but then logs errors every 10 seconds and fails its device tag API. So
// only what stops that is here: an empty list of tags, and the console's status stream, held open
// with no news. Anything else is refused at once rather than left waiting, as Network makes some
// calls with no deadline, from threads doing other work.
type coreGRPC struct {
	// log receives what it doesn't know.
	log io.Writer
}

func (g *coreGRPC) serve() error {
	protocols := &http.Protocols{}
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{Addr: coreGRPCAddr, Handler: g, Protocols: protocols, ReadHeaderTimeout: coreReadTimeout}
	return server.ListenAndServe()
}

func (g *coreGRPC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), grpcContentType) {
		http.Error(w, "not gRPC", http.StatusUnsupportedMediaType)
		return
	}
	w.Header().Set("Content-Type", grpcContentType)
	switch r.URL.Path {
	case "/unifi.core.console.v1.DeviceTagsAPI/GetDeviceTags":
		setGRPCStatus(w, grpcOK)
		_, _ = w.Write(grpcEmptyMessage)
	case "/unifi.core.console.v1.ConsoleAPI/ConsoleStatus":
		setGRPCStatus(w, grpcUnavailable)
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
		<-r.Context().Done()
	default:
		_, _ = fmt.Fprintf(g.log, "core stub: unexpected gRPC call %s\n", r.URL.Path)
		setGRPCStatus(w, grpcUnimplemented)
		w.WriteHeader(http.StatusOK)
	}
}

// setGRPCStatus sends it as a trailer, after the reply.
func setGRPCStatus(w http.ResponseWriter, status int) {
	w.Header().Set(http.TrailerPrefix+"Grpc-Status", strconv.Itoa(status))
}
