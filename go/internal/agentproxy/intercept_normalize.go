// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	// upstreamScheme is the scheme used to reach the real provider after TLS
	// termination of an intercepted CONNECT tunnel.
	upstreamScheme = "https://"
	// contentTypeHeader names the response media-type header.
	contentTypeHeader = "Content-Type"
	// contentEncodingHeader names the body compression header.
	contentEncodingHeader = "Content-Encoding"
	// mediaEventStream marks a Server-Sent Events response.
	mediaEventStream = "text/event-stream"
	// encodingGzip names gzip body compression.
	encodingGzip = "gzip"
	// encodingDeflate names deflate (zlib/raw) body compression.
	encodingDeflate = "deflate"
)

// decryptedHandler builds the HTTP handler that runs for each decrypted request
// on an intercepted CONNECT tunnel. The handler is stateless because HTTP/2
// invokes it concurrently across streams. The pinned CONNECT authority binds
// every decrypted request to the host the allow-list decision was made against.
func (proxy *InterceptProxy) decryptedHandler(host, authority string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		proxy.serveRequest(writer, request, host, authority)
	}
}

// serveRequest forwards a single decrypted request to the upstream verbatim,
// records body-free outbound and inbound evidence, and streams or buffers the
// response back to the client without mutating any byte.
func (proxy *InterceptProxy) serveRequest(
	writer http.ResponseWriter,
	request *http.Request,
	host string,
	authority string,
) {
	if !authorityMatches(request.Host, authority) {
		http.Error(
			writer,
			http.StatusText(http.StatusMisdirectedRequest),
			http.StatusMisdirectedRequest,
		)
		proxy.recordAuthorityMismatch(request.Context(), host, sessionID(request.Header))

		return
	}

	reqBytes, reqBody, reqTruncated := readBounded(request.Body, proxy.maxNormalize)

	upstreamReq, err := proxy.buildUpstreamRequest(request, reqBody, authority)
	if err != nil {
		http.Error(
			writer,
			http.StatusText(http.StatusBadGateway),
			http.StatusBadGateway,
		)
		proxy.recordRouteError(request.Context(), host, sessionID(request.Header))

		return
	}

	reqCtx := RequestContext{
		Method:      request.Method,
		Host:        host,
		Path:        request.URL.Path,
		ContentType: request.Header.Get(contentTypeHeader),
	}
	adapter, matched := proxy.registry.Match(reqCtx)

	encoding := request.Header.Get(contentEncodingHeader)
	outbound := outboundInput{
		host:      host,
		sessionID: sessionID(request.Header),
		reqBytes:  reqBytes,
		decoded:   decodeForAdapter(reqBytes, encoding, proxy.maxNormalize),
		encoding:  encoding,
		reqCtx:    reqCtx,
		adapter:   adapter,
		matched:   matched,
		truncated: reqTruncated,
	}

	if !proxy.enforceOutbound(writer, request, outbound) {
		// enforceOutbound denied the request and recorded the deny event itself;
		// it never reaches the upstream and no allow event is recorded.
		return
	}

	proxy.recordOutbound(request.Context(), outbound)

	// #nosec G704 -- an intercepting proxy forwards to the client's own host.
	response, err := proxy.client.Do(upstreamReq)
	if err != nil {
		http.Error(
			writer,
			http.StatusText(http.StatusBadGateway),
			http.StatusBadGateway,
		)
		proxy.recordRouteError(request.Context(), host, sessionID(request.Header))

		return
	}

	defer func() { _ = response.Body.Close() }()

	proxy.branchResponse(writer, request, response, branchInput{
		host:      host,
		sessionID: sessionID(request.Header),
		adapter:   adapter,
		matched:   matched,
	})
}

