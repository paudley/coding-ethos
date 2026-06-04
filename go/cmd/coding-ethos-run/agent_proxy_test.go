// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

func TestRunAgentProxyHandlerRequiresSubcommand(t *testing.T) {
	err := runAgentProxyHandler(runtimePaths{Root: t.TempDir()}, nil)
	if !errors.Is(err, errAgentProxyCommandRequired) {
		t.Fatalf("error = %v, want %v", err, errAgentProxyCommandRequired)
	}
}

func TestRunAgentProxyHandlerRejectsUnknownSubcommand(t *testing.T) {
	err := runAgentProxyHandler(runtimePaths{Root: t.TempDir()}, []string{"unknown"})
	if !errors.Is(err, errUnknownAgentProxyCommand) {
		t.Fatalf("error = %v, want %v", err, errUnknownAgentProxyCommand)
	}
}

func TestRunAgentProxyCAStatusReportsDisabled(t *testing.T) {
	t.Setenv(envAgentAPIProxyIntercept, "")

	output := captureRuntimeStdout(t, func() {
		err := runAgentProxyHandler(
			runtimePaths{Root: t.TempDir()},
			[]string{"ca-status"},
		)
		if err != nil {
			t.Fatalf("ca-status: %v", err)
		}
	})

	var evidence agentproxy.InterceptionEvidence

	err := json.Unmarshal([]byte(output), &evidence)
	if err != nil {
		t.Fatalf("unmarshal ca-status evidence: %v (output=%q)", err, output)
	}

	if evidence.Enabled || evidence.Denied {
		t.Fatalf("expected disabled interception evidence, got %#v", evidence)
	}
}

func TestRunAgentProxyPassthroughRequiresUpstream(t *testing.T) {
	err := runAgentProxyHandler(runtimePaths{Root: t.TempDir()}, []string{"passthrough"})
	if !errors.Is(err, errAgentProxyUpstreamRequired) {
		t.Fatalf("error = %v, want %v", err, errAgentProxyUpstreamRequired)
	}
}

func TestRunAgentProxyPassthroughReturnsServeError(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte("ok"))
		},
	))
	defer provider.Close()

	err := runAgentProxyHandler(runtimePaths{Root: t.TempDir()}, []string{
		"passthrough",
		"--upstream",
		provider.URL,
		"--listen",
		"127.0.0.1:notaport",
	})
	if err == nil ||
		!strings.Contains(err.Error(), "listen for pass-through agent proxy") {
		t.Fatalf("error = %v, want pass-through serve error", err)
	}
}
