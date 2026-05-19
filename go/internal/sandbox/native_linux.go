// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const nativeRuntimeProbeTimeout = 5 * time.Second

const (
	nativeNamespaceUnsupportedReason = "Linux user namespaces are unavailable; " +
		"filesystem sandbox remains required"
	nestedProcessPolicyReason = "process is already constrained by no_new_privs"
)

// ValidateNativeRuntime proves that Linux namespace creation is usable.
func ValidateNativeRuntime() (Evidence, error) {
	evidence := nativeRuntimeEvidence()
	if nativeNestedProcessPolicyRestricted() {
		evidence.Reason = nestedProcessPolicyReason
		evidence.NamespaceEnforced = false
		evidence.ProcessIsolated = false
		evidence.NetworkIsolated = false

		return evidence, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), nativeRuntimeProbeTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "/bin/true")
	command.SysProcAttr = SysProcAttr(nil, evidence)

	output, err := command.CombinedOutput()
	if err == nil {
		return evidence, nil
	}

	evidence.Denied = true
	evidence.Reason = strings.TrimSpace(string(output))

	if evidence.Reason == "" {
		evidence.Reason = err.Error()
	}

	return evidence, fmt.Errorf("%w: %w", ErrBackendUnavailable, err)
}

func nativeRuntimeEvidence() Evidence {
	namespaceSupported := nativeNamespaceSupported()
	reason := ""

	if !namespaceSupported {
		reason = nativeNamespaceUnsupportedReason
	}

	return Evidence{
		Mode:              ModeRequired,
		Backend:           BackendNative,
		Enabled:           true,
		Reason:            reason,
		NamespaceEnforced: namespaceSupported,
		ProcessIsolated:   namespaceSupported,
		NetworkIsolated:   namespaceSupported,
		GitReadOnly:       true,
	}
}

func nativeNestedProcessPolicyRestricted() bool {
	value, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)

	return err == nil && value == 1
}
