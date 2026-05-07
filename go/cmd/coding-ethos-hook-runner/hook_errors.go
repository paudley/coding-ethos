// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	defaultDirPerm        = 0o755
	defaultFilePerm       = 0o644
	hookRewriteFilePerm   = 0o666
	scannerBufferCapacity = 64 * kibibyte
	scannerTokenLimit     = 10 * kibibyte * kibibyte
)

var (
	errBundleRootNotFound = apperror.StaticError("could not locate pre-commit bundle")
	errCheckTypeValue     = apperror.StaticError("--check-type requires a value")
	errGeminiAPIResponse  = apperror.StaticError("gemini API returned error response")
	errGeminiAPINoText    = apperror.StaticError(
		"gemini API returned no candidate text",
	)
	errGeminiPackMissingChecks = apperror.StaticError(
		"prompt pack missing checks",
	)
	errGeminiPackMissingPrompts = apperror.StaticError(
		"prompt pack missing prompts",
	)
	errGeminiPackNotFound = apperror.StaticError("could not locate Gemini prompt pack")
	errGeminiServiceTier  = apperror.StaticError("unsupported service tier")
	errGeminiCreateNoName = apperror.StaticError(
		"gemini cachedContents.create returned no cache name",
	)
	errManifestCandidateNotFound = apperror.StaticError("manifest candidate not found")
	errPlanPathEscapesRoot       = apperror.StaticError("path escapes plan root")
	errPytestGateCommandEmpty    = apperror.StaticError("pytest gate command is empty")
	errPythonParse               = apperror.StaticError("failed to parse python source")
	errUnknownFlag               = apperror.StaticError("unknown flag")
	errUnknownGeminiCheckType    = apperror.StaticError("unknown Gemini check type")
	errUnterminatedModuleDoc     = apperror.StaticError("unterminated module docstring")
	errUnterminatedTripleDoc     = apperror.StaticError(
		"unterminated triple-quoted module docstring",
	)
)
