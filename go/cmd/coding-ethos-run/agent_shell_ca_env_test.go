// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"slices"
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

func TestAgentShellProcessEnvOverridesHostProxyWhenRoutingEnabled(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://host-proxy.invalid:8080")
	t.Setenv("HTTPS_PROXY", "http://host-proxy.invalid:8080")
	t.Setenv("http_proxy", "http://host-proxy.invalid:8080")
	t.Setenv("https_proxy", "http://host-proxy.invalid:8080")
	t.Setenv(envAgentAPIProxyEnabled, "1")
	t.Setenv(envAgentAPIProxyURL, "http://127.0.0.1:18080")

	env := agentShellProcessEnv(t.TempDir(), "/wrapper/git", "/real/git", "")

	for _, key := range agentShellProxyEnvNames() {
		value, found := envValue(env, key)
		if !found || value != "http://127.0.0.1:18080" {
			t.Fatalf("%s = %q (found=%v), want explicit agent proxy URL", key, value, found)
		}
	}
}

func TestAgentShellEnvBindingsRecordNamesOnly(t *testing.T) {
	t.Setenv(envAgentAPIProxyEnabled, "1")
	t.Setenv(envAgentAPIProxyURL, "http://127.0.0.1:18080")

	got := agentShellEnvBindings("/sandbox/intercept-ca.pem")

	for _, want := range append(agentShellProxyEnvNames(), agentShellCAEnvNames()...) {
		if !slices.Contains(got, want) {
			t.Fatalf("env bindings missing %q: %#v", want, got)
		}
	}

	for _, leaked := range []string{"127.0.0.1", "18080", "/sandbox/intercept-ca.pem"} {
		if strings.Contains(strings.Join(got, "\n"), leaked) {
			t.Fatalf("env bindings leaked value %q: %#v", leaked, got)
		}
	}
}
