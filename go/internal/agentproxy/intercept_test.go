// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/adapter"
)

// testClock returns the wall clock so minted leaves validate against the test
// client's real-time certificate verification.
func testClock() func() time.Time {
	return time.Now
}

// recordingRecorder captures every recorded event for assertion.
type recordingRecorder struct {
	mu     sync.Mutex
	events []agentproxy.ProviderEvent
}

// RecordProxyEvent appends an event under lock so concurrent HTTP/2 streams stay
// race-free during assertions.
func (recorder *recordingRecorder) RecordProxyEvent(
	_ context.Context,
	event agentproxy.ProviderEvent,
) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	recorder.events = append(recorder.events, event)

	return nil
}

// snapshot returns a copy of the recorded events for assertion.
func (recorder *recordingRecorder) snapshot() []agentproxy.ProviderEvent {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	out := make([]agentproxy.ProviderEvent, len(recorder.events))
	copy(out, recorder.events)

	return out
}

// inTestIssuer is a self-contained agentproxy.LeafIssuer backed by an in-memory
// throwaway CA. It deliberately avoids the ca package so the agentproxy unit
// tests never import ca (ca imports agentproxy, so that import would form a
// cycle). The real ca-backed end-to-end coverage lives in internal/e2e, which
// already depends on both packages.
type inTestIssuer struct {
	signer    *ecdsa.PrivateKey
	caCert    *x509.Certificate
	caCertDER []byte
	now       func() time.Time
}

// MintLeaf mints a fresh leaf certificate for host, signed by the in-test CA and
// valid at now. The returned chain carries the leaf first and the CA second.
func (issuer *inTestIssuer) MintLeaf(
	host string,
	now time.Time,
) (tls.Certificate, error) {
	bare := strings.ToLower(strings.TrimSpace(host))
	if stripped, _, err := net.SplitHostPort(bare); err == nil {
		bare = stripped
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: bare},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(bare); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{bare}
	}

	leafDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		issuer.caCert,
		&leafKey.PublicKey,
		issuer.signer,
	)
	if err != nil {
		return tls.Certificate{}, err
	}

	parsed, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{leafDER, issuer.caCertDER},
		PrivateKey:  leafKey,
		Leaf:        parsed,
	}, nil
}

// GetCertificate adapts MintLeaf to the tls.Config GetCertificate callback,
// minting a leaf for the handshake's SNI server name.
func (issuer *inTestIssuer) GetCertificate(
	hello *tls.ClientHelloInfo,
) (*tls.Certificate, error) {
	cert, err := issuer.MintLeaf(hello.ServerName, issuer.now())
	if err != nil {
		return nil, err
	}

	return &cert, nil
}

// newTestIssuer builds an in-memory CA and returns a self-contained leaf issuer
// plus an x509 pool trusting that CA, so the test client can verify minted
// leaves without touching disk or the ca package.
func newTestIssuer(t *testing.T, now func() time.Time) (*inTestIssuer, *x509.CertPool) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate CA serial: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "coding-ethos in-test CA"},
		NotBefore:             now().Add(-5 * time.Minute),
		NotAfter:              now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&caKey.PublicKey,
		caKey,
	)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	issuer := &inTestIssuer{
		signer:    caKey,
		caCert:    caCert,
		caCertDER: caDER,
		now:       now,
	}

	return issuer, pool
}

// interceptHarness wires an upstream TLS server, a configured InterceptProxy, a
// CONNECT-capable client, and the recorder for end-to-end assertions.
type interceptHarness struct {
	upstream *httptest.Server
	recorder *recordingRecorder
	client   *http.Client
	proxyURL string
	cancel   context.CancelFunc
	wait     chan error
}

// close shuts the harness down and reports the serve error.
func (harness *interceptHarness) close(t *testing.T) {
	t.Helper()

	harness.cancel()
	harness.upstream.Close()

	select {
	case <-harness.wait:
	case <-time.After(5 * time.Second):
		t.Fatal("intercept proxy did not stop")
	}
}

// upstreamHost returns the bare upstream host without its port.
func (harness *interceptHarness) upstreamHost(t *testing.T) string {
	t.Helper()

	host, _, err := net.SplitHostPort(harness.upstream.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split upstream host: %v", err)
	}

	return host
}

// newInterceptHarness builds a harness whose proxy intercepts the upstream host.
func newInterceptHarness(
	t *testing.T,
	handler http.HandlerFunc,
	allowUpstream bool,
) *interceptHarness {
	t.Helper()

	now := testClock()
	upstream := httptest.NewTLSServer(handler)

	upstreamHost, _, err := net.SplitHostPort(upstream.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split upstream host: %v", err)
	}

	issuer, caPool := newTestIssuer(t, now)
	recorder := &recordingRecorder{}

	allow := []string{}
	if allowUpstream {
		allow = []string{upstreamHost}
	}

	proxy := buildProxy(t, proxyConfig{
		now:          now,
		issuer:       issuer,
		recorder:     recorder,
		allow:        allow,
		upstreamPool: upstreamTrustPool(upstream),
	})

	proxyURL, cancel, wait := startProxy(t, proxy)

	return &interceptHarness{
		upstream: upstream,
		recorder: recorder,
		client:   connectClient(caPool),
		proxyURL: proxyURL,
		cancel:   cancel,
		wait:     wait,
	}
}

