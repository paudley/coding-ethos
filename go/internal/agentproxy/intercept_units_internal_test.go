// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

// errReader is a reader that fails after returning a fixed prefix, exercising
// readBounded's read-error branch.
type errReader struct {
	prefix []byte
	read   bool
}

func (reader *errReader) Read(buffer []byte) (int, error) {
	if !reader.read {
		reader.read = true
		copied := copy(buffer, reader.prefix)

		return copied, nil
	}

	return 0, errors.New("read failure")
}

func TestReadBoundedNilReader(t *testing.T) {
	t.Parallel()

	buffered, reader, truncated := readBounded(nil, 16)
	if buffered != nil {
		t.Fatalf("buffered = %v, want nil", buffered)
	}

	if truncated {
		t.Fatal("nil reader marked truncated")
	}

	remaining, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read remainder: %v", err)
	}

	if len(remaining) != 0 {
		t.Fatalf("remainder = %v, want empty", remaining)
	}
}

func TestReadBoundedExactFit(t *testing.T) {
	t.Parallel()

	payload := []byte("hello")

	buffered, reader, truncated := readBounded(bytes.NewReader(payload), 16)
	if truncated {
		t.Fatal("payload within bound marked truncated")
	}

	if !bytes.Equal(buffered, payload) {
		t.Fatalf("buffered = %q, want %q", buffered, payload)
	}

	replay, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read replay: %v", err)
	}

	if !bytes.Equal(replay, payload) {
		t.Fatalf("replay = %q, want %q", replay, payload)
	}
}

func TestReadBoundedTruncatedReplaysFullPayload(t *testing.T) {
	t.Parallel()

	payload := []byte("0123456789")

	buffered, reader, truncated := readBounded(bytes.NewReader(payload), 4)
	if !truncated {
		t.Fatal("oversized payload not marked truncated")
	}

	if len(buffered) < 4 {
		t.Fatalf("buffered prefix too short: %q", buffered)
	}

	replay, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read replay: %v", err)
	}

	if !bytes.Equal(replay, payload) {
		t.Fatalf("replay = %q, want full payload %q", replay, payload)
	}
}

func TestReadBoundedReadError(t *testing.T) {
	t.Parallel()

	reader := &errReader{prefix: []byte("abc")}

	buffered, replay, truncated := readBounded(reader, 64)
	if truncated {
		t.Fatal("errored read marked truncated")
	}

	if !bytes.Equal(buffered, []byte("abc")) {
		t.Fatalf("buffered = %q, want prefix", buffered)
	}

	if replay == nil {
		t.Fatal("replay reader is nil after read error")
	}
}

