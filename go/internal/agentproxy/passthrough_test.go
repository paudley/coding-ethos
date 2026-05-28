// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recordingProxyEvents struct {
	events []ProviderEvent
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
