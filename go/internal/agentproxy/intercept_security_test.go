// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

// newBoundedHarness builds an interception harness whose proxy intercepts the
// upstream host with a configurable normalization byte bound, so truncation
// behavior can be exercised against a small limit.
func newBoundedHarness(
	t *testing.T,
	handler http.HandlerFunc,
	maxNormalize int64,
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

	proxy := buildProxy(t, proxyConfig{
		now:          now,
		issuer:       issuer,
		recorder:     recorder,
		allow:        []string{upstreamHost},
		upstreamPool: upstreamTrustPool(upstream),
		maxNormalize: maxNormalize,
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

// failingIssuer is a LeafIssuer whose mint always fails, exercising the
// canMintLeaf-false branches in handleConnect (fail-closed and passthrough).
type failingIssuer struct{}

func (failingIssuer) MintLeaf(string, time.Time) (tls.Certificate, error) {
	return tls.Certificate{}, errMintAlwaysFails
}

func (failingIssuer) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, errMintAlwaysFails
}

var errMintAlwaysFails = errors.New("mint always fails")

func TestInterceptProxyDisabledBlindTunnels(t *testing.T) {
	t.Parallel()

	handler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"disabled":true}`))
	}

	now := testClock()
	upstream := httptest.NewTLSServer(http.HandlerFunc(handler))
	defer upstream.Close()

	recorder := &recordingRecorder{}
	issuer, _ := newTestIssuer(t, now)

	proxy, err := agentproxy.NewInterceptProxy(agentproxy.InterceptOptions{
		Now:      now,
		Recorder: recorder,
		Registry: registryStub{},
		Issuer:   issuer,
		Enabled:  false,
	})
	if err != nil {
		t.Fatalf("new disabled proxy: %v", err)
	}

	proxyURL, cancel, wait := startProxy(t, proxy)
	defer func() {
		cancel()
		<-wait
	}()

	// A disabled proxy performs end-to-end TLS, so the client trusts the upstream
	// certificate directly.
	client := proxiedClient(connectClient(upstreamTrustPool(upstream)), proxyURL)

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		upstream.URL+"/v1/chat/completions",
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
		t.Fatalf("read body: %v", err)
	}

	if string(body) != `{"disabled":true}` {
		t.Fatalf("disabled tunnel body altered: %q", body)
	}

	events := recorder.snapshot()
	if len(events) != 1 || events[0].Metadata["reason"] != "interception_disabled" {
		t.Fatalf("disabled blind-tunnel evidence = %#v", events)
	}
}

func TestInterceptProxyFailClosedDropsWhenMintFails(t *testing.T) {
	t.Parallel()

	handler := func(http.ResponseWriter, *http.Request) {}

	now := testClock()
	upstream := httptest.NewTLSServer(http.HandlerFunc(handler))
	defer upstream.Close()

	upstreamHost, _, err := net.SplitHostPort(upstream.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host: %v", err)
	}

	proxy, err := agentproxy.NewInterceptProxy(agentproxy.InterceptOptions{
		Now:        now,
		Registry:   registryStub{},
		Issuer:     failingIssuer{},
		Evaluator:  allowEvaluator{},
		AllowHosts: []string{upstreamHost},
		OnError:    "fail_closed",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("new fail-closed proxy: %v", err)
	}

	proxyURL, cancel, wait := startProxy(t, proxy)
	defer func() {
		cancel()
		<-wait
	}()

	client := proxiedClient(connectClient(upstreamTrustPool(upstream)), proxyURL)

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		upstream.URL+"/v1/chat/completions",
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	// With minting impossible and fail-closed policy, the proxy closes the tunnel
	// after the CONNECT acknowledgement, so the client request errors out.
	_, err = client.Do(request)
	if err == nil {
		t.Fatal("expected fail-closed proxy to drop the connection")
	}
}

func TestInterceptProxyRejectsForgedAuthority(t *testing.T) {
	t.Parallel()

	var upstreamHit bool

	handler := func(writer http.ResponseWriter, _ *http.Request) {
		upstreamHit = true

		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"ok":true}`))
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
		strings.NewReader(`{"model":"x"}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	// The CONNECT target stays the allow-listed upstream, but the decrypted
	// request forges a different Host authority. The proxy must refuse rather
	// than route to the forged host.
	request.Host = "evil.example:443"

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusMisdirectedRequest)
	}

	if upstreamHit {
		t.Fatal("forged-authority request reached upstream; allow list was bypassed")
	}

	assertAuthorityMismatchRecorded(t, harness.recorder.snapshot(), host)
}

// assertAuthorityMismatchRecorded verifies a route-error event with the
// authority_mismatch class was recorded for the rejected request and that no
// inbound response event was produced.
func assertAuthorityMismatchRecorded(
	t *testing.T,
	events []agentproxy.ProviderEvent,
	host string,
) {
	t.Helper()

	for _, event := range events {
		if event.Kind == agentproxy.EventProviderResponse {
			t.Fatalf("unexpected inbound event for rejected request: %#v", event)
		}
	}

	for _, event := range events {
		if event.Metadata["error"] != "authority_mismatch" {
			continue
		}

		if event.Decision != "route_error" {
			t.Fatalf("mismatch decision = %q, want route_error", event.Decision)
		}

		if event.Metadata["intercepted"] != "true" {
			t.Fatalf("mismatch event not intercepted: %#v", event)
		}

		if event.Metadata["host"] != host {
			t.Fatalf("mismatch host = %q, want %q", event.Metadata["host"], host)
		}

		return
	}

	t.Fatalf("no authority_mismatch route-error event recorded: %#v", events)
}

func TestInterceptProxyMatchingAuthorityIsAllowed(t *testing.T) {
	t.Parallel()

	handler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"ok":true}`))
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
		t.Fatalf("proxied request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for matching authority", response.StatusCode)
	}
}

