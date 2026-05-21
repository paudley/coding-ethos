// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

var errProxySearchReplaceInvalidPath = errors.New("invalid search/replace edit path")

const (
	searchReplaceBinaryProbeBytes = 8192

	toolEdit      = "Edit"
	toolMultiEdit = "MultiEdit"
	toolWrite     = "Write"
)

func proxySearchReplaceEditDecisions(event Event) []policy.Decision {
	if event.HookEventName != eventPreToolUse || !isEditTool(event.ToolName) {
		return nil
	}

	switch event.ToolName {
	case toolWrite:
		return proxyWriteDecisions(event)
	case toolEdit, toolMultiEdit:
		return proxyEditDecisions(event)
	default:
		return nil
	}
}

func proxyWriteDecisions(event Event) []policy.Decision {
	file, ok := singleEventFile(event)
	if !ok || !regularTextFileExists(event.Cwd, file) {
		return nil
	}

	return []policy.Decision{
		proxySearchReplaceDecision(event, map[string]any{
			"file":   file,
			"reason": "write_existing_file",
		}),
	}
}

func proxyEditDecisions(event Event) []policy.Decision {
	file, foundFile := singleEventFile(event)
	if !foundFile {
		return nil
	}

	content, ok := readSearchReplaceTextFile(event.Cwd, file)
	if !ok {
		return nil
	}

	blocks, foundBlocks := eventSearchReplaceBlocks(event)
	if !foundBlocks {
		if event.ToolName == toolMultiEdit {
			return []policy.Decision{proxySearchReplaceDecision(event, map[string]any{
				"file":                 file,
				"reason":               "malformed_multiedit",
				"current_content_hash": agentproxy.HashText(content),
			})}
		}

		return nil
	}

	result := agentproxy.ApplySearchReplacePatch(agentproxy.SearchReplacePatchRequest{
		Path:           file,
		CurrentContent: content,
		Blocks:         blocks,
	})

	for _, block := range result.Blocks {
		if block.Status != agentproxy.SearchReplaceStatusOK {
			return []policy.Decision{proxySearchReplaceDecision(event, map[string]any{
				"file":                 file,
				"reason":               block.Status,
				"block_index":          block.Index,
				"match_count":          block.MatchCount,
				"current_content_hash": result.CurrentContentHash,
			})}
		}
	}

	return nil
}

func proxySearchReplaceDecision(event Event, evidence map[string]any) policy.Decision {
	decision := policy.NewDecision("block", policy.ProxySearchReplaceEditPolicy())
	decision.Evidence = map[string]any{
		"event": event.HookEventName,
		"tool":  event.ToolName,
	}
	maps.Copy(decision.Evidence, evidence)

	return decision
}

func singleEventFile(event Event) (string, bool) {
	files := event.Files()
	if len(files) != 1 {
		return "", false
	}

	clean := filepath.Clean(files[0])
	if invalidSearchReplaceFile(clean) {
		return "", false
	}

	return clean, true
}

func regularTextFileExists(cwd, file string) bool {
	path, err := searchReplacePath(cwd, file)
	if err != nil {
		return false
	}

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}

	handle, err := os.Open(path)
	if err != nil {
		return false
	}
	defer handle.Close()

	buffer := make([]byte, searchReplaceBinaryProbeBytes)
	count, err := handle.Read(buffer)

	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}

	return !bytes.Contains(buffer[:count], []byte{0})
}

func readSearchReplaceTextFile(cwd, file string) (string, bool) {
	content, exists, binary := readSearchReplaceFile(cwd, file)

	return content, exists && !binary
}

func readSearchReplaceFile(cwd, file string) (string, bool, bool) {
	path, err := searchReplacePath(cwd, file)
	if err != nil {
		return "", false, false
	}

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, false
	}

	if bytes.Contains(data, []byte{0}) {
		return "", true, true
	}

	return string(data), true, false
}

func searchReplacePath(cwd, file string) (string, error) {
	if cwd == "" || file == "" {
		return "", errProxySearchReplaceInvalidPath
	}

	clean := filepath.Clean(file)
	if invalidSearchReplaceFile(clean) {
		return "", errProxySearchReplaceInvalidPath
	}

	return filepath.Join(cwd, clean), nil
}

func invalidSearchReplaceFile(file string) bool {
	return slices.Contains([]string{"", ".", ".."}, file) ||
		filepath.IsAbs(file) ||
		strings.HasPrefix(file, ".."+string(filepath.Separator))
}

func eventSearchReplaceBlocks(event Event) ([]agentproxy.SearchReplaceBlock, bool) {
	if event.ToolName == toolMultiEdit {
		return multiEditBlocks(event.ToolInput["edits"])
	}

	search := event.OldContent()
	replace := event.Content()

	if search == "" && replace == "" {
		return nil, false
	}

	return []agentproxy.SearchReplaceBlock{{
		Search:  search,
		Replace: replace,
	}}, true
}

func multiEditBlocks(raw any) ([]agentproxy.SearchReplaceBlock, bool) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil, false
	}

	blocks := make([]agentproxy.SearchReplaceBlock, 0, len(items))
	for _, item := range items {
		edit, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}

		search := stringMapValue(edit, "old_string")
		replace := stringMapValue(edit, "new_string")
		blocks = append(blocks, agentproxy.SearchReplaceBlock{
			Search:  search,
			Replace: replace,
		})
	}

	return blocks, true
}
