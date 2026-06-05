// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy_test

import (
	"context"
	"crypto/x509"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

func TestInterceptProxyBlindTunnelsUnlistedHost(t *testing.T) {
	t.Parallel()

	handler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tunneled":true}`))
	}

	// allowUpstream=false leaves the upstream host off the allow list, so the
	// proxy must blind-tunnel the connection end to end.
	harness := newInterceptHarness(t, handler, false)
	defer harness.close(t)

	host := harness.upstreamHost(t)

	// A blind tunnel performs end-to-end TLS, so the client trusts the upstream
	// certificate directly rather than a minted leaf.
	client := proxiedClient(
		connectClient(upstreamTrustPool(harness.upstream)),
		harness.proxyURL,
	)
	target := harness.upstream.URL + "/v1/chat/completions"

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		target,
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("tunneled request: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if string(body) != `{"tunneled":true}` {
		t.Fatalf("tunneled body altered: %q", body)
	}

	assertBlindTunnel(t, harness.recorder.snapshot(), host)
}

// assertBlindTunnel verifies that a single not-intercepted event was recorded
// for a blind tunnel and that no decrypted evidence was captured.
func assertBlindTunnel(t *testing.T, events []agentproxy.ProviderEvent, host string) {
	t.Helper()

	if len(events) != 1 {
		t.Fatalf("blind tunnel recorded events = %#v", events)
	}

	event := events[0]
	if event.Metadata["intercepted"] != "false" {
		t.Fatalf("blind tunnel event not marked unintercepted: %#v", event)
	}

	if event.Metadata["host"] != host {
		t.Fatalf("blind tunnel host = %q, want %q", event.Metadata["host"], host)
	}

	if event.Kind != agentproxy.EventProviderCall {
		t.Fatalf("blind tunnel event kind = %q", event.Kind)
	}
}

// cannedSSE is a provider-valid OpenAI chat-completions SSE body so the
// streaming path exercises real reconstruction rather than a payload the adapter
// cannot recognize. The deltas concatenate into one assistant message.
const cannedSSE = "data: {\"model\":\"gpt-test\",\"choices\":" +
	"[{\"delta\":{\"role\":\"assistant\",\"content\":\"chunk-one\"}}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"chunk-two\"}}]}\n\n" +
	"data: [DONE]\n\n"

func TestInterceptProxyForwardsRedirectWithoutFollowing(t *testing.T) {
	t.Parallel()

	const redirectTarget = "https://example.invalid/relocated"

	handler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", redirectTarget)
		writer.WriteHeader(http.StatusFound)
		_, _ = writer.Write([]byte(`{"redirect":true}`))
	}

	harness := newInterceptHarness(t, handler, true)
	defer harness.close(t)

	// The end client must not follow the redirect either, so it can observe the
	// verbatim 3xx the proxy forwarded rather than chasing the Location target.
	client := proxiedClient(harness.client, harness.proxyURL)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	target := harness.upstream.URL + "/v1/chat/completions"

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		target,
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d (proxy must not follow redirect)",
			response.StatusCode, http.StatusFound)
	}

	if got := response.Header.Get("Location"); got != redirectTarget {
		t.Fatalf("Location = %q, want %q", got, redirectTarget)
	}
}

func TestInterceptProxyStreamsServerSentEvents(t *testing.T) {
	t.Parallel()

	handler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(cannedSSE))

		flusher, ok := writer.(http.Flusher)
		if ok {
			flusher.Flush()
		}
	}

	harness := newInterceptHarness(t, handler, true)
	defer harness.close(t)

	client := proxiedClient(harness.client, harness.proxyURL)
	target := harness.upstream.URL + "/v1/chat/completions"

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		target,
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("sse request: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read sse: %v", err)
	}

	if string(body) != cannedSSE {
		t.Fatalf("sse stream altered: %q", body)
	}

	assertReconstructedEvidence(t, harness.recorder)
}

