// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolchaincli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

var (
	errSandboxModeRequired = apperror.StaticError(
		"validate-sandbox-runtime requires --sandbox-mode",
	)
	errUnsupportedSandboxMode = apperror.StaticError("unsupported sandbox mode")
	errSandboxWrapperPath     = apperror.StaticError(
		"determine native sandbox wrapper path",
	)
)

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

func validateSandboxRuntimeCommand(args []string) error {
	flags := flag.NewFlagSet("validate-sandbox-runtime", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sandboxMode := flags.String(
		"sandbox-mode",
		"",
		"Sandbox mode that the managed runtime will advertise",
	)

	err := flags.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return fmt.Errorf("parse validate-sandbox-runtime flags: %w", err)
	}

	return validateSandboxRuntime(*sandboxMode)
}

func validateSandboxRuntime(sandboxMode string) error {
	switch strings.ToLower(strings.TrimSpace(sandboxMode)) {
	case sandbox.ModeOff:
		return nil
	case "":
		return errSandboxModeRequired
	}

	wrapperPath, err := defaultNativeSandboxWrapperPath()
	if err != nil {
		return sandboxDependencyDiagnostic(err)
	}

	return validateSandboxRuntimeWithWrapperPath(
		sandboxMode,
		wrapperPath,
	)
}

func validateSandboxRuntimeWithWrapperPath(sandboxMode, wrapperPath string) error {
	switch strings.ToLower(strings.TrimSpace(sandboxMode)) {
	case sandbox.ModeOff:
		return nil
	case sandbox.ModeAuto, sandbox.ModeRequired:
		_, err := sandbox.ValidateNativeRuntimeWithHelper(wrapperPath)
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

func defaultNativeSandboxWrapperPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("%w: %w", errSandboxWrapperPath, err)
	}

	return filepath.Join(filepath.Dir(executable), "coding-ethos-sandbox"), nil
}

func sandboxDependencyDiagnostic(cause error) error {
	return sandboxDependencyDiagnosticError{
		cause: cause,
		diagnostic: diagnostics.Diagnostic{
			Tool:     "managed-toolchain",
			Severity: "error",
			Code:     "native-sandbox-unavailable",
			PolicyID: "runtime.sandbox_dependency",
			SkillID:  "managed-toolchain",
			Message: "Native Linux namespace sandboxing is required for " +
				"sandboxed managed tool execution on Linux.",
			Advice: "Run on a Linux host that permits unprivileged namespace " +
				"creation, or use a non-Linux platform where namespace " +
				"enforcement is not advertised.",
			Detail: cause.Error(),
			Metadata: map[string]any{
				"provisioning_dependency": "linux_namespaces",
				"repair_command":          "make build",
			},
		},
	}
}
