// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	passThroughDefaultTimeout  = 30 * time.Second
	passThroughPolicyID        = "proxy.pass_through"
	passThroughDecisionAllow   = "allow"
	responseCopyBufferSize     = 32 * 1024
	defaultHopByHopHeaderCount = 8
)

var (
	errPassThroughUpstreamScheme = apperror.StaticError(
		"upstream proxy URL must use http or https",
	)
	errPassThroughUpstreamHost = apperror.StaticError(
		"upstream proxy URL host is required",
	)
)

// EventRecorder stores pass-through routing evidence without retaining
// provider payload bodies.
type EventRecorder interface {
	RecordProxyEvent(ctx context.Context, event ProviderEvent) error
}

// PassThroughOptions configures the baseline Agent API proxy mode.
type PassThroughOptions struct {
	Now      func() time.Time
	Client   *http.Client
	Recorder EventRecorder
	Upstream string
	Provider string
	RepoRoot string
}

// PassThroughProxy forwards HTTP provider traffic without payload mutation.
type PassThroughProxy struct {
	now      func() time.Time
	client   *http.Client
	recorder EventRecorder
	upstream *url.URL
	provider string
	repoRoot string
	sequence atomic.Uint64
}

// NewPassThroughProxy returns a proxy that forwards requests to an upstream
// provider endpoint and records body-free routing evidence.
func NewPassThroughProxy(options PassThroughOptions) (*PassThroughProxy, error) {
	upstream, err := url.Parse(strings.TrimSpace(options.Upstream))
	if err != nil {
		return nil, fmt.Errorf("parse upstream proxy URL: %w", err)
	}

	if upstream.Scheme != "http" && upstream.Scheme != "https" {
		return nil, errPassThroughUpstreamScheme
	}

	if upstream.Host == "" {
		return nil, errPassThroughUpstreamHost
	}

	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: passThroughDefaultTimeout}
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	return &PassThroughProxy{
		now:      now,
		client:   client,
		recorder: options.Recorder,
		upstream: upstream,
		provider: strings.TrimSpace(options.Provider),
		repoRoot: strings.TrimSpace(options.RepoRoot),
	}, nil
}

// ServeHTTP forwards a single HTTP request and preserves upstream status,
// headers, and body.
func (proxy *PassThroughProxy) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	startedAt := proxy.now().UTC()
	upstreamURL := proxy.upstreamURL(request)

	// #nosec G107,G704 -- operator-configured pass-through upstream.
	upstreamRequest, err := http.NewRequestWithContext(
		request.Context(),
		request.Method,
		upstreamURL.String(),
		request.Body,
	)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		proxy.record(request, upstreamURL, startedAt, 0, err)

		return
	}

	copyHeaders(upstreamRequest.Header, request.Header)
	upstreamRequest.Host = proxy.upstream.Host

	response, err := proxy.client.Do(
		upstreamRequest,
	) // #nosec G107,G704 -- target is constrained to explicit upstream base URL.
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		proxy.record(request, upstreamURL, startedAt, 0, err)

		return
	}
	defer response.Body.Close()

	copyHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)

	_, copyErr := copyResponseBody(writer, response.Body)
	if copyErr != nil {
		proxy.record(request, upstreamURL, startedAt, response.StatusCode, copyErr)

		return
	}

	proxy.record(request, upstreamURL, startedAt, response.StatusCode, nil)
}

// ListenAndServe starts the pass-through proxy until the server exits or the
// context is canceled.
func (proxy *PassThroughProxy) ListenAndServe(
	ctx context.Context,
	listenAddress string,
) error {
	server := &http.Server{
		Addr:              strings.TrimSpace(listenAddress),
		Handler:           proxy,
		ReadHeaderTimeout: passThroughDefaultTimeout,
	}

	errs := make(chan error, 1)

	go func() {
		errs <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := passThroughShutdownContext()
		defer cancel()

		err := server.Shutdown(shutdownCtx) //nolint:contextcheck
		if err != nil {
			return fmt.Errorf("shutdown pass-through proxy: %w", err)
		}

		return fmt.Errorf("pass-through proxy context canceled: %w", ctx.Err())
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve pass-through proxy: %w", err)
	}
}

// ListenAndServeOnListener serves a pre-created listener. It is primarily used
// by tests that need the bound address before sending requests.
func (proxy *PassThroughProxy) ListenAndServeOnListener(
	ctx context.Context,
	listener net.Listener,
) error {
	server := &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: passThroughDefaultTimeout,
	}

	errs := make(chan error, 1)

	go func() {
		errs <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := passThroughShutdownContext()
		defer cancel()

		err := server.Shutdown(shutdownCtx) //nolint:contextcheck
		if err != nil {
			return fmt.Errorf("shutdown pass-through proxy: %w", err)
		}

		return fmt.Errorf("pass-through proxy context canceled: %w", ctx.Err())
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve pass-through proxy: %w", err)
	}
}

func (proxy *PassThroughProxy) upstreamURL(request *http.Request) *url.URL {
	target := *proxy.upstream
	target.Path = joinURLPath(proxy.upstream.Path, request.URL.Path)
	target.RawQuery = request.URL.RawQuery

	return &target
}

