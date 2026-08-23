// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
)

const (
	lifecycleStateDirMode  = 0o700
	lifecycleStateFileMode = 0o600
	lifecycleStateRootEnv  = "CODE_ETHOS_STATE_ROOT"
)

var errLifecycleStateRoot = errors.New(
	"lifecycle state requires a repository or state root",
)

type lifecycleSessionRecord struct {
	PendingRemediations   []remediationReference `json:"pending_remediations,omitempty"`
	AttemptedRemediations []remediationReference `json:"attempted_remediations,omitempty"`
	PendingSubagents      int                    `json:"pending_subagents"`
	Relevant              bool                   `json:"relevant"`
	PendingBatchGuidance  bool                   `json:"pending_batch_guidance"`
}

type lifecycleSessionTransition struct {
	Relevant        bool
	EventRelevant   bool
	BatchReady      bool
	SubagentStarted bool
	SubagentStopped bool
}

func transitionLifecycleSession(event Event) lifecycleSessionTransition {
	if !lifecycleSessionEventRequiresState(event) {
		return lifecycleTransitionWithoutSession(event)
	}

	if strings.TrimSpace(event.SessionID) == "" {
		return lifecycleTransitionWithoutSession(event)
	}

	transition := lifecycleSessionTransition{}

	err := withLifecycleSessionRecord(event, func(record *lifecycleSessionRecord) {
		transition = applyLifecycleSessionEvent(event, record)
	})
	if err != nil {
		debuglog.Debug(
			"hooks.lifecycle_state.warn",
			zap.String("event", event.HookEventName),
			zap.String("session_id", event.SessionID),
			zap.Error(err),
		)

		return lifecycleTransitionWithoutSession(event)
	}

	return transition
}

func lifecycleSessionEventRequiresState(event Event) bool {
	switch event.HookEventName {
	case eventSessionStart, eventPostToolBatch, eventStop, eventSessionEnd,
		eventSubagentStop:
		return true
	case eventUserPromptSubmit:
		return lifecycleEventRelevant(event)
	case eventSubagentStart:
		return strings.TrimSpace(event.Content()) == "" || lifecycleEventRelevant(event)
	case eventPostToolUse:
		return isEditTool(event.ToolName) && lifecycleFilesRelevant(event.Files()) ||
			event.HasReturnCode() && event.ReturnCode() != 0
	default:
		return false
	}
}

func lifecycleTransitionWithoutSession(event Event) lifecycleSessionTransition {
	relevant := lifecycleEventRelevant(event)

	return lifecycleSessionTransition{
		Relevant:      relevant,
		EventRelevant: relevant,
		BatchReady:    event.HookEventName == eventPostToolBatch && relevant,
	}
}

func applyLifecycleSessionEvent(
	event Event,
	record *lifecycleSessionRecord,
) lifecycleSessionTransition {
	eventRelevant := lifecycleEventRelevant(event)
	if eventRelevant {
		record.Relevant = true
	}

	transition := lifecycleSessionTransition{
		Relevant:      record.Relevant,
		EventRelevant: eventRelevant,
	}

	switch event.HookEventName {
	case eventPostToolUse:
		if postToolBatchGuidanceRelevant(event, record.Relevant) {
			record.PendingBatchGuidance = true
		}
	case eventPostToolBatch:
		transition.BatchReady = record.PendingBatchGuidance
		record.PendingBatchGuidance = false
	case eventSubagentStart:
		if record.Relevant &&
			(strings.TrimSpace(event.Content()) == "" || eventRelevant) {
			record.PendingSubagents++
			transition.SubagentStarted = true
		}
	case eventSubagentStop:
		if record.PendingSubagents > 0 {
			record.PendingSubagents--
			transition.SubagentStopped = true
		}
	case eventSessionEnd:
		record.Relevant = false
		record.PendingBatchGuidance = false
		record.PendingSubagents = 0
	}

	return transition
}

func postToolBatchGuidanceRelevant(event Event, sessionRelevant bool) bool {
	if !sessionRelevant {
		return false
	}

	if isEditTool(event.ToolName) {
		return lifecycleFilesRelevant(event.Files())
	}

	return event.HasReturnCode() && event.ReturnCode() != 0
}

func lifecycleEventRelevant(event Event) bool {
	if lifecycleFilesRelevant(event.Files()) {
		return true
	}

	return lifecycleTextRelevant(event.Content())
}

func lifecycleFilesRelevant(files []string) bool {
	for _, file := range files {
		if _, supported := astfacts.LanguageForPath(file); supported {
			return true
		}
	}

	return false
}

