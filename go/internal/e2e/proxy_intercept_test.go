// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/e2e"
)

// interceptAllowedHost is the bare host the TLS fixtures bind to. httptest
// servers listen on 127.0.0.1, so the proxy mints an IP-SAN leaf for it.
const interceptAllowedHost = "127.0.0.1"

// secretToken is sent in an Authorization header and must never be recorded.
const secretToken = "E2ESECRET"

// TestAgentProxyInterceptForwardsAndRecordsBodyFreeEvidence drives real HTTPS
// traffic through the CONNECT TLS-MITM interception proxy backed by a real
// local CA and the code-intel DuckDB ledger, asserting verbatim forwarding,
// body-free recording, auth non-leak, and intercepted-true correlation.
func TestAgentProxyInterceptForwardsAndRecordsBodyFreeEvidence(t *testing.T) {
	if testing.Short() {
		t.Skip("agent proxy intercept e2e uses a real CA and TLS handshakes")
	}

	repo, store := newInterceptRepoStore(t)
	provider := e2e.NewTLSProxyProviderServer(t)
	upstream := e2e.NewInterceptUpstreamClient(t, provider)
	proxy := e2e.NewProxyInterceptServer(
		t,
		repo.Root,
		store,
		upstream,
		[]string{interceptAllowedHost},
	)
	client := e2e.NewInterceptClientThroughProxy(
		t,
		proxy.URL(),
		proxy.CACertPath(),
		false,
	)

	sessionID := "intercept-post"
	body := interceptChatRequest(t, client, provider, sessionID, secretToken)

	if string(body) != tlsFixtureChatResponse() {
		t.Fatalf("intercepted body mismatch: %q", string(body))
	}

	assertBodyFreeProviderCall(t, store, sessionID)
	assertProviderResponseRecorded(t, store, sessionID)
	assertNoSecretRecorded(t, store, sessionID)
}

// TestAgentProxyInterceptBlindTunnelsUnlistedHost confirms that a host outside
// the allow list is blind-tunneled: the response still works end to end and the
// recorded event is marked intercepted=false.
func TestAgentProxyInterceptBlindTunnelsUnlistedHost(t *testing.T) {
	if testing.Short() {
		t.Skip("agent proxy intercept e2e uses a real CA and TLS handshakes")
	}

	repo, store := newInterceptRepoStore(t)
	provider := e2e.NewTLSProxyProviderServer(t)
	upstream := e2e.NewInterceptUpstreamClient(t, provider)
	proxy := e2e.NewProxyInterceptServer(t, repo.Root, store, upstream, nil)

	client := blindTunnelClient(t, proxy.URL(), provider)
	sessionID := "intercept-blind"
	body := interceptChatRequest(t, client, provider, sessionID, "")

	if string(body) != tlsFixtureChatResponse() {
		t.Fatalf("blind-tunnel body mismatch: %q", string(body))
	}

	events := waitForProxyEvents(t, store, codeintel.ProxyEventQuery{
		Kind: string(agentproxy.EventProviderCall),
	})
	if len(events) == 0 {
		t.Fatalf("expected a blind-tunnel provider call event")
	}

	if events[0].Metadata["intercepted"] != "false" {
		t.Fatalf("blind-tunnel event not marked intercepted=false: %#v", events[0])
	}
}

// TestAgentProxyInterceptStreamsSSE confirms an upstream text/event-stream
// response streams through verbatim and is recorded as reconstructed structural
// facts (streaming_reconstructed) rather than left unparsed.
func TestAgentProxyInterceptStreamsSSE(t *testing.T) {
	if testing.Short() {
		t.Skip("agent proxy intercept e2e uses a real CA and TLS handshakes")
	}

	repo, store := newInterceptRepoStore(t)
	provider := e2e.NewTLSProxyProviderServer(t)
	upstream := e2e.NewInterceptUpstreamClient(t, provider)
	proxy := e2e.NewProxyInterceptServer(
		t,
		repo.Root,
		store,
		upstream,
		[]string{interceptAllowedHost},
	)
	client := e2e.NewInterceptClientThroughProxy(
		t,
		proxy.URL(),
		proxy.CACertPath(),
		false,
	)

	sessionID := "intercept-sse"
	body := interceptStreamRequest(t, client, provider, sessionID)

	if string(body) != tlsFixtureStreamResponse() {
		t.Fatalf("streamed body mismatch: %q", string(body))
	}

	events := waitForProxyEvents(t, store, codeintel.ProxyEventQuery{
		Kind:      string(agentproxy.EventProviderResponse),
		SessionID: sessionID,
	})
	if len(events) == 0 {
		t.Fatalf("expected a streamed provider response event")
	}

	if events[0].Metadata["streaming_reconstructed"] != "true" {
		t.Fatalf("streamed event not reconstructed: %#v", events[0])
	}

	if events[0].Metadata["streaming_not_normalized"] == "true" {
		t.Fatalf("reconstructed stream still marked not normalized: %#v", events[0])
	}

	if events[0].OutputHash == "" {
		t.Fatalf("reconstructed stream missing output hash: %#v", events[0])
	}

	if events[0].Model != "fixture-model" {
		t.Fatalf("reconstructed stream missing model: %#v", events[0])
	}
}

