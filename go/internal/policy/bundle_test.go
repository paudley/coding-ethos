// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy_test

import (
	. "blackcat.ca/coding-ethos/go/internal/policy"
	"bytes"
	"strings"
	"testing"
)

func TestExampleBundleValidates(t *testing.T) {
	t.Parallel()

	bundle := ExampleBundle()

	err := bundle.Validate()
	if err != nil {
		t.Fatalf("expected example bundle to validate, got %v", err)
	}
}

func TestEncodeDecodeBundleRoundTrips(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer

	original := ExampleBundle()

	err := EncodeBundle(&buffer, original)
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	decoded, err := DecodeBundle(&buffer)
	if err != nil {
		t.Fatalf("decode bundle: %v", err)
	}

	if decoded.BundleID != original.BundleID {
		t.Fatalf(
			"bundle id mismatch: got %q want %q",
			decoded.BundleID,
			original.BundleID,
		)
	}

	if _, ok := decoded.Policies["git.hook_bypass"]; !ok {
		t.Fatalf("decoded bundle missing git.hook_bypass policy")
	}

	if decoded.Policies["git.hook_bypass"].DefenseLayers.Mediate != "wrapper" {
		t.Fatalf(
			"decoded bundle missing git wrapper defense layer: %#v",
			decoded.Policies["git.hook_bypass"].DefenseLayers,
		)
	}
}

func TestValidateRejectsUnknownDispatchPolicy(t *testing.T) {
	t.Parallel()

	bundle := ExampleBundle()
	bundle.Dispatch.Linter["files"] = append(
		bundle.Dispatch.Linter["files"],
		"missing.policy",
	)

	err := bundle.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}

	if !strings.Contains(
		err.Error(),
		`dispatch.linter.files references unknown policy "missing.policy"`,
	) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsUnsupportedDispatchMode(t *testing.T) {
	t.Parallel()

	bundle := ExampleBundle()
	bundle.Dispatch.Hooks["PreToolUse"]["Write"][0].Mode = "ask"

	err := bundle.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}

	if !strings.Contains(
		err.Error(),
		`mode "ask" is not supported by policy "python.conditional_imports"`,
	) {
		t.Fatalf("unexpected error: %v", err)
	}
}
