// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

func TestScanRequestSecretDetectors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		payload    string
		wantType   string
		wantReason string
	}{
		{
			name:       "openai key",
			payload:    "token=" + openAIKeyFixture() + " here",
			wantType:   "secret",
			wantReason: "openai_api_key_prefix",
		},
		{
			name:       "aws access key",
			payload:    "key " + awsKeyFixture() + " rest",
			wantType:   "secret",
			wantReason: "aws_access_key_id_prefix",
		},
		{
			name:       "github token",
			payload:    githubTokenFixture(),
			wantType:   "secret",
			wantReason: "github_token_prefix",
		},
		{
			name:       "slack token",
			payload:    slackTokenFixture(),
			wantType:   "secret",
			wantReason: "slack_token_prefix",
		},
		{
			name:       "stripe live key",
			payload:    stripeKeyFixture(),
			wantType:   "secret",
			wantReason: "stripe_live_secret_key_prefix",
		},
		{
			name:       "pem header",
			payload:    pemHeaderFixture(),
			wantType:   "secret",
			wantReason: "pem_private_key_header",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			facts := agentproxy.ScanRequest([]byte(testCase.payload), "")
			if !hasFact(facts, testCase.wantType, testCase.wantReason) {
				t.Fatalf(
					"expected fact type=%q reason=%q in %#v",
					testCase.wantType,
					testCase.wantReason,
					facts,
				)
			}
		})
	}
}

func TestScanRequestNegatives(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload string
		path    string
	}{
		{name: "plain prose", payload: "hello world, no secrets here", path: "notes.txt"},
		{name: "short sk", payload: "sk-short", path: ""},
		{name: "ordinary path", payload: "content", path: "src/main.go"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			facts := agentproxy.ScanRequest([]byte(testCase.payload), testCase.path)
			if len(facts) != 0 {
				t.Fatalf("expected no facts, got %#v", facts)
			}
		})
	}
}

func TestScanRequestPathFacts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		wantType   string
		wantReason string
		wantPath   string
	}{
		{
			name:       "dotenv file",
			path:       "project/.env",
			wantType:   "credential_file",
			wantReason: "credential_file_basename",
			wantPath:   ".env",
		},
		{
			name:       "dotenv variant",
			path:       "project/.env.production",
			wantType:   "credential_file",
			wantReason: "credential_file_basename",
			wantPath:   ".env.production",
		},
		{
			name:       "ssh private key",
			path:       "workdir/.ssh/id_rsa",
			wantType:   "credential_file",
			wantReason: "credential_file_basename",
			wantPath:   "id_rsa",
		},
		{
			name:       "pem file",
			path:       "certs/server.pem",
			wantType:   "credential_file",
			wantReason: "credential_file_basename",
			wantPath:   "server.pem",
		},
		{
			name:       "protected ssh dir",
			path:       "workdir/.ssh/config",
			wantType:   "protected_path",
			wantReason: "protected_path_segment",
			wantPath:   "config",
		},
		{
			name:       "protected secrets dir",
			path:       "app/secrets/token.json",
			wantType:   "protected_path",
			wantReason: "protected_path_segment",
			wantPath:   "token.json",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			facts := agentproxy.ScanRequest([]byte("content"), testCase.path)
			fact, found := findFact(facts, testCase.wantType)
			if !found {
				t.Fatalf("expected fact type=%q in %#v", testCase.wantType, facts)
			}

			if fact.Reason != testCase.wantReason {
				t.Fatalf("reason = %q, want %q", fact.Reason, testCase.wantReason)
			}

			if fact.Path != testCase.wantPath {
				t.Fatalf("path = %q, want %q", fact.Path, testCase.wantPath)
			}
		})
	}
}

