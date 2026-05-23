// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	fileReadCacheTransform = "file-read-cache"
	fileReadCachePolicyID  = "proxy.file_read_cache"
	fileReadCacheStub      = "[CACHED: You already have this file in your " +
		"context. Do not read it again unless instructed.]"
)

var errFileReadCacheTargetOutsideRoot = apperror.StaticError(
	"file-read cache target outside repo root",
)

type FileReadCacheRequest struct {
	RecordedAtUTC time.Time
	Provider      string
	Tool          string
	Model         string
	EventID       string
	SessionID     string
	RepoRoot      string
	Cwd           string
	TargetPath    string
	TraceID       string
	TrackingID    string
}

type FileReadCacheResult struct {
	Text         string `json:"text"`
	TargetPath   string `json:"target_path"`
	ContentHash  string `json:"content_hash"`
	CacheKey     string `json:"cache_key"`
	Decision     string `json:"decision"`
	originalText string

	Event    agentproxy.ProviderEvent `json:"event"`
	CacheHit bool                     `json:"cache_hit"`
}

func (store *Store) ReadFileWithCache(
	ctx context.Context,
	request FileReadCacheRequest,
) (FileReadCacheResult, error) {
	targetPath, fullPath, err := resolvedReadCachePath(
		request.RepoRoot,
		request.TargetPath,
	)
	if err != nil {
		return FileReadCacheResult{}, err
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return FileReadCacheResult{}, fmt.Errorf(
			"read file for proxy cache %s: %w",
			targetPath,
			err,
		)
	}

	sessionID := strings.TrimSpace(request.SessionID)
	text := string(content)
	contentHash := agentproxy.HashText(text)
	cacheKey := "file-read:" + sessionID + ":" + targetPath

	cacheHit, err := store.fileReadCacheHit(
		ctx,
		sessionID,
		targetPath,
		contentHash,
	)
	if err != nil {
		return FileReadCacheResult{}, err
	}

	result := FileReadCacheResult{
		Text:         text,
		TargetPath:   targetPath,
		ContentHash:  contentHash,
		CacheKey:     cacheKey,
		Decision:     "allow",
		CacheHit:     cacheHit,
		originalText: text,
	}
	if cacheHit {
		result.Text = fileReadCacheStub
		result.Decision = "cache_hit"
	}

	result.Event = fileReadCacheEvent(request, result)

	err = store.RecordProxyEvent(ctx, result.Event)
	if err != nil {
		return FileReadCacheResult{}, fmt.Errorf("record file-read cache event: %w", err)
	}

	return result, nil
}

func resolvedReadCachePath(root, target string) (string, string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}

	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", apperror.StaticError("file-read cache target path is required")
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve repo root %q: %w", root, err)
	}

	absoluteRoot, err = filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve repo root symlinks %q: %w", root, err)
	}

	absoluteTarget := target
	if !filepath.IsAbs(absoluteTarget) {
		absoluteTarget = filepath.Join(absoluteRoot, absoluteTarget)
	}

	absoluteTarget, err = filepath.Abs(absoluteTarget)
	if err != nil {
		return "", "", fmt.Errorf("resolve file-read cache target %q: %w", target, err)
	}

	absoluteTarget, err = filepath.EvalSymlinks(absoluteTarget)
	if err != nil {
		return "", "", fmt.Errorf(
			"resolve file-read cache target symlinks %q: %w",
			target,
			err,
		)
	}

	relative, err := filepath.Rel(absoluteRoot, absoluteTarget)
	if err != nil {
		return "", "", fmt.Errorf("relativize file-read cache target %q: %w", target, err)
	}

	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%w: %s", errFileReadCacheTargetOutsideRoot, target)
	}

	return filepath.ToSlash(relative), absoluteTarget, nil
}

func (store *Store) fileReadCacheHit(
	ctx context.Context,
	sessionID string,
	targetPath string,
	contentHash string,
) (bool, error) {
	if strings.TrimSpace(sessionID) == "" {
		return false, apperror.StaticError("file-read cache session id is required")
	}

	var eventID string

	err := store.database.QueryRowContext(
		ctx,
		`SELECT event_id
		FROM proxy_events
		WHERE session_id = ?
			AND target_path = ?
			AND output_hash = ?
			AND event_kind = ?
		ORDER BY recorded_at_utc DESC, event_id DESC
		LIMIT 1`,
		sessionID,
		targetPath,
		contentHash,
		string(agentproxy.EventFileRead),
	).Scan(&eventID)
	if err == nil {
		return true, nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	return false, fmt.Errorf("query file-read cache: %w", err)
}

func fileReadCacheEvent(
	request FileReadCacheRequest,
	result FileReadCacheResult,
) agentproxy.ProviderEvent {
	kind := agentproxy.EventFileRead
	if result.CacheHit {
		kind = agentproxy.EventCacheHit
	}

	eventID := strings.TrimSpace(request.EventID)
	if eventID == "" {
		eventID = stableID(
			"proxy-file-read",
			request.SessionID,
			result.TargetPath,
			result.Decision,
			result.ContentHash,
			time.Now().UTC().Format(time.RFC3339Nano),
		)
	}

	recordedAt := request.RecordedAtUTC
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}

	return agentproxy.ProviderEvent{
		ID:            eventID,
		SessionID:     strings.TrimSpace(request.SessionID),
		Kind:          kind,
		Provider:      strings.TrimSpace(request.Provider),
		Tool:          strings.TrimSpace(request.Tool),
		Model:         strings.TrimSpace(request.Model),
		RecordedAtUTC: recordedAt,
		RepoRoot:      strings.TrimSpace(request.RepoRoot),
		Cwd:           strings.TrimSpace(request.Cwd),
		TargetPath:    result.TargetPath,
		TraceID:       strings.TrimSpace(request.TraceID),
		TrackingID:    strings.TrimSpace(request.TrackingID),
		Direction:     agentproxy.DirectionLocal,
		PayloadKind:   agentproxy.PayloadFileContent,
		CacheKey:      result.CacheKey,
		InputHash:     result.CacheKey,
		OutputHash:    result.ContentHash,
		PolicyID:      fileReadCachePolicyID,
		Decision:      result.Decision,
		Policy: agentproxy.PolicyEvidence{
			PolicyID: fileReadCachePolicyID,
			Decision: result.Decision,
			Reason:   "session-scoped file read cache",
		},
		Payload: agentproxy.PayloadMeasurement{
			Bytes: len([]byte(result.Text)),
			Lines: lineCount(result.Text),
		},
		Transforms: fileReadCacheTransforms(result),
	}
}

func fileReadCacheTransforms(result FileReadCacheResult) []agentproxy.TransformRecord {
	if !result.CacheHit {
		return nil
	}

	tokenizer := agentproxy.ApproximateTokenizer{}

	return []agentproxy.TransformRecord{{
		Name:         fileReadCacheTransform,
		Reason:       "unchanged file already read in this session",
		InputHash:    result.ContentHash,
		OutputHash:   agentproxy.HashText(result.Text),
		PolicyID:     fileReadCachePolicyID,
		Decision:     result.Decision,
		InputTokens:  tokenizer.Count(result.originalText),
		OutputTokens: tokenizer.Count(result.Text),
		BytesRemoved: max(0, len([]byte(result.originalText))-len([]byte(result.Text))),
	}}
}

func lineCount(text string) int {
	count := 0
	for range strings.Lines(text) {
		count++
	}

	return count
}