// buildUpstreamRequest constructs the verbatim upstream request, preserving the
// original method, request URI, headers (minus hop-by-hop), host, and declared
// content length. The upstream authority is the pinned CONNECT target, never the
// decrypted request's Host, so a forged decrypted Host cannot retarget the
// upstream dial and escape the allow list.
func (proxy *InterceptProxy) buildUpstreamRequest(
	request *http.Request,
	body io.Reader,
	authority string,
) (*http.Request, error) {
	upstreamURL := upstreamScheme + authority + request.URL.RequestURI()

	// #nosec G704 -- the URL host is the client's own CONNECT target.
	upstreamReq, err := http.NewRequestWithContext(
		request.Context(),
		request.Method,
		upstreamURL,
		body,
	)
	if err != nil {
		return nil, fmt.Errorf("build intercept upstream request: %w", err)
	}

	copyHeaders(upstreamReq.Header, request.Header)
	upstreamReq.Host = authority
	upstreamReq.ContentLength = request.ContentLength

	return upstreamReq, nil
}

// branchInput carries the response-handling correlation facts shared by the SSE
// and buffered paths.
type branchInput struct {
	adapter   Adapter
	host      string
	sessionID string
	matched   bool
}

// branchResponse dispatches the upstream response to the streaming SSE path or
// the buffered path based on its content type. Both paths forward bytes verbatim.
func (proxy *InterceptProxy) branchResponse(
	writer http.ResponseWriter,
	request *http.Request,
	response *http.Response,
	input branchInput,
) {
	if strings.Contains(response.Header.Get(contentTypeHeader), mediaEventStream) {
		proxy.streamSSE(writer, request, response, input)

		return
	}

	proxy.bufferAndForward(writer, request, response, input)
}

// streamSSE forwards a Server-Sent Events response verbatim with live flushing
// while teeing a bounded copy of the bytes. After the stream completes it
// reconstructs the accumulated copy into structural facts via the matched
// adapter, falling back to a streamed marker only when reconstruction cannot
// run or the copy failed mid-stream. The tee never alters the client bytes.
func (proxy *InterceptProxy) streamSSE(
	writer http.ResponseWriter,
	request *http.Request,
	response *http.Response,
	input branchInput,
) {
	copyHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)

	accumulator := newBoundedBuffer(proxy.maxNormalize)
	teeReader := io.TeeReader(response.Body, accumulator)

	var copyErr error
	if flusher, ok := writer.(http.Flusher); ok {
		_, copyErr = copyResponseBodyFlushing(writer, teeReader, flusher)
	} else {
		_, copyErr = io.Copy(writer, teeReader)
	}

	discardClientWriteError(copyErr)

	respCtx := ResponseContext{
		ContentType: response.Header.Get(contentTypeHeader),
		StatusCode:  response.StatusCode,
	}
	norm := proxy.normalizeStreamedResponse(streamedNormalizeInput{
		accumulated: accumulator.Bytes(),
		encoding:    response.Header.Get(contentEncodingHeader),
		respCtx:     respCtx,
		input:       input,
		truncated:   accumulator.Truncated(),
		copyFailed:  copyErr != nil,
	})
	proxy.recordInbound(request.Context(), input, norm)
}

// streamedNormalizeInput carries the facts needed to reconstruct an accumulated
// SSE body without retaining raw content beyond the bounded copy.
type streamedNormalizeInput struct {
	accumulated []byte
	encoding    string
	respCtx     ResponseContext
	input       branchInput
	truncated   bool
	copyFailed  bool
}

