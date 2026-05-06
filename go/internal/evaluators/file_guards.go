// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	defaultPrivateKeyPattern = `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`
	executePermissionMask    = 0o111
	kibibyte                 = 1024
	binaryProbeBytes         = 1024
)

func EvaluateFileMergeConflict(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	markers := stringSliceOption(
		context.EvaluatorOptions,
		"markers",
		[]string{"<<<<<<<", "=======", ">>>>>>>", "|||||||"},
	)

	for _, file := range context.Files {
		path := resolveGuardPath(context.Cwd, file)

		found, err := scanGuardLines(
			path,
			func(lineNumber int, line string) ([]policy.Decision, bool) {
				for _, marker := range markers {
					if strings.HasPrefix(line, marker) {
						return []policy.Decision{
							fileGuardDecision(
								policyDef,
								"merge_conflict",
								file,
								lineNumber,
								marker,
								"unresolved merge conflict marker",
							),
						}, true
					}
				}

				return nil, false
			},
		)
		if err != nil {
			return nil, err
		}

		if len(found) > 0 {
			return found, nil
		}
	}

	return nil, nil
}

func EvaluateFilePrivateKey(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	pattern := stringOption(
		context.EvaluatorOptions,
		"pattern",
		defaultPrivateKeyPattern,
	)

	privateKey, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile private key pattern: %w", err)
	}

	for _, file := range context.Files {
		path := resolveGuardPath(context.Cwd, file)

		regular, err := isRegularGuardFile(path)
		if err != nil {
			return nil, err
		}

		if !regular {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("read private-key candidate %s: %w", file, err)
		}

		if privateKey.Match(data) {
			return []policy.Decision{
				fileGuardDecision(
					policyDef,
					"private_key",
					file,
					0,
					"",
					"possible private key detected",
				),
			}, nil
		}
	}

	return nil, nil
}

func EvaluateFileShebang(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	for _, file := range context.Files {
		path := resolveGuardPath(context.Cwd, file)

		regular, err := isRegularGuardFile(path)
		if err != nil {
			return nil, err
		}

		if !regular {
			continue
		}

		text, binary, err := readGuardText(path)
		if err != nil {
			return nil, err
		}

		if binary {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("stat shebang file %s: %w", file, err)
		}

		executable := info.Mode()&executePermissionMask != 0

		hasShebang := strings.HasPrefix(text, "#!")
		switch {
		case executable && !hasShebang:
			return []policy.Decision{
				fileGuardDecision(
					policyDef,
					"shebangs",
					file,
					1,
					"",
					"executable file has no shebang",
				),
			}, nil
		case hasShebang && !executable:
			return []policy.Decision{
				fileGuardDecision(
					policyDef,
					"shebangs",
					file,
					1,
					"",
					"shebang script is not executable",
				),
			}, nil
		}
	}

	return nil, nil
}

func EvaluatePIIScrubber(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	patterns, err := piiPatterns(context.EvaluatorOptions)
	if err != nil {
		return nil, err
	}

	exemptPrefixes := stringSliceOption(context.EvaluatorOptions, "exempt_prefixes", nil)

	for _, file := range context.Files {
		if hasConfiguredPrefix(file, exemptPrefixes) ||
			hasHiddenDirectoryComponent(file) {
			continue
		}

		path := resolveGuardPath(context.Cwd, file)

		found, err := scanGuardLines(
			path,
			func(lineNumber int, line string) ([]policy.Decision, bool) {
				for _, pattern := range patterns {
					if pattern.MatchString(line) {
						return []policy.Decision{
							fileGuardDecision(
								policyDef,
								"pii",
								file,
								lineNumber,
								"",
								"local machine detail detected",
							),
						}, true
					}
				}

				return nil, false
			},
		)
		if err != nil {
			return nil, err
		}

		if len(found) > 0 {
			return found, nil
		}
	}

	return nil, nil
}

func EvaluateLicenseHeader(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if decision, err := evaluateLicenseFile(
		policyDef,
		context,
	); decision != nil ||
		err != nil {
		return decision, err
	}

	extensions := stringSet(normalizedSuffixes(stringSliceOption(
		context.EvaluatorOptions,
		"extensions",
		[]string{".go", ".py", ".sh"},
	)))
	exemptPrefixes := stringSliceOption(context.EvaluatorOptions, "exempt_prefixes", nil)
	exemptBasenames := stringSet(
		stringSliceOption(context.EvaluatorOptions, "exempt_basenames", nil),
	)
	required := stringSliceOption(context.EvaluatorOptions, "required", []string{
		"SPDX-FileCopyrightText:",
		"SPDX-License-Identifier:",
	})

	for _, file := range context.Files {
		if !extensions[strings.ToLower(filepath.Ext(file))] ||
			hasConfiguredPrefix(file, exemptPrefixes) ||
			exemptBasenames[filepath.Base(file)] {
			continue
		}

		text, binary, err := readGuardText(resolveGuardPath(context.Cwd, file))
		if err != nil {
			return nil, err
		}

		if binary {
			continue
		}

		header := firstGuardLines(text, intOption(context.EvaluatorOptions, "scan_lines", 5))
		for _, requiredText := range required {
			if !strings.Contains(header, requiredText) {
				return []policy.Decision{
					fileGuardDecision(
						policyDef,
						"license_header",
						file,
						1,
						requiredText,
						"missing required license header text",
					),
				}, nil
			}
		}
	}

	return nil, nil
}

