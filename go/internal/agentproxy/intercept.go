// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	// interceptDefaultMaxNormalize bounds how many bytes are buffered for
	// structural normalization before a payload is treated as too large.
	interceptDefaultMaxNormalize = 8 << 20
	// interceptReadHeaderTimeout bounds the decrypted request header read.
	interceptReadHeaderTimeout = 30 * time.Second
	// interceptShutdownTimeout bounds graceful listener shutdown.
	interceptShutdownTimeout = 30 * time.Second
	// interceptRecordTimeout bounds a single evidence-record call.
	interceptRecordTimeout = 5 * time.Second
	// interceptPolicyID labels every event emitted by the intercept proxy.
	interceptPolicyID = "proxy.intercept"
	// interceptDecisionAllow marks a successfully routed interception.
	interceptDecisionAllow = "allow"
	// interceptDecisionRouteError marks an upstream routing failure.
	interceptDecisionRouteError = "route_error"
	// interceptOnErrorFailClosed refuses traffic when interception cannot run.
	interceptOnErrorFailClosed = "fail_closed"
	// interceptOnErrorPassthrough tunnels traffic when interception cannot run.
	interceptOnErrorPassthrough = "passthrough"
	// interceptDefaultSessionID labels traffic with no session header.
	interceptDefaultSessionID = "agent-api-proxy"
	// interceptSessionHeader carries the caller-supplied session identifier.
	interceptSessionHeader = "X-Coding-Ethos-Session"
	// interceptEventIDPrefix prefixes every generated event identifier.
	interceptEventIDPrefix = "agent-api-proxy-"
	// interceptEventIDHexLen bounds the hex slice used in event identifiers.
	interceptEventIDHexLen = 24
	// metaIntercepted records whether interception terminated the connection.
	metaIntercepted = "intercepted"
	// metaHost records the upstream host an event targeted.
	metaHost = "host"
	// metaReason records why a connection was tunneled instead of intercepted.
	metaReason = "reason"
	// metaNormalized records whether an adapter normalized the payload.
	metaNormalized = "normalized"
	// metaNormalizationError records an adapter normalization failure.
	metaNormalizationError = "normalization_error"
	// metaPayloadTooLarge records that a payload exceeded the normalize bound.
	metaPayloadTooLarge = "payload_too_large_for_normalization"
	// metaError records a sanitized routing error class.
	metaError = "error"
	// metaAuthorityMismatch flags a decrypted request whose Host authority did
	// not match the pinned CONNECT authority the allow-list decision used.
	metaAuthorityMismatch = "authority_mismatch"
	// reasonHostNotAllowed explains a blind tunnel for an unlisted host.
	reasonHostNotAllowed = "host_not_in_allow_list"
	// reasonInterceptionDisabled explains a blind tunnel while disabled.
	reasonInterceptionDisabled = "interception_disabled"
	// reasonInterceptUnavailable explains an on-error passthrough tunnel taken
	// because a leaf certificate could not be minted for the host.
	reasonInterceptUnavailable = "intercept_unavailable"
	// dlpLargePayload marks a payload that bypassed structural normalization.
	dlpLargePayload = "large_payload"
	// routeFailedErrorClass is the sanitized, non-sensitive error class reported
	// for any upstream routing failure across the proxy modes.
	routeFailedErrorClass = "proxy_route_failed"
)

// errInterceptIssuerRequired reports that interception was requested without a
// leaf issuer to terminate TLS.
var errInterceptIssuerRequired = apperror.StaticError(
	"intercept proxy requires a leaf issuer when enabled",
)

// errInterceptRegistryRequired reports that the proxy was built without an
// adapter registry, which it always needs to classify intercepted traffic.
var errInterceptRegistryRequired = apperror.StaticError(
	"intercept proxy requires an adapter registry",
)

// errInterceptEvaluatorRequired reports that interception was requested without
// a proxy policy evaluator. Enforcement is mandatory when interception is
// enabled, so the proxy fails closed rather than decrypting traffic it cannot
// adjudicate.
var errInterceptEvaluatorRequired = apperror.StaticError(
	"intercept proxy requires a policy evaluator when enabled",
)