// normalizeStreamedResponse reconstructs an accumulated SSE body into structural
// facts. A truncated or copy-failed stream is recorded as a streamed marker (too
// large or copy error) because the accumulator is incomplete, so reconstruction
// would emit a misleading result. A matched, complete stream is parsed by its
// adapter after Content-Encoding decoding; a parse failure is recorded as an
// explicit normalization error. An unmatched stream stays a plain marker.
func (proxy *InterceptProxy) normalizeStreamedResponse(
	args streamedNormalizeInput,
) ResponseNormalization {
	if args.truncated {
		return ResponseNormalization{
			Metadata: map[string]string{
				metaIntercepted:     metaValueTrue,
				metaPayloadTooLarge: metaValueTrue,
			},
			Streamed:    true,
			Measurement: Measure(args.accumulated),
		}
	}

	if args.copyFailed {
		return ResponseNormalization{
			Metadata: map[string]string{
				metaIntercepted: metaValueTrue,
				metaError:       routeFailedErrorClass,
			},
			Streamed:    true,
			Measurement: Measure(args.accumulated),
		}
	}

	if !args.input.matched {
		return ResponseNormalization{
			Metadata: map[string]string{metaIntercepted: metaValueTrue},
			Streamed: true,
		}
	}

	decoded := decodeForAdapter(args.accumulated, args.encoding, proxy.maxNormalize)

	norm, err := args.input.adapter.NormalizeResponse(decoded, args.respCtx)
	if err != nil {
		return ResponseNormalization{
			Metadata: map[string]string{
				metaIntercepted:        metaValueTrue,
				metaNormalizationError: metaValueTrue,
			},
			Streamed:    true,
			BodyHash:    HashText(string(decoded)),
			Measurement: Measure(decoded),
		}
	}

	if norm.Metadata == nil {
		norm.Metadata = map[string]string{}
	}

	norm.Metadata[metaIntercepted] = metaValueTrue

	return norm
}

// bufferAndForward buffers up to the normalize bound, forwards every byte
// verbatim (including the unbuffered remainder when truncated), and records an
// inbound event built only from structural facts.
func (proxy *InterceptProxy) bufferAndForward(
	writer http.ResponseWriter,
	request *http.Request,
	response *http.Response,
	input branchInput,
) {
	respBytes, respBody, truncated := readBounded(response.Body, proxy.maxNormalize)

	copyHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)

	// respBody already replays the buffered prefix plus the unread remainder when
	// truncated, so writing respBytes as well would duplicate the prefix and
	// corrupt the body. Forward exactly one source: respBody when truncated,
	// otherwise the fully buffered respBytes.
	if truncated {
		_, copyErr := io.Copy(writer, respBody)
		discardClientWriteError(copyErr)
	} else {
		_, writeErr := writer.Write(respBytes)
		discardClientWriteError(writeErr)
	}

	respCtx := ResponseContext{
		ContentType: response.Header.Get(contentTypeHeader),
		StatusCode:  response.StatusCode,
	}
	norm := proxy.normalizeResponse(normalizeResponseInput{
		respBytes: respBytes,
		encoding:  response.Header.Get(contentEncodingHeader),
		respCtx:   respCtx,
		adapter:   input.adapter,
		matched:   input.matched,
		truncated: truncated,
	})
	proxy.recordInbound(request.Context(), input, norm)
}

// outboundInput carries the facts needed to build a body-free outbound event.
// decoded is the Content-Encoding-decompressed view of reqBytes computed once in
// serveRequest so enforcement and recording inspect identical bytes; it is
// transient and never stored in any recorded event.
type outboundInput struct {
	adapter   Adapter
	reqCtx    RequestContext
	host      string
	sessionID string
	encoding  string
	reqBytes  []byte
	decoded   []byte
	matched   bool
	truncated bool
}

// recordOutbound records the outbound provider-call event. It normalizes the
// buffered request body when an adapter matched and the payload fit the bound;
// otherwise it records a minimal structural event. Raw bytes never leave here.
func (proxy *InterceptProxy) recordOutbound(
	ctx context.Context,
	input outboundInput,
) {
	now := proxy.now().UTC()
	identity := proxy.identity(input.sessionID, input.host, now)

	if input.truncated {
		proxy.record(ctx, largeOutboundEvent(identity, input))

		return
	}

	if !input.matched {
		// TLS was terminated and the payload inspected even without an adapter
		// match, so the event is intercepted (just not normalized). Only the
		// blind-tunnel path records intercepted=false.
		proxy.record(ctx, minimalOutboundEvent(identity, input, true))

		return
	}

	norm, err := input.adapter.NormalizeRequest(input.decoded, input.reqCtx)
	if err != nil {
		event := minimalOutboundEvent(identity, input, true)
		event.Metadata[metaNormalizationError] = metaValueTrue
		proxy.record(ctx, event)

		return
	}

	event := OutboundEvent(identity, norm)
	event.Metadata[metaIntercepted] = metaValueTrue

	if event.InputHash == "" {
		event.InputHash = HashText(string(input.decoded))
	}

	proxy.record(ctx, event)
}

