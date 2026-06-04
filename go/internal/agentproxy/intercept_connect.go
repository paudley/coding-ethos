// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	// connectEstablished is the raw status line returned to a CONNECT client
	// once the tunnel is ready for either blind relay or TLS termination.
	connectEstablished = "HTTP/1.1 200 Connection Established\r\n\r\n"
	// blindDialTimeout bounds the upstream dial for a blind tunnel.
	blindDialTimeout = 30 * time.Second
)

// errSingleConnConsumed signals that a single-connection listener has already
// yielded its one connection, causing Serve to exit cleanly.
var errSingleConnConsumed = apperror.StaticError("single-connection listener exhausted")

// halfCloser is the optional half-close capability of a stream connection. The
// blind tunnel uses it to propagate EOF in one direction without tearing down
// the opposite direction prematurely.
type halfCloser interface {
	CloseWrite() error
}

// handleConnect hijacks the client connection, decides whether to intercept,
// acknowledges the tunnel, and then either terminates TLS or relays bytes
// blindly. The hijacked connection is always closed when this returns.
func (proxy *InterceptProxy) handleConnect(
	writer http.ResponseWriter,
	request *http.Request,
) {
	host := hostOnly(request.Host)
	intercept := proxy.enabled && proxy.allowed(host)

	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		http.Error(
			writer,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)

		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(
			writer,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)

		return
	}

	defer func() { _ = clientConn.Close() }()

	_, err = clientConn.Write([]byte(connectEstablished))
	if err != nil {
		return
	}

	ctx := request.Context()
	if intercept && proxy.canMintLeaf(host) {
		proxy.interceptTLS(ctx, clientConn, host)

		return
	}

	reason := proxy.reasonFor(host)
	if intercept {
		if proxy.onError != interceptOnErrorPassthrough {
			return
		}

		reason = reasonInterceptUnavailable
	}

	proxy.blindTunnel(ctx, clientConn, request.Host, host, reason)
}

// canMintLeaf reports whether the issuer can produce a leaf for host now. It
// performs the mint up front so a CA failure is resolved by the configured
// on-error policy before TLS termination begins, rather than collapsing the
// handshake opaquely.
func (proxy *InterceptProxy) canMintLeaf(host string) bool {
	_, err := proxy.issuer.MintLeaf(host, proxy.now())

	return err == nil
}

// blindTunnel relays bytes between the client and the real upstream without
// decryption, recording one body-free event that marks the connection as not
// intercepted. A dial failure records a route-error event and closes the tunnel.
func (proxy *InterceptProxy) blindTunnel(
	ctx context.Context,
	clientConn net.Conn,
	target string,
	host string,
	reason string,
) {
	dialer := &net.Dialer{Timeout: blindDialTimeout}

	// #nosec G704 -- a CONNECT proxy must dial the client-requested target.
	upstreamConn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		proxy.recordBlind(ctx, host, reason, err)

		return
	}

	defer func() { _ = upstreamConn.Close() }()

	proxy.recordBlind(ctx, host, reason, nil)

	var waitGroup sync.WaitGroup

	waitGroup.Add(2) //nolint:mnd // one copy per tunnel direction.

	go pipeConn(&waitGroup, upstreamConn, clientConn)
	go pipeConn(&waitGroup, clientConn, upstreamConn)

	waitGroup.Wait()
}

// pipeConn copies from source to destination, then half-closes the destination
// so the peer observes EOF without collapsing the reverse direction. Copy and
// close failures are expected when either peer hangs up and carry no actionable
// signal, so they are read and discarded. It always signals completion.
func pipeConn(waitGroup *sync.WaitGroup, destination, source net.Conn) {
	defer waitGroup.Done()

	_, copyErr := io.Copy(destination, source)
	discardTunnelError(copyErr)

	if closer, ok := destination.(halfCloser); ok {
		discardTunnelError(closer.CloseWrite())

		return
	}

	discardTunnelError(destination.Close())
}

// discardTunnelError intentionally consumes a best-effort tunnel teardown error.
// Tunnel copy and half-close errors mean a peer disconnected; the relay can take
// no recovery action, so the error is observed and dropped to satisfy errcheck
// without masking an actionable failure.
func discardTunnelError(_ error) {}