// LeafIssuer mints TLS leaf certificates for on-demand interception. It mirrors
// the concrete ca.LeafIssuer surface the proxy depends on; defining it here
// keeps agentproxy free of an import cycle with the ca package and makes the
// dependency substitutable in tests.
type LeafIssuer interface {
	// MintLeaf returns a leaf certificate for host valid at now.
	MintLeaf(host string, now time.Time) (tls.Certificate, error)
	// GetCertificate resolves a leaf for a TLS ClientHello via its SNI.
	GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// InterceptOptions configures the CONNECT TLS-MITM interception proxy.
type InterceptOptions struct {
	Recorder     EventRecorder
	Registry     AdapterRegistry
	Issuer       LeafIssuer
	Evaluator    ProxyPolicyEvaluator
	Now          func() time.Time
	Client       *http.Client
	Provider     string
	RepoRoot     string
	OnError      string
	AllowHosts   []string
	MaxNormalize int64
	Enabled      bool
}

// InterceptProxy terminates CONNECT tunnels, decrypts allow-listed HTTPS
// traffic with a locally minted leaf, forwards the bytes verbatim to the real
// upstream, and records body-free structural evidence. Hosts outside the allow
// list, or all hosts while disabled, are tunneled blindly without decryption.
type InterceptProxy struct {
	recorder     EventRecorder
	registry     AdapterRegistry
	issuer       LeafIssuer
	evaluator    ProxyPolicyEvaluator
	now          func() time.Time
	client       *http.Client
	allowHosts   map[string]struct{}
	provider     string
	repoRoot     string
	onError      string
	maxNormalize int64
	sequence     atomic.Uint64
	enabled      bool
}

// NewInterceptProxy builds an interception proxy from validated options. It
// fails closed when interception is enabled without a leaf issuer, defaults the
// normalization bound and error policy, and lowercases the allow list into a
// set. The returned proxy is always a pointer because it carries a no-copy
// atomic sequence counter.
func NewInterceptProxy(options InterceptOptions) (*InterceptProxy, error) {
	if options.Enabled && options.Issuer == nil {
		return nil, errInterceptIssuerRequired
	}

	if options.Enabled && options.Evaluator == nil {
		return nil, errInterceptEvaluatorRequired
	}

	maxNormalize := options.MaxNormalize
	if maxNormalize <= 0 {
		maxNormalize = interceptDefaultMaxNormalize
	}

	onError := strings.TrimSpace(options.OnError)
	if onError == "" {
		onError = interceptOnErrorFailClosed
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	client := options.Client
	if client == nil {
		client = defaultInterceptHTTPClient()
	}

	registry := options.Registry
	if registry == nil {
		return nil, errInterceptRegistryRequired
	}

	return &InterceptProxy{
		now:          now,
		recorder:     options.Recorder,
		registry:     registry,
		issuer:       options.Issuer,
		evaluator:    options.Evaluator,
		client:       client,
		allowHosts:   buildAllowHosts(options.AllowHosts),
		provider:     strings.TrimSpace(options.Provider),
		repoRoot:     strings.TrimSpace(options.RepoRoot),
		onError:      onError,
		maxNormalize: maxNormalize,
		enabled:      options.Enabled,
	}, nil
}

// ServeHTTP accepts only CONNECT requests; every other method is rejected so
// the proxy never silently mishandles plain HTTP traffic.
func (proxy *InterceptProxy) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if request.Method != http.MethodConnect {
		http.Error(
			writer,
			http.StatusText(http.StatusMethodNotAllowed),
			http.StatusMethodNotAllowed,
		)

		return
	}

	proxy.handleConnect(writer, request)
}

// ListenAndServeOnListener serves CONNECT traffic on a pre-bound listener until
// the listener fails or the context is canceled, then shuts down gracefully.
func (proxy *InterceptProxy) ListenAndServeOnListener(
	ctx context.Context,
	listener net.Listener,
) error {
	server := &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: interceptReadHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errs := make(chan error, 1)

	go func() {
		errs <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			interceptShutdownTimeout,
		)
		defer cancel()

		err := server.Shutdown(shutdownCtx) //nolint:contextcheck
		if err != nil {
			return fmt.Errorf("shutdown intercept proxy: %w", err)
		}

		return fmt.Errorf("intercept proxy context canceled: %w", ctx.Err())
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve intercept proxy: %w", err)
	}
}

