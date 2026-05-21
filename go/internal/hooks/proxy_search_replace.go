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

func proxySearchReplaceEditDecisions(
	bundle policy.Bundle,
	event Event,
) []policy.Decision {
	if event.HookEventName != eventPreToolUse || !isEditTool(event.ToolName) {
		return nil
	}

	policyDef := proxySearchReplacePolicy(bundle)
	switch event.ToolName {
	case toolWrite:
		return proxyWriteDecisions(event, policyDef)
	case toolEdit, toolMultiEdit:
		return proxyEditDecisions(event, policyDef)
	default:
		return nil
	}
}

func proxySearchReplacePolicy(bundle policy.Bundle) policy.Policy {
	policyDef, ok := bundle.Policies[policy.ProxySearchReplaceEditPolicyID]
	if ok {
		return policyDef
	}

	return policy.ProxySearchReplaceEditPolicy()
}

func proxyWriteDecisions(event Event, policyDef policy.Policy) []policy.Decision {
	file, ok := singleEventFile(event)
	if !ok || !regularTextFileExists(event.Cwd, file) {
		return nil
	}

	return []policy.Decision{
		proxySearchReplaceDecision(policyDef, event, map[string]any{
			"file":   file,
			"reason": "write_existing_file",
		}),
	}
}

func proxyEditDecisions(event Event, policyDef policy.Policy) []policy.Decision {
	files := event.Files()
	if len(files) > 1 {
		return []policy.Decision{proxySearchReplaceDecision(policyDef, event, map[string]any{
			"reason":     "invalid_edit_target",
			"file_count": len(files),
		})}
	}

	file, foundFile := singleEventFile(event)
	if !foundFile {
		return nil
	}

	content, foundText, rejected := readSearchReplaceTextFile(event.Cwd, file)
	if rejected {
		return []policy.Decision{proxySearchReplaceDecision(policyDef, event, map[string]any{
			"file":   file,
			"reason": "invalid_edit_target",
		})}
	}

	if !foundText {
		return nil
	}

	blocks, foundBlocks := eventSearchReplaceBlocks(event)
	if !foundBlocks {
		reason := "missing"
		if event.ToolName == toolMultiEdit {
			reason = "malformed_multiedit"
		}

		return []policy.Decision{proxySearchReplaceDecision(policyDef, event, map[string]any{
			"file":                 file,
			"reason":               reason,
			"current_content_hash": agentproxy.HashText(content),
		})}
	}

	result := agentproxy.ApplySearchReplacePatch(agentproxy.SearchReplacePatchRequest{
		Path:           file,
		CurrentContent: content,
		Blocks:         blocks,
	})

	for _, block := range result.Blocks {
		if block.Status != agentproxy.SearchReplaceStatusOK {
			return []policy.Decision{proxySearchReplaceDecision(policyDef, event, map[string]any{
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

func proxySearchReplaceDecision(
	policyDef policy.Policy,
	event Event,
	evidence map[string]any,
) policy.Decision {
	decision := policy.NewDecision("block", policyDef)
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
	path, exists, regular, rejected := searchReplaceFilePath(cwd, file)
	if rejected {
		return true
	}

	if !exists || !regular {
		return false
	}

	handle, err := os.Open(path)
	if err != nil {
		return true
	}
	defer handle.Close()

	buffer := make([]byte, searchReplaceBinaryProbeBytes)
	count, err := handle.Read(buffer)

	if err != nil && !errors.Is(err, io.EOF) {
		return true
	}

	return !bytes.Contains(buffer[:count], []byte{0})
}

func readSearchReplaceTextFile(cwd, file string) (string, bool, bool) {
	content, exists, binary, rejected := readSearchReplaceFile(cwd, file)
	if rejected {
		return "", false, true
	}

	return content, exists && !binary, false
}

func readSearchReplaceFile(cwd, file string) (string, bool, bool, bool) {
	path, exists, regular, rejected := searchReplaceFilePath(cwd, file)
	if rejected {
		return "", true, false, true
	}

	if !exists || !regular {
		return "", false, false, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, false, false
	}

	if bytes.Contains(data, []byte{0}) {
		return "", true, true, false
	}

	return string(data), true, false, false
}

func searchReplaceFilePath(cwd, file string) (string, bool, bool, bool) {
	path, err := searchReplacePath(cwd, file)
	if err != nil {
		return "", false, false, false
	}

	info, err := os.Lstat(path)
	if err != nil {
		return "", false, false, false
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return path, true, false, true
	}

	return path, true, info.Mode().IsRegular(), false
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
	return file == "." || !filepath.IsLocal(file)
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
