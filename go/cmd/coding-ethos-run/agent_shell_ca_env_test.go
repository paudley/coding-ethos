// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"
)

// envValue returns the value of the first key= entry in env, or "" with found
// reporting absence so a test can distinguish an unset variable from an empty
// value.
func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix), true
		}
	}

	return "", false
}

func TestAgentShellProcessEnvPreservesHostCAWhenInterceptionDisabled(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/host/trust/ca.pem")
	t.Setenv("REQUESTS_CA_BUNDLE", "/host/trust/bundle.pem")
	t.Setenv("NODE_EXTRA_CA_CERTS", "/host/trust/node.pem")

	env := agentShellProcessEnv(t.TempDir(), "/wrapper/git", "/real/git", "")

	value, found := envValue(env, "SSL_CERT_FILE")
	if !found || value != "/host/trust/ca.pem" {
		t.Fatalf("SSL_CERT_FILE = %q (found=%v), want host value preserved", value, found)
	}

	bundle, found := envValue(env, "REQUESTS_CA_BUNDLE")
	if !found || bundle != "/host/trust/bundle.pem" {
		t.Fatalf("REQUESTS_CA_BUNDLE = %q (found=%v), want preserved", bundle, found)
	}
}

func TestAgentShellProcessEnvReplacesHostCAWhenInterceptionEnabled(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/host/trust/ca.pem")
	t.Setenv("REQUESTS_CA_BUNDLE", "/host/trust/bundle.pem")
	t.Setenv("NODE_EXTRA_CA_CERTS", "/host/trust/node.pem")

	const interceptCA = "/sandbox/intercept-ca.pem"

	env := agentShellProcessEnv(t.TempDir(), "/wrapper/git", "/real/git", interceptCA)

	for _, key := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS"} {
		value, found := envValue(env, key)
		if !found || value != interceptCA {
			t.Fatalf("%s = %q (found=%v), want intercept CA path", key, value, found)
		}
	}
}