// TestAgentProxyInterceptOverHTTP2 drives one request over HTTP/2 to prove the
// proxy preserves the negotiated protocol and still intercepts the traffic.
func TestAgentProxyInterceptOverHTTP2(t *testing.T) {
	if testing.Short() {
		t.Skip("agent proxy intercept e2e uses a real CA and TLS handshakes")
	}

	repo, store := newInterceptRepoStore(t)
	provider := e2e.NewTLSProxyProviderServer(t)
	upstream := e2e.NewInterceptUpstreamClient(t, provider)
	proxy := e2e.NewProxyInterceptServer(
		t,
		repo.Root,
		store,
		upstream,
		[]string{interceptAllowedHost},
	)
	client := e2e.NewInterceptClientThroughProxy(
		t,
		proxy.URL(),
		proxy.CACertPath(),
		true,
	)

	sessionID := "intercept-h2"
	body, protoMajor := interceptChatRequestProto(t, client, provider, sessionID)

	if string(body) != tlsFixtureChatResponse() {
		t.Fatalf("http/2 intercepted body mismatch: %q", string(body))
	}

	if protoMajor != 2 {
		t.Fatalf("expected HTTP/2 over the tunnel, got proto major %d", protoMajor)
	}

	assertBodyFreeProviderCall(t, store, sessionID)
}

// newInterceptRepoStore builds an isolated temp repository plus its code-intel
// store, mirroring the existing proxy harness test setup.
func newInterceptRepoStore(t *testing.T) (e2e.Repo, *codeintel.Store) {
	t.Helper()

	sourceRoot := repoRootFromWorkingDirectory(t)
	repo := e2e.FromReference(t, sourceRoot, "policy-lint-basic")

	store, err := codeintel.Open(
		context.Background(),
		filepath.Join(repo.Root, ".coding-ethos", "code-intel.duckdb"),
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return repo, store
}

// interceptChatRequest POSTs a chat-completions request through the proxy to the
// TLS fixture and returns the verbatim response body.
func interceptChatRequest(
	t *testing.T,
	client *http.Client,
	provider *e2e.TLSProxyProviderServer,
	sessionID string,
	authToken string,
) []byte {
	t.Helper()

	target := provider.URL() + "/v1/chat/completions"
	payload := `{"model":"fixture-model","messages":` +
		`[{"role":"user","content":"hello"}]}`

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		target,
		strings.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("create chat request: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Coding-Ethos-Session", sessionID)

	if authToken != "" {
		request.Header.Set("Authorization", "Bearer "+authToken)
	}

	return doInterceptRequest(t, client, request)
}

// interceptChatRequestProto POSTs a chat request through the proxy and returns
// the verbatim response body together with the negotiated HTTP protocol major
// version, so a caller can prove the tunnel preserved HTTP/2.
func interceptChatRequestProto(
	t *testing.T,
	client *http.Client,
	provider *e2e.TLSProxyProviderServer,
	sessionID string,
) ([]byte, int) {
	t.Helper()

	target := provider.URL() + "/v1/chat/completions"
	payload := `{"model":"fixture-model","messages":` +
		`[{"role":"user","content":"hello"}]}`

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		target,
		strings.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("create chat request: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Coding-Ethos-Session", sessionID)

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request through proxy: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	return body, response.ProtoMajor
}

// interceptStreamRequest GETs the SSE endpoint through the proxy and returns the
// verbatim streamed body.
func interceptStreamRequest(
	t *testing.T,
	client *http.Client,
	provider *e2e.TLSProxyProviderServer,
	sessionID string,
) []byte {
	t.Helper()

	target := provider.URL() + "/v1/chat/completions/stream"

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		target,
		nil,
	)
	if err != nil {
		t.Fatalf("create stream request: %v", err)
	}

	request.Header.Set("X-Coding-Ethos-Session", sessionID)

	return doInterceptRequest(t, client, request)
}

