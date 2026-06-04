// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter_test

import (
	"errors"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/adapter"
)

func TestGeminiNormalizeRequestRejectsMalformedFields(t *testing.T) {
	t.Parallel()

	reqCtx := agentproxy.RequestContext{
		Host: "generativelanguage.googleapis.com",
		Path: "/v1/models/gemini-pro:generateContent",
	}

	cases := []struct {
		name string
		body string
	}{
		{name: "non-object", body: `["not","an","object"]`},
		{name: "contents wrong type", body: `{"contents": 5}`},
		{name: "nested part wrong type", body: `{"contents":[{"role":"user","parts":5}]}`},
		{name: "tools wrong type", body: `{"tools": "oops"}`},
		{name: "system instruction wrong type", body: `{"systemInstruction": 7}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := adapter.Gemini{}.NormalizeRequest([]byte(testCase.body), reqCtx)
			if !errors.Is(err, adapter.ErrUnsupportedSchema) {
				t.Fatalf("err = %v, want ErrUnsupportedSchema", err)
			}
		})
	}
}

func TestGeminiNormalizeResponseRejectsMalformedFields(t *testing.T) {
	t.Parallel()

	respCtx := agentproxy.ResponseContext{ContentType: "application/json"}

	cases := []struct {
		name string
		body string
	}{
		{name: "non-object", body: `42`},
		{name: "candidates wrong type", body: `{"candidates": "nope"}`},
		{name: "usage wrong type", body: `{"usageMetadata": 9}`},
		{name: "usage count wrong type", body: `{"usageMetadata":{"promptTokenCount":{}}}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := adapter.Gemini{}.NormalizeResponse([]byte(testCase.body), respCtx)
			if !errors.Is(err, adapter.ErrUnsupportedSchema) {
				t.Fatalf("err = %v, want ErrUnsupportedSchema", err)
			}
		})
	}
}

func TestGeminiNormalizeRequestParsesValidBody(t *testing.T) {
	t.Parallel()

	reqCtx := agentproxy.RequestContext{
		Host: "generativelanguage.googleapis.com",
		Path: "/v1/models/gemini-pro:generateContent",
	}
	body := `{"systemInstruction":{"parts":[{"text":"be terse"}]},` +
		`"contents":[{"role":"user","parts":[{"text":"hi"}]}],` +
		`"tools":[{"functionDeclarations":[{"name":"lookup"}]}]}`

	norm, err := adapter.Gemini{}.NormalizeRequest([]byte(body), reqCtx)
	if err != nil {
		t.Fatalf("normalize valid request: %v", err)
	}

	if norm.Model != "gemini-pro" {
		t.Fatalf("model = %q, want gemini-pro", norm.Model)
	}

	if len(norm.Messages) == 0 {
		t.Fatal("expected at least one normalized message")
	}
}
