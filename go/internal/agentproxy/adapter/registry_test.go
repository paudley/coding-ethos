// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/adapter"
)

func TestDefaultRegistryMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		reqCtx  agentproxy.RequestContext
		adapter string
		matched bool
	}{
		{
			name: "openai host and path",
			reqCtx: agentproxy.RequestContext{
				Host: "api.openai.com",
				Path: "/v1/chat/completions",
			},
			adapter: "openai",
			matched: true,
		},
		{
			name: "anthropic host and path",
			reqCtx: agentproxy.RequestContext{
				Host: "api.anthropic.com",
				Path: "/v1/messages",
			},
			adapter: "anthropic",
			matched: true,
		},
		{
			name: "gemini host and path",
			reqCtx: agentproxy.RequestContext{
				Host: "generativelanguage.googleapis.com",
				Path: "/v1beta/models/gemini-pro:generateContent",
			},
			adapter: "gemini",
			matched: true,
		},
		{
			name:   "unknown provider",
			reqCtx: agentproxy.RequestContext{Host: "example.test"},
		},
	}

	registry := adapter.DefaultRegistry()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, matched := registry.Match(test.reqCtx)
			if matched != test.matched {
				t.Fatalf("matched = %v, want %v", matched, test.matched)
			}

			if !matched {
				return
			}

			if got.Name() != test.adapter {
				t.Fatalf("adapter = %q, want %q", got.Name(), test.adapter)
			}
		})
	}
}

func TestRegistryPrefersHigherSpecificity(t *testing.T) {
	t.Parallel()

	registry := adapter.NewRegistry(adapter.OpenAI{}, adapter.Anthropic{})

	reqCtx := agentproxy.RequestContext{
		Host: "api.openai.com",
		Path: "/v1/chat/completions",
	}

	got, matched := registry.Match(reqCtx)
	if !matched {
		t.Fatal("expected a match")
	}

	if got.Name() != "openai" {
		t.Fatalf("adapter = %q, want openai", got.Name())
	}
}
