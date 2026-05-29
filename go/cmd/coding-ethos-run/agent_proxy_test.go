// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