func (proxy *PassThroughProxy) record(
	request *http.Request,
	upstream *url.URL,
	recordedAt time.Time,
	statusCode int,
	routeErr error,
) {
	if proxy.recorder == nil {
		return
	}

	decision := passThroughDecisionAllow
	if routeErr != nil {
		decision = "route_error"
	}

	metadata := map[string]string{
		"method":                request.Method,
		"payload_body_retained": "false",
		"status_code":           strconv.Itoa(statusCode),
		"upstream_host":         upstream.Host,
		"upstream_scheme":       upstream.Scheme,
	}
	if routeErr != nil {
		metadata["error"] = routeErr.Error()
	}

	sessionID := strings.TrimSpace(request.Header.Get("X-Coding-Ethos-Session"))
	if sessionID == "" {
		sessionID = "agent-api-proxy"
	}

	provider := proxy.provider
	if provider == "" {
		provider = upstream.Hostname()
	}

	event := ProviderEvent{
		ID: passThroughEventID(
			recordedAt,
			request.Method,
			upstream,
			proxy.sequence.Add(1),
		),
		SessionID:     sessionID,
		Kind:          EventProviderCall,
		Provider:      provider,
		RepoRoot:      proxy.repoRoot,
		RecordedAtUTC: recordedAt,
		Direction:     DirectionOutbound,
		PayloadKind:   PayloadPrompt,
		PolicyID:      passThroughPolicyID,
		Decision:      decision,
		Metadata:      metadata,
		Payload: PayloadMeasurement{
			Bytes: contentLengthBytes(request.ContentLength),
		},
	}

	err := proxy.recorder.RecordProxyEvent(request.Context(), event)
	if err != nil && !errors.Is(err, context.Canceled) {
		return
	}
}

func copyHeaders(target, source http.Header) {
	excluded := hopByHopHeaderSet(source)

	for name, values := range source {
		if _, found := excluded[http.CanonicalHeaderKey(name)]; found {
			continue
		}

		for _, value := range values {
			target.Add(name, value)
		}
	}
}

func hopByHopHeaderSet(headers http.Header) map[string]struct{} {
	excluded := make(map[string]struct{}, defaultHopByHopHeaderCount)
	addDefaultHopByHopHeaders(excluded)

	for _, headerValue := range headers.Values("Connection") {
		for name := range strings.SplitSeq(headerValue, ",") {
			canonical := http.CanonicalHeaderKey(strings.TrimSpace(name))
			if canonical != "" {
				excluded[canonical] = struct{}{}
			}
		}
	}

	return excluded
}

func addDefaultHopByHopHeaders(headers map[string]struct{}) {
	headers["Connection"] = struct{}{}
	headers["Keep-Alive"] = struct{}{}
	headers["Proxy-Authenticate"] = struct{}{}
	headers["Proxy-Authorization"] = struct{}{}
	headers["Te"] = struct{}{}
	headers["Trailer"] = struct{}{}
	headers["Transfer-Encoding"] = struct{}{}
	headers["Upgrade"] = struct{}{}
}

func copyResponseBody(writer http.ResponseWriter, body io.Reader) (int64, error) {
	flusher, flushes := writer.(http.Flusher)
	if !flushes {
		written, err := io.Copy(writer, body)
		if err != nil {
			return written, fmt.Errorf("copy proxy response body: %w", err)
		}

		return written, nil
	}

	return copyResponseBodyFlushing(writer, body, flusher)
}

func copyResponseBodyFlushing(
	writer io.Writer,
	body io.Reader,
	flusher http.Flusher,
) (int64, error) {
	buffer := make([]byte, responseCopyBufferSize)

	var written int64

	for {
		count, readErr := body.Read(buffer)
		if count > 0 {
			writeCount, writeErr := writer.Write(buffer[:count])
			written += int64(writeCount)

			flusher.Flush()

			if writeErr != nil {
				return written, fmt.Errorf("write proxy response body: %w", writeErr)
			}

			if writeCount != count {
				return written, io.ErrShortWrite
			}
		}

		switch {
		case errors.Is(readErr, io.EOF):
			return written, nil
		case readErr != nil:
			return written, fmt.Errorf("read proxy response body: %w", readErr)
		}
	}
}

func passThroughShutdownContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.Background(),
		passThroughDefaultTimeout,
	)
}

func joinURLPath(base, path string) string {
	switch {
	case base == "" || base == "/":
		if path == "" {
			return "/"
		}

		return path
	case path == "":
		return base
	case strings.HasSuffix(base, "/") && strings.HasPrefix(path, "/"):
		return base + strings.TrimPrefix(path, "/")
	case !strings.HasSuffix(base, "/") && !strings.HasPrefix(path, "/"):
		return base + "/" + path
	default:
		return base + path
	}
}

func passThroughEventID(
	recordedAt time.Time,
	method string,
	upstream *url.URL,
	sequence uint64,
) string {
	hash := sha256.Sum256([]byte(
		recordedAt.Format(time.RFC3339Nano) + "\n" + method + "\n" +
			upstream.String() + "\n" + strconv.FormatUint(sequence, 10),
	))

	return "agent-api-proxy-" + hex.EncodeToString(hash[:])[:24]
}

func contentLengthBytes(length int64) int {
	if length < 0 {
		return 0
	}

	if length > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}

	return int(length)
}