// recordBlind records a single body-free event for a blind tunnel, flagging the
// connection as not intercepted and attaching the tunnel reason or route error.
func (proxy *InterceptProxy) recordBlind(
	ctx context.Context,
	host string,
	reason string,
	routeErr error,
) {
	decision := interceptDecisionAllow
	if routeErr != nil {
		decision = interceptDecisionRouteError
	}

	metadata := map[string]string{
		metaIntercepted:         metaValueFalse,
		metaHost:                host,
		metaReason:              reason,
		metaPayloadBodyRetained: metaValueFalse,
	}
	if routeErr != nil {
		metadata[metaError] = safeInterceptError(routeErr)
	}

	now := proxy.now().UTC()
	identity := proxy.identity(interceptDefaultSessionID, host, now)

	proxy.record(ctx, ProviderEvent{
		RecordedAtUTC: now,
		Metadata:      metadata,
		Kind:          EventProviderCall,
		RepoRoot:      identity.RepoRoot,
		ID:            identity.ID,
		Provider:      identity.Provider,
		PolicyID:      interceptPolicyID,
		Decision:      decision,
		SessionID:     identity.SessionID,
		Direction:     DirectionOutbound,
	})
}

// interceptTLS terminates TLS on the hijacked client connection using a leaf
// minted for host, then serves the decrypted HTTP(S) request through a
// per-CONNECT server. ServeTLS auto-negotiates HTTP/2 and HTTP/1.1 via ALPN; the
// proxy never forces a downgrade.
func (proxy *InterceptProxy) interceptTLS(
	ctx context.Context,
	clientConn net.Conn,
	host string,
) {
	server := &http.Server{
		Handler:           proxy.decryptedHandler(host),
		ReadHeaderTimeout: interceptReadHeaderTimeout,
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: proxy.leafFor(host),
		},
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	listener := newSingleConnListener(clientConn)

	serveErr := server.ServeTLS(listener, "", "")
	discardTunnelError(serveErr)
}

// leafFor returns a GetCertificate callback that mints a leaf for the CONNECT
// host when the ClientHello omits SNI, and otherwise delegates to the issuer's
// SNI-driven lookup.
func (proxy *InterceptProxy) leafFor(
	host string,
) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if hello.ServerName == "" {
			cert, err := proxy.issuer.MintLeaf(host, proxy.now())
			if err != nil {
				return nil, apperror.Wrapf(
					apperror.StaticError("mint intercept leaf"),
					"mint intercept leaf for %s: %v",
					host,
					err,
				)
			}

			return &cert, nil
		}

		cert, err := proxy.issuer.GetCertificate(hello)
		if err != nil {
			return nil, apperror.Wrapf(
				apperror.StaticError("resolve intercept leaf"),
				"resolve intercept leaf for %s: %v",
				hello.ServerName,
				err,
			)
		}

		return cert, nil
	}
}

// singleConnListener is a net.Listener that yields exactly one pre-accepted
// connection and then reports exhaustion so http.Server.Serve exits after the
// connection closes.
type singleConnListener struct {
	conn net.Conn
	done chan struct{}
	once sync.Once
}

// newSingleConnListener wraps an already-accepted connection in a listener that
// returns it on the first Accept and blocks no further connections.
func newSingleConnListener(conn net.Conn) *singleConnListener {
	return &singleConnListener{
		conn: conn,
		done: make(chan struct{}),
	}
}

// Accept returns the wrapped connection once, then blocks until Close and
// returns the exhaustion sentinel so Serve terminates without busy-looping.
func (listener *singleConnListener) Accept() (net.Conn, error) {
	var conn net.Conn

	listener.once.Do(func() {
		conn = listener.conn
	})

	if conn != nil {
		return conn, nil
	}

	<-listener.done

	return nil, errSingleConnConsumed
}

// Close releases any Accept caller waiting for further connections. It never
// closes the wrapped connection, which the CONNECT handler owns.
func (listener *singleConnListener) Close() error {
	listener.once.Do(func() {})

	select {
	case <-listener.done:
	default:
		close(listener.done)
	}

	return nil
}

// Addr reports the wrapped connection's local address for server bookkeeping.
func (listener *singleConnListener) Addr() net.Addr {
	return listener.conn.LocalAddr()
}
