// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

const (
	traceDirMode  = 0o700
	traceFileMode = 0o600
)

type TraceRecord struct {
	TraceID            string                      `json:"trace_id"`
	RecordedAtUTC      string                      `json:"recorded_at_utc"`
	RepoRoot           string                      `json:"repo_root"`
	Result             Result                      `json:"result"`
	Findings           []evidence.Finding          `json:"findings,omitempty"`
	AgentRemediation   []agentmsg.Remediation      `json:"agent_remediation,omitempty"`
	RemediationEvents  []evidence.RemediationEvent `json:"remediation_events,omitempty"`
	RemediationSummary agentmsg.Summary            `json:"remediation_summary,omitzero"`
	SchemaVersion      int                         `json:"schema_version"`
}

func LogResult(cwd string, result Result) (string, error) {
	root := cwd
	if root == "" {
		resolvedRoot, cwdErr := os.Getwd()
		if cwdErr != nil {
			return "", fmt.Errorf("resolve lint trace root: %w", cwdErr)
		}

		root = resolvedRoot
	}

	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")

	dir := filepath.Join(root, ".coding-ethos", "lint-runs")

	inlineErr0 := os.MkdirAll(dir, traceDirMode)
	if inlineErr0 != nil {
		return "", fmt.Errorf("create lint trace dir: %w", inlineErr0)
	}

	EnsureTraceID(&result)
	traceID := result.TraceID
	path := filepath.Join(dir, traceID)

	file, err := os.OpenFile(
		filepath.Clean(path),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		traceFileMode,
	)
	if err != nil {
		return "", fmt.Errorf("create lint trace: %w", err)
	}

	remediation := agentmsg.FromDiagnostics(OutputDiagnostics(result))
	findings := evidence.FromDiagnostics(OutputDiagnostics(result))
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	inlineErr1 := encoder.Encode(TraceRecord{
		SchemaVersion:      evidence.SchemaVersion,
		TraceID:            traceID,
		RecordedAtUTC:      timestamp,
		RepoRoot:           root,
		Result:             result,
		Findings:           findings,
		AgentRemediation:   remediation,
		RemediationSummary: agentmsg.Summarize(remediation),
		RemediationEvents: evidence.RemediationEvents(
			remediation,
			findings,
			traceID,
			"suggested",
		),
	})
	if inlineErr1 != nil {
		_ = file.Close()

		return "", fmt.Errorf("write lint trace: %w", inlineErr1)
	}

	closeErr := file.Close()
	if closeErr != nil {
		return "", fmt.Errorf("close lint trace: %w", closeErr)
	}

	return path, nil
}

func SARIFPathForTracePath(tracePath string) string {
	extension := filepath.Ext(tracePath)
	if extension == "" {
		return tracePath + ".sarif"
	}

	return strings.TrimSuffix(tracePath, extension) + ".sarif"
}

func WriteSARIFSidecar(tracePath, payload string) error {
	path := SARIFPathForTracePath(tracePath)

	err := os.WriteFile(filepath.Clean(path), []byte(payload), traceFileMode)
	if err != nil {
		return fmt.Errorf("write SARIF sidecar: %w", err)
	}

	return nil
}

func EnsureTraceID(result *Result) {
	if result == nil || strings.TrimSpace(result.TraceID) != "" {
		return
	}

	result.TraceID = NewTraceID(result.Scope)
}

func NewTraceID(scope string) string {
	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")

	return fmt.Sprintf("%s-%d-%s.json", timestamp, os.Getpid(), safeTraceScope(scope))
}

func safeTraceScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "unknown"
	}

	var builder strings.Builder
	builder.Grow(len(scope))

	lastWasSeparator := false

	for _, char := range scope {
		if isSafeTraceScopeChar(char) {
			builder.WriteRune(char)

			lastWasSeparator = false

			continue
		}

		if !lastWasSeparator {
			builder.WriteByte('_')

			lastWasSeparator = true
		}
	}

	safe := strings.Trim(builder.String(), "._-")
	if safe == "" {
		return "unknown"
	}

	return safe
}

func isSafeTraceScopeChar(char rune) bool {
	return char >= 'a' && char <= 'z' ||
		char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' ||
		char == '.' ||
		char == '_' ||
		char == '-'
}
