// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/adapter"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/ca"
)

const fixtureProviderOutputTokens = 3

const (
	// tlsFixtureChatPath is the OpenAI chat-completions endpoint the TLS fake
	// answers with a deterministic JSON body so an adapter can normalize it.
	tlsFixtureChatPath = "/v1/chat/completions"
	// tlsFixtureStreamPath is the endpoint the TLS fake answers with a
	// Server-Sent Events stream for the streaming interception assertion.
	tlsFixtureStreamPath = "/v1/chat/completions/stream"
	// tlsFixtureChatBody is the canned chat-completions response body. Tests
	// assert this exact string never appears in any recorded ledger event.
	tlsFixtureChatBody = `{"model":"fixture-model",` +
		`"choices":[{"message":{"role":"assistant","content":"fixture-secret-body"}}],` +
		`"usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12}}`
	// tlsFixtureStreamBody is the canned SSE body the stream endpoint emits.
	tlsFixtureStreamBody = "data: {\"delta\":\"fixture-stream-token\"}\n\n" +
		"data: [DONE]\n\n"
)

type ProxyProviderServer struct {
	server *httptest.Server
}

type ProxyPassThroughServer struct {
	server *httptest.Server
}

func NewProxyProviderServer(t *testing.T) *ProxyProviderServer {
	t.Helper()

	provider := &ProxyProviderServer{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.handle))
	t.Cleanup(provider.server.Close)

	return provider
}

func (provider *ProxyProviderServer) URL() string {
	return provider.server.URL
}

