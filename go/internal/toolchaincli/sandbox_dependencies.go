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
	"runtime"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

var (
	errSandboxWrapperPath = apperror.StaticError(
		"determine native sandbox wrapper path",
	)
	errUnexpectedSandboxRuntimeArg = apperror.StaticError(
		"unexpected validate-sandbox-runtime argument",
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

	err := flags.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return fmt.Errorf("parse validate-sandbox-runtime flags: %w", err)
	}

	if flags.NArg() > 0 {
		return fmt.Errorf("%w: %s", errUnexpectedSandboxRuntimeArg, flags.Arg(0))
	}

	return validateSandboxRuntime()
}

func validateSandboxRuntime() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	wrapperPath, err := defaultNativeSandboxWrapperPath()
	if err != nil {
		return sandboxDependencyDiagnostic(err)
	}

	return validateSandboxRuntimeWithWrapperPath(
		wrapperPath,
	)
}

func validateSandboxRuntimeWithWrapperPath(wrapperPath string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	_, err := sandbox.ValidateNativeRuntimeWithHelper(wrapperPath)
	if err != nil {
		return sandboxDependencyDiagnostic(err)
	}

	return nil
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
