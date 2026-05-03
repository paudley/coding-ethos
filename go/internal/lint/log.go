// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

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
	Result             Result                      `json:"result"`
	SchemaVersion      int                         `json:"schema_version"`
	TraceID            string                      `json:"trace_id"`
	RecordedAtUTC      string                      `json:"recorded_at_utc"`
	RepoRoot           string                      `json:"repo_root"`
	Findings           []evidence.Finding          `json:"findings,omitempty"`
	AgentRemediation   []agentmsg.Remediation      `json:"agent_remediation,omitempty"`
	RemediationSummary agentmsg.Summary            `json:"remediation_summary,omitempty"`
	RemediationEvents  []evidence.RemediationEvent `json:"remediation_events,omitempty"`
}

func LogResult(cwd string, result Result) (tracePath string, err error) {
	root := cwd
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve lint trace root: %w", err)
		}
	}

	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	dir := filepath.Join(root, ".coding-ethos", "lint-runs")
	if err := os.MkdirAll(dir, traceDirMode); err != nil {
		return "", fmt.Errorf("create lint trace dir: %w", err)
	}

	traceID := fmt.Sprintf("%s-%d-%s.json", timestamp, os.Getpid(), safeTraceScope(result.Scope))
	path := filepath.Join(dir, traceID)
	file, err := os.OpenFile(
		filepath.Clean(path),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		traceFileMode,
	)
	if err != nil {
		return "", fmt.Errorf("create lint trace: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close lint trace: %w", closeErr)
		}
	}()

	remediation := agentmsg.FromDiagnostics(OutputDiagnostics(result))
	findings := evidence.FromDiagnostics(OutputDiagnostics(result))
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(TraceRecord{
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
	}); err != nil {
		return "", fmt.Errorf("write lint trace: %w", err)
	}

	return path, nil
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