// proxyConfig bundles the inputs needed to build a test InterceptProxy.
type proxyConfig struct {
	now          func() time.Time
	issuer       *inTestIssuer
	recorder     *recordingRecorder
	upstreamPool *x509.CertPool
	allow        []string
	maxNormalize int64
}

// buildProxy constructs an enabled InterceptProxy whose upstream client trusts
// the upstream TLS server.
func buildProxy(t *testing.T, config proxyConfig) *agentproxy.InterceptProxy {
	t.Helper()

	// The upstream client mirrors production defaultInterceptHTTPClient: it does
	// not follow redirects, so 3xx responses are forwarded to the client verbatim.
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    config.upstreamPool,
			},
			DisableCompression: true,
		},
	}

	proxy, err := agentproxy.NewInterceptProxy(agentproxy.InterceptOptions{
		Now:          config.now,
		Recorder:     config.recorder,
		Registry:     adapter.DefaultRegistry(),
		Issuer:       config.issuer,
		Client:       client,
		AllowHosts:   config.allow,
		Provider:     "fixture",
		MaxNormalize: config.maxNormalize,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("new intercept proxy: %v", err)
	}

	return proxy
}

// upstreamTrustPool extracts the upstream TLS server's certificate into a pool.
func upstreamTrustPool(upstream *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())

	return pool
}

// startProxy binds the proxy to a loopback listener and serves it until the
// returned cancel func fires, exposing the proxy URL for CONNECT clients.
func startProxy(
	t *testing.T,
	proxy *agentproxy.InterceptProxy,
) (string, context.CancelFunc, chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	wait := make(chan error, 1)

	go func() {
		wait <- proxy.ListenAndServeOnListener(ctx, listener)
	}()

	return "http://" + listener.Addr().String(), cancel, wait
}

// connectClient builds an HTTP client template that trusts the supplied CA for
// the minted leaf; proxiedClient later points it at the proxy address.
func connectClient(rootCAs *x509.CertPool) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    rootCAs,
			},
		},
		Timeout: 10 * time.Second,
	}
}

// proxiedClient rewires a CONNECT client to route through the proxy address.
func proxiedClient(base *http.Client, proxyURL string) *http.Client {
	parsed, _ := url.Parse(proxyURL)
	transport, _ := base.Transport.(*http.Transport)
	clone := transport.Clone()
	clone.Proxy = http.ProxyURL(parsed)

	return &http.Client{Transport: clone, Timeout: base.Timeout}
}

const cannedChatResponse = `{"model":"gpt-test","choices":` +
	`[{"message":{"role":"assistant","content":"hello from upstream"}}],` +
	`"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`

func TestInterceptProxyForwardsResponseVerbatimWithoutRetainingBody(t *testing.T) {
	t.Parallel()

	var gotAuth string

	handler := func(writer http.ResponseWriter, request *http.Request) {
		gotAuth = request.Header.Get("Authorization")

		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(cannedChatResponse))
	}

	harness := newInterceptHarness(t, handler, true)
	defer harness.close(t)

	host := harness.upstreamHost(t)
	client := proxiedClient(harness.client, harness.proxyURL)
	target := harness.upstream.URL + "/v1/chat/completions"

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		target,
		strings.NewReader(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer SECRET123")

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if string(body) != cannedChatResponse {
		t.Fatalf("response body altered: %q", body)
	}

	if gotAuth != "Bearer SECRET123" {
		t.Fatalf("upstream did not receive auth header verbatim: %q", gotAuth)
	}

	assertInterceptedEvidence(t, harness.recorder.snapshot(), host)
}

// assertInterceptedEvidence verifies that intercepted evidence was recorded with
// hashes, the intercepted flag, and no leaked payload or auth material.
func assertInterceptedEvidence(
	t *testing.T,
	events []agentproxy.ProviderEvent,
	host string,
) {
	t.Helper()

	var (
		outbound agentproxy.ProviderEvent
		inbound  agentproxy.ProviderEvent
		haveOut  bool
		haveIn   bool
	)

	for _, event := range events {
		switch event.Kind {
		case agentproxy.EventProviderCall:
			outbound = event
			haveOut = true
		case agentproxy.EventProviderResponse:
			inbound = event
			haveIn = true
		}
	}

	if !haveOut || !haveIn {
		t.Fatalf("missing intercepted events: %#v", events)
	}

	if outbound.Metadata["intercepted"] != "true" || outbound.Provider != "fixture" {
		t.Fatalf("outbound event not intercepted: %#v", outbound)
	}

	if outbound.InputHash == "" || inbound.OutputHash == "" {
		t.Fatalf("hashes missing: out=%q in=%q", outbound.InputHash, inbound.OutputHash)
	}

	if inbound.Metadata["intercepted"] != "true" {
		t.Fatalf("inbound event not intercepted: %#v", inbound)
	}

	assertNoLeak(t, events, host)
}

// assertNoLeak marshals every event and fails if raw payload or auth material
// surfaces in the recorded evidence.
func assertNoLeak(t *testing.T, events []agentproxy.ProviderEvent, host string) {
	t.Helper()

	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}

		text := string(encoded)
		if strings.Contains(text, "hello from upstream") {
			t.Fatalf("raw response body leaked into event: %s", text)
		}

		if strings.Contains(text, "SECRET123") {
			t.Fatalf("auth secret leaked into event: %s", text)
		}
	}

	_ = host
}
