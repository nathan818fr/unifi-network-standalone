package supervisor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Plain HTTP/2 on both sides, as Network does.
func grpcServer(t *testing.T) (*httptest.Server, *http.Client, *bytes.Buffer) {
	t.Helper()
	log := &bytes.Buffer{}
	server := httptest.NewUnstartedServer(&coreGRPC{log: log})
	server.Config.Protocols = &http.Protocols{}
	server.Config.Protocols.SetUnencryptedHTTP2(true)
	server.Start()
	t.Cleanup(server.Close)
	protocols := &http.Protocols{}
	protocols.SetUnencryptedHTTP2(true)
	return server, &http.Client{Transport: &http.Transport{Protocols: protocols}}, log
}

func grpcCall(ctx context.Context, t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(grpcEmptyMessage))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", grpcContentType)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestGRPCDeviceTagsAreEmpty(t *testing.T) {
	server, client, _ := grpcServer(t)
	response := grpcCall(t.Context(), t, client, server.URL+"/unifi.core.console.v1.DeviceTagsAPI/GetDeviceTags")
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, grpcEmptyMessage) {
		t.Errorf("got %x", body)
	}
	if got := response.Trailer.Get("Grpc-Status"); got != strconv.Itoa(grpcOK) {
		t.Errorf("got status %q", got)
	}
}

// Network makes some calls with no deadline: leaving them waiting would block its threads.
func TestGRPCUnknownCallsAreRefusedAtOnce(t *testing.T) {
	server, client, log := grpcServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	response := grpcCall(ctx, t, client, server.URL+"/unifi.core.console.v1.CloudProxyAPI/FabricSystemLogs")
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	if got := response.Trailer.Get("Grpc-Status"); got != strconv.Itoa(grpcUnimplemented) {
		t.Errorf("got status %q", got)
	}
	if !strings.Contains(log.String(), "unexpected gRPC call") {
		t.Errorf("logged %q", log)
	}
}