func TestDecodeForAdapterGzip(t *testing.T) {
	t.Parallel()

	original := []byte("decoded gzip payload")

	var buffer bytes.Buffer

	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(original); err != nil {
		t.Fatalf("gzip write: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	decoded := decodeForAdapter(buffer.Bytes(), "gzip", 1024)
	if !bytes.Equal(decoded, original) {
		t.Fatalf("gzip decode = %q, want %q", decoded, original)
	}
}

func TestDecodeForAdapterZlibDeflate(t *testing.T) {
	t.Parallel()

	original := []byte("decoded zlib payload")

	var buffer bytes.Buffer

	writer := zlib.NewWriter(&buffer)
	if _, err := writer.Write(original); err != nil {
		t.Fatalf("zlib write: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	decoded := decodeForAdapter(buffer.Bytes(), "deflate", 1024)
	if !bytes.Equal(decoded, original) {
		t.Fatalf("zlib decode = %q, want %q", decoded, original)
	}
}

func TestDecodeForAdapterRawFlate(t *testing.T) {
	t.Parallel()

	original := []byte("decoded raw flate payload")

	var buffer bytes.Buffer

	writer, err := flate.NewWriter(&buffer, flate.DefaultCompression)
	if err != nil {
		t.Fatalf("flate writer: %v", err)
	}

	if _, err := writer.Write(original); err != nil {
		t.Fatalf("flate write: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("flate close: %v", err)
	}

	decoded := decodeForAdapter(buffer.Bytes(), "deflate", 1024)
	if !bytes.Equal(decoded, original) {
		t.Fatalf("raw flate decode = %q, want %q", decoded, original)
	}
}

func TestDecodeForAdapterFallbacks(t *testing.T) {
	t.Parallel()

	raw := []byte("not actually compressed")

	if got := decodeForAdapter(raw, "identity", 1024); !bytes.Equal(got, raw) {
		t.Fatalf("identity decode = %q, want raw", got)
	}

	if got := decodeForAdapter(raw, "gzip", 1024); !bytes.Equal(got, raw) {
		t.Fatalf("invalid gzip decode = %q, want raw fallback", got)
	}

	if got := decodeForAdapter(raw, "deflate", 1024); !bytes.Equal(got, raw) {
		t.Fatalf("invalid deflate decode = %q, want raw fallback", got)
	}
}

func TestAuthorityMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requestID string
		pinned    string
		want      bool
	}{
		{name: "absent host matches", requestID: "", pinned: "api.host:443", want: true},
		{
			name:      "case-insensitive match",
			requestID: "API.Host:443",
			pinned:    "api.host:443",
			want:      true,
		},
		{
			name:      "mismatch",
			requestID: "evil.example:443",
			pinned:    "api.host:443",
			want:      false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := authorityMatches(
				testCase.requestID,
				testCase.pinned,
			); got != testCase.want {
				t.Fatalf("authorityMatches = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestHostOnlyStripsPort(t *testing.T) {
	t.Parallel()

	if got := hostOnly("API.Example:443"); got != "api.example" {
		t.Fatalf("hostOnly with port = %q", got)
	}

	if got := hostOnly("Bare.Host"); got != "bare.host" {
		t.Fatalf("hostOnly without port = %q", got)
	}
}

func TestBuildAllowHostsNormalizes(t *testing.T) {
	t.Parallel()

	allow := buildAllowHosts([]string{" API.One ", "two.example", "", "  "})
	if len(allow) != 2 {
		t.Fatalf("allow size = %d, want 2", len(allow))
	}

	if _, ok := allow["api.one"]; !ok {
		t.Fatal("expected lowercased trimmed host in allow set")
	}
}

func TestReasonForBranches(t *testing.T) {
	t.Parallel()

	disabled := &InterceptProxy{enabled: false}
	if got := disabled.reasonFor("h"); got != reasonInterceptionDisabled {
		t.Fatalf("disabled reason = %q", got)
	}

	enabled := &InterceptProxy{
		enabled:    true,
		allowHosts: buildAllowHosts([]string{"ok.host"}),
	}
	if got := enabled.reasonFor("other.host"); got != reasonHostNotAllowed {
		t.Fatalf("not-allowed reason = %q", got)
	}

	if got := enabled.reasonFor("ok.host"); got != "" {
		t.Fatalf("allowed reason = %q, want empty", got)
	}
}

func TestSafeInterceptError(t *testing.T) {
	t.Parallel()

	if got := safeInterceptError(nil); got != "" {
		t.Fatalf("nil error class = %q, want empty", got)
	}

	if got := safeInterceptError(errors.New("boom")); got != "proxy_route_failed" {
		t.Fatalf("error class = %q", got)
	}
}

func TestInterceptEventIDStableAndUnique(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000000, 0).UTC()

	first := interceptEventID(now, "host", 1)
	second := interceptEventID(now, "host", 2)

	if first == second {
		t.Fatal("event IDs collided across sequence numbers")
	}

	if interceptEventID(now, "host", 1) != first {
		t.Fatal("event ID not deterministic for identical inputs")
	}
}

func TestSessionIDFallback(t *testing.T) {
	t.Parallel()

	header := map[string][]string{}
	if got := sessionID(header); got != interceptDefaultSessionID {
		t.Fatalf("empty session = %q", got)
	}

	header[interceptSessionHeader] = []string{" session-7 "}
	if got := sessionID(header); got != "session-7" {
		t.Fatalf("trimmed session = %q", got)
	}
}

func TestIdentityFallsBackToHostProvider(t *testing.T) {
	t.Parallel()

	proxy := &InterceptProxy{repoRoot: "/repo"}
	identity := proxy.identity("session", "api.host", time.Now())

	if identity.Provider != "api.host" {
		t.Fatalf("provider fallback = %q, want host", identity.Provider)
	}

	if identity.RepoRoot != "/repo" || identity.SessionID != "session" {
		t.Fatalf("identity fields = %#v", identity)
	}
}

func TestDefaultInterceptHTTPClientNoRedirectNoCompression(t *testing.T) {
	t.Parallel()

	client := defaultInterceptHTTPClient()
	if client.CheckRedirect == nil {
		t.Fatal("redirect policy is nil")
	}

	if err := client.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v, want ErrUseLastResponse", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T", client.Transport)
	}

	if !transport.DisableCompression || transport.Proxy != nil {
		t.Fatalf("transport not hardened: %#v", transport)
	}
}

func TestNewInterceptProxyValidationErrors(t *testing.T) {
	t.Parallel()

	_, err := NewInterceptProxy(InterceptOptions{Enabled: true})
	if !errors.Is(err, errInterceptIssuerRequired) {
		t.Fatalf("missing issuer error = %v", err)
	}

	_, err = NewInterceptProxy(InterceptOptions{Enabled: false})
	if !errors.Is(err, errInterceptRegistryRequired) {
		t.Fatalf("missing registry error = %v", err)
	}
}

// countingRecorder counts recorded events for record-path coverage.
type countingRecorder struct {
	events []ProviderEvent
}

func (recorder *countingRecorder) RecordProxyEvent(
	_ context.Context,
	event ProviderEvent,
) error {
	recorder.events = append(recorder.events, event)

	return nil
}

func TestRecordRouteErrorAndAuthorityMismatch(t *testing.T) {
	t.Parallel()

	recorder := &countingRecorder{}
	proxy := &InterceptProxy{now: time.Now, recorder: recorder}

	proxy.recordRouteError(context.Background(), "api.host", "session")
	proxy.recordAuthorityMismatch(context.Background(), "api.host", "session")

	if len(recorder.events) != 2 {
		t.Fatalf("recorded %d events, want 2", len(recorder.events))
	}

	if recorder.events[0].Metadata["error"] != "proxy_route_failed" {
		t.Fatalf("route error class = %q", recorder.events[0].Metadata["error"])
	}

	if recorder.events[1].Metadata["error"] != metaAuthorityMismatch {
		t.Fatalf("authority mismatch class = %q", recorder.events[1].Metadata["error"])
	}

	for _, event := range recorder.events {
		if event.Decision != interceptDecisionRouteError {
			t.Fatalf("decision = %q, want route_error", event.Decision)
		}
	}
}

func TestRecordBlindRouteError(t *testing.T) {
	t.Parallel()

	recorder := &countingRecorder{}
	proxy := &InterceptProxy{now: time.Now, recorder: recorder}

	proxy.recordBlind(
		context.Background(),
		"api.host",
		reasonHostNotAllowed,
		errors.New("dial failed"),
	)

	if len(recorder.events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(recorder.events))
	}

	event := recorder.events[0]
	if event.Decision != interceptDecisionRouteError {
		t.Fatalf("blind dial-error decision = %q", event.Decision)
	}

	if event.Metadata[metaIntercepted] != metaValueFalse {
		t.Fatalf("blind event intercepted = %q, want false", event.Metadata[metaIntercepted])
	}
}

func TestLargeAndMinimalOutboundEvents(t *testing.T) {
	t.Parallel()

	identity := EventIdentity{RecordedAtUTC: time.Now(), Provider: "p"}
	input := outboundInput{host: "api.host", reqBytes: []byte("payload")}

	large := largeOutboundEvent(identity, input)
	if large.Metadata[metaPayloadTooLarge] != metaValueTrue {
		t.Fatalf("large event missing payload-too-large flag: %#v", large.Metadata)
	}

	if len(large.DLPFacts) != 1 || large.DLPFacts[0].Type != dlpLargePayload {
		t.Fatalf("large event DLP facts = %#v", large.DLPFacts)
	}

	intercepted := minimalOutboundEvent(identity, input, true)
	if intercepted.Metadata[metaIntercepted] != metaValueTrue {
		t.Fatalf("intercepted minimal event flag = %q", intercepted.Metadata[metaIntercepted])
	}

	blind := minimalOutboundEvent(identity, input, false)
	if blind.Metadata[metaIntercepted] != metaValueFalse {
		t.Fatalf("blind minimal event flag = %q", blind.Metadata[metaIntercepted])
	}
}

// stubIssuer is a LeafIssuer whose results are controlled per call so leafFor's
// SNI and empty-SNI branches can be exercised without real TLS.
type stubIssuer struct {
	mintErr error
	getErr  error
}

func (issuer stubIssuer) MintLeaf(string, time.Time) (tls.Certificate, error) {
	if issuer.mintErr != nil {
		return tls.Certificate{}, issuer.mintErr
	}

	return tls.Certificate{}, nil
}

func (issuer stubIssuer) GetCertificate(
	*tls.ClientHelloInfo,
) (*tls.Certificate, error) {
	if issuer.getErr != nil {
		return nil, issuer.getErr
	}

	return &tls.Certificate{}, nil
}

func TestLeafForEmptySNIMintsForConnectHost(t *testing.T) {
	t.Parallel()

	proxy := &InterceptProxy{now: time.Now, issuer: stubIssuer{}}
	callback := proxy.leafFor("api.host")

	cert, err := callback(&tls.ClientHelloInfo{ServerName: ""})
	if err != nil {
		t.Fatalf("empty-SNI mint: %v", err)
	}

	if cert == nil {
		t.Fatal("empty-SNI mint returned nil certificate")
	}
}

func TestLeafForEmptySNIWrapsMintError(t *testing.T) {
	t.Parallel()

	proxy := &InterceptProxy{
		now:    time.Now,
		issuer: stubIssuer{mintErr: errors.New("mint boom")},
	}
	callback := proxy.leafFor("api.host")

	if _, err := callback(&tls.ClientHelloInfo{ServerName: ""}); err == nil {
		t.Fatal("expected wrapped mint error for empty SNI")
	}
}

func TestLeafForSNIDelegatesToGetCertificate(t *testing.T) {
	t.Parallel()

	proxy := &InterceptProxy{now: time.Now, issuer: stubIssuer{}}
	callback := proxy.leafFor("api.host")

	cert, err := callback(&tls.ClientHelloInfo{ServerName: "sni.host"})
	if err != nil {
		t.Fatalf("SNI resolve: %v", err)
	}

	if cert == nil {
		t.Fatal("SNI resolve returned nil certificate")
	}
}

func TestLeafForSNIWrapsResolveError(t *testing.T) {
	t.Parallel()

	proxy := &InterceptProxy{
		now:    time.Now,
		issuer: stubIssuer{getErr: errors.New("resolve boom")},
	}
	callback := proxy.leafFor("api.host")

	if _, err := callback(&tls.ClientHelloInfo{ServerName: "sni.host"}); err == nil {
		t.Fatal("expected wrapped resolve error for SNI handshake")
	}
}

func TestCanMintLeafReflectsIssuer(t *testing.T) {
	t.Parallel()

	ok := &InterceptProxy{now: time.Now, issuer: stubIssuer{}}
	if !ok.canMintLeaf("api.host") {
		t.Fatal("canMintLeaf should be true when issuer succeeds")
	}

	bad := &InterceptProxy{now: time.Now, issuer: stubIssuer{mintErr: errors.New("no")}}
	if bad.canMintLeaf("api.host") {
		t.Fatal("canMintLeaf should be false when issuer fails")
	}
}

func TestNormalizeResponseUnmatchedAndTruncated(t *testing.T) {
	t.Parallel()

	proxy := &InterceptProxy{maxNormalize: 1024}

	truncated := proxy.normalizeResponse(normalizeResponseInput{
		respBytes: []byte("prefix"),
		truncated: true,
	})
	if truncated.Metadata[metaPayloadTooLarge] != metaValueTrue {
		t.Fatalf("truncated normalization missing flag: %#v", truncated.Metadata)
	}

	unmatched := proxy.normalizeResponse(normalizeResponseInput{
		respBytes: []byte("body"),
		matched:   false,
	})
	if unmatched.Metadata[metaNormalized] != metaValueFalse {
		t.Fatalf("unmatched normalization flag = %q", unmatched.Metadata[metaNormalized])
	}

	if unmatched.BodyHash == "" {
		t.Fatal("unmatched normalization missing body hash")
	}
}

// recordingAdapter is a test Adapter whose normalization results are scripted so
// the matched request/response paths can be driven without a real provider. The
// optional gotResponseBody pointer captures the bytes the response normalizer
// received so a test can prove Content-Encoding decoding happened first.
type recordingAdapter struct {
	reqErr          error
	respErr         error
	gotResponseBody *[]byte
}

func (recordingAdapter) Name() string { return "recording" }

func (recordingAdapter) Detect(RequestContext) MatchResult {
	return MatchResult{Matched: true, Specificity: 1}
}

func (adapter recordingAdapter) NormalizeRequest(
	body []byte,
	_ RequestContext,
) (RequestNormalization, error) {
	if adapter.reqErr != nil {
		return RequestNormalization{}, adapter.reqErr
	}

	return RequestNormalization{
		BodyHash:    HashText(string(body)),
		Measurement: Measure(body),
		Metadata:    map[string]string{},
	}, nil
}

func (adapter recordingAdapter) NormalizeResponse(
	body []byte,
	_ ResponseContext,
) (ResponseNormalization, error) {
	if adapter.gotResponseBody != nil {
		*adapter.gotResponseBody = append([]byte(nil), body...)
	}

	if adapter.respErr != nil {
		return ResponseNormalization{}, adapter.respErr
	}

	return ResponseNormalization{
		BodyHash:    HashText(string(body)),
		Measurement: Measure(body),
		Metadata:    map[string]string{},
	}, nil
}

// TestNormalizeStreamedResponseDecodesContentEncoding proves the SSE normalize
// path decompresses the accumulated body by its Content-Encoding before handing
// it to the adapter, so a gzip text/event-stream still reconstructs (FIX 3).
func TestNormalizeStreamedResponseDecodesContentEncoding(t *testing.T) {
	t.Parallel()

	plain := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")

	var buffer bytes.Buffer

	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(plain); err != nil {
		t.Fatalf("gzip write: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	var seen []byte

	proxy := &InterceptProxy{maxNormalize: 1024}
	norm := proxy.normalizeStreamedResponse(streamedNormalizeInput{
		accumulated: buffer.Bytes(),
		encoding:    encodingGzip,
		input: branchInput{
			adapter: recordingAdapter{gotResponseBody: &seen},
			matched: true,
		},
	})

	if !bytes.Equal(seen, plain) {
		t.Fatalf("adapter received %q, want decoded %q", seen, plain)
	}

	if norm.Metadata[metaIntercepted] != metaValueTrue {
		t.Fatalf("decoded stream normalization = %#v", norm.Metadata)
	}
}

// TestNormalizeStreamedResponseSkipsReconstructionOnCopyFailure proves a stream
// whose copy failed mid-flight is recorded as a streamed marker with an error,
// never reconstructed from the incomplete accumulator (FIX 4).
func TestNormalizeStreamedResponseSkipsReconstructionOnCopyFailure(t *testing.T) {
	t.Parallel()

	proxy := &InterceptProxy{maxNormalize: 1024}
	norm := proxy.normalizeStreamedResponse(streamedNormalizeInput{
		accumulated: []byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n",
		),
		input:      branchInput{adapter: recordingAdapter{}, matched: true},
		copyFailed: true,
	})

	if !norm.Streamed {
		t.Fatal("copy-failed stream not marked streamed")
	}

	// streaming_reconstructed is the adapter-side marker; the streamed path must
	// never set it when the copy failed before the body was fully read.
	if norm.Metadata["streaming_reconstructed"] == metaValueTrue {
		t.Fatalf("copy-failed stream reconstructed: %#v", norm.Metadata)
	}

	if norm.Metadata[metaError] == "" {
		t.Fatalf("copy-failed stream missing error marker: %#v", norm.Metadata)
	}
}

// TestNormalizeStreamedResponseMatchedParseFailure proves a matched stream whose
// adapter fails to reconstruct is marked with an explicit normalization error
// plus its body hash and measurement rather than a bare streamed marker (FIX 5).
func TestNormalizeStreamedResponseMatchedParseFailure(t *testing.T) {
	t.Parallel()

	decoded := []byte("data: garbage\n\n")

	proxy := &InterceptProxy{maxNormalize: 1024}
	norm := proxy.normalizeStreamedResponse(streamedNormalizeInput{
		accumulated: decoded,
		input: branchInput{
			adapter: recordingAdapter{respErr: errors.New("parse fail")},
			matched: true,
		},
	})

	if norm.Metadata[metaNormalizationError] != metaValueTrue {
		t.Fatalf("parse failure missing normalization_error: %#v", norm.Metadata)
	}

	if !norm.Streamed {
		t.Fatal("parse failure not marked streamed")
	}

	if norm.BodyHash != HashText(string(decoded)) {
		t.Fatalf("parse failure body hash = %q", norm.BodyHash)
	}

	if norm.Measurement.Bytes != len(decoded) {
		t.Fatalf(
			"parse failure measurement = %d, want %d",
			norm.Measurement.Bytes,
			len(decoded),
		)
	}
}

func TestRecordOutboundMatchedSuccessAndError(t *testing.T) {
	t.Parallel()

	success := &countingRecorder{}
	proxy := &InterceptProxy{now: time.Now, recorder: success, maxNormalize: 1024}
	proxy.recordOutbound(context.Background(), outboundInput{
		host:     "api.host",
		reqBytes: []byte(`{"model":"x"}`),
		adapter:  recordingAdapter{},
		matched:  true,
	})

	if len(success.events) != 1 ||
		success.events[0].Metadata[metaIntercepted] != metaValueTrue {
		t.Fatalf("matched outbound success event = %#v", success.events)
	}

	failed := &countingRecorder{}
	proxyErr := &InterceptProxy{now: time.Now, recorder: failed, maxNormalize: 1024}
	proxyErr.recordOutbound(context.Background(), outboundInput{
		host:     "api.host",
		reqBytes: []byte("bad"),
		adapter:  recordingAdapter{reqErr: errors.New("parse fail")},
		matched:  true,
	})

	if len(failed.events) != 1 ||
		failed.events[0].Metadata[metaNormalizationError] != metaValueTrue {
		t.Fatalf("matched outbound error event = %#v", failed.events)
	}
}

func TestNormalizeResponseMatchedSuccessAndError(t *testing.T) {
	t.Parallel()

	proxy := &InterceptProxy{maxNormalize: 1024}

	ok := proxy.normalizeResponse(normalizeResponseInput{
		respBytes: []byte(`{"ok":true}`),
		adapter:   recordingAdapter{},
		matched:   true,
	})
	if ok.Metadata[metaIntercepted] != metaValueTrue || ok.BodyHash == "" {
		t.Fatalf("matched response normalization = %#v", ok)
	}

	failed := proxy.normalizeResponse(normalizeResponseInput{
		respBytes: []byte("bad"),
		adapter:   recordingAdapter{respErr: errors.New("parse fail")},
		matched:   true,
	})
	if failed.Metadata[metaNormalizationError] != metaValueTrue {
		t.Fatalf("matched response error normalization = %#v", failed)
	}
}
