// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	continuationDirMode   = 0o700
	continuationFileMode  = 0o600
	continuationLineLimit = 40
)

type continuationRecord struct {
	CapturedAtUTC  string   `json:"captured_at_utc"`
	SessionID      string   `json:"session_id"`
	TranscriptPath string   `json:"transcript_path,omitempty"`
	Tail           []string `json:"tail,omitempty"`
}

func continuationOutput(event Event) *HookSpecificOutput {
	switch event.HookEventName {
	case "PreCompact":
		return captureContinuationOutput(event)
	case eventSessionStart:
		if event.Matcher != "compact" && event.Source != "compact" {
			return nil
		}

		return injectContinuationOutput(event)
	default:
		return nil
	}
}

func captureContinuationOutput(event Event) *HookSpecificOutput {
	if event.SessionID == "" {
		return nil
	}

	record := continuationRecord{
		CapturedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		SessionID:      event.SessionID,
		TranscriptPath: event.TranscriptPath,
		Tail:           transcriptTail(event.TranscriptPath),
	}

	path, err := continuationPath(event)
	if err != nil {
		return continuationFailureOutput(event, "resolve continuation path", err)
	}

	err = os.MkdirAll(filepath.Dir(path), continuationDirMode)
	if err != nil {
		return continuationFailureOutput(event, "create continuation directory", err)
	}

	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return continuationFailureOutput(event, "encode continuation record", err)
	}

	err = os.WriteFile(path, append(payload, '\n'), continuationFileMode)
	if err != nil {
		return continuationFailureOutput(event, "write continuation record", err)
	}

	return &HookSpecificOutput{
		HookEventName:     event.HookEventName,
		AdditionalContext: "captured deterministic state",
	}
}

func continuationFailureOutput(
	event Event,
	operation string,
	err error,
) *HookSpecificOutput {
	message := "coding-ethos continuation capture failed during " +
		operation +
		": " +
		err.Error() +
		"\nContinuation is advisory, but this failure should stay visible."

	return &HookSpecificOutput{
		HookEventName: event.HookEventName,
		AdditionalContext: hookOutputNormalizer(event.Cwd).preserveLines(
			message,
		),
	}
}

func injectContinuationOutput(event Event) *HookSpecificOutput {
	if event.SessionID == "" {
		return nil
	}

	path, err := continuationPath(event)
	if err != nil {
		return nil
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var record continuationRecord

	err = json.Unmarshal(payload, &record)
	if err != nil {
		return nil
	}

	return &HookSpecificOutput{
		HookEventName: event.HookEventName,
		AdditionalContext: hookOutputNormalizer(event.Cwd).preserveLines(
			formatContinuationContext(record),
		),
	}
}

func formatContinuationContext(record continuationRecord) string {
	lines := []string{
		"session_id: " + record.SessionID,
		"captured_at_utc: " + record.CapturedAtUTC,
	}

	if record.TranscriptPath != "" {
		lines = append(lines, "transcript_path: "+record.TranscriptPath)
	}

	if len(record.Tail) > 0 {
		lines = append(lines, "", "transcript_tail:")
		for _, line := range record.Tail {
			lines = append(lines, "- "+line)
		}
	}

	lines = append(
		lines,
		"",
		"Use this as deterministic carry-forward context after compaction.",
		"Verify current repo state before acting on any stale transcript detail.",
	)

	return strings.Join(lines, "\n")
}

func transcriptTail(path string) []string {
	if path == "" {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	lines := []string{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}

		lines = append(lines, text)
		if len(lines) > continuationLineLimit {
			lines = lines[1:]
		}
	}

	return lines
}

func continuationPath(event Event) (string, error) {
	gitDir, err := gitCommonDir(event.Cwd)
	if err != nil {
		return "", err
	}

	return filepath.Join(
		gitDir,
		"coding-ethos-hooks",
		"continuation",
		safeSessionFilename(event.SessionID)+".json",
	), nil
}

func gitCommonDir(cwd string) (string, error) {
	if cwd == "" {
		cwd = "."
	}

	command := exec.CommandContext(
		context.Background(),
		"git",
		"rev-parse",
		"--path-format=absolute",
		"--git-common-dir",
	)
	command.Dir = cwd

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve git common dir: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func safeSessionFilename(sessionID string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"..", "_",
	)

	name := strings.TrimSpace(replacer.Replace(sessionID))
	if name == "" {
		return "session"
	}

	return name
}
