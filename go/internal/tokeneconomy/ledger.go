// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:cyclop,gocyclo,lll,mnd,noinlineerr,wsl_v5 // Native ledger parsing keeps gates adjacent.
package tokeneconomy

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const (
	maximumLedgerBytes  = 512 << 20
	maximumLedgerLine   = 32 << 20
	cumulativeUsageKind = "cumulative"
)

var errInvalidLedger = errors.New("invalid provider token ledger")

// ParseLedger reads one provider-native ledger without retaining prompt or
// response bodies in the returned evidence.
func ParseLedger(provider Provider, path string) (Ledger, error) {
	canonical, err := canonicalLedgerPath(path)
	if err != nil {
		return Ledger{}, err
	}

	before, err := ledgerFileSHA256(canonical)
	if err != nil {
		return Ledger{}, err
	}

	file, err := os.Open(canonical)
	if err != nil {
		return Ledger{}, fmt.Errorf("open provider ledger: %w", err)
	}

	var ledger Ledger
	switch provider {
	case ProviderCodex:
		ledger, err = parseCodexLedger(file)
	case ProviderClaude:
		ledger, err = parseClaudeLedger(file)
	default:
		err = fmt.Errorf("%w: unsupported provider %q", errInvalidLedger, provider)
	}

	closeErr := file.Close()
	if err != nil {
		return Ledger{}, err
	}
	if closeErr != nil {
		return Ledger{}, fmt.Errorf("close provider ledger: %w", closeErr)
	}

	after, err := ledgerFileSHA256(canonical)
	if err != nil {
		return Ledger{}, err
	}
	if before != after {
		return Ledger{}, fmt.Errorf("%w: ledger changed while it was read", errInvalidLedger)
	}

	ledger.Provider = provider
	ledger.SourcePath = canonical
	ledger.SourceSHA256 = before

	return ledger, nil
}

func canonicalLedgerPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: ledger path must be absolute", errInvalidLedger)
	}

	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat provider ledger: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: ledger is not a regular file", errInvalidLedger)
	}
	if info.Size() > maximumLedgerBytes {
		return "", fmt.Errorf(
			"%w: ledger exceeds %d bytes",
			errInvalidLedger,
			maximumLedgerBytes,
		)
	}

	return path, nil
}

func ledgerFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open provider ledger for hashing: %w", err)
	}
	defer file.Close()

	digest := sha256.New()
	_, err = io.Copy(digest, file)
	if err != nil {
		return "", fmt.Errorf("hash provider ledger: %w", err)
	}

	return hex.EncodeToString(digest.Sum(nil)), nil
}

type codexLedgerLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		Type      string `json:"type"`
		Model     string `json:"model"`
		Info      struct {
			TotalTokenUsage TokenUsage `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

func parseCodexLedger(reader io.Reader) (Ledger, error) {
	ledger := Ledger{Events: []UsageEvent{}}
	scanner := newLedgerScanner(reader)

	for scanner.Scan() {
		var line codexLedgerLine
		err := json.Unmarshal(scanner.Bytes(), &line)
		if err != nil {
			return Ledger{}, fmt.Errorf(
				"%w: decode Codex ledger line: %w",
				errInvalidLedger,
				err,
			)
		}

		err = acceptCodexLine(&ledger, line)
		if err != nil {
			return Ledger{}, err
		}
	}

	if err := scanner.Err(); err != nil {
		return Ledger{}, fmt.Errorf("scan Codex ledger: %w", err)
	}
	if ledger.SessionID == "" {
		return Ledger{}, fmt.Errorf("%w: Codex session id is missing", errInvalidLedger)
	}
	if len(ledger.Events) == 0 {
		return Ledger{}, fmt.Errorf(
			"%w: Codex token_count event is missing",
			errInvalidLedger,
		)
	}

	ledger.Usage = ledger.Events[len(ledger.Events)-1].Usage

	return ledger, nil
}

func acceptCodexLine(ledger *Ledger, line codexLedgerLine) error {
	switch line.Type {
	case "session_meta":
		sessionID := firstLedgerValue(line.Payload.SessionID, line.Payload.ID)

		return setStableLedgerValue(&ledger.SessionID, sessionID, "Codex session id")
	case "turn_context":
		return setStableLedgerValue(&ledger.Model, line.Payload.Model, "Codex model")
	case "event_msg":
		if line.Payload.Type != "token_count" {
			return nil
		}

		usage := line.Payload.Info.TotalTokenUsage
		if err := validateUsage(usage); err != nil {
			return err
		}
		if len(ledger.Events) > 0 {
			previous := ledger.Events[len(ledger.Events)-1].Usage
			if usageDecreased(previous, usage) {
				return fmt.Errorf("%w: Codex cumulative usage decreased", errInvalidLedger)
			}
		}

		ledger.Events = append(ledger.Events, UsageEvent{
			RecordedAtUTC: line.Timestamp,
			UsageKind:     cumulativeUsageKind,
			Ordinal:       len(ledger.Events),
			Usage:         usage,
		})
	}

	return nil
}

type claudeLedgerLine struct {
	Type string `json:"type"`
	//nolint:tagliatelle // Claude's provider-native ledger uses sessionId.
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens              int64 `json:"input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func parseClaudeLedger(reader io.Reader) (Ledger, error) {
	ledger := Ledger{Events: []UsageEvent{}}
	seen := map[string]TokenUsage{}
	scanner := newLedgerScanner(reader)

	for scanner.Scan() {
		var line claudeLedgerLine
		err := json.Unmarshal(scanner.Bytes(), &line)
		if err != nil {
			return Ledger{}, fmt.Errorf(
				"%w: decode Claude ledger line: %w",
				errInvalidLedger,
				err,
			)
		}

		if line.SessionID != "" {
			err = setStableLedgerValue(&ledger.SessionID, line.SessionID, "Claude session id")
			if err != nil {
				return Ledger{}, err
			}
		}
		if line.Type != "assistant" ||
			line.Message.ID == "" ||
			line.Message.ID == "<synthetic>" ||
			line.Message.Model == "<synthetic>" {
			continue
		}

		err = acceptClaudeUsage(&ledger, seen, line)
		if err != nil {
			return Ledger{}, err
		}
	}

	if err := scanner.Err(); err != nil {
		return Ledger{}, fmt.Errorf("scan Claude ledger: %w", err)
	}
	if ledger.SessionID == "" {
		return Ledger{}, fmt.Errorf("%w: Claude session id is missing", errInvalidLedger)
	}
	if len(ledger.Events) == 0 {
		return Ledger{}, fmt.Errorf("%w: Claude usage event is missing", errInvalidLedger)
	}

	return ledger, nil
}

