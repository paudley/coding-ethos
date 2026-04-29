// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	traceDirMode  = 0o700
	traceFileMode = 0o600
)

type TraceRecord struct {
	Result        Result `json:"result"`
	RecordedAtUTC string `json:"recorded_at_utc"`
	RepoRoot      string `json:"repo_root"`
}

func LogResult(cwd string, result Result) (string, error) {
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

	path := filepath.Join(
		dir,
		fmt.Sprintf("%s-%d-%s.json", timestamp, os.Getpid(), result.Scope),
	)
	file, err := os.OpenFile(
		filepath.Clean(path),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		traceFileMode,
	)
	if err != nil {
		return "", fmt.Errorf("create lint trace: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(TraceRecord{
		RecordedAtUTC: timestamp,
		RepoRoot:      root,
		Result:        result,
	}); err != nil {
		return "", fmt.Errorf("write lint trace: %w", err)
	}

	return path, nil
}