// assertReconstructedEvidence verifies that the inbound event for an SSE stream
// is reconstructed into structural facts rather than left unparsed. The event is
// recorded on the handler goroutine just after the client observes the streamed
// EOF, so the assertion polls the recorder.
func assertReconstructedEvidence(t *testing.T, recorder *recordingRecorder) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, event := range recorder.snapshot() {
			if event.Kind != agentproxy.EventProviderResponse {
				continue
			}

			if event.Metadata["streaming_reconstructed"] != "true" {
				t.Fatalf("sse inbound event not reconstructed: %#v", event)
			}

			if event.Metadata["streaming_not_normalized"] == "true" {
				t.Fatalf("reconstructed sse event still marked not normalized: %#v", event)
			}

			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("no inbound streaming event recorded: %#v", recorder.snapshot())
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func TestInterceptProxyRejectsNonConnectMethods(t *testing.T) {
	t.Parallel()

	now := testClock()
	issuer, _ := newTestIssuer(t, now)

	proxy, err := agentproxy.NewInterceptProxy(agentproxy.InterceptOptions{
		Now:      now,
		Registry: registryStub{},
		Issuer:   issuer,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("new intercept proxy: %v", err)
	}

	recorder := newStatusRecorder()
	proxy.ServeHTTP(recorder, newPlainRequest(t))

	if recorder.status != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.status, http.StatusMethodNotAllowed)
	}
}

func TestInterceptProxyNegotiatesHTTP2AndHTTP1(t *testing.T) {
	t.Parallel()

	handler := func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-Proto", request.Proto)
		_, _ = writer.Write([]byte(cannedChatResponse))
	}

	for _, testCase := range []struct {
		name      string
		wantProto string
		nextProto []string
	}{
		{name: "http2", wantProto: "HTTP/2.0", nextProto: []string{"h2", "http/1.1"}},
		{name: "http1", wantProto: "HTTP/1.1", nextProto: []string{"http/1.1"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newInterceptHarness(t, handler, true)
			defer harness.close(t)

			client := proxiedClientWithALPN(
				connectClient(clientTrustPool(t, harness)),
				harness.proxyURL,
				testCase.nextProto,
			)
			target := harness.upstream.URL + "/v1/chat/completions"

			request, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				target,
				nil,
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

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
				t.Fatalf("response body altered over %s: %q", testCase.name, body)
			}

			if response.Proto != testCase.wantProto {
				t.Fatalf(
					"client negotiated %s with proxy, want %s",
					response.Proto,
					testCase.wantProto,
				)
			}
		})
	}
}

// clientTrustPool returns the proxy CA pool the CONNECT client already trusts.
func clientTrustPool(t *testing.T, harness *interceptHarness) *x509.CertPool {
	t.Helper()

	transport, ok := harness.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport = %T", harness.client.Transport)
	}

	return transport.TLSClientConfig.RootCAs
}

// proxiedClientWithALPN clones the CONNECT client, routes it through the proxy,
// and constrains the TLS ALPN protocol list so the test drives a specific
// negotiated protocol against the proxy's terminated TLS.
func proxiedClientWithALPN(
	base *http.Client,
	proxyURL string,
	nextProtos []string,
) *http.Client {
	client := proxiedClient(base, proxyURL)
	transport, _ := client.Transport.(*http.Transport)
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.NextProtos = nextProtos
	transport.ForceAttemptHTTP2 = containsProto(nextProtos, "h2")

	return client
}

// containsProto reports whether protocol appears in the ALPN list.
func containsProto(protocols []string, protocol string) bool {
	for _, candidate := range protocols {
		if candidate == protocol {
			return true
		}
	}

	return false
}

// registryStub is an AdapterRegistry that never matches; the non-CONNECT
// rejection path needs a registry but no real adapters.
type registryStub struct{}

// Match always reports no adapter.
func (registryStub) Match(agentproxy.RequestContext) (agentproxy.Adapter, bool) {
	return nil, false
}

// statusRecorder captures the response status for non-streaming handler tests.
type statusRecorder struct {
	header http.Header
	status int
}

// newStatusRecorder builds an empty status recorder.
func newStatusRecorder() *statusRecorder {
	return &statusRecorder{header: http.Header{}}
}

// Header exposes the recorder header map.
func (recorder *statusRecorder) Header() http.Header {
	return recorder.header
}

// Write discards body bytes while reporting success.
func (recorder *statusRecorder) Write(content []byte) (int, error) {
	return len(content), nil
}

// WriteHeader records the status code.
func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
}

// newPlainRequest builds a non-CONNECT request for rejection testing.
func newPlainRequest(t *testing.T) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://proxy.local/v1/chat/completions",
		strings.NewReader(""),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	return request
}
