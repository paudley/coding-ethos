// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

// openAIStreamBody is a parseable OpenAI-style chat-completions SSE body. The
// proxy must forward these bytes verbatim while reconstructing the deltas into
// structural facts via the OpenAI adapter.
const openAIStreamBody = "data: {\"model\":\"gpt-test\",\"choices\":" +
	"[{\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"from upstream\"}}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{}}]," +
	"\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":5,\"total_tokens\":8}}\n\n" +
	"data: [DONE]\n\n"

// streamHandler writes the supplied SSE body as a text/event-stream response so
// the proxy exercises its streaming path.
func streamHandler(body string) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(body))
	}
}

// streamThroughProxy drives an SSE GET through the intercept proxy and returns
// the verbatim streamed body the client received.
func streamThroughProxy(
	t *testing.T,
	harness *interceptHarness,
	sessionID string,
) []byte {
	t.Helper()

	client := proxiedClient(harness.client, harness.proxyURL)
	target := harness.upstream.URL + "/v1/chat/completions/stream"

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		target,
		nil,
	)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}

	request.Header.Set("X-Coding-Ethos-Session", sessionID)

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("proxied stream request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read streamed response: %v", err)
	}

	return body
}

// waitForInbound polls the recorder until an inbound provider-response event is
// recorded, since the proxy records the reconstructed inbound event on the
// handler goroutine slightly after the client observes the streamed EOF.
func waitForInbound(
	t *testing.T,
	recorder *recordingRecorder,
) agentproxy.ProviderEvent {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, event := range recorder.snapshot() {
			if event.Kind == agentproxy.EventProviderResponse {
				return event
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("no inbound provider response event recorded: %#v",
				recorder.snapshot())
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// TestInterceptProxyReconstructsSSEStream proves a streamed OpenAI response is
// forwarded byte-identically while the recorded inbound event is reconstructed
// into structural facts rather than left as streaming_not_normalized.
func TestInterceptProxyReconstructsSSEStream(t *testing.T) {
	t.Parallel()

	harness := newInterceptHarness(t, streamHandler(openAIStreamBody), true)
	defer harness.close(t)

	body := streamThroughProxy(t, harness, "sse-reconstruct")

	if string(body) != openAIStreamBody {
		t.Fatalf("streamed body altered: %q", body)
	}

	event := waitForInbound(t, harness.recorder)

	if event.Metadata["streaming_reconstructed"] != "true" {
		t.Fatalf("stream not reconstructed: %#v", event.Metadata)
	}

	if event.Metadata["streaming_not_normalized"] == "true" {
		t.Fatalf("reconstructed stream still marked not normalized: %#v", event.Metadata)
	}

	if event.OutputHash == "" {
		t.Fatalf("reconstructed stream missing output hash: %#v", event)
	}

	if event.Model != "gpt-test" {
		t.Fatalf("reconstructed model not extracted: %q", event.Model)
	}

	if event.Metadata["message_count"] != "1" {
		t.Fatalf("reconstructed message count not recorded: %#v", event.Metadata)
	}

	assertNoStreamLeak(t, harness.recorder.snapshot())
}

// TestInterceptProxyForwardsOversizedSSEStreamVerbatim proves an SSE stream
// larger than the normalization bound still reaches the client byte-identically
// while the recorded event is marked too large rather than reconstructed.
func TestInterceptProxyForwardsOversizedSSEStreamVerbatim(t *testing.T) {
	t.Parallel()

	now := testClock()
	upstream := newTLSUpstream(t, streamHandler(openAIStreamBody))
	issuer, caPool := newTestIssuer(t, now)
	recorder := &recordingRecorder{}

	proxy := buildProxy(t, proxyConfig{
		now:          now,
		issuer:       issuer,
		recorder:     recorder,
		allow:        []string{upstreamHostOf(t, upstream)},
		upstreamPool: upstreamTrustPool(upstream),
		maxNormalize: 16,
	})

	proxyURL, cancel, wait := startProxy(t, proxy)

	harness := &interceptHarness{
		upstream: upstream,
		recorder: recorder,
		client:   connectClient(caPool),
		proxyURL: proxyURL,
		cancel:   cancel,
		wait:     wait,
	}
	defer harness.close(t)

	body := streamThroughProxy(t, harness, "sse-too-large")

	if string(body) != openAIStreamBody {
		t.Fatalf("oversized streamed body altered: %q", body)
	}

	event := waitForInbound(t, harness.recorder)

	if event.Metadata["payload_too_large_for_normalization"] != "true" {
		t.Fatalf("oversized stream not marked too large: %#v", event.Metadata)
	}

	if event.Metadata["streaming_reconstructed"] == "true" {
		t.Fatalf("oversized stream must not be reconstructed: %#v", event.Metadata)
	}
}

// newTLSUpstream starts a TLS upstream serving handler and registers cleanup,
// mirroring the upstream the standard intercept harness builds inline.
func newTLSUpstream(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	upstream := httptest.NewTLSServer(handler)
	t.Cleanup(upstream.Close)

	return upstream
}

// upstreamHostOf returns the bare host of upstream without its port for use in
// the proxy allow list.
func upstreamHostOf(t *testing.T, upstream *httptest.Server) string {
	t.Helper()

	host, _, err := net.SplitHostPort(upstream.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split upstream host: %v", err)
	}

	return host
}

// assertNoStreamLeak fails if any recorded event leaks the streamed content
// bytes, proving the bounded tee is used only for hashes and structural facts.
func assertNoStreamLeak(t *testing.T, events []agentproxy.ProviderEvent) {
	t.Helper()

	for _, event := range events {
		for _, value := range event.Metadata {
			if strings.Contains(value, "from upstream") {
				t.Fatalf("streamed content leaked into event metadata: %q", value)
			}
		}
	}
}
