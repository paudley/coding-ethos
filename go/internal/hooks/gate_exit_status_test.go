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
		"make check &",
		"bash -c 'make check &'",
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

func TestRequiredGateExitStatusTracksPipefailInExecutionOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		command   string
		inherited bool
		masked    bool
	}{
		{
			name:    "disabled by default",
			command: "make check | tail -1",
			masked:  true,
		},
		{
			name:      "inherited enabled",
			command:   "make check | tail -1",
			inherited: true,
		},
		{
			name:    "enabled before gate",
			command: "set -o pipefail; make check | tail -1",
		},
		{
			name:      "disabled before gate",
			command:   "set +o pipefail; make check | tail -1",
			inherited: true,
			masked:    true,
		},
		{
			name:      "disable then enable",
			command:   "set +o pipefail; set -o pipefail; make check | tail -1",
			inherited: true,
		},
		{
			name:      "enable then disable",
			command:   "set -o pipefail; set +o pipefail; make check | tail -1",
			inherited: true,
			masked:    true,
		},
		{
			name:    "later enable is not retroactive",
			command: "make check | tail -1; set -o pipefail",
			masked:  true,
		},
		{
			name:      "unrelated enable preserves inherited state",
			command:   "set -o nounset; make check | tail -1",
			inherited: true,
		},
		{
			name:    "unrelated enable preserves disabled state",
			command: "set -o nounset; make check | tail -1",
			masked:  true,
		},
		{
			name:      "nested explicit disable overrides inherited state",
			command:   "bash +o pipefail -c 'make check | tail -1'",
			inherited: true,
			masked:    true,
		},
		{
			name:    "nested explicit enable",
			command: "bash -o pipefail -c 'make check | tail -1'",
		},
		{
			name:    "nested disable then enable",
			command: "bash +o pipefail -o pipefail -c 'make check | tail -1'",
		},
		{
			name:      "nested enable then disable",
			command:   "bash -o pipefail +o pipefail -c 'make check | tail -1'",
			inherited: true,
			masked:    true,
		},
		{
			name:    "outer enable reaches nested command",
			command: "set -o pipefail; bash -c 'make check | tail -1'",
		},
		{
			name:      "nested unrelated option preserves inherited state",
			command:   "bash -o nounset -c 'make check | tail -1'",
			inherited: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reason, masked := maskedRequiredGateStatus(
				test.command,
				test.inherited,
				0,
			)
			if masked != test.masked {
				t.Fatalf(
					"command %q masked = %t, want %t (reason %q)",
					test.command,
					masked,
					test.masked,
					reason,
				)
			}
			if masked && reason != gatePipelineReason {
				t.Fatalf(
					"command %q reason = %q, want %q",
					test.command,
					reason,
					gatePipelineReason,
				)
			}
		})
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
