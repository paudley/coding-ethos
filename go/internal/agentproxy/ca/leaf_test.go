// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package ca_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy/ca"
)

func newIssuer(t *testing.T) (*ca.LeafIssuer, *x509.Certificate) {
	t.Helper()

	repoRoot := t.TempDir()

	authority, err := ca.EnsureCA(repoRoot, fixedNow())
	if err != nil {
		t.Fatalf("ensure CA: %v", err)
	}

	issuer, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{Now: fixedNow})
	if err != nil {
		t.Fatalf("new leaf issuer: %v", err)
	}

	caCertDER, err := readCACertDER(authority.CertPath())
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}

	return issuer, caCert
}

func readCACertDER(path string) ([]byte, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("invalid CA cert PEM")
	}

	return block.Bytes, nil
}

func TestMintLeafSubjectAndChain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		wantName string
		isIP     bool
	}{
		{name: "dns host", host: "example.com", wantName: "example.com", isIP: false},
		{
			name:     "host with port",
			host:     "Example.com:8443",
			wantName: "example.com",
			isIP:     false,
		},
		{name: "ip host", host: "192.0.2.10", wantName: "192.0.2.10", isIP: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			issuer, caCert := newIssuer(t)

			cert, err := issuer.MintLeaf(testCase.host, fixedNow())
			if err != nil {
				t.Fatalf("mint leaf: %v", err)
			}

			if len(cert.Certificate) != 2 {
				t.Fatalf("chain length = %d want 2", len(cert.Certificate))
			}

			if cert.Leaf.Subject.CommonName != testCase.wantName {
				t.Fatalf(
					"common name = %q want %q",
					cert.Leaf.Subject.CommonName,
					testCase.wantName,
				)
			}

			assertHostBinding(t, cert.Leaf, testCase.wantName, testCase.isIP)
			assertVerifies(t, cert.Leaf, caCert, testCase.wantName)
		})
	}
}

func assertHostBinding(t *testing.T, leaf *x509.Certificate, host string, isIP bool) {
	t.Helper()

	if isIP {
		if len(leaf.DNSNames) != 0 {
			t.Fatalf("DNSNames = %v want empty for IP host", leaf.DNSNames)
		}

		if len(leaf.IPAddresses) != 1 || leaf.IPAddresses[0].String() != host {
			t.Fatalf("IPAddresses = %v want [%s]", leaf.IPAddresses, host)
		}

		return
	}

	if len(leaf.IPAddresses) != 0 {
		t.Fatalf("IPAddresses = %v want empty for DNS host", leaf.IPAddresses)
	}

	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != host {
		t.Fatalf("DNSNames = %v want [%s]", leaf.DNSNames, host)
	}
}

func assertVerifies(t *testing.T, leaf, caCert *x509.Certificate, host string) {
	t.Helper()

	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	opts := x509.VerifyOptions{
		Roots:       pool,
		DNSName:     host,
		CurrentTime: fixedNow().Add(time.Hour),
	}

	if net.ParseIP(host) != nil {
		opts.DNSName = ""
	}

	if _, err := leaf.Verify(opts); err != nil {
		t.Fatalf("leaf verify: %v", err)
	}
}

func TestMintLeafCachesPerHost(t *testing.T) {
	t.Parallel()

	issuer, _ := newIssuer(t)

	first, err := issuer.MintLeaf("example.com", fixedNow())
	if err != nil {
		t.Fatalf("first mint: %v", err)
	}

	second, err := issuer.MintLeaf("example.com", fixedNow())
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}

	if first.Leaf.SerialNumber.Cmp(second.Leaf.SerialNumber) != 0 {
		t.Fatal("expected cached leaf with identical serial for same host")
	}

	other, err := issuer.MintLeaf("other.example", fixedNow())
	if err != nil {
		t.Fatalf("other mint: %v", err)
	}

	if other.Leaf.SerialNumber.Cmp(first.Leaf.SerialNumber) == 0 {
		t.Fatal("expected distinct leaf for different host")
	}
}

func TestNewLeafIssuerWithoutCA(t *testing.T) {
	t.Parallel()

	_, err := ca.NewLeafIssuer(t.TempDir(), ca.LeafIssuerOptions{})
	if !errors.Is(err, ca.ErrCANotProvisioned) {
		t.Fatalf("expected ErrCANotProvisioned, got %v", err)
	}
}

func TestGetCertificateEmptySNI(t *testing.T) {
	t.Parallel()

	issuer, _ := newIssuer(t)

	_, err := issuer.GetCertificate(&tls.ClientHelloInfo{ServerName: ""})
	if !errors.Is(err, ca.ErrEmptyServerName) {
		t.Fatalf("expected ErrEmptyServerName, got %v", err)
	}
}

func TestHandshakeEndToEnd(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	authority, err := ca.EnsureCA(repoRoot, time.Now())
	if err != nil {
		t.Fatalf("ensure CA: %v", err)
	}

	issuer, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{})
	if err != nil {
		t.Fatalf("new leaf issuer: %v", err)
	}

	caCertDER, err := readCACertDER(authority.CertPath())
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}

	serverConn, clientConn := net.Pipe()

	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	const host = "service.internal"

	server := tls.Server(serverConn, &tls.Config{
		GetCertificate: issuer.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	})
	client := tls.Client(clientConn, &tls.Config{
		RootCAs:    pool,
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})

	errCh := make(chan error, 1)

	go func() {
		errCh <- server.Handshake()
	}()

	if err := client.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("server handshake: %v", err)
	}

	state := client.ConnectionState()
	if len(state.PeerCertificates) == 0 ||
		state.PeerCertificates[0].Subject.CommonName != host {
		t.Fatalf("unexpected peer certificate subject: %v", state.PeerCertificates)
	}

	_ = client.Close()
	_ = server.Close()
	_, _ = io.Copy(io.Discard, clientConn)
}