// normalizeResponseInput carries the facts needed to normalize a buffered
// response body without retaining raw content.
type normalizeResponseInput struct {
	adapter   Adapter
	encoding  string
	respBytes []byte
	respCtx   ResponseContext
	matched   bool
	truncated bool
}

// normalizeResponse builds the inbound response normalization, parsing the
// buffered body when an adapter matched and the payload fit the bound. Truncated
// or unmatched payloads yield a minimal structural normalization.
func (proxy *InterceptProxy) normalizeResponse(
	input normalizeResponseInput,
) ResponseNormalization {
	metadata := map[string]string{metaIntercepted: metaValueTrue}

	if input.truncated {
		metadata[metaPayloadTooLarge] = metaValueTrue

		return ResponseNormalization{
			Metadata:    metadata,
			Measurement: Measure(input.respBytes),
		}
	}

	if !input.matched {
		metadata[metaNormalized] = metaValueFalse

		return ResponseNormalization{
			Metadata:    metadata,
			BodyHash:    HashText(string(input.respBytes)),
			Measurement: Measure(input.respBytes),
		}
	}

	decoded := decodeForAdapter(input.respBytes, input.encoding, proxy.maxNormalize)

	norm, err := input.adapter.NormalizeResponse(decoded, input.respCtx)
	if err != nil {
		metadata[metaNormalizationError] = metaValueTrue

		return ResponseNormalization{
			Metadata:    metadata,
			BodyHash:    HashText(string(decoded)),
			Measurement: Measure(decoded),
		}
	}

	if norm.Metadata == nil {
		norm.Metadata = map[string]string{}
	}

	norm.Metadata[metaIntercepted] = metaValueTrue
	if norm.BodyHash == "" {
		norm.BodyHash = HashText(string(decoded))
	}

	return norm
}

// recordInbound records the inbound provider-response event built solely from a
// response normalization, never from raw bytes or headers.
func (proxy *InterceptProxy) recordInbound(
	ctx context.Context,
	input branchInput,
	norm ResponseNormalization,
) {
	now := proxy.now().UTC()
	identity := proxy.identity(input.sessionID, input.host, now)

	proxy.record(ctx, InboundEvent(identity, norm))
}

// recordRouteError records a body-free route-error event for an upstream
// failure on an intercepted request.
func (proxy *InterceptProxy) recordRouteError(
	ctx context.Context,
	host string,
	session string,
) {
	now := proxy.now().UTC()
	identity := proxy.identity(session, host, now)

	proxy.record(ctx, ProviderEvent{
		RecordedAtUTC: now,
		Metadata: map[string]string{
			metaIntercepted:         metaValueTrue,
			metaHost:                host,
			metaError:               safeInterceptError(errInterceptRoute),
			metaPayloadBodyRetained: metaValueFalse,
		},
		Kind:      EventProviderCall,
		RepoRoot:  identity.RepoRoot,
		ID:        identity.ID,
		Provider:  identity.Provider,
		PolicyID:  interceptPolicyID,
		Decision:  interceptDecisionRouteError,
		SessionID: identity.SessionID,
		Direction: DirectionOutbound,
	})
}

// authorityMatches reports whether the decrypted request's Host authority is
// consistent with the pinned CONNECT authority. The comparison is
// case-insensitive, and an absent decrypted Host is treated as matching because
// HTTP/2 may omit :authority for a request whose target host is already implied
// by the connection.
func authorityMatches(requestHost, pinnedAuthority string) bool {
	requestHost = strings.TrimSpace(requestHost)
	if requestHost == "" {
		return true
	}

	return strings.EqualFold(requestHost, strings.TrimSpace(pinnedAuthority))
}