func NewProxyPassThroughServer(
	t *testing.T,
	upstream string,
	recorder agentproxy.EventRecorder,
) *ProxyPassThroughServer {
	t.Helper()

	proxy, err := agentproxy.NewPassThroughProxy(agentproxy.PassThroughOptions{
		Recorder: recorder,
		Upstream: upstream,
		Provider: "fixture",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	server := &ProxyPassThroughServer{server: httptest.NewServer(proxy)}
	t.Cleanup(server.server.Close)

	return server
}

func (server *ProxyPassThroughServer) URL() string {
	return server.server.URL
}

func (server *ProxyPassThroughServer) Send(
	request agentproxy.ProviderRequest,
) (agentproxy.ProviderResponse, error) {
	return sendProviderRequest(server.URL(), request)
}

func (provider *ProxyProviderServer) Send(
	request agentproxy.ProviderRequest,
) (agentproxy.ProviderResponse, error) {
	return sendProviderRequest(provider.URL(), request)
}

func sendProviderRequest(
	target string,
	request agentproxy.ProviderRequest,
) (agentproxy.ProviderResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return agentproxy.ProviderResponse{}, fmt.Errorf("encode provider request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		target,
		bytes.NewReader(payload),
	)
	if err != nil {
		return agentproxy.ProviderResponse{}, fmt.Errorf("create provider request: %w", err)
	}

	httpRequest.Header.Set("Content-Type", "application/json")

	if request.SessionID != "" {
		httpRequest.Header.Set("X-Coding-Ethos-Session", request.SessionID)
	}

	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		return agentproxy.ProviderResponse{}, fmt.Errorf("send provider request: %w", err)
	}
	defer response.Body.Close()

	var decoded agentproxy.ProviderResponse

	err = json.NewDecoder(response.Body).Decode(&decoded)
	if err != nil {
		return agentproxy.ProviderResponse{}, fmt.Errorf("decode provider response: %w", err)
	}

	return decoded, nil
}

// TLSProxyProviderServer is an httptest TLS fake that answers OpenAI-shaped chat
// completions and Server-Sent Events streams. It stands in for a real remote
// provider TLS endpoint so the interception scenario can perform a genuine TLS
// handshake against a trusted certificate without billed, nondeterministic
// upstream calls. See KNOWN_DEFECTS.md, "Agent Proxy TLS Fixture Provider".
type TLSProxyProviderServer struct {
	server *httptest.Server
}

// NewTLSProxyProviderServer starts a TLS fake provider that serves a canned chat
// response on tlsFixtureChatPath and a canned SSE stream on tlsFixtureStreamPath.
func NewTLSProxyProviderServer(t *testing.T) *TLSProxyProviderServer {
	t.Helper()

	provider := &TLSProxyProviderServer{}
	provider.server = httptest.NewTLSServer(http.HandlerFunc(provider.handle))

	t.Cleanup(provider.server.Close)

	return provider
}

// URL returns the base HTTPS URL of the TLS fake provider.
func (provider *TLSProxyProviderServer) URL() string {
	return provider.server.URL
}

// Host returns the host:port authority of the TLS fake provider, suitable for an
// interception allow list and for CONNECT targets.
func (provider *TLSProxyProviderServer) Host(t *testing.T) string {
	t.Helper()

	parsed, err := url.Parse(provider.server.URL)
	if err != nil {
		t.Fatalf("parse tls fixture url: %v", err)
	}

	return parsed.Host
}

// Certificate returns the self-signed certificate the TLS fake presents so an
// upstream client can be configured to trust it.
func (provider *TLSProxyProviderServer) Certificate() *x509.Certificate {
	return provider.server.Certificate()
}

// handle answers chat completions with canned JSON and the stream path with SSE.
// Every other path returns 404 so an unexpected route fails loudly.
func (provider *TLSProxyProviderServer) handle(
	writer http.ResponseWriter,
	request *http.Request,
) {
	defer func() { _ = request.Body.Close() }()

	switch request.URL.Path {
	case tlsFixtureChatPath:
		writeFixtureBody(writer, "application/json", tlsFixtureChatBody)
	case tlsFixtureStreamPath:
		writeFixtureBody(writer, "text/event-stream", tlsFixtureStreamBody)
	default:
		http.NotFound(writer, request)
	}
}

// writeFixtureBody writes a canned response body with the given content type. A
// failed write means the intercepting client hung up mid-handshake and the fake
// can take no recovery action, so the error is observed and dropped.
func writeFixtureBody(writer http.ResponseWriter, contentType, body string) {
	writer.Header().Set("Content-Type", contentType)
	writer.WriteHeader(http.StatusOK)

	_, writeErr := writer.Write([]byte(body))
	discardFixtureWriteError(writeErr)
}

// discardFixtureWriteError intentionally consumes a best-effort write to the
// intercepted client so errcheck is satisfied without masking an actionable
// fault: the fake provider has no recovery path once the client disconnects.
func discardFixtureWriteError(_ error) {}

// ProxyInterceptServer wraps a CONNECT TLS-MITM interception proxy over a real
// local CA. The proxy itself speaks plain HTTP for CONNECT; minted leaves chain
// to the CA whose certificate path is exposed for the driving test client.
type ProxyInterceptServer struct {
	server     *httptest.Server
	caCertPath string
}

// NewProxyInterceptServer provisions a CA under repoRoot, builds a leaf issuer,
// and serves an interception proxy that records to store and forwards through
// upstreamClient. The proxy intercepts only hosts in allowHosts; all others are
// blind-tunneled. The CA is provisioned at a fixed instant while the issuer and
// proxy run on the real clock so minted leaves verify at real wall-clock time.
func NewProxyInterceptServer(
	t *testing.T,
	repoRoot string,
	store agentproxy.EventRecorder,
	upstreamClient *http.Client,
	allowHosts []string,
) *ProxyInterceptServer {
	t.Helper()

	// The CA and the leaf issuer both run on the real clock so the minted CA and
	// the short-lived leaves are valid at the driving client's real verification
	// time. A fixed provisioning instant would let the CA's 90-day window lapse
	// and silently expire the suite once wall-clock time passed it.
	authority, err := ca.EnsureCA(repoRoot, time.Now())
	if err != nil {
		t.Fatalf("provision intercept CA: %v", err)
	}

	issuer, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{
		Now: time.Now,
	})
	if err != nil {
		t.Fatalf("build leaf issuer: %v", err)
	}

	proxy, err := agentproxy.NewInterceptProxy(agentproxy.InterceptOptions{
		Recorder:   store,
		Registry:   adapter.DefaultRegistry(),
		Issuer:     issuer,
		Now:        time.Now,
		Client:     upstreamClient,
		RepoRoot:   repoRoot,
		AllowHosts: allowHosts,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("build intercept proxy: %v", err)
	}

	server := &ProxyInterceptServer{
		server:     httptest.NewServer(proxy),
		caCertPath: authority.CertPath(),
	}
	t.Cleanup(server.server.Close)

	return server
}

// URL returns the plain-HTTP URL agents point HTTPS_PROXY at for CONNECT.
func (server *ProxyInterceptServer) URL() string {
	return server.server.URL
}

// CACertPath returns the path to the local CA certificate the driving test
// client must trust to accept the leaves the proxy mints for upstream hosts.
func (server *ProxyInterceptServer) CACertPath() string {
	return server.caCertPath
}

// NewInterceptUpstreamClient builds the client the interception proxy uses to
// reach the TLS fake upstream. It trusts the fake's self-signed certificate,
// disables compression, and refuses to follow redirects so the proxy forwards
// upstream responses verbatim, mirroring the production upstream client.
func NewInterceptUpstreamClient(
	t *testing.T,
	provider *TLSProxyProviderServer,
) *http.Client {
	t.Helper()

	pool := x509.NewCertPool()
	pool.AddCert(provider.Certificate())

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport is not *http.Transport")
	}

	cloned := transport.Clone()
	cloned.Proxy = nil
	cloned.DisableCompression = true
	cloned.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}

	return &http.Client{
		Transport: cloned,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// NewInterceptClientThroughProxy builds the client that drives traffic THROUGH
// the interception proxy. It routes every request through proxyURL and trusts the
// local CA at caCertPath so it accepts the leaves the proxy mints for upstream
// hosts. When forceHTTP2 is set the transport prefers HTTP/2 over the tunnel.
func NewInterceptClientThroughProxy(
	t *testing.T,
	proxyURL string,
	caCertPath string,
	forceHTTP2 bool,
) *http.Client {
	t.Helper()

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	pemBytes, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		t.Fatalf("append CA cert from %s", caCertPath)
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport is not *http.Transport")
	}

	cloned := transport.Clone()
	cloned.Proxy = http.ProxyURL(parsed)
	cloned.ForceAttemptHTTP2 = forceHTTP2
	cloned.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}

	return &http.Client{Transport: cloned}
}

func (provider *ProxyProviderServer) handle(
	writer http.ResponseWriter,
	request *http.Request,
) {
	defer request.Body.Close()

	var decoded agentproxy.ProviderRequest

	err := json.NewDecoder(request.Body).Decode(&decoded)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)

		return
	}

	response := agentproxy.ProviderResponse{
		SessionID: decoded.SessionID,
		Provider:  decoded.Provider,
		Model:     decoded.Model,
		Messages: []agentproxy.Message{{
			Role:    agentproxy.RoleAssistant,
			Content: "fixture provider response",
		}},
		Usage: agentproxy.TokenUsage{
			InputTokens:  len(decoded.Messages),
			OutputTokens: fixtureProviderOutputTokens,
			TotalTokens: len(decoded.Messages) +
				fixtureProviderOutputTokens,
		},
	}

	writer.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(writer).Encode(response)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}
