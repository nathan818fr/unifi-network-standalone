package migration

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"
)

const (
	requestTimeout = time.Minute
	backupTimeout  = 10 * time.Minute
	// UniFi OS serves Network under this path.
	networkPrefix = "/proxy/network"
)

// DownloadOptions says how to reach a controller, and who to log in as.
type DownloadOptions struct {
	URL      string
	User     string
	Password string
	// Insecure doesn't verify the certificate.
	Insecure bool
	// CACert is a file of certificates to trust, if not empty.
	CACert string
	// AskCode is called when the login needs a 2FA code.
	AskCode func() (string, error)
	// Log receives what is going on.
	Log func(message string)
}

// Download asks a running controller, UniFi OS or standalone, for a new full backup. From UniFi OS,
// its Inform Host Override is turned off, as when converting a console backup.
func Download(ctx context.Context, options DownloadOptions) (Converted, error) {
	client, err := newClient(options)
	if err != nil {
		return Converted{}, err
	}
	session := &session{client: client, base: strings.TrimRight(options.URL, "/")}

	loginPath, kind := "/api/login", "a standalone controller"
	isOS, err := session.isUniFiOS(ctx)
	if err != nil {
		return Converted{}, err
	}
	if isOS {
		session.prefix = networkPrefix
		loginPath, kind = "/api/auth/login", "UniFi OS"
	}
	options.Log(fmt.Sprintf("this is %s, logging in as %s", kind, options.User))
	if err := session.logIn(ctx, loginPath, options); err != nil {
		return Converted{}, err
	}

	options.Log("asking Network for a full backup (it can take a minute)")
	backupPath, err := session.requestBackup(ctx)
	if err != nil {
		return Converted{}, err
	}
	options.Log("downloading it")
	data, err := session.download(ctx, session.prefix+backupPath)
	if err != nil || !isOS {
		return Converted{Backup: data}, err
	}
	var converted Converted
	converted.Backup, err = turnOffInformOverrideInBackup(data, &converted)
	return converted, err
}

func newClient(options DownloadOptions) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	config := &tls.Config{InsecureSkipVerify: options.Insecure}
	if options.CACert != "" {
		pem, err := os.ReadFile(options.CACert)
		if err != nil {
			return nil, err
		}
		config.RootCAs = x509.NewCertPool()
		if !config.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s holds no certificate", options.CACert)
		}
	}
	return &http.Client{
		Jar:       jar,
		Transport: &http.Transport{TLSClientConfig: config},
	}, nil
}

type session struct {
	client *http.Client
	base   string
	// prefix is where Network's paths start.
	prefix string
	csrf   string
}

// isUniFiOS asks core, which answers this without a login. A standalone controller doesn't have it.
func (s *session) isUniFiOS(ctx context.Context) (bool, error) {
	status, _, body, err := s.do(ctx, http.MethodGet, "/api/system", nil, requestTimeout)
	if err != nil {
		return false, err
	}
	var system struct {
		Hardware json.RawMessage `json:"hardware"`
	}
	return status == http.StatusOK && json.Unmarshal(body, &system) == nil && system.Hardware != nil, nil
}

func (s *session) logIn(ctx context.Context, path string, options DownloadOptions) error {
	status, header, body, err := s.sendLogin(ctx, path, options, "")
	if err != nil {
		return err
	}
	if needsCode(status, body) {
		code, err := options.AskCode()
		if err != nil {
			return err
		}
		status, header, body, err = s.sendLogin(ctx, path, options, code)
		if err != nil {
			return err
		}
	}
	if status != http.StatusOK {
		return fmt.Errorf("the login answered %d %s. Is this a local admin? An account linked to a UI account may have no password",
			status, reason(body))
	}
	s.csrf = firstNonEmpty(header.Get("X-Updated-Csrf-Token"), header.Get("X-Csrf-Token"))
	return nil
}

func (s *session) sendLogin(ctx context.Context, path string, options DownloadOptions, code string) (int, http.Header, []byte, error) {
	request := map[string]any{
		"username": options.User, "password": options.Password,
		"remember": false, "rememberMe": false,
	}
	if code != "" {
		// UniFi OS and standalone controllers each read their own name for it.
		request["token"], request["ubic_2fa_token"] = code, code
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return 0, nil, nil, err
	}
	return s.do(ctx, http.MethodPost, path, encoded, requestTimeout)
}

// needsCode tells a request for a 2FA code from a refused login.
func needsCode(status int, body []byte) bool {
	if status == 499 {
		return true
	}
	if status != http.StatusBadRequest && status != http.StatusUnauthorized {
		return false
	}
	text := strings.ToLower(reason(body))
	return strings.Contains(text, "2fa") || strings.Contains(text, "mfa")
}

func (s *session) requestBackup(ctx context.Context) (string, error) {
	path := s.prefix + "/api/s/default/cmd/backup"
	status, _, body, err := s.do(ctx, http.MethodPost, path, []byte(`{"cmd":"backup","days":0}`), backupTimeout)
	if err != nil {
		return "", err
	}
	var reply struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &reply); err != nil || status != http.StatusOK ||
		len(reply.Data) == 0 || reply.Data[0].URL == "" {
		return "", fmt.Errorf("no backup from Network: it answered %d %s", status, reason(body))
	}
	return reply.Data[0].URL, nil
}

func (s *session) download(ctx context.Context, path string) ([]byte, error) {
	status, _, body, err := s.do(ctx, http.MethodGet, path, nil, backupTimeout)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("the download answered %d", status)
	}
	return body, nil
}

// do sends a request. An answer of any status isn't an error: only having no answer is.
func (s *session) do(ctx context.Context, method, path string, body []byte, timeout time.Duration) (int, http.Header, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, s.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if s.csrf != "" {
		request.Header.Set("X-Csrf-Token", s.csrf)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return 0, nil, nil, explain(err)
	}
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("reading the answer of %s: %w", path, err)
	}
	return response.StatusCode, response.Header, content, nil
}

func explain(err error) error {
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return fmt.Errorf("the controller's certificate is not trusted: use --cacert <file>, or --insecure if you accept that the password could be intercepted (%w)", err)
	}
	return fmt.Errorf("could not reach the controller: %w", err)
}

// reason finds the error's name in a reply, as UniFi OS and standalone controllers each put it.
func reason(body []byte) string {
	var reply struct {
		Meta struct {
			Message string `json:"msg"`
		} `json:"meta"`
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if json.Unmarshal(body, &reply) != nil {
		return ""
	}
	return firstNonEmpty(reply.Meta.Message, reply.Message, reply.Code)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