// recordAuthorityMismatch records a body-free route-error event for a decrypted
// request whose Host authority diverged from the pinned CONNECT authority,
// making the rejected allow-list bypass attempt explicit in the evidence trail.
func (proxy *InterceptProxy) recordAuthorityMismatch(
	ctx context.Context,
	host string,
	session string,
) {
	now := proxy.now().UTC()
	identity := proxy.identity(session, host, now)

	proxy.record(ctx, ProviderEvent{
		RecordedAtUTC: now,
		Metadata: map[string]string{
			metaIntercepted:         metaValueTrue,
			metaHost:                host,
			metaError:               metaAuthorityMismatch,
			metaPayloadBodyRetained: metaValueFalse,
		},
		Kind:      EventProviderCall,
		RepoRoot:  identity.RepoRoot,
		ID:        identity.ID,
		Provider:  identity.Provider,
		PolicyID:  interceptPolicyID,
		Decision:  interceptDecisionRouteError,
		SessionID: identity.SessionID,
		Direction: DirectionOutbound,
	})
}

// largeOutboundEvent builds an outbound event for a request that exceeded the
// normalization bound, attaching a large-payload DLP fact so the bypass is
// auditable while still forwarding the full payload upstream.
func largeOutboundEvent(
	identity EventIdentity,
	input outboundInput,
) ProviderEvent {
	return ProviderEvent{
		RecordedAtUTC: identity.RecordedAtUTC,
		Metadata: map[string]string{
			metaIntercepted:         metaValueTrue,
			metaHost:                input.host,
			metaPayloadTooLarge:     metaValueTrue,
			metaPayloadBodyRetained: metaValueFalse,
		},
		Kind:        EventProviderCall,
		RepoRoot:    identity.RepoRoot,
		ID:          identity.ID,
		Provider:    identity.Provider,
		PolicyID:    interceptPolicyID,
		Decision:    interceptDecisionAllow,
		SessionID:   identity.SessionID,
		Direction:   DirectionOutbound,
		PayloadKind: PayloadPrompt,
		DLPFacts:    []DLPFact{{Type: dlpLargePayload}},
		Payload:     Measure(input.reqBytes),
	}
}

// minimalOutboundEvent builds a structural outbound event without adapter
// normalization, recording whether interception terminated the connection.
func minimalOutboundEvent(
	identity EventIdentity,
	input outboundInput,
	intercepted bool,
) ProviderEvent {
	interceptedValue := metaValueFalse
	if intercepted {
		interceptedValue = metaValueTrue
	}

	return ProviderEvent{
		RecordedAtUTC: identity.RecordedAtUTC,
		Metadata: map[string]string{
			metaIntercepted:         interceptedValue,
			metaHost:                input.host,
			metaNormalized:          metaValueFalse,
			metaPayloadBodyRetained: metaValueFalse,
		},
		Kind:        EventProviderCall,
		RepoRoot:    identity.RepoRoot,
		ID:          identity.ID,
		Provider:    identity.Provider,
		PolicyID:    interceptPolicyID,
		Decision:    interceptDecisionAllow,
		SessionID:   identity.SessionID,
		Direction:   DirectionOutbound,
		PayloadKind: PayloadPrompt,
		Payload:     Measure(input.reqBytes),
	}
}

// readBounded reads up to limit bytes from reader into a buffer. When the body
// exceeds limit, truncated is true and the returned reader replays the buffered
// prefix followed by the unread remainder so the full payload still forwards;
// otherwise the reader replays only the buffered bytes. No bytes are dropped.
func readBounded(reader io.Reader, limit int64) ([]byte, io.Reader, bool) {
	if reader == nil {
		return nil, bytes.NewReader(nil), false
	}

	limited := io.LimitReader(reader, limit+1)

	buffered, err := io.ReadAll(limited)
	if err != nil {
		return buffered, io.MultiReader(bytes.NewReader(buffered), reader), false
	}

	if int64(len(buffered)) > limit {
		return buffered, io.MultiReader(bytes.NewReader(buffered), reader), true
	}

	return buffered, bytes.NewReader(buffered), false
}