func TestScanRequestContentPathFacts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		payload    string
		wantType   string
		wantReason string
	}{
		{
			name:       "ssh path in body",
			payload:    `cat "workdir/.ssh/id_rsa"`,
			wantType:   "protected_path",
			wantReason: "protected_path_content_segment",
		},
		{
			name:       "aws dir in body",
			payload:    "look at app/.aws/config please",
			wantType:   "protected_path",
			wantReason: "protected_path_content_segment",
		},
		{
			name:       "uppercase secrets dir in body",
			payload:    "app/Secrets/token.json",
			wantType:   "protected_path",
			wantReason: "protected_path_content_segment",
		},
		{
			name:       "dotenv path in body",
			payload:    "send project/.env.production over",
			wantType:   "credential_file",
			wantReason: "credential_file_content_path",
		},
		{
			name:       "id_rsa path in body",
			payload:    "key at workdir/id_rsa here",
			wantType:   "credential_file",
			wantReason: "credential_file_content_path",
		},
		{
			name:       "pem path in body",
			payload:    "load certs/server.pem now",
			wantType:   "credential_file",
			wantReason: "credential_file_content_path",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			facts := agentproxy.ScanRequest([]byte(testCase.payload), "")
			if !hasFact(facts, testCase.wantType, testCase.wantReason) {
				t.Fatalf(
					"expected fact type=%q reason=%q in %#v",
					testCase.wantType,
					testCase.wantReason,
					facts,
				)
			}
		})
	}
}

func TestScanRequestContentPathNegatives(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload string
	}{
		{name: "bare env word in prose", payload: "set the .env value to enabled"},
		{name: "non-boundary secrets word", payload: "see mysecrets/notes for info"},
		{name: "ordinary path", payload: "open src/main.go in the editor"},
		{name: "credentials as plain word", payload: "your credentials are valid"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			facts := agentproxy.ScanRequest([]byte(testCase.payload), "")
			for _, fact := range facts {
				if fact.Type == "credential_file" || fact.Type == "protected_path" {
					t.Fatalf("unexpected path fact for prose: %#v", facts)
				}
			}
		})
	}
}

func TestScanRequestPathCaseAndBoundary(t *testing.T) {
	t.Parallel()

	// Upper-cased basename and segment must still match.
	upperFacts := agentproxy.ScanRequest([]byte("x"), "workdir/.SSH/ID_RSA")
	if !hasFact(upperFacts, "credential_file", "credential_file_basename") {
		t.Fatalf("expected case-insensitive credential_file fact: %#v", upperFacts)
	}

	if !hasFact(upperFacts, "protected_path", "protected_path_segment") {
		t.Fatalf("expected case-insensitive protected_path fact: %#v", upperFacts)
	}

	// A non-boundary substring (mysecrets) must not match the secrets segment.
	noMatch := agentproxy.ScanRequest([]byte("x"), "app/mysecrets/data.json")
	if _, found := findFact(noMatch, "protected_path"); found {
		t.Fatalf("mysecrets should not match secrets segment: %#v", noMatch)
	}

	// Backslash Windows-style separators must normalize before basename check.
	winFacts := agentproxy.ScanRequest([]byte("x"), `workdir\.ssh\id_rsa`)
	if !hasFact(winFacts, "credential_file", "credential_file_basename") {
		t.Fatalf("expected backslash-normalized credential_file fact: %#v", winFacts)
	}
}

func TestScanRequestBinaryPayload(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		wantReason string
		payload    []byte
	}{
		{name: "nul byte", payload: []byte("abc\x00def"), wantReason: "nul_byte"},
		{name: "invalid utf8", payload: []byte{0xff, 0xfe, 0x41}, wantReason: "invalid_utf8"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			facts := agentproxy.ScanRequest(testCase.payload, "")
			if !hasFact(facts, "binary_payload", testCase.wantReason) {
				t.Fatalf("expected binary_payload reason=%q in %#v", testCase.wantReason, facts)
			}
		})
	}
}

func TestScanRequestMultipleFacts(t *testing.T) {
	t.Parallel()

	payload := []byte(awsKeyFixture() + "\x00trailing")

	facts := agentproxy.ScanRequest(payload, "")
	if !hasFact(facts, "secret", "aws_access_key_id_prefix") {
		t.Fatalf("expected aws secret fact in %#v", facts)
	}

	if !hasFact(facts, "binary_payload", "nul_byte") {
		t.Fatalf("expected binary_payload fact in %#v", facts)
	}
}

func TestScanRequestLineColumn(t *testing.T) {
	t.Parallel()

	payload := []byte("line one\nline two\nkey " + awsKeyFixture() + " tail")

	fact, found := findFact(agentproxy.ScanRequest(payload, ""), "secret")
	if !found {
		t.Fatal("expected secret fact")
	}

	if fact.Line != 3 {
		t.Fatalf("line = %d, want 3", fact.Line)
	}

	// "key " is 4 bytes before the match on line 3; column is 1-based.
	if fact.Column != 5 {
		t.Fatalf("column = %d, want 5", fact.Column)
	}
}

