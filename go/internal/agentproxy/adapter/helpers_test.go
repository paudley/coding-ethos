// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter_test

import (
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

// loadFixture reads a provider fixture file from testdata and fails the test if
// the file cannot be read. It centralizes fixture access across adapter tests.
func loadFixture(t *testing.T, provider, name string) []byte {
	t.Helper()

	path := filepath.Join("testdata", provider, name)

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	return body
}

// responseExpectation captures the asserted facts of a normalized response so
// the per-provider tests share a single comparison routine.
type responseExpectation struct {
	content   string
	callNames []string
	streamed  bool
	input     int
}

// assertResponse compares a normalized response against the expectation and
// fails the test on the first mismatch.
func assertResponse(
	t *testing.T,
	norm agentproxy.ResponseNormalization,
	want responseExpectation,
) {
	t.Helper()

	if norm.Streamed != want.streamed {
		t.Fatalf("streamed = %v, want %v", norm.Streamed, want.streamed)
	}

	if want.streamed {
		assertStreamed(t, norm)

		return
	}

	if want.content != "" && norm.Messages[0].Content != want.content {
		t.Fatalf("content = %q, want %q", norm.Messages[0].Content, want.content)
	}

	assertCallNames(t, norm.ToolCalls, want.callNames)

	if want.input != 0 && norm.Usage.InputTokens != want.input {
		t.Fatalf("input tokens = %d, want %d", norm.Usage.InputTokens, want.input)
	}
}

// assertStreamed verifies a streamed normalization retains size but no content.
func assertStreamed(t *testing.T, norm agentproxy.ResponseNormalization) {
	t.Helper()

	if len(norm.Messages) != 0 {
		t.Fatalf("streamed messages = %d, want 0", len(norm.Messages))
	}

	if norm.Measurement.Bytes == 0 {
		t.Fatal("streamed measurement bytes = 0")
	}

	if norm.BodyHash == "" {
		t.Fatal("streamed body hash empty")
	}
}

// assertCallNames verifies the normalized tool-call names match the expectation.
func assertCallNames(
	t *testing.T,
	calls []agentproxy.ToolCall,
	want []string,
) {
	t.Helper()

	if len(calls) != len(want) {
		t.Fatalf("tool calls = %d, want %d", len(calls), len(want))
	}

	for index, name := range want {
		if calls[index].Name != name {
			t.Fatalf("call %d name = %q, want %q", index, calls[index].Name, name)
		}

		if calls[index].ArgsHash == "" {
			t.Fatalf("call %d args hash empty", index)
		}
	}
}
