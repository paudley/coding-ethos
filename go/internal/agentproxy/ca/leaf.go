// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	defaultMaxLeaves = 256
	defaultLeafTTL   = 24 * time.Hour
	leafBackdateSkew = 5 * time.Minute
	leafRenewalSkew  = 1 * time.Minute
	leafSerialBits   = caSerialBits
)

// ErrEmptyServerName reports that an inbound TLS handshake omitted SNI, leaving
// no host for the leaf issuer to mint a certificate against.
var ErrEmptyServerName = apperror.StaticError(
	"TLS ClientHello carried no server name",
)

// ErrInvalidCAMaterial reports that the loaded CA certificate and signer cannot
// sign leaves: the certificate is not a CA, lacks the certificate-signing key
// usage, or its public key does not match the loaded private key.
var ErrInvalidCAMaterial = apperror.StaticError(
	"agent-proxy CA material cannot sign leaves",
)

// LeafIssuer mints short-lived leaf certificates signed by the local agent-proxy
// CA for on-demand TLS interception. The CA private key is contained entirely
// within this type and is never written back to disk; minted leaves are cached
// in memory only and never persisted.
type LeafIssuer struct {
	signer    *ecdsa.PrivateKey
	caCert    *x509.Certificate
	now       func() time.Time
	cache     map[string]cachedLeaf
	caCertDER []byte
	lru       []string
	maxLeaves int
	leafTTL   time.Duration
	mu        sync.Mutex
}

// cachedLeaf is a minted leaf certificate held in memory with its expiry so the
// issuer can reuse it until shortly before it lapses.
type cachedLeaf struct {
	notAfter time.Time
	cert     tls.Certificate
}

// LeafIssuerOptions configures a LeafIssuer. Zero-valued fields fall back to the
// package defaults: Now defaults to time.Now, MaxLeaves to 256, and LeafTTL to
// 24 hours.
type LeafIssuerOptions struct {
	Now       func() time.Time
	MaxLeaves int
	LeafTTL   time.Duration
}

// NewLeafIssuer loads the agent-proxy CA key and certificate from disk and
// returns an issuer ready to mint leaves. It returns ErrCANotProvisioned when no
// CA exists yet and wraps any parse or read failure with operator context.
func NewLeafIssuer(repoRoot string, options LeafIssuerOptions) (*LeafIssuer, error) {
	signer, err := loadCASigner(repoRoot)
	if err != nil {
		return nil, err
	}

	caCertDER, err := readCertDER(certPath(repoRoot))
	if err != nil {
		return nil, err
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, fmt.Errorf("parse agent-proxy CA certificate: %w", err)
	}

	err = validateCAMaterial(caCert, signer)
	if err != nil {
		return nil, err
	}

	options = options.withDefaults()

	return &LeafIssuer{
		signer:    signer,
		caCert:    caCert,
		now:       options.Now,
		cache:     make(map[string]cachedLeaf),
		caCertDER: caCertDER,
		lru:       make([]string, 0, options.MaxLeaves),
		maxLeaves: options.MaxLeaves,
		leafTTL:   options.LeafTTL,
	}, nil
}

// withDefaults returns a copy of the options with zero-valued fields replaced by
// the package defaults.
func (options LeafIssuerOptions) withDefaults() LeafIssuerOptions {
	if options.Now == nil {
		options.Now = time.Now
	}

	if options.MaxLeaves <= 0 {
		options.MaxLeaves = defaultMaxLeaves
	}

	if options.LeafTTL <= 0 {
		options.LeafTTL = defaultLeafTTL
	}

	return options
}

// validateCAMaterial asserts the loaded certificate can actually issue leaves:
// it must be a CA with the certificate-signing key usage, and its ECDSA public
// key must match the loaded private key so signatures verify against the
// published certificate. Any mismatch returns ErrInvalidCAMaterial.
func validateCAMaterial(caCert *x509.Certificate, signer *ecdsa.PrivateKey) error {
	if !caCert.IsCA {
		return fmt.Errorf("%w: certificate is not a CA", ErrInvalidCAMaterial)
	}

	if caCert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return fmt.Errorf(
			"%w: certificate lacks the cert-sign key usage",
			ErrInvalidCAMaterial,
		)
	}

	certPublicKey, ok := caCert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf(
			"%w: certificate public key is not ECDSA",
			ErrInvalidCAMaterial,
		)
	}

	if !certPublicKey.Equal(&signer.PublicKey) {
		return fmt.Errorf(
			"%w: certificate public key does not match the CA private key",
			ErrInvalidCAMaterial,
		)
	}

	return nil
}

// loadCASigner reads and parses the CA EC private key from disk, returning
// ErrCANotProvisioned when the key file is absent.
func loadCASigner(repoRoot string) (*ecdsa.PrivateKey, error) {
	pemBytes, err := os.ReadFile(filepath.Clean(keyPath(repoRoot)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCANotProvisioned
		}

		return nil, fmt.Errorf("read agent-proxy CA key: %w", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, fmt.Errorf(
			"decode agent-proxy CA key: %w",
			apperror.StaticError("invalid CA key PEM"),
		)
	}

	signer, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse agent-proxy CA key: %w", err)
	}

	return signer, nil
}

