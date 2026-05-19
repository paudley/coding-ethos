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
)

const nativeRuntimeProbeTimeout = 5 * time.Second

// ValidateNativeRuntime proves that Linux namespace creation is usable.
func ValidateNativeRuntime() (Evidence, error) {
	evidence := nativeRuntimeEvidence()

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
	return Evidence{
		Mode:              ModeRequired,
		Backend:           BackendNative,
		Enabled:           true,
		NamespaceEnforced: true,
		ProcessIsolated:   true,
		NetworkIsolated:   true,
		GitReadOnly:       true,
	}
}