// doInterceptRequest sends request through client and returns the response body,
// failing the test on any transport or read error.
func doInterceptRequest(
	t *testing.T,
	client *http.Client,
	request *http.Request,
) []byte {
	t.Helper()

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request through proxy: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	return body
}

// blindTunnelClient builds a client that routes through the proxy but trusts the
// upstream fixture's own certificate, because a blind tunnel relays the upstream
// TLS handshake verbatim rather than presenting a CA-minted leaf.
func blindTunnelClient(
	t *testing.T,
	proxyURL string,
	provider *e2e.TLSProxyProviderServer,
) *http.Client {
	t.Helper()

	pool := x509.NewCertPool()
	pool.AddCert(provider.Certificate())

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport is not *http.Transport")
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	cloned := transport.Clone()
	cloned.Proxy = http.ProxyURL(parsed)
	cloned.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}

	return &http.Client{Transport: cloned}
}

// assertBodyFreeProviderCall asserts an intercepted outbound provider_call
// event was recorded for sessionID with an InputHash and no retained body.
func assertBodyFreeProviderCall(
	t *testing.T,
	store *codeintel.Store,
	sessionID string,
) {
	t.Helper()

	events := waitForProxyEvents(t, store, codeintel.ProxyEventQuery{
		Kind:      string(agentproxy.EventProviderCall),
		SessionID: sessionID,
	})
	if len(events) == 0 {
		t.Fatalf("expected a provider call event for %s", sessionID)
	}

	event := events[0]
	if event.Metadata["intercepted"] != "true" {
		t.Fatalf("provider call not intercepted: %#v", event.Metadata)
	}

	if event.Metadata["payload_body_retained"] != "false" {
		t.Fatalf("provider call retained body: %#v", event.Metadata)
	}

	if event.InputHash == "" {
		t.Fatalf("provider call missing input hash: %#v", event)
	}
}

// assertProviderResponseRecorded asserts an inbound provider_response event was
// recorded for sessionID with an OutputHash set.
func assertProviderResponseRecorded(
	t *testing.T,
	store *codeintel.Store,
	sessionID string,
) {
	t.Helper()

	events := waitForProxyEvents(t, store, codeintel.ProxyEventQuery{
		Kind:      string(agentproxy.EventProviderResponse),
		SessionID: sessionID,
	})
	if len(events) == 0 {
		t.Fatalf("expected a provider response event for %s", sessionID)
	}

	if events[0].OutputHash == "" {
		t.Fatalf("provider response missing output hash: %#v", events[0])
	}
}

// assertNoSecretRecorded marshals every event for sessionID and asserts neither
// the canned provider body nor the auth token appears anywhere in the ledger.
func assertNoSecretRecorded(
	t *testing.T,
	store *codeintel.Store,
	sessionID string,
) {
	t.Helper()

	events, err := store.ProxyEvents(
		context.Background(),
		codeintel.ProxyEventQuery{SessionID: sessionID},
	)
	if err != nil {
		t.Fatalf("query session events: %v", err)
	}

	if len(events) == 0 {
		t.Fatalf("expected recorded events for %s", sessionID)
	}

	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}

	if strings.Contains(string(encoded), "fixture-secret-body") {
		t.Fatalf("raw provider body leaked into ledger")
	}

	if strings.Contains(string(encoded), secretToken) {
		t.Fatalf("auth token leaked into ledger")
	}
}

// tlsFixtureChatResponse is the verbatim chat-completions body the TLS fixture
// returns, used to assert byte-identical forwarding.
func tlsFixtureChatResponse() string {
	return `{"model":"fixture-model",` +
		`"choices":[{"message":{"role":"assistant","content":"fixture-secret-body"}}],` +
		`"usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12}}`
}

// tlsFixtureStreamResponse is the verbatim SSE body the TLS fixture returns. It
// mirrors proxy_harness.go's tlsFixtureStreamBody: a parseable OpenAI stream the
// interception proxy reconstructs into structural facts.
func tlsFixtureStreamResponse() string {
	return "data: {\"model\":\"fixture-model\",\"choices\":" +
		"[{\"delta\":{\"role\":\"assistant\",\"content\":\"fixture-stream-token\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{}}]," +
		"\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n\n" +
		"data: [DONE]\n\n"
}
