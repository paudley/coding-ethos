// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package ca_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy/ca"
)

// writeCAMaterial overwrites the on-disk CA certificate and key for repoRoot so
// NewLeafIssuer loads the supplied material. The directory is created first so
// the writes never race a missing parent.
func writeCAMaterial(
	t *testing.T,
	repoRoot string,
	certDER []byte,
	key *ecdsa.PrivateKey,
) {
	t.Helper()

	dir := filepath.Join(repoRoot, ".coding-ethos", "cache", "agent-proxy-ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create CA dir: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(filepath.Join(dir, "ca-cert.pem"), certPEM, 0o600); err != nil {
		t.Fatalf("write CA cert: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal CA key: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, "ca-key.pem"), keyPEM, 0o600); err != nil {
		t.Fatalf("write CA key: %v", err)
	}
}

// selfSignedCert mints a self-signed certificate for key using the supplied
// template tweaks, returning its DER bytes.
func selfSignedCert(
	t *testing.T,
	key *ecdsa.PrivateKey,
	isCA bool,
	usage x509.KeyUsage,
) []byte {
	t.Helper()

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "test material CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              usage,
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}

	certDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&key.PublicKey,
		key,
	)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	return certDER
}

func newCAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	return key
}

func TestNewLeafIssuerRejectsNonCACertificate(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	key := newCAKey(t)
	certDER := selfSignedCert(t, key, false, x509.KeyUsageCertSign)
	writeCAMaterial(t, repoRoot, certDER, key)

	_, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{})
	if !errors.Is(err, ca.ErrInvalidCAMaterial) {
		t.Fatalf("expected ErrInvalidCAMaterial for non-CA cert, got %v", err)
	}
}

func TestNewLeafIssuerRejectsMissingCertSignUsage(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	key := newCAKey(t)
	certDER := selfSignedCert(t, key, true, x509.KeyUsageDigitalSignature)
	writeCAMaterial(t, repoRoot, certDER, key)

	_, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{})
	if !errors.Is(err, ca.ErrInvalidCAMaterial) {
		t.Fatalf("expected ErrInvalidCAMaterial for missing cert-sign usage, got %v", err)
	}
}

func TestNewLeafIssuerRejectsPublicKeyMismatch(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	certKey := newCAKey(t)
	otherKey := newCAKey(t)
	// The certificate carries certKey's public key but the on-disk private key is
	// otherKey, so the signer cannot produce signatures verifiable against the
	// published certificate.
	certDER := selfSignedCert(t, certKey, true, x509.KeyUsageCertSign)
	writeCAMaterial(t, repoRoot, certDER, otherKey)

	_, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{})
	if !errors.Is(err, ca.ErrInvalidCAMaterial) {
		t.Fatalf("expected ErrInvalidCAMaterial for key mismatch, got %v", err)
	}
}

func TestNewLeafIssuerAcceptsValidCAMaterial(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	key := newCAKey(t)
	certDER := selfSignedCert(
		t,
		key,
		true,
		x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature,
	)
	writeCAMaterial(t, repoRoot, certDER, key)

	issuer, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{})
	if err != nil {
		t.Fatalf("expected valid CA material to be accepted, got %v", err)
	}

	if _, err := issuer.MintLeaf("example.com", time.Now()); err != nil {
		t.Fatalf("mint leaf with valid material: %v", err)
	}
}

func TestMintLeafIPHostBindsAddress(t *testing.T) {
	t.Parallel()

	issuer, _ := newIssuer(t)

	cert, err := issuer.MintLeaf("198.51.100.7", fixedNow())
	if err != nil {
		t.Fatalf("mint IP leaf: %v", err)
	}

	if len(cert.Leaf.IPAddresses) != 1 ||
		cert.Leaf.IPAddresses[0].String() != "198.51.100.7" {
		t.Fatalf("IP leaf addresses = %v", cert.Leaf.IPAddresses)
	}

	if net.ParseIP(cert.Leaf.Subject.CommonName) == nil {
		t.Fatalf("IP leaf subject = %q", cert.Leaf.Subject.CommonName)
	}
}

func TestMintLeafRenewsAfterRenewalSkew(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if _, err := ca.EnsureCA(repoRoot, fixedNow()); err != nil {
		t.Fatalf("ensure CA: %v", err)
	}

	issuer, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{
		Now:     fixedNow,
		LeafTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	first, err := issuer.MintLeaf("example.com", fixedNow())
	if err != nil {
		t.Fatalf("first mint: %v", err)
	}

	// Within the validity window the cached leaf is reused (identical serial).
	cached, err := issuer.MintLeaf("example.com", fixedNow().Add(30*time.Minute))
	if err != nil {
		t.Fatalf("cached mint: %v", err)
	}

	if first.Leaf.SerialNumber.Cmp(cached.Leaf.SerialNumber) != 0 {
		t.Fatal("expected cached leaf reuse inside validity window")
	}

	// Inside the renewal-skew window before expiry the cache is bypassed and a
	// fresh leaf with a new serial is minted.
	renewed, err := issuer.MintLeaf("example.com", fixedNow().Add(time.Hour))
	if err != nil {
		t.Fatalf("renewed mint: %v", err)
	}

	if first.Leaf.SerialNumber.Cmp(renewed.Leaf.SerialNumber) == 0 {
		t.Fatal("expected fresh leaf inside renewal-skew window")
	}
}

func TestMintLeafEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if _, err := ca.EnsureCA(repoRoot, fixedNow()); err != nil {
		t.Fatalf("ensure CA: %v", err)
	}

	issuer, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{
		Now:       fixedNow,
		MaxLeaves: 2,
	})
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	first, err := issuer.MintLeaf("a.example", fixedNow())
	if err != nil {
		t.Fatalf("mint a: %v", err)
	}

	if _, err := issuer.MintLeaf("b.example", fixedNow()); err != nil {
		t.Fatalf("mint b: %v", err)
	}

	// Minting a third host exceeds MaxLeaves and evicts the least-recently-used
	// entry (a.example), so re-minting it yields a fresh serial.
	if _, err := issuer.MintLeaf("c.example", fixedNow()); err != nil {
		t.Fatalf("mint c: %v", err)
	}

	reminted, err := issuer.MintLeaf("a.example", fixedNow())
	if err != nil {
		t.Fatalf("re-mint a: %v", err)
	}

	if first.Leaf.SerialNumber.Cmp(reminted.Leaf.SerialNumber) == 0 {
		t.Fatal("expected evicted host to mint a fresh leaf")
	}
}

func TestGetCertificateMintsForSNI(t *testing.T) {
	t.Parallel()

	issuer, _ := newIssuer(t)

	cert, err := issuer.GetCertificate(&tls.ClientHelloInfo{ServerName: "sni.example"})
	if err != nil {
		t.Fatalf("get certificate: %v", err)
	}

	if cert.Leaf.Subject.CommonName != "sni.example" {
		t.Fatalf("SNI leaf subject = %q", cert.Leaf.Subject.CommonName)
	}
}
