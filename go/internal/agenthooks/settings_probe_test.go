// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthooks

import (
	"strings"
	"testing"
)

func TestValidateCodexBlockReasonAcceptsNativeStderr(t *testing.T) {
	t.Parallel()

	result := hookProbeResult{
		exitCode: nativeBlockExitCode,
		stderr: "policy_ids: git.wrapper_required\n" +
			"Resubmit the command through: cerun -- 'git status --short'\n",
	}

	if err := validateCodexBlockProbe(result); err != nil {
		t.Fatalf("validate native Codex block: %v", err)
	}
}

func TestValidateCodexBlockReasonRejectsInvalidNativeStderr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		exitCode   int
		stderr     string
		wantMarker string
	}{
		{
			name:       "missing reason",
			exitCode:   nativeBlockExitCode,
			wantMarker: "missing Codex block reason on stderr",
		},
		{
			name:       "missing policy marker",
			exitCode:   nativeBlockExitCode,
			stderr:     "Resubmit the command through: cerun -- 'git status --short'",
			wantMarker: "git.wrapper_required",
		},
		{
			name:       "wrong native block exit",
			exitCode:   1,
			stderr:     "policy_ids: git.wrapper_required",
			wantMarker: "Codex native block exit = 1, want 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateCodexBlockProbe(hookProbeResult{
				exitCode: test.exitCode,
				stderr:   test.stderr,
			})
			if err == nil || !strings.Contains(err.Error(), test.wantMarker) {
				t.Fatalf("error = %v, want marker %q", err, test.wantMarker)
			}
		})
	}
}

func TestValidateKimiStopContinuationAcceptsSupervisorAllow(t *testing.T) {
	t.Parallel()

	result := hookProbeResult{
		exitCode: 0,
		stdout:   "{\"message\":\"\"}\n",
		payload:  map[string]any{"message": ""},
	}

	if err := validateKimiStopContinuationProbe(result); err != nil {
		t.Fatalf("validate Kimi supervisor Stop allow: %v", err)
	}
}

func TestValidateKimiStopContinuationRejectsMissingResponse(t *testing.T) {
	t.Parallel()

	err := validateKimiStopContinuationProbe(hookProbeResult{
		exitCode: 0,
		stdout:   "{}\n",
		payload:  map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "no native response") {
		t.Fatalf("error = %v, want missing native response", err)
	}
}

func TestValidateKimiStopContinuationRejectsBooleanPermissionDecision(t *testing.T) {
	t.Parallel()

	err := validateKimiStopContinuationProbe(hookProbeResult{
		exitCode: 0,
		stdout: "{\"message\":\"\",\"hookSpecificOutput\":" +
			"{\"permissionDecision\":true}}\n",
		payload: map[string]any{
			"message": "",
			"hookSpecificOutput": map[string]any{
				"permissionDecision": true,
			},
		},
	})
	if err == nil ||
		!strings.Contains(err.Error(), "permissionDecision must be a string") {
		t.Fatalf("error = %v, want non-string permissionDecision rejection", err)
	}
}