// MintLeaf returns a leaf certificate for host, valid at now, reusing a cached
// leaf when one exists and has not entered its renewal window. The returned
// chain carries the leaf first and the CA second so it presents a complete path
// to verifying clients.
func (issuer *LeafIssuer) MintLeaf(
	host string,
	now time.Time,
) (tls.Certificate, error) {
	key, err := normalizeHost(host)
	if err != nil {
		return tls.Certificate{}, err
	}

	issuer.mu.Lock()
	if cert, ok := issuer.cacheGet(key, now); ok {
		issuer.mu.Unlock()

		return cert, nil
	}
	issuer.mu.Unlock()

	// Key generation and signing are expensive and touch only immutable issuer
	// fields, so they run outside the lock to avoid serializing all minting
	// across hosts. The cache is re-checked under the lock afterward.
	cert, notAfter, err := issuer.createLeaf(key, now)
	if err != nil {
		return tls.Certificate{}, err
	}

	issuer.mu.Lock()
	defer issuer.mu.Unlock()

	// A concurrent mint for the same host may have won the race while this
	// goroutine was signing; reuse its cached leaf so the cache holds a single
	// authoritative entry per host.
	if existing, ok := issuer.cacheGet(key, now); ok {
		return existing, nil
	}

	issuer.cachePut(key, cachedLeaf{notAfter: notAfter, cert: cert})

	return cert, nil
}

// GetCertificate adapts MintLeaf to the tls.Config GetCertificate callback,
// minting a leaf for the handshake's SNI server name. It returns
// ErrEmptyServerName when the ClientHello omits SNI; callers that intercept
// connections without SNI must supply a non-empty host through their own wiring.
func (issuer *LeafIssuer) GetCertificate(
	hello *tls.ClientHelloInfo,
) (*tls.Certificate, error) {
	if hello.ServerName == "" {
		return nil, ErrEmptyServerName
	}

	cert, err := issuer.MintLeaf(hello.ServerName, issuer.now())
	if err != nil {
		return nil, err
	}

	return &cert, nil
}

// cacheGet returns a cached leaf for key when present and still outside its
// renewal window, bumping it to the most-recently-used position. The caller must
// hold the issuer mutex.
func (issuer *LeafIssuer) cacheGet(key string, now time.Time) (tls.Certificate, bool) {
	entry, ok := issuer.cache[key]
	if !ok {
		return tls.Certificate{}, false
	}

	if !now.Before(entry.notAfter.Add(-leafRenewalSkew)) {
		return tls.Certificate{}, false
	}

	issuer.touchLRU(key)

	return entry.cert, true
}

// cachePut stores entry under key, records it as most-recently-used, and evicts
// the least-recently-used leaf when the cache exceeds maxLeaves. The caller must
// hold the issuer mutex.
func (issuer *LeafIssuer) cachePut(key string, entry cachedLeaf) {
	if _, ok := issuer.cache[key]; !ok {
		issuer.lru = append(issuer.lru, key)
	} else {
		issuer.touchLRU(key)
	}

	issuer.cache[key] = entry

	if len(issuer.cache) > issuer.maxLeaves {
		evicted := issuer.lru[0]
		issuer.lru = issuer.lru[1:]
		delete(issuer.cache, evicted)
	}
}

// touchLRU moves key to the most-recently-used end of the LRU order. The caller
// must hold the issuer mutex.
func (issuer *LeafIssuer) touchLRU(key string) {
	for index, existing := range issuer.lru {
		if existing == key {
			issuer.lru = append(issuer.lru[:index], issuer.lru[index+1:]...)

			break
		}
	}

	issuer.lru = append(issuer.lru, key)
}

// createLeaf generates a fresh leaf key, signs a leaf certificate for host valid
// at now, and assembles it into a tls.Certificate. It returns the certificate
// and its NotAfter so the caller can cache the expiry.
func (issuer *LeafIssuer) createLeaf(
	host string,
	now time.Time,
) (tls.Certificate, time.Time, error) {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, time.Time{}, fmt.Errorf(
			"generate agent-proxy leaf key: %w",
			err,
		)
	}

	template, err := issuer.buildLeafTemplate(host, now)
	if err != nil {
		return tls.Certificate{}, time.Time{}, err
	}

	leafDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		issuer.caCert,
		&leafKey.PublicKey,
		issuer.signer,
	)
	if err != nil {
		return tls.Certificate{}, time.Time{}, fmt.Errorf(
			"create agent-proxy leaf certificate: %w",
			err,
		)
	}

	cert, err := issuer.assembleTLSCertificate(leafDER, leafKey)
	if err != nil {
		return tls.Certificate{}, time.Time{}, err
	}

	return cert, template.NotAfter, nil
}

// buildLeafTemplate builds the x509 template for a host leaf valid at now,
// setting IPAddresses when host is an IP literal and DNSNames otherwise.
func (issuer *LeafIssuer) buildLeafTemplate(
	host string,
	now time.Time,
) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), leafSerialBits))
	if err != nil {
		return nil, fmt.Errorf("generate agent-proxy leaf serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             now.Add(-leafBackdateSkew),
		NotAfter:              now.Add(issuer.leafTTL),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}

	return template, nil
}

// assembleTLSCertificate parses leafDER and assembles a tls.Certificate with the
// leaf first and the CA certificate second, presetting Leaf to avoid a re-parse
// during the handshake.
func (issuer *LeafIssuer) assembleTLSCertificate(
	leafDER []byte,
	leafKey *ecdsa.PrivateKey,
) (tls.Certificate, error) {
	parsed, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf(
			"parse agent-proxy leaf certificate: %w",
			err,
		)
	}

	return tls.Certificate{
		Certificate: [][]byte{leafDER, issuer.caCertDER},
		PrivateKey:  leafKey,
		Leaf:        parsed,
	}, nil
}

// normalizeHost lowercases host and strips any port, returning the bare host
// suitable as a cache key and certificate subject.
func normalizeHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return "", ErrEmptyServerName
	}

	stripped, _, err := net.SplitHostPort(host)
	if err == nil {
		return stripped, nil
	}

	return host, nil
}