func acceptClaudeUsage(
	ledger *Ledger,
	seen map[string]TokenUsage,
	line claudeLedgerLine,
) error {
	usage := TokenUsage{
		InputTokens:              line.Message.Usage.InputTokens,
		CacheCreationInputTokens: line.Message.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     line.Message.Usage.CacheReadInputTokens,
		OutputTokens:             line.Message.Usage.OutputTokens,
	}
	usage.TotalTokens = usage.InputTokens +
		usage.CacheCreationInputTokens +
		usage.CacheReadInputTokens +
		usage.OutputTokens

	if err := validateUsage(usage); err != nil {
		return err
	}
	if previous, found := seen[line.Message.ID]; found {
		if !reflect.DeepEqual(previous, usage) {
			return fmt.Errorf(
				"%w: Claude message %q has conflicting usage",
				errInvalidLedger,
				line.Message.ID,
			)
		}

		return nil
	}

	if err := setStableLedgerValue(
		&ledger.Model,
		line.Message.Model,
		"Claude model",
	); err != nil {
		return err
	}

	seen[line.Message.ID] = usage
	ledger.Events = append(ledger.Events, UsageEvent{
		ProviderMessageID: line.Message.ID,
		RecordedAtUTC:     line.Timestamp,
		UsageKind:         "incremental",
		Ordinal:           len(ledger.Events),
		Usage:             usage,
	})
	ledger.Usage = addUsage(ledger.Usage, usage)

	return nil
}

func newLedgerScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maximumLedgerLine)

	return scanner
}

func validateUsage(usage TokenUsage) error {
	values := []int64{
		usage.InputTokens,
		usage.CachedInputTokens,
		usage.CacheCreationInputTokens,
		usage.CacheReadInputTokens,
		usage.OutputTokens,
		usage.ReasoningOutputTokens,
		usage.TotalTokens,
	}
	for _, value := range values {
		if value < 0 {
			return fmt.Errorf("%w: token counts cannot be negative", errInvalidLedger)
		}
	}

	return nil
}

func usageDecreased(previous, current TokenUsage) bool {
	return current.InputTokens < previous.InputTokens ||
		current.CachedInputTokens < previous.CachedInputTokens ||
		current.CacheCreationInputTokens < previous.CacheCreationInputTokens ||
		current.CacheReadInputTokens < previous.CacheReadInputTokens ||
		current.OutputTokens < previous.OutputTokens ||
		current.ReasoningOutputTokens < previous.ReasoningOutputTokens ||
		current.TotalTokens < previous.TotalTokens
}

func addUsage(left, right TokenUsage) TokenUsage {
	return TokenUsage{
		InputTokens:              left.InputTokens + right.InputTokens,
		CachedInputTokens:        left.CachedInputTokens + right.CachedInputTokens,
		CacheCreationInputTokens: left.CacheCreationInputTokens + right.CacheCreationInputTokens,
		CacheReadInputTokens:     left.CacheReadInputTokens + right.CacheReadInputTokens,
		OutputTokens:             left.OutputTokens + right.OutputTokens,
		ReasoningOutputTokens:    left.ReasoningOutputTokens + right.ReasoningOutputTokens,
		TotalTokens:              left.TotalTokens + right.TotalTokens,
	}
}

func setStableLedgerValue(target *string, incoming, field string) error {
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return nil
	}
	if *target == "" {
		*target = incoming

		return nil
	}
	if *target != incoming {
		return fmt.Errorf(
			"%w: %s changed from %q to %q",
			errInvalidLedger,
			field,
			*target,
			incoming,
		)
	}

	return nil
}

func firstLedgerValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
