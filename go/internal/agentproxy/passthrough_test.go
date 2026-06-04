// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recordingProxyEvents struct {
	events []ProviderEvent
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func (recorder *recordingProxyEvents) RecordProxyEvent(
	_ context.Context,
	event ProviderEvent,
) error {
	recorder.events = append(recorder.events, event)

	return nil
}

func TestPassThroughProxyPreservesProviderResponse(t *testing.T) {
	t.Parallel()

	provider := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost || request.URL.Path != "/v1/messages" {
				t.Fatalf("unexpected request target: %s %s", request.Method, request.URL)
			}

			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			if string(body) != `{"prompt":"keep exact"}` {
				t.Fatalf("request body mutated: %q", body)
			}

			writer.Header().Set("X-Provider-Fixture", "kept")
			writer.WriteHeader(http.StatusAccepted)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		},
	))
	defer provider.Close()

	recorder := &recordingProxyEvents{}
	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Now: func() time.Time {
			return time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
		},
		Recorder: recorder,
		Upstream: provider.URL,
		Provider: "fixture",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"http://proxy.local/v1/messages?stream=false",
		strings.NewReader(`{"prompt":"keep exact"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Coding-Ethos-Session", "session-1")
	response := httptest.NewRecorder()

	proxy.ServeHTTP(response, request)

	result := response.Result()
	defer result.Body.Close()

	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if result.StatusCode != http.StatusAccepted ||
		result.Header.Get("X-Provider-Fixture") != "kept" ||
		string(body) != `{"ok":true}` {
		t.Fatalf(
			"response not preserved: status=%d header=%q body=%q",
			result.StatusCode,
			result.Header.Get("X-Provider-Fixture"),
			body,
		)
	}

	if len(recorder.events) != 1 {
		t.Fatalf("recorded events = %#v", recorder.events)
	}
	event := recorder.events[0]
	if event.SessionID != "session-1" ||
		event.Kind != EventProviderCall ||
		event.Decision != "allow" ||
		event.Metadata["payload_body_retained"] != "false" ||
		event.Metadata["status_code"] != "202" {
		t.Fatalf("event = %#v", event)
	}
}

func TestPassThroughProxyPreservesRequestContentLength(t *testing.T) {
	t.Parallel()

	provider := httptest.NewServer(http.HandlerFunc(
		func(_ http.ResponseWriter, request *http.Request) {
			if request.ContentLength != int64(len("fixed body")) {
				t.Fatalf("content length = %d", request.ContentLength)
			}
		},
	))
	defer provider.Close()

	proxy, err := NewPassThroughProxy(PassThroughOptions{Upstream: provider.URL})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"http://proxy.local/content-length",
		strings.NewReader("fixed body"),
	)

	proxy.ServeHTTP(httptest.NewRecorder(), request)
}

func TestPassThroughProxyAddsDefaultUpstreamDeadline(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(
			func(request *http.Request) (*http.Response, error) {
				if _, ok := request.Context().Deadline(); !ok {
					t.Fatal("upstream request missing default deadline")
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("ok")),
					Request:    request,
				}, nil
			},
		),
	}

	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Client:   client,
		Upstream: "https://provider.example",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	proxy.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "http://proxy.local/deadline", nil),
	)
}

func TestPassThroughProxyPreservesShorterUpstreamDeadline(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(time.Second)
	client := &http.Client{
		Transport: roundTripFunc(
			func(request *http.Request) (*http.Response, error) {
				got, ok := request.Context().Deadline()
				if !ok || !got.Equal(deadline) {
					t.Fatalf("deadline = %v found=%t, want %v", got, ok, deadline)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("ok")),
					Request:    request,
				}, nil
			},
		),
	}

	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Client:   client,
		Upstream: "https://provider.example",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	request := httptest.NewRequest(http.MethodGet, "http://proxy.local/deadline", nil)
	proxy.ServeHTTP(httptest.NewRecorder(), request.WithContext(ctx))
}

func TestPassThroughProxyPreservesUpstreamFailureStatus(t *testing.T) {
	t.Parallel()

	provider := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			http.Error(writer, "provider failure", http.StatusTeapot)
		},
	))
	defer provider.Close()

	recorder := &recordingProxyEvents{}
	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Recorder: recorder,
		Upstream: provider.URL,
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	response := httptest.NewRecorder()
	proxy.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "http://proxy.local/fail", nil),
	)

	result := response.Result()
	defer result.Body.Close()

	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if result.StatusCode != http.StatusTeapot ||
		!strings.Contains(string(body), "provider failure") {
		t.Fatalf("failure not preserved: status=%d body=%q", result.StatusCode, body)
	}

	if len(recorder.events) != 1 ||
		recorder.events[0].Decision != "allow" ||
		recorder.events[0].Metadata["status_code"] != "418" {
		t.Fatalf("events = %#v", recorder.events)
	}
}

func TestNewPassThroughProxyValidatesUpstream(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		wantErr error
		name    string
		value   string
	}{
		{
			name:    "empty",
			value:   "",
			wantErr: errPassThroughUpstreamHost,
		},
		{
			name:    "scheme",
			value:   "ftp://provider.example",
			wantErr: errPassThroughUpstreamScheme,
		},
		{
			name:    "host",
			value:   "https:///v1/messages",
			wantErr: errPassThroughUpstreamHost,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewPassThroughProxy(PassThroughOptions{
				Upstream: testCase.value,
			})
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestNewPassThroughProxyDefaultClientPreservesProxySemantics(t *testing.T) {
	t.Parallel()

	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Upstream: "https://provider.example",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	transport, ok := proxy.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", proxy.client.Transport)
	}
	if transport.Proxy != nil {
		request := httptest.NewRequest(http.MethodGet, "https://provider.example", nil)
		proxyURL, err := transport.Proxy(request)
		if err != nil {
			t.Fatalf("resolve proxy: %v", err)
		}
		if proxyURL != nil {
			t.Fatalf("default pass-through transport uses proxy %v", proxyURL)
		}
	}
	if !transport.DisableCompression {
		t.Fatal("default pass-through transport should disable transparent compression")
	}
	if proxy.client.Timeout != 0 {
		t.Fatalf(
			"default pass-through client timeout = %s, want request-context deadline",
			proxy.client.Timeout,
		)
	}
	if proxy.client.CheckRedirect == nil {
		t.Fatal("default pass-through client should not follow redirects")
	}
}

func TestPassThroughProxyPreservesRedirectResponses(t *testing.T) {
	t.Parallel()

	provider := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			http.Redirect(
				writer,
				httptest.NewRequest(http.MethodGet, "/", nil),
				"/next",
				http.StatusFound,
			)
		},
	))
	defer provider.Close()

	proxy, err := NewPassThroughProxy(PassThroughOptions{Upstream: provider.URL})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	response := httptest.NewRecorder()
	proxy.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "http://proxy.local/start", nil),
	)

	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusFound ||
		result.Header.Get("Location") != "/next" {
		t.Fatalf(
			"redirect not preserved: status=%d headers=%#v",
			result.StatusCode,
			result.Header,
		)
	}
}

func TestPassThroughProxyPreservesEscapedRequestPath(t *testing.T) {
	t.Parallel()

	provider := httptest.NewServer(http.HandlerFunc(
		func(_ http.ResponseWriter, request *http.Request) {
			if request.RequestURI != "/base/a%2Fb%2Bc?raw=%2F" {
				t.Fatalf("request URI = %q", request.RequestURI)
			}
		},
	))
	defer provider.Close()

	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Upstream: provider.URL + "/base",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	response := httptest.NewRecorder()
	proxy.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "http://proxy.local/a%2Fb%2Bc?raw=%2F", nil),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestPassThroughProxyStripsHopByHopHeaders(t *testing.T) {
	t.Parallel()

	provider := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			for _, header := range []string{
				"Connection",
				"Keep-Alive",
				"Proxy-Authorization",
				"Upgrade",
				"X-Hop-Request",
			} {
				if value := request.Header.Get(header); value != "" {
					t.Fatalf("hop-by-hop request header %s forwarded as %q", header, value)
				}
			}

			writer.Header().Set("Connection", "X-Hop-Response")
			writer.Header().Set("X-Hop-Response", "drop")
			writer.Header().Set("X-End-To-End", "keep")
			_, _ = writer.Write([]byte("ok"))
		},
	))
	defer provider.Close()

	proxy, err := NewPassThroughProxy(PassThroughOptions{Upstream: provider.URL})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://proxy.local/headers", nil)
	request.Header.Set("Connection", "X-Hop-Request")
	request.Header.Set("Keep-Alive", "timeout=5")
	request.Header.Set("Proxy-Authorization", "Bearer token")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("X-Hop-Request", "drop")
	response := httptest.NewRecorder()

	proxy.ServeHTTP(response, request)

	result := response.Result()
	defer result.Body.Close()

	if result.Header.Get("X-End-To-End") != "keep" {
		t.Fatalf("end-to-end response header was not preserved: %#v", result.Header)
	}
	for _, header := range []string{"Connection", "X-Hop-Response"} {
		if value := result.Header.Get(header); value != "" {
			t.Fatalf("hop-by-hop response header %s forwarded as %q", header, value)
		}
	}
}

func TestPassThroughProxyRecordsRouteError(t *testing.T) {
	t.Parallel()

	recorder := &recordingProxyEvents{}
	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Recorder: recorder,
		Upstream: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	response := httptest.NewRecorder()
	proxy.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "http://proxy.local/unreachable", nil),
	)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}
	if body := response.Body.String(); body != "Bad Gateway\n" {
		t.Fatalf("body = %q, want generic Bad Gateway", body)
	}
	if len(recorder.events) != 1 ||
		recorder.events[0].Decision != passThroughDecisionError ||
		recorder.events[0].Metadata["error"] != "Post" {
		t.Fatalf("route error event = %#v", recorder.events)
	}
	if _, found := recorder.events[0].Metadata["status_code"]; found {
		t.Fatalf(
			"route error should not record synthetic status code: %#v",
			recorder.events[0].Metadata,
		)
	}
	if recorder.events[0].PayloadKind != "" {
		t.Fatalf(
			"pass-through event payload kind = %q, want empty baseline classification",
			recorder.events[0].PayloadKind,
		)
	}
}

type contextCheckingProxyEvents struct {
	err    error
	events []ProviderEvent
}

func (recorder *contextCheckingProxyEvents) RecordProxyEvent(
	ctx context.Context,
	event ProviderEvent,
) error {
	recorder.err = ctx.Err()
	recorder.events = append(recorder.events, event)

	return nil
}

func TestPassThroughProxyRecordsEvidenceAfterRequestCancellation(t *testing.T) {
	t.Parallel()

	recorder := &contextCheckingProxyEvents{}
	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Recorder: recorder,
		Upstream: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	request := httptest.NewRequest(http.MethodPost, "http://proxy.local/canceled", nil)
	request = request.WithContext(ctx)

	proxy.ServeHTTP(httptest.NewRecorder(), request)

	if recorder.err != nil {
		t.Fatalf("record context error = %v, want active background context", recorder.err)
	}
	if len(recorder.events) != 1 ||
		recorder.events[0].Decision != passThroughDecisionError {
		t.Fatalf("events = %#v", recorder.events)
	}
}

func TestPassThroughProxyEventIDsAreUniqueForSameTimestampAndTarget(t *testing.T) {
	t.Parallel()

	provider := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte("ok"))
		},
	))
	defer provider.Close()

	recorder := &recordingProxyEvents{}
	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Now: func() time.Time {
			return time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
		},
		Recorder: recorder,
		Upstream: provider.URL,
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	for range 2 {
		response := httptest.NewRecorder()
		proxy.ServeHTTP(
			response,
			httptest.NewRequest(http.MethodPost, "http://proxy.local/same", nil),
		)
	}

	if len(recorder.events) != 2 {
		t.Fatalf("recorded events = %#v", recorder.events)
	}
	if recorder.events[0].ID == recorder.events[1].ID {
		t.Fatalf("duplicate event IDs = %q", recorder.events[0].ID)
	}
}

func TestPassThroughProxyListenAndServeOnListenerStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Upstream: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- proxy.ListenAndServeOnListener(ctx, listener)
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("serve error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop after context cancellation")
	}
}

func TestPassThroughProxyListenAndServeReturnsListenError(t *testing.T) {
	t.Parallel()

	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Upstream: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	err = proxy.ListenAndServe(context.Background(), "127.0.0.1:notaport")
	if err == nil || !strings.Contains(err.Error(), "serve pass-through proxy") {
		t.Fatalf("error = %v, want serve error", err)
	}
}

func TestPassThroughProxyListenAndServeStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	proxy, err := NewPassThroughProxy(PassThroughOptions{
		Upstream: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("create pass-through proxy: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- proxy.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("serve error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop after context cancellation")
	}
}

type flushingResponseWriter struct {
	header  http.Header
	body    strings.Builder
	flushes int
	status  int
}

func newFlushingResponseWriter() *flushingResponseWriter {
	return &flushingResponseWriter{header: http.Header{}}
}

func (writer *flushingResponseWriter) Header() http.Header {
	return writer.header
}

func (writer *flushingResponseWriter) Write(content []byte) (int, error) {
	return writer.body.Write(content)
}

func (writer *flushingResponseWriter) WriteHeader(status int) {
	writer.status = status
}

func (writer *flushingResponseWriter) Flush() {
	writer.flushes++
}

func TestCopyResponseBodyFlushesStreamedWrites(t *testing.T) {
	t.Parallel()

	writer := newFlushingResponseWriter()

	written, err := copyResponseBody(writer, strings.NewReader("stream chunk"))
	if err != nil {
		t.Fatalf("copy response body: %v", err)
	}
	if written != int64(len("stream chunk")) ||
		writer.body.String() != "stream chunk" ||
		writer.flushes == 0 {
		t.Fatalf(
			"copy result written=%d body=%q flushes=%d",
			written,
			writer.body.String(),
			writer.flushes,
		)
	}
}

func TestCopyResponseBodySupportsNonFlushingWriters(t *testing.T) {
	t.Parallel()

	response := newNonFlushingResponseWriter()

	written, err := copyResponseBody(
		response,
		strings.NewReader("plain body"),
	)
	if err != nil {
		t.Fatalf("copy response body: %v", err)
	}
	if written != int64(len("plain body")) || response.body.String() != "plain body" {
		t.Fatalf("copy result written=%d body=%q", written, response.body.String())
	}
}

func TestJoinURLPath(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		base string
		path string
		want string
	}{
		{name: "empty base and path", base: "", path: "", want: "/"},
		{name: "empty path", base: "/v1", path: "", want: "/v1"},
		{name: "trim duplicate slash", base: "/v1/", path: "/messages", want: "/v1/messages"},
		{name: "add missing slash", base: "/v1", path: "messages", want: "/v1/messages"},
		{name: "preserve single slash", base: "/v1", path: "/messages", want: "/v1/messages"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := joinURLPath(testCase.base, testCase.path)
			if got != testCase.want {
				t.Fatalf("joinURLPath(%q, %q) = %q, want %q",
					testCase.base,
					testCase.path,
					got,
					testCase.want,
				)
			}
		})
	}
}

func TestContentLengthBytes(t *testing.T) {
	t.Parallel()

	if contentLengthBytes(-1) != 0 {
		t.Fatal("negative content length should report zero retained bytes")
	}

	if contentLengthBytes(42) != 42 {
		t.Fatal("positive content length should be preserved")
	}
}

type nonFlushingResponseWriter struct {
	header http.Header
	body   strings.Builder
	status int
}

func newNonFlushingResponseWriter() *nonFlushingResponseWriter {
	return &nonFlushingResponseWriter{header: http.Header{}}
}

func (writer *nonFlushingResponseWriter) Header() http.Header {
	return writer.header
}

func (writer *nonFlushingResponseWriter) Write(content []byte) (int, error) {
	return writer.body.Write(content)
}

func (writer *nonFlushingResponseWriter) WriteHeader(status int) {
	writer.status = status
}
