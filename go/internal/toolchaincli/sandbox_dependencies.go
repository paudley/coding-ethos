// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolchaincli

import (
	"flag"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

var errSandboxModeRequired = apperror.StaticError(
	"validate-sandbox-dependencies requires --sandbox-mode",
)
var errUnsupportedSandboxMode = apperror.StaticError("unsupported sandbox mode")

type sandboxDependencyDiagnosticError struct {
	cause      error
	diagnostic diagnostics.Diagnostic
}

func (err sandboxDependencyDiagnosticError) Error() string {
	return err.cause.Error()
}

func (err sandboxDependencyDiagnosticError) Unwrap() error {
	return err.cause
}

func (err sandboxDependencyDiagnosticError) Diagnostics() []diagnostics.Diagnostic {
	return []diagnostics.Diagnostic{err.diagnostic}
}

func validateSandboxDependenciesCommand(args []string) error {
	flags := flag.NewFlagSet("validate-sandbox-dependencies", flag.ExitOnError)
	sandboxMode := flags.String(
		"sandbox-mode",
		"",
		"Sandbox mode that the managed runtime will advertise",
	)
	backendPath := flags.String(
		"backend-path",
		"",
		"Optional Bubblewrap executable path",
	)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse validate-sandbox-dependencies flags: %w", err)
	}

	return validateSandboxDependencies(*sandboxMode, *backendPath)
}

func validateSandboxDependencies(sandboxMode, backendPath string) error {
	switch strings.ToLower(strings.TrimSpace(sandboxMode)) {
	case sandbox.ModeOff:
		return nil
	case sandbox.ModeAuto, sandbox.ModeRequired:
		_, err := sandbox.ValidateBubblewrapDependency(backendPath)
		if err != nil {
			return sandboxDependencyDiagnostic(err)
		}

		return nil
	case "":
		return errSandboxModeRequired
	default:
		return fmt.Errorf("%w: %q", errUnsupportedSandboxMode, sandboxMode)
	}
}

func sandboxDependencyDiagnostic(cause error) error {
	return sandboxDependencyDiagnosticError{
		cause: cause,
		diagnostic: diagnostics.Diagnostic{
			Tool:     "managed-toolchain",
			Severity: "error",
			Code:     "bubblewrap-unavailable",
			PolicyID: "runtime.sandbox_dependency",
			SkillID:  "managed-toolchain",
			Message: "Bubblewrap is required for sandboxed managed tool " +
				"execution.",
			Advice: "Install Bubblewrap or configure a valid bwrap backend " +
				"before running make build.",
			Detail: cause.Error(),
			Metadata: map[string]any{
				"repair_command": "install bubblewrap, then rerun make build",
			},
		},
	}
}
