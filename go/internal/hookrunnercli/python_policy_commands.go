// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"os"
	"path/filepath"
)

func runPythonPolicyCommand[V any](
	args []string,
	load func() (bool, error),
	find func(string) ([]V, error),
	report func([]V),
) int {
	enabled, err := load()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !enabled || len(args) == 0 {
		return 0
	}

	violations := make([]V, 0)

	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}

		found, err := find(path)
		if err != nil {
			writef(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}

		violations = append(violations, found...)
	}

	if len(violations) == 0 {
		return 0
	}

	report(violations)

	return 1
}

func checkStructuredLoggingCommand(_ Config, args []string) int {
	settings, err := loadStructuredLoggingSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	return runPythonPolicyCommand(
		args,
		func() (bool, error) { return settings.Enabled, nil },
		func(path string) ([]structuredLoggingViolation, error) {
			return findStructuredLoggingViolations(path, settings)
		},
		reportStructuredLoggingViolations,
	)
}

func reportStructuredLoggingViolations(violations []structuredLoggingViolation) {
	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:    "structured_logging",
			File:    violation.File,
			Line:    violation.Line,
			Code:    violation.Method,
			Message: "logger call has no structured context",
			Detail:  violation.Preview,
		})
	}

	emitHookReport(os.Stderr, hookReport{
		Tool:     "structured_logging",
		Title:    "STRUCTURED LOGGING CHECK FAILED (ETHOS §11)",
		Summary:  "Logger calls must include keyword arguments for structured context.",
		Findings: findings,
		Guidance: []string{
			`Add keyword arguments: logger.info("event.name", key=value, other=data).`,
			`For exceptions, use exc_info or logger.exception().`,
		},
	}, selectedHookOutputFormat())
}

func checkConditionalImportsCommand(_ Config, args []string) int {
	settings, err := loadConditionalImportsSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	return runPythonPolicyCommand(
		args,
		func() (bool, error) { return settings.Enabled, nil },
		func(path string) ([]conditionalImportViolation, error) {
			return findConditionalImportViolations(path, settings)
		},
		reportConditionalImportViolations,
	)
}

func reportConditionalImportViolations(violations []conditionalImportViolation) {
	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:    "conditional_imports",
			File:    violation.File,
			Line:    violation.Line,
			Code:    violation.Module,
			Message: "conditional import",
			Detail:  violation.Pattern,
		})
	}

	emitHookReport(os.Stderr, hookReport{
		Tool:  "conditional_imports",
		Title: "CONDITIONAL IMPORT CHECK FAILED (ETHOS §3)",
		Summary: "Required imports must fail at import time instead of " +
			"hiding missing dependencies.",
		Findings: findings,
		Guidance: []string{
			"Remove the try/except and import directly.",
			"Add the dependency to pyproject.toml if needed.",
		},
	}, selectedHookOutputFormat())
}

func checkTypeCheckingImportsCommand(_ Config, args []string) int {
	settings, err := loadTypeCheckingImportsSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := make([]typeCheckingViolation, 0)

	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}

		found, err := findTypeCheckingImportViolations(path, settings)
		if err != nil {
			writef(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}

		violations = append(violations, found...)
	}

	if len(violations) == 0 {
		return 0
	}

	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:    "type_checking_imports",
			File:    violation.File,
			Line:    violation.Line,
			Message: violation.Pattern,
		})
	}

	emitHookReport(os.Stderr, hookReport{
		Tool:     "type_checking_imports",
		Title:    "STRING ANNOTATION PATTERN DETECTED (ETHOS §3, §12)",
		Summary:  "TYPE_CHECKING and future annotations make types unavailable at runtime.",
		Findings: findings,
		Guidance: []string{
			"Remove `from __future__ import annotations`.",
			"Extract shared types into a shared protocols module.",
			"Keep types runtime-visible.",
		},
	}, selectedHookOutputFormat())

	return 1
}

func checkCatchAndSilenceCommand(_ Config, args []string) int {
	settings, err := loadCatchSilenceSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	return runPythonPolicyCommand(
		args,
		func() (bool, error) { return settings.Enabled, nil },
		func(path string) ([]catchSilenceViolation, error) {
			return findCatchSilenceViolations(path, settings)
		},
		reportCatchSilenceViolations,
	)
}

func reportCatchSilenceViolations(violations []catchSilenceViolation) {
	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:    "catch_and_silence",
			File:    violation.File,
			Line:    violation.Line,
			Code:    violation.ExceptionType,
			Message: "exception handler silences failure",
			Detail:  violation.HandlerBody,
		})
	}

	emitHookReport(os.Stderr, hookReport{
		Tool:     "catch_and_silence",
		Title:    "CATCH-AND-SILENCE CHECK FAILED (ETHOS §23)",
		Summary:  "Exceptions must never be silently swallowed.",
		Findings: findings,
		Guidance: []string{
			"Handle the exception, transform and re-raise, or log and re-raise.",
			`Use logger.warning("operation_failed", error=str(exc)) with ` +
				`a raise where recovery is not complete.`,
		},
	}, selectedHookOutputFormat())
}

func checkOptionalReturnsCommand(_ Config, args []string) int {
	settings, err := loadOptionalReturnsSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := make([]optionalTypeViolation, 0)

	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}

		found, err := findOptionalTypeViolations(path, settings)
		if err != nil {
			writef(os.Stderr, "ERROR: %s: %v\n", path, err)

			continue
		}

		violations = append(violations, found...)
	}

	if len(violations) == 0 {
		return 0
	}

	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:    "optional_returns",
			File:    violation.File,
			Line:    violation.Line,
			Message: violation.Context,
		})
	}

	emitHookReport(os.Stderr, hookReport{
		Tool:     "optional_returns",
		Title:    "OPTIONAL TYPE ANNOTATION CHECK FAILED",
		Summary:  "All types must be non-optional. Use exceptions, not | None.",
		Findings: findings,
		Guidance: []string{
			"Replace optional return or dependency types with explicit " +
				"exceptions or required values.",
		},
	}, selectedHookOutputFormat())

	return 1
}

func checkSecurityPatternsCommand(_ Config, args []string) int {
	settings, err := loadSecurityPatternsSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := collectSecurityPatternViolations(args, settings)
	if len(violations) == 0 {
		return 0
	}

	reportSecurityPatternViolations(violations)

	return 1
}

func collectSecurityPatternViolations(
	args []string,
	settings securityPatternsSettings,
) []securityViolation {
	violations := make([]securityViolation, 0)

	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}

		found, err := findSecurityViolations(path, settings)
		if err != nil {
			writef(os.Stderr, "  %s: %v\n", path, err)

			continue
		}

		violations = append(violations, found...)
	}

	return violations
}

func reportSecurityPatternViolations(violations []securityViolation) {
	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:     "security_patterns",
			File:     violation.File,
			Line:     violation.Line,
			Code:     violation.Category,
			Severity: "error",
			Message:  violation.Message,
			Detail:   violation.Snippet,
		})
	}

	emitHookReport(os.Stderr, hookReport{
		Tool:  "security_patterns",
		Title: "SECURITY ANTI-PATTERNS DETECTED",
		Summary: "Security checks found unsafe defaults, SQL injection " +
			"risks, or bootstrap bypasses.",
		Findings: findings,
		Guidance: []string{
			"Use parameterized queries instead of SQL f-strings.",
			"Remove default values from secret-related getenv calls.",
			"Use fixtures that call bootstrap instead of direct environment assignment.",
		},
	}, selectedHookOutputFormat())
}
