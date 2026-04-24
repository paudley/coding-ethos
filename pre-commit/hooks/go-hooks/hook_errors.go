// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import "errors"

const (
	defaultDirPerm        = 0o755
	defaultFilePerm       = 0o644
	hookRewriteFilePerm   = 0o666
	scannerBufferCapacity = 64 * kibibyte
	scannerTokenLimit     = 10 * kibibyte * kibibyte
)

var (
	errBundleRootNotFound      = errors.New("could not locate pre-commit bundle")
	errCheckTypeValue          = errors.New("--check-type requires a value")
	errGeminiAPIResponse       = errors.New("gemini API returned error response")
	errGeminiAPINoText         = errors.New("gemini API returned no candidate text")
	errGeminiPackMissingChecks = errors.New(
		"prompt pack missing checks",
	)
	errGeminiPackMissingPrompts = errors.New(
		"prompt pack missing prompts",
	)
	errGeminiPackNotFound = errors.New("could not locate Gemini prompt pack")
	errGeminiServiceTier  = errors.New("unsupported service tier")
	errGeminiCreateNoName = errors.New(
		"gemini cachedContents.create returned no cache name",
	)
	errManifestCandidateNotFound = errors.New("manifest candidate not found")
	errPytestGateCommandEmpty    = errors.New("pytest gate command is empty")
	errPythonParse               = errors.New("failed to parse python source")
	errUnknownFlag               = errors.New("unknown flag")
	errUnknownGeminiCheckType    = errors.New("unknown Gemini check type")
	errUnterminatedModuleDoc     = errors.New("unterminated module docstring")
	errUnterminatedTripleDoc     = errors.New(
		"unterminated triple-quoted module docstring",
	)
)
