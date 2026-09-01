// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import "testing"

func TestRequiredGateExitStatusBlocksMaskedFailures(t *testing.T) {
	t.Parallel()

	for _, command := range []string{
		"make check > gate.log 2>&1; echo EXIT_CODE=$? >> gate.log",
		"make check | tail -100",
		"make check || true",
		"bash -c 'make check; echo done'",
		"bash --norc -c 'make check || true'",
		"git -c core.useBuiltinFSMonitor=false commit -m verified | tee commit.log",
		"cargo --locked test | tee cargo.log",
		"go -C ./go test ./... | tee go.log",
		"python3 -m pytest | tee pytest.log",
		"python3.13 -m pytest | tee pytest.log",
		"env -u CI make check | tee make.log",
		"nice make check | tee make.log",
		"timeout 30s make check | tee make.log",
		"flock /tmp/gate.lock make check | tee make.log",
		"make go-test | tee go.log",
		"make go-e2e-test | tee go-e2e.log",
		"make lint | tee lint.log",
		"make purrdf-extractor-check | tee purrdf.log",
		"make check | tail -100 && set -o pipefail",
	} {
		route := requiredGateExitStatusRouteFor(Event{
			HookEventName: eventPreToolUse,
			ToolName:      toolBash,
			ToolInput: map[string]any{
				"command": command,
			},
		})
		if !route.Block || route.BlockPolicyID != gateExitStatusPolicyID {
			t.Fatalf("command %q route = %#v, want required-gate block", command, route)
		}
	}
}

func TestRequiredGateExitStatusFailsClosedBeyondShellDepth(t *testing.T) {
	t.Parallel()

	reason, masked := maskedRequiredGateStatus(
		"make check",
		false,
		maxGateShellDepth+1,
	)
	if !masked || reason != gateNestingReason {
		t.Fatalf(
			"over-nested inspection = (%q, %t), want fail-closed nesting block",
			reason,
			masked,
		)
	}
}

func TestRequiredGateExitStatusAllowsAuthoritativeStatus(t *testing.T) {
	t.Parallel()

	for _, command := range []string{
		"make check",
		"make check && echo gate-passed",
		"bash -o pipefail -c 'make check | tail -100'",
		"set -o pipefail; make check | tail -100",
		"make check > gate.log 2>&1; status=$?; echo EXIT_CODE=$status >> gate.log; exit \"$status\"",
		"rg -n 'make check' src",
	} {
		route := requiredGateExitStatusRouteFor(Event{
			HookEventName: eventPreToolUse,
			ToolName:      toolBash,
			ToolInput: map[string]any{
				"command": command,
			},
		})
		if route.Block {
			t.Fatalf("authoritative command %q was blocked: %#v", command, route)
		}
	}
}