// allowed reports whether host is in the allow list. The host must already be
// stripped of any port and lowercased by the caller.
func (proxy *InterceptProxy) allowed(host string) bool {
	_, found := proxy.allowHosts[host]

	return found
}

// identity assembles the caller-owned correlation fields shared by every event
// the proxy records. The event ID is derived from the recording time, host, and
// monotonic sequence so two events at the same instant never collide.
func (proxy *InterceptProxy) identity(
	sessionID string,
	host string,
	recordedAt time.Time,
) EventIdentity {
	provider := proxy.provider
	if provider == "" {
		provider = host
	}

	return EventIdentity{
		RecordedAtUTC: recordedAt,
		ID:            interceptEventID(recordedAt, host, proxy.sequence.Add(1)),
		SessionID:     sessionID,
		Provider:      provider,
		RepoRoot:      proxy.repoRoot,
	}
}

// record stores a single event, ignoring recorder absence and benign context
// cancellation so evidence capture never blocks the data path.
func (proxy *InterceptProxy) record(ctx context.Context, event ProviderEvent) {
	if proxy.recorder == nil {
		return
	}

	recordCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		interceptRecordTimeout,
	)
	defer cancel()

	err := proxy.recorder.RecordProxyEvent(recordCtx, event)
	if err != nil && !errors.Is(err, context.Canceled) {
		return
	}
}

// sessionID extracts the caller session header, falling back to the default
// proxy session label when absent.
func sessionID(header http.Header) string {
	value := strings.TrimSpace(header.Get(interceptSessionHeader))
	if value == "" {
		return interceptDefaultSessionID
	}

	return value
}

// reasonFor explains why a host is tunneled rather than intercepted.
func (proxy *InterceptProxy) reasonFor(host string) string {
	if !proxy.enabled {
		return reasonInterceptionDisabled
	}

	if !proxy.allowed(host) {
		return reasonHostNotAllowed
	}

	return ""
}

// buildAllowHosts lowercases and trims each configured host into a lookup set,
// discarding blank entries.
func buildAllowHosts(hosts []string) map[string]struct{} {
	allow := make(map[string]struct{}, len(hosts))

	for _, host := range hosts {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized != "" {
			allow[normalized] = struct{}{}
		}
	}

	return allow
}

// hostOnly strips any port from an authority, returning the bare host. Values
// without a port are returned unchanged.
func hostOnly(authority string) string {
	authority = strings.TrimSpace(authority)

	host, _, err := net.SplitHostPort(authority)
	if err != nil {
		return strings.ToLower(authority)
	}

	return strings.ToLower(host)
}

// defaultInterceptHTTPClient mirrors defaultPassThroughHTTPClient: it disables
// compression and does NOT follow redirects. A transparent MITM proxy must
// return the upstream response verbatim, including 3xx status and Location
// headers, so the agent follows redirects itself. Following them in the proxy
// would break byte-identity (the client would see 200 instead of 302) and let
// a redirect escape the per-host interception allow list.
func defaultInterceptHTTPClient() *http.Client {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		panic("http.DefaultTransport must be *http.Transport")
	}

	transport := baseTransport.Clone()
	transport.Proxy = nil
	transport.DisableCompression = true

	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: transport,
	}
}

// interceptEventID derives a stable, collision-resistant event identifier from
// the recording time, host, and monotonic sequence number.
func interceptEventID(recordedAt time.Time, host string, sequence uint64) string {
	hash := sha256.Sum256([]byte(
		recordedAt.Format(time.RFC3339Nano) + "\n" + host + "\n" +
			strconv.FormatUint(sequence, 10),
	))

	return interceptEventIDPrefix + hex.EncodeToString(hash[:])[:interceptEventIDHexLen]
}

// safeInterceptError reduces a routing error to a stable, non-sensitive class.
func safeInterceptError(routeErr error) string {
	if routeErr == nil {
		return ""
	}

	return routeFailedErrorClass
}