// TestScanRequestRetention is the critical security property: no matched secret
// value or payload content may appear in any field of any returned fact, even
// after JSON marshaling.
func TestScanRequestRetention(t *testing.T) {
	t.Parallel()

	secrets := []string{
		awsKeyFixture(),
		openAIKeyFixture(),
		githubTokenFixture(),
	}

	for _, secret := range secrets {
		payload := []byte("prefix " + secret + " suffix")

		facts := agentproxy.ScanRequest(payload, "")
		if len(facts) == 0 {
			t.Fatalf("expected at least one fact for secret-bearing payload")
		}

		assertNoSecretRetained(t, facts, secret)
	}

	// A credential-file path's content must also never leak into facts.
	credentialContent := awsKeyFixture()
	credFacts := agentproxy.ScanRequest(
		[]byte(credentialContent),
		"workdir/.aws/credentials",
	)
	assertNoSecretRetained(t, credFacts, credentialContent)

	// The content path-match detectors must also retain only their label and
	// match location, never the matched path token.
	credentialPathToken := "workdir/.ssh/id_rsa"
	contentPath := "open " + credentialPathToken + " now"
	pathFacts := agentproxy.ScanRequest([]byte(contentPath), "")
	if !hasFact(pathFacts, "credential_file", "credential_file_content_path") {
		t.Fatalf("expected content credential_file fact in %#v", pathFacts)
	}

	if !hasFact(pathFacts, "protected_path", "protected_path_content_segment") {
		t.Fatalf("expected content protected_path fact in %#v", pathFacts)
	}

	assertNoSecretRetained(t, pathFacts, credentialPathToken)
}

func assertNoSecretRetained(t *testing.T, facts []agentproxy.DLPFact, secret string) {
	t.Helper()

	for _, fact := range facts {
		encoded, err := json.Marshal(fact)
		if err != nil {
			t.Fatalf("marshal fact: %v", err)
		}

		if strings.Contains(string(encoded), secret) {
			t.Fatalf("fact leaked secret substring: %s", string(encoded))
		}
	}
}

// pemHeaderFixture builds a PEM private-key header at runtime so the literal
// banned by the repo private-key scanner never appears in source text.
func pemHeaderFixture() string {
	return "-----BEGIN RSA " + "PRIVATE" + " KEY-----\nMII..."
}

// awsKeyFixture builds an AWS-access-key-shaped value at runtime by
// concatenation so no contiguous, scannable token literal is committed to
// source while the AWS detector is still exercised.
func awsKeyFixture() string {
	return "AKIA" + "IOSFODNN7" + "EXAMPLE"
}

// openAIKeyFixture builds an OpenAI-key-shaped value at runtime so no scannable
// token literal is committed to source.
func openAIKeyFixture() string {
	return "sk-" + "abcdef0123456789" + "ABCDEFXYZ0123456789"
}

// githubTokenFixture builds a GitHub-token-shaped value at runtime so no
// scannable token literal is committed to source.
func githubTokenFixture() string {
	return "ghp_" + "0123456789abcdef" + "ABCDEF0123456789abcdef"
}

// slackTokenFixture builds a Slack-token-shaped value at runtime so no scannable
// token literal is committed to source.
func slackTokenFixture() string {
	return "xoxb-" + "0123456789-" + "abcdefXYZ"
}

// stripeKeyFixture builds a Stripe-live-key-shaped value at runtime so no
// scannable token literal is committed to source.
func stripeKeyFixture() string {
	return "sk_live_" + "0123456789" + "abcdefABCDEF"
}

func hasFact(facts []agentproxy.DLPFact, wantType, wantReason string) bool {
	for _, fact := range facts {
		if fact.Type == wantType && fact.Reason == wantReason {
			return true
		}
	}

	return false
}

func findFact(facts []agentproxy.DLPFact, wantType string) (agentproxy.DLPFact, bool) {
	for _, fact := range facts {
		if fact.Type == wantType {
			return fact, true
		}
	}

	return agentproxy.DLPFact{}, false
}