// boundedBuffer accumulates a bounded copy of streamed bytes for structural
// normalization. It retains at most limit bytes and flags truncation once the
// stream exceeds the bound, so a large stream can never grow memory without
// limit. Write always reports the full input length so an io.TeeReader or
// io.Copy driving the buffer never observes a short write while the buffer
// silently discards the overflow.
type boundedBuffer struct {
	data      []byte
	limit     int64
	truncated bool
}

// newBoundedBuffer returns a boundedBuffer that retains at most limit bytes.
func newBoundedBuffer(limit int64) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

// Write appends bytes up to the retention bound and discards the overflow,
// setting the truncated flag once the bound is reached. It always returns the
// full input length so the driving reader treats every byte as consumed.
func (buffer *boundedBuffer) Write(payload []byte) (int, error) {
	remaining := buffer.limit - int64(len(buffer.data))
	if remaining <= 0 {
		if len(payload) > 0 {
			buffer.truncated = true
		}

		return len(payload), nil
	}

	if int64(len(payload)) > remaining {
		// remaining is in (0, len(payload)] here, so it fits an int index without
		// truncation; the explicit cast keeps the slice bound int-typed and
		// satisfies gosec G115 against the int64 retention counter.
		keep := int(remaining)
		buffer.data = append(buffer.data, payload[:keep]...)
		buffer.truncated = true

		return len(payload), nil
	}

	buffer.data = append(buffer.data, payload...)

	return len(payload), nil
}

// Bytes returns the retained prefix of the streamed body.
func (buffer *boundedBuffer) Bytes() []byte {
	return buffer.data
}

// Truncated reports whether the stream exceeded the retention bound.
func (buffer *boundedBuffer) Truncated() bool {
	return buffer.truncated
}

// decodeForAdapter decompresses raw for structural normalization only, bounding
// the decompressed size with limit to guard against zip bombs. Any decode error
// or unrecognized encoding returns the original raw bytes so the adapter still
// receives content to inspect.
func decodeForAdapter(raw []byte, contentEncoding string, limit int64) []byte {
	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case encodingGzip:
		return decodeGzip(raw, limit)
	case encodingDeflate:
		return decodeDeflate(raw, limit)
	default:
		return raw
	}
}

// decodeGzip inflates gzip-encoded bytes within the size bound, returning raw on
// any error.
func decodeGzip(raw []byte, limit int64) []byte {
	reader, gzipErr := gzip.NewReader(bytes.NewReader(raw))
	if gzipErr != nil {
		return raw
	}

	defer func() { _ = reader.Close() }()

	return readDecoded(reader, raw, limit)
}

// decodeDeflate inflates deflate-encoded bytes, trying zlib framing first and
// falling back to raw flate, returning raw on any error.
func decodeDeflate(raw []byte, limit int64) []byte {
	zlibReader, err := zlib.NewReader(bytes.NewReader(raw))
	if err == nil {
		defer func() { _ = zlibReader.Close() }()

		return readDecoded(zlibReader, raw, limit)
	}

	return decodeFlate(raw, limit)
}

// decodeFlate inflates raw DEFLATE bytes (no zlib framing) within the size
// bound, returning raw on any error.
func decodeFlate(raw []byte, limit int64) []byte {
	flateReader := flate.NewReader(bytes.NewReader(raw))

	defer func() { _ = flateReader.Close() }()

	return readDecoded(flateReader, raw, limit)
}

// readDecoded reads decompressed bytes up to limit, returning the fallback bytes
// when decompression fails.
func readDecoded(reader io.Reader, fallback []byte, limit int64) []byte {
	decoded, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return fallback
	}

	return decoded
}

// discardClientWriteError intentionally consumes a best-effort write to the
// intercepted client. A failed write means the client disconnected; the proxy
// has already captured evidence and cannot recover the connection, so the error
// is observed and dropped to satisfy errcheck without masking real faults.
func discardClientWriteError(_ error) {}

// errInterceptRoute classifies upstream routing failures on intercepted
// requests for stable, non-sensitive error reporting.
var errInterceptRoute = apperror.StaticError("route intercepted request failed")
