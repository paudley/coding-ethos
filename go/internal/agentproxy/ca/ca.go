// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package ca provisions and loads the opt-in local root certificate authority
// used by the future HTTPS-interception agent proxy. The package owns the CA
// lifecycle on disk and exposes only the certificate path and fingerprint that
// downstream interception wiring needs.
package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	caStorageDir       = ".coding-ethos/cache/agent-proxy-ca"
	caCertFileName     = "ca-cert.pem"
	caKeyFileName      = "ca-key.pem"
	caMetadataFileName = "metadata.json"
	caCommonName       = "coding-ethos local agent-proxy CA"
	caValidityDuration = 90 * 24 * time.Hour
	caSerialBits       = 128
	caDirMode          = 0o700
	caCertMode         = 0o644
	caKeyMode          = 0o600
	caMetadataMode     = 0o600
)

// ErrCANotProvisioned reports that no agent-proxy CA exists on disk yet.
var ErrCANotProvisioned = apperror.StaticError("agent-proxy CA is not provisioned")

// CA is a provisioned local root certificate authority on disk.
type CA struct {
	notBefore   time.Time
	notAfter    time.Time
	certPath    string
	fingerprint string
}

// CertPath returns the absolute path to the public root certificate PEM file.
func (authority CA) CertPath() string {
	return authority.certPath
}

// Fingerprint returns the lowercase hex SHA-256 digest of the DER certificate.
func (authority CA) Fingerprint() string {
	return authority.fingerprint
}

type caMetadata struct {
	Fingerprint string `json:"fingerprint"`
	NotBefore   string `json:"not_before"`
	NotAfter    string `json:"not_after"`
}

func storageDir(repoRoot string) string {
	return filepath.Join(repoRoot, caStorageDir)
}

func certPath(repoRoot string) string {
	return filepath.Join(storageDir(repoRoot), caCertFileName)
}

func keyPath(repoRoot string) string {
	return filepath.Join(storageDir(repoRoot), caKeyFileName)
}

func metadataPath(repoRoot string) string {
	return filepath.Join(storageDir(repoRoot), caMetadataFileName)
}

// EnsureCA loads an existing valid CA or mints a new one when absent or expired.
func EnsureCA(repoRoot string, now time.Time) (CA, error) {
	existing, err := Load(repoRoot)
	if err == nil && now.Before(existing.notAfter) {
		return existing, nil
	}

	return mintCA(repoRoot, now)
}

// Load loads an existing CA from disk and returns ErrCANotProvisioned if absent.
func Load(repoRoot string) (CA, error) {
	certDER, err := readCertDER(certPath(repoRoot))
	if err != nil {
		return CA{}, err
	}

	_, err = os.Stat(keyPath(repoRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return CA{}, ErrCANotProvisioned
		}

		return CA{}, fmt.Errorf("stat agent-proxy CA key: %w", err)
	}

	certificate, err := x509.ParseCertificate(certDER)
	if err != nil {
		return CA{}, fmt.Errorf("parse agent-proxy CA certificate: %w", err)
	}

	return CA{
		certPath:    certPath(repoRoot),
		fingerprint: fingerprintDER(certDER),
		notBefore:   certificate.NotBefore,
		notAfter:    certificate.NotAfter,
	}, nil
}

func readCertDER(path string) ([]byte, error) {
	pemBytes, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCANotProvisioned
		}

		return nil, fmt.Errorf("read agent-proxy CA certificate: %w", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf(
			"decode agent-proxy CA certificate: %w",
			apperror.StaticError("invalid CA certificate PEM"),
		)
	}

	return block.Bytes, nil
}

func mintCA(repoRoot string, now time.Time) (CA, error) {
	dir := storageDir(repoRoot)

	err := os.MkdirAll(dir, caDirMode)
	if err != nil {
		return CA{}, fmt.Errorf("create agent-proxy CA directory: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return CA{}, fmt.Errorf("generate agent-proxy CA key: %w", err)
	}

	certDER, err := mintCertificate(key, now)
	if err != nil {
		return CA{}, err
	}

	err = writeCAArtifacts(repoRoot, key, certDER, now)
	if err != nil {
		return CA{}, err
	}

	return CA{
		certPath:    certPath(repoRoot),
		fingerprint: fingerprintDER(certDER),
		notBefore:   now,
		notAfter:    now.Add(caValidityDuration),
	}, nil
}

func mintCertificate(key *ecdsa.PrivateKey, now time.Time) ([]byte, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), caSerialBits))
	if err != nil {
		return nil, fmt.Errorf("generate agent-proxy CA serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonName},
		NotBefore:             now,
		NotAfter:              now.Add(caValidityDuration),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&key.PublicKey,
		key,
	)
	if err != nil {
		return nil, fmt.Errorf("create agent-proxy CA certificate: %w", err)
	}

	return certDER, nil
}

func writeCAArtifacts(
	repoRoot string,
	key *ecdsa.PrivateKey,
	certDER []byte,
	now time.Time,
) error {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal agent-proxy CA key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	err = os.WriteFile(keyPath(repoRoot), keyPEM, caKeyMode)
	if err != nil {
		return fmt.Errorf("write agent-proxy CA key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	err = os.WriteFile(certPath(repoRoot), certPEM, caCertMode)
	if err != nil {
		return fmt.Errorf("write agent-proxy CA certificate: %w", err)
	}

	return writeCAMetadata(repoRoot, fingerprintDER(certDER), now)
}

func writeCAMetadata(repoRoot, fingerprint string, now time.Time) error {
	metadata := caMetadata{
		Fingerprint: fingerprint,
		NotBefore:   now.Format(time.RFC3339),
		NotAfter:    now.Add(caValidityDuration).Format(time.RFC3339),
	}

	encoded, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agent-proxy CA metadata: %w", err)
	}

	err = os.WriteFile(metadataPath(repoRoot), append(encoded, '\n'), caMetadataMode)
	if err != nil {
		return fmt.Errorf("write agent-proxy CA metadata: %w", err)
	}

	return nil
}

func fingerprintDER(certDER []byte) string {
	digest := sha256.Sum256(certDER)

	return hex.EncodeToString(digest[:])
}