func evaluateLicenseFile(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	expected := stringOption(context.EvaluatorOptions, "expected_license_text", "")
	if expected == "" {
		return nil, nil
	}

	licenseFile := stringOption(context.EvaluatorOptions, "license_file", "LICENSE")
	path := resolveGuardPath(context.Cwd, licenseFile)

	text, binary, err := readGuardText(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []policy.Decision{
				fileGuardDecision(
					policyDef,
					"license_file",
					licenseFile,
					1,
					stringOption(context.EvaluatorOptions, "spdx_id", ""),
					"configured SPDX license file is missing",
				),
			}, nil
		}

		return nil, err
	}

	if binary || normalizeGuardLicenseText(text) != normalizeGuardLicenseText(expected) {
		return []policy.Decision{
			fileGuardDecision(
				policyDef,
				"license_file",
				licenseFile,
				1,
				stringOption(context.EvaluatorOptions, "spdx_id", ""),
				"LICENSE does not match the configured SPDX license text",
			),
		}, nil
	}

	return nil, nil
}

func normalizeGuardLicenseText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	return strings.Join(lines, "\n") + "\n"
}

func piiPatterns(options map[string]any) ([]*regexp.Regexp, error) {
	rawPatterns := stringSliceOption(options, "patterns", []string{
		`/(home|Users)/[A-Za-z0-9._-]+/`,
		`lbox-worktrees/[A-Za-z0-9._-]+`,
		`/tmp/tmp\.[A-Za-z0-9._-]+`,
	})

	patterns := make([]*regexp.Regexp, 0, len(rawPatterns))
	for _, raw := range rawPatterns {
		pattern, err := regexp.Compile(raw)
		if err != nil {
			return nil, fmt.Errorf("compile PII pattern %q: %w", raw, err)
		}

		patterns = append(patterns, pattern)
	}

	for _, literal := range stringSliceOption(options, "literals", nil) {
		patterns = append(patterns, regexp.MustCompile(regexp.QuoteMeta(literal)))
	}

	return patterns, nil
}

func hasHiddenDirectoryComponent(path string) bool {
	normalized := filepath.ToSlash(path)
	normalized = strings.TrimPrefix(normalized, "./")

	parts := strings.Split(normalized, "/")
	for index, part := range parts {
		if index == len(parts)-1 || part == "" || part == "." || part == ".." {
			continue
		}

		if strings.HasPrefix(part, ".") {
			return true
		}
	}

	return false
}

func resolveGuardPath(cwd, path string) string {
	if filepath.IsAbs(path) || cwd == "" {
		return path
	}

	return filepath.Join(cwd, path)
}

func firstGuardLines(text string, count int) string {
	if count <= 0 {
		return ""
	}

	lines := strings.SplitN(text, "\n", count+1)
	if len(lines) > count {
		lines = lines[:count]
	}

	return strings.Join(lines, "\n")
}

func readGuardText(path string) (string, bool, error) {
	regular, err := isRegularGuardFile(path)
	if err != nil || !regular {
		return "", false, err
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("read file %s: %w", path, err)
	}
	defer file.Close()

	probe := make([]byte, binaryProbeBytes)

	read, err := file.Read(probe)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, fmt.Errorf("read file %s: %w", path, err)
	}

	if isBinaryBytes(probe[:read]) {
		return "", true, nil
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", false, fmt.Errorf("seek file %s: %w", path, err)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return "", false, fmt.Errorf("read file %s: %w", path, err)
	}

	if isBinaryBytes(data) {
		return "", true, nil
	}

	return string(data), false, nil
}

func isBinaryBytes(data []byte) bool {
	return slices.Contains(data, 0)
}

func normalizedSuffixes(suffixes []string) []string {
	normalized := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		value := strings.ToLower(strings.TrimSpace(suffix))
		if value == "" {
			continue
		}

		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}

		normalized = append(normalized, value)
	}

	return normalized
}

func scanGuardLines(
	path string,
	visit func(lineNumber int, line string) ([]policy.Decision, bool),
) ([]policy.Decision, error) {
	regular, err := isRegularGuardFile(path)
	if err != nil || !regular {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*kibibyte), 10*kibibyte*kibibyte)

	lineNumber := 0
	for scanner.Scan() {
		lineNumber++

		line := scanner.Text()
		if strings.ContainsRune(line, 0) {
			return nil, nil
		}

		if decision, ok := visit(lineNumber, line); ok {
			return decision, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan file %s: %w", path, err)
	}

	return nil, nil
}

func isRegularGuardFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("stat file %s: %w", path, err)
	}

	return info.Mode().IsRegular(), nil
}

func fileGuardDecision(
	policyDef policy.Policy,
	tool string,
	file string,
	line int,
	code string,
	message string,
) policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Diagnostics = []diagnostics.Diagnostic{{
		Tool:     tool,
		File:     file,
		Line:     line,
		Severity: blockDecision,
		Code:     code,
		PolicyID: policyDef.ID,
		Message:  message,
		Advice:   policyDef.Suggestion,
	}}

	decision.Evidence = map[string]any{
		"file": file,
	}
	if code != "" {
		decision.Evidence["code"] = code
	}

	return decision
}
