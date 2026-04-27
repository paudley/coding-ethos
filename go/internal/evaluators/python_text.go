// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

var (
	conditionalImportPattern = regexp.MustCompile(
		`^\s*except\s+(\([^)]*)?(ImportError|ModuleNotFoundError)\b`,
	)
	optionalReturnPattern = regexp.MustCompile(
		`^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\([^)]*\)\s*->\s*([^:]+):`,
	)
	logCallPattern = regexp.MustCompile(
		`\b(?:logger|_logger|log|_log)\.(?:debug|info|warning|error|critical)\s*\((.*)`,
	)
	directImportPattern = regexp.MustCompile(
		`^\s*(?:from\s+(coding_ethos)\.|import\s+(coding_ethos)\.)`,
	)
)

const (
	logCallMatchCount        = 2
	optionalReturnMatchCount = 3
)

func EvaluatePythonConditionalImports(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonLines(policyDef, context, func(line string) bool {
		return conditionalImportPattern.MatchString(line)
	})
}

func EvaluatePythonOptionalReturns(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonLines(policyDef, context, func(line string) bool {
		matches := optionalReturnPattern.FindStringSubmatch(line)
		if len(matches) != optionalReturnMatchCount {
			return false
		}

		if matches[1] == "__exit__" || matches[1] == "__aexit__" {
			return false
		}

		annotation := strings.ReplaceAll(matches[2], " ", "")

		return strings.Contains(annotation, "|None") ||
			strings.Contains(annotation, "Optional[") ||
			strings.Contains(annotation, "Union[") &&
				strings.Contains(annotation, "None")
	})
}

func EvaluatePythonCatchAndSilence(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	sources, err := pythonSources(context)
	if err != nil {
		return nil, err
	}

	for _, source := range sources {
		decision := silentExceptionDecision(policyDef, source)
		if decision != nil {
			return []policy.Decision{*decision}, nil
		}
	}

	return nil, nil
}

func silentExceptionDecision(
	policyDef policy.Policy,
	source pythonSource,
) *policy.Decision {
	lines := strings.Split(source.Text, "\n")
	for idx, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "except") {
			continue
		}

		decision := firstSilentExceptionBody(policyDef, source, lines, idx)
		if decision != nil {
			return decision
		}
	}

	return nil
}

func firstSilentExceptionBody(
	policyDef policy.Policy,
	source pythonSource,
	lines []string,
	exceptIndex int,
) *policy.Decision {
	indent := leadingSpaces(lines[exceptIndex])
	for next := exceptIndex + 1; next < len(lines); next++ {
		trimmed := strings.TrimSpace(lines[next])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if leadingSpaces(lines[next]) <= indent {
			return nil
		}

		if isSilentExceptionBody(trimmed) {
			decision := pythonDecision(policyDef, source, next+1, trimmed)

			return &decision
		}

		return nil
	}

	return nil
}

func isSilentExceptionBody(trimmed string) bool {
	return trimmed == "pass" || trimmed == "return None" || trimmed == "return"
}

func EvaluatePythonStructuredLogging(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonLines(policyDef, context, func(line string) bool {
		matches := logCallPattern.FindStringSubmatch(line)
		if len(matches) != logCallMatchCount {
			return false
		}

		args := strings.TrimSpace(matches[1])

		return strings.HasPrefix(args, "f\"") ||
			strings.HasPrefix(args, "f'") ||
			strings.Contains(args, ".format(") ||
			strings.Contains(args, "%")
	})
}

func EvaluatePythonDirectImports(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if len(context.Files) > 0 && insidePackage(context.Files[0], "coding_ethos") {
		return nil, nil
	}

	return evaluatePythonLines(policyDef, context, func(line string) bool {
		return directImportPattern.MatchString(line)
	})
}

type pythonSource struct {
	Path string
	Text string
}

func evaluatePythonLines(
	policyDef policy.Policy,
	context Context,
	violates func(string) bool,
) ([]policy.Decision, error) {
	sources, err := pythonSources(context)
	if err != nil {
		return nil, err
	}

	for _, source := range sources {
		for idx, line := range strings.Split(source.Text, "\n") {
			if violates(line) {
				return []policy.Decision{
					pythonDecision(policyDef, source, idx+1, strings.TrimSpace(line)),
				}, nil
			}
		}
	}

	return nil, nil
}

func pythonSources(context Context) ([]pythonSource, error) {
	if context.Content != "" {
		return []pythonSource{
			{Path: firstFile(context.Files), Text: context.Content},
		}, nil
	}

	sources := []pythonSource{}

	for _, file := range context.Files {
		if filepath.Ext(file) != ".py" {
			continue
		}

		payload, err := os.ReadFile(file)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("read python source %s: %w", file, err)
		}

		sources = append(sources, pythonSource{Path: file, Text: string(payload)})
	}

	return sources, nil
}

func firstFile(files []string) string {
	if len(files) == 0 {
		return ""
	}

	return files[0]
}

func pythonDecision(
	policyDef policy.Policy,
	source pythonSource,
	line int,
	snippet string,
) policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)

	decision.Evidence = map[string]any{
		"line":    line,
		"snippet": snippet,
	}
	if source.Path != "" {
		decision.Evidence["file"] = source.Path
	}

	return decision
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func insidePackage(path string, packageName string) bool {
	normalized := strings.TrimPrefix(filepath.ToSlash(path), "./")

	return normalized == packageName || strings.HasPrefix(normalized, packageName+"/")
}