func lifecycleTextRelevant(content string) bool {
	text := strings.ToLower(strings.TrimSpace(content))
	if text == "" {
		return false
	}

	for _, word := range strings.FieldsFunc(text, lifecycleTextSeparator) {
		if _, supported := astfacts.LanguageForPath(word); supported {
			return true
		}
	}

	for _, keyword := range []string{
		"build", "code", "commit", "compile", "function", "hook", "implement",
		"issue", "lint", "package", "pull request", "refactor", "repo",
		"test",
	} {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	return false
}

func lifecycleTextSeparator(character rune) bool {
	return character == ' ' || character == '\n' || character == '\t' ||
		character == '`' || character == '"' || character == '\'' ||
		character == '(' || character == ')' || character == ',' ||
		character == ':' || character == ';'
}

func withLifecycleSessionRecord(
	event Event,
	operation func(*lifecycleSessionRecord),
) (err error) {
	path, err := lifecycleSessionPath(event)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(path), lifecycleStateDirMode)
	if err != nil {
		return fmt.Errorf("create lifecycle state directory: %w", err)
	}

	lockDescriptor, err := syscall.Open(
		path+".lock",
		syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC,
		lifecycleStateFileMode,
	)
	if err != nil {
		return fmt.Errorf("open lifecycle state lock: %w", err)
	}

	defer func() {
		closeErr := syscall.Close(lockDescriptor)
		if err == nil && closeErr != nil {
			err = fmt.Errorf("close lifecycle state lock: %w", closeErr)
		}
	}()

	err = syscall.Flock(lockDescriptor, syscall.LOCK_EX)
	if err != nil {
		return fmt.Errorf("lock lifecycle state: %w", err)
	}

	defer func() {
		unlockErr := syscall.Flock(lockDescriptor, syscall.LOCK_UN)
		if err == nil && unlockErr != nil {
			err = fmt.Errorf("unlock lifecycle state: %w", unlockErr)
		}
	}()

	record, err := readLifecycleSessionRecord(path)
	if err != nil {
		return err
	}

	before := record
	operation(&record)

	if lifecycleSessionRecordsEqual(before, record) {
		return nil
	}

	return writeLifecycleSessionRecord(path, record)
}

func lifecycleSessionRecordsEqual(
	left lifecycleSessionRecord,
	right lifecycleSessionRecord,
) bool {
	return left.Relevant == right.Relevant &&
		left.PendingBatchGuidance == right.PendingBatchGuidance &&
		left.PendingSubagents == right.PendingSubagents &&
		slices.Equal(left.PendingRemediations, right.PendingRemediations) &&
		slices.Equal(left.AttemptedRemediations, right.AttemptedRemediations)
}

func lifecycleSessionPath(event Event) (string, error) {
	root := strings.TrimSpace(os.Getenv(lifecycleStateRootEnv))
	if root == "" {
		root = gitRootFromPath(event.Cwd)
	}

	if root == "" {
		return "", errLifecycleStateRoot
	}

	return filepath.Join(
		filepath.Clean(root),
		".coding-ethos",
		"hook-sessions",
		sha256Hex(strings.TrimSpace(event.SessionID))+".json",
	), nil
}

func readLifecycleSessionRecord(path string) (lifecycleSessionRecord, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return lifecycleSessionRecord{}, nil
	}

	if err != nil {
		return lifecycleSessionRecord{}, fmt.Errorf("read lifecycle state: %w", err)
	}

	record := lifecycleSessionRecord{}

	err = json.Unmarshal(payload, &record)
	if err != nil {
		return lifecycleSessionRecord{}, fmt.Errorf("decode lifecycle state: %w", err)
	}

	return record, nil
}

func writeLifecycleSessionRecord(path string, record lifecycleSessionRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode lifecycle state: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".lifecycle-*.tmp")
	if err != nil {
		return fmt.Errorf("create lifecycle state temporary file: %w", err)
	}

	temporaryPath := temporary.Name()

	defer func() {
		_ = os.Remove(temporaryPath)
	}()

	_, err = temporary.Write(append(payload, '\n'))
	if err != nil {
		_ = temporary.Close()

		return fmt.Errorf("write lifecycle state temporary file: %w", err)
	}

	err = temporary.Chmod(lifecycleStateFileMode)
	if err != nil {
		_ = temporary.Close()

		return fmt.Errorf("chmod lifecycle state temporary file: %w", err)
	}

	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("close lifecycle state temporary file: %w", err)
	}

	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("replace lifecycle state: %w", err)
	}

	return nil
}