func TestInterceptProxyUnmatchedRequestRecordsIntercepted(t *testing.T) {
	t.Parallel()

	handler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("plain body"))
	}

	harness := newInterceptHarness(t, handler, true)
	defer harness.close(t)

	client := proxiedClient(harness.client, harness.proxyURL)
	// A path no adapter matches still terminates TLS and inspects the payload, so
	// the outbound event must be intercepted=true even though it is not
	// normalized.
	target := harness.upstream.URL + "/unmatched/path"

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

	_, _ = io.ReadAll(response.Body)

	assertUnmatchedIntercepted(t, harness.recorder.snapshot())
}

// assertUnmatchedIntercepted verifies the outbound event for an unmatched but
// decrypted request is marked intercepted=true and not normalized.
func assertUnmatchedIntercepted(t *testing.T, events []agentproxy.ProviderEvent) {
	t.Helper()

	for _, event := range events {
		if event.Kind != agentproxy.EventProviderCall {
			continue
		}

		if event.Metadata["intercepted"] != "true" {
			t.Fatalf(
				"unmatched outbound intercepted = %q, want true",
				event.Metadata["intercepted"],
			)
		}

		if event.Metadata["normalized"] != "false" {
			t.Fatalf(
				"unmatched outbound normalized = %q, want false",
				event.Metadata["normalized"],
			)
		}

		return
	}

	t.Fatalf("no outbound event recorded: %#v", events)
}

func TestInterceptProxyRecordsRouteErrorOnUpstreamFailure(t *testing.T) {
	t.Parallel()

	handler := func(http.ResponseWriter, *http.Request) {}

	harness := newInterceptHarness(t, handler, true)

	client := proxiedClient(harness.client, harness.proxyURL)
	target := harness.upstream.URL + "/v1/chat/completions"

	// Closing the upstream before issuing the request forces the proxy's upstream
	// round trip to fail, exercising the 502 route-error path.
	harness.upstream.Close()

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

	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 on upstream failure", response.StatusCode)
	}

	harness.cancel()

	select {
	case <-harness.wait:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not stop after upstream failure")
	}

	assertRouteErrorRecorded(t, harness.recorder.snapshot())
}

// assertRouteErrorRecorded verifies a route_error decision event was recorded.
func assertRouteErrorRecorded(t *testing.T, events []agentproxy.ProviderEvent) {
	t.Helper()

	for _, event := range events {
		if event.Decision == "route_error" {
			return
		}
	}

	t.Fatalf("no route_error event recorded: %#v", events)
}

func TestInterceptProxyTruncatedResponseForwardsBodyExactlyOnce(t *testing.T) {
	t.Parallel()

	// The upstream body is larger than the normalization bound so the proxy must
	// buffer a prefix and stream the remainder. The body must arrive exactly once,
	// byte-identical, with no duplicated prefix.
	const bodySize = 64 * 1024

	body := make([]byte, bodySize)
	for index := range body {
		body[index] = byte('A' + index%26)
	}

	handler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(body)
	}

	harness := newBoundedHarness(t, handler, 1024)
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
		t.Fatalf("proxied request: %v", err)
	}
	defer response.Body.Close()

	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if len(got) != bodySize {
		t.Fatalf("body length = %d, want %d (prefix likely duplicated)", len(got), bodySize)
	}

	if string(got) != string(body) {
		t.Fatal("truncated response body was not byte-identical to the upstream body")
	}
}

func TestInterceptTLSReturnsAfterClientCloses(t *testing.T) {
	// This test inspects the process-global goroutine count, so it must not run
	// in parallel with other tests that would perturb that count.
	handler := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}

	harness := newInterceptHarness(t, handler, true)
	defer harness.close(t)

	target := harness.upstream.URL + "/v1/chat/completions"

	baseline := goroutineCount()

	// Each iteration completes a CONNECT/intercept/close cycle on its own client
	// transport. With the leak, every cycle strands the per-CONNECT serve
	// goroutine and the goroutine count climbs without bound; with the fix the
	// count settles back near the baseline once the connections close.
	for range 5 {
		driveAndClose(t, harness, target)
	}

	if !goroutineSettles(baseline+5, 10*time.Second) {
		t.Fatalf(
			"goroutine count did not settle (baseline %d, now %d); "+
				"per-CONNECT serve goroutine likely leaked",
			baseline,
			goroutineCount(),
		)
	}
}

// driveAndClose performs one proxied request through a dedicated transport and
// then closes that transport's connections, ending the tunnel so the proxy's
// per-CONNECT serve goroutine can return.
func driveAndClose(t *testing.T, harness *interceptHarness, target string) {
	t.Helper()

	client := proxiedClient(harness.client, harness.proxyURL)

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

	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport = %T", client.Transport)
	}

	transport.CloseIdleConnections()
}

// goroutineCount reports the current number of goroutines.
func goroutineCount() int {
	return runtime.NumGoroutine()
}

// goroutineSettles polls until the goroutine count drops to at most want or the
// timeout elapses, reporting whether it settled in time.
func goroutineSettles(want int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if goroutineCount() <= want {
			return true
		}

		time.Sleep(50 * time.Millisecond)
	}

	return goroutineCount() <= want
}
