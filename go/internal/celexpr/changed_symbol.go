// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"bytes"
	"path"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

type ChangedSymbolInput struct {
	Base                      string  `json:"base"`
	CurrentContentHash        string  `json:"current_content_hash"`
	Dir                       string  `json:"dir"`
	Ext                       string  `json:"ext"`
	File                      string  `json:"file"`
	Language                  string  `json:"language"`
	NodeKind                  string  `json:"node_kind"`
	OriginalContentHash       string  `json:"original_content_hash"`
	SymbolKind                string  `json:"symbol_kind"`
	SymbolName                string  `json:"symbol_name"`
	SymbolPath                string  `json:"symbol_path"`
	Action                    string  `json:"action"`
	ChangedLines              []int64 `json:"changed_lines"`
	CurrentEndLine            int64   `json:"current_end_line"`
	CurrentLineCount          int64   `json:"current_line_count"`
	CurrentNonBlankLineCount  int64   `json:"current_nonblank_line_count"`
	CurrentStartLine          int64   `json:"current_start_line"`
	LineDelta                 int64   `json:"line_delta"`
	NonBlankLineDelta         int64   `json:"nonblank_line_delta"`
	OriginalEndLine           int64   `json:"original_end_line"`
	OriginalLineCount         int64   `json:"original_line_count"`
	OriginalNonBlankLineCount int64   `json:"original_nonblank_line_count"`
	OriginalStartLine         int64   `json:"original_start_line"`
	IsGenerated               bool    `json:"is_generated"`
	IsTest                    bool    `json:"is_test"`
	LineCountGrows            bool    `json:"line_count_grows"`
	LineCountShrinks          bool    `json:"line_count_shrinks"`
	NonBlankLineCountGrows    bool    `json:"nonblank_line_count_grows"`
	NonBlankLineCountShrinks  bool    `json:"nonblank_line_count_shrinks"`
}

func changedSymbolInputs(cwd string, files []string, hunks []DiffHunkInput) []ChangedSymbolInput {
	if cwd == "" {
		return nil
	}

	statuses := gitFileStatuses(cwd)
	changes := []ChangedSymbolInput{}
	for _, file := range cleanStringSlice(files) {
		status := statuses[file]
		fileChanges := changedSymbolsForFile(cwd, file, status, hunks)
		changes = append(changes, fileChanges...)
	}

	return changes
}

func changedSymbolsForFile(
	cwd string,
	file string,
	status gitFileStatus,
	hunks []DiffHunkInput,
) []ChangedSymbolInput {
	currentContent, hasCurrent := gitTextBlob(cwd, ":"+file)
	originalFile := file
	if status.OldFile != "" {
		originalFile = status.OldFile
	}
	originalContent, hasOriginal := gitTextBlob(cwd, "HEAD:"+originalFile)
	if !hasCurrent && !hasOriginal {
		return nil
	}

	currentFile, currentOK := analyzeOptionalFile(file, currentContent, hasCurrent)
	originalFileFacts, originalOK := analyzeOptionalFile(file, originalContent, hasOriginal)
	if !currentOK && !originalOK {
		return nil
	}

	current := symbolsByKey(currentFile.Symbols)
	original := symbolsByKey(originalFileFacts.Symbols)
	keys := sortedSymbolKeys(original, current)
	addedLines, removedLines := changedLineSets(file, hunks)
	changes := make([]ChangedSymbolInput, 0, len(keys))
	for _, key := range keys {
		originalSymbol, hasOriginalSymbol := original[key]
		currentSymbol, hasCurrentSymbol := current[key]
		change := changedSymbolInput(
			file,
			originalSymbol,
			hasOriginalSymbol,
			currentSymbol,
			hasCurrentSymbol,
			addedLines,
			removedLines,
		)
		if change.Action != "unchanged" && symbolTouched(change, addedLines, removedLines) {
			changes = append(changes, change)
		}
	}

	return changes
}

func analyzeOptionalFile(file string, content string, ok bool) (astfacts.File, bool) {
	if !ok {
		return astfacts.File{}, false
	}
	facts, supported, err := astfacts.Analyze(file, []byte(content))
	if err != nil || !supported {
		return astfacts.File{}, false
	}

	return facts, true
}

func gitTextBlob(cwd string, ref string) (string, bool) {
	content, err := gitOutput(cwd, "show", ref)
	if err != nil || bytes.Contains([]byte(content), []byte{0}) {
		return "", false
	}

	return content, true
}

func changedLineSets(file string, hunks []DiffHunkInput) (map[int64]bool, map[int64]bool) {
	added := map[int64]bool{}
	removed := map[int64]bool{}
	for _, hunk := range hunks {
		if hunk.File != file {
			continue
		}
		for _, line := range hunk.AddedLines {
			if line.NewLine > 0 {
				added[line.NewLine] = true
			}
		}
		for _, line := range hunk.RemovedLines {
			if line.OldLine > 0 {
				removed[line.OldLine] = true
			}
		}
	}

	return added, removed
}

func changedSymbolInput(
	file string,
	original astfacts.Symbol,
	hasOriginal bool,
	current astfacts.Symbol,
	hasCurrent bool,
	addedLines map[int64]bool,
	removedLines map[int64]bool,
) ChangedSymbolInput {
	symbol := current
	if !hasCurrent {
		symbol = original
	}
	originalLines := 0
	currentLines := 0
	originalNonBlankLines := 0
	currentNonBlankLines := 0
	if hasOriginal {
		originalLines = original.LineCount
		originalNonBlankLines = countNonBlankLines(original.RawText)
	}
	if hasCurrent {
		currentLines = current.LineCount
		currentNonBlankLines = countNonBlankLines(current.RawText)
	}
	action := "unchanged"
	switch {
	case !hasOriginal && hasCurrent:
		action = "added"
	case hasOriginal && !hasCurrent:
		action = "deleted"
	case original.ContentHash != current.ContentHash:
		action = "modified"
	}

	return ChangedSymbolInput{
		Base:                      path.Base(file),
		CurrentContentHash:        current.ContentHash,
		Dir:                       path.Dir(file),
		Ext:                       strings.ToLower(path.Ext(file)),
		File:                      file,
		Language:                  symbol.Language,
		NodeKind:                  symbol.NodeKind,
		OriginalContentHash:       original.ContentHash,
		SymbolKind:                symbol.SymbolKind,
		SymbolName:                symbol.SymbolName,
		SymbolPath:                symbol.SymbolPath,
		Action:                    action,
		ChangedLines:              changedSymbolLines(original, hasOriginal, current, hasCurrent, addedLines, removedLines),
		CurrentEndLine:            int64(current.EndLine),
		CurrentLineCount:          int64(currentLines),
		CurrentNonBlankLineCount:  int64(currentNonBlankLines),
		CurrentStartLine:          int64(current.StartLine),
		LineDelta:                 int64(currentLines - originalLines),
		NonBlankLineDelta:         int64(currentNonBlankLines - originalNonBlankLines),
		OriginalEndLine:           int64(original.EndLine),
		OriginalLineCount:         int64(originalLines),
		OriginalNonBlankLineCount: int64(originalNonBlankLines),
		OriginalStartLine:         int64(original.StartLine),
		IsGenerated:               isGeneratedPath(file),
		IsTest:                    isTestPath(file),
		LineCountGrows:            currentLines > originalLines,
		LineCountShrinks:          currentLines < originalLines,
		NonBlankLineCountGrows:    currentNonBlankLines > originalNonBlankLines,
		NonBlankLineCountShrinks:  currentNonBlankLines < originalNonBlankLines,
	}
}

func symbolTouched(
	change ChangedSymbolInput,
	addedLines map[int64]bool,
	removedLines map[int64]bool,
) bool {
	if len(change.ChangedLines) > 0 {
		return true
	}
	if change.Action == "added" {
		return spanIntersectsLines(change.CurrentStartLine, change.CurrentEndLine, addedLines)
	}
	if change.Action == "deleted" {
		return spanIntersectsLines(change.OriginalStartLine, change.OriginalEndLine, removedLines)
	}

	return false
}

func changedSymbolLines(
	original astfacts.Symbol,
	hasOriginal bool,
	current astfacts.Symbol,
	hasCurrent bool,
	addedLines map[int64]bool,
	removedLines map[int64]bool,
) []int64 {
	lines := []int64{}
	if hasCurrent {
		lines = append(lines, linesInSpan(current.StartLine, current.EndLine, addedLines)...)
	}
	if hasOriginal {
		lines = append(lines, linesInSpan(original.StartLine, original.EndLine, removedLines)...)
	}

	return uniqueInt64s(lines)
}

func linesInSpan(start int, end int, lines map[int64]bool) []int64 {
	return linesInInt64Span(int64(start), int64(end), lines)
}

func spanIntersectsLines(start int64, end int64, lines map[int64]bool) bool {
	return len(linesInInt64Span(start, end, lines)) > 0
}

func linesInInt64Span(start int64, end int64, lines map[int64]bool) []int64 {
	if start <= 0 || end <= 0 || end < start {
		return nil
	}
	matches := []int64{}
	for line := range lines {
		if line >= start && line <= end {
			matches = append(matches, line)
		}
	}

	return sortedInt64s(matches)
}

func uniqueInt64s(values []int64) []int64 {
	seen := map[int64]bool{}
	unique := []int64{}
	for _, value := range sortedInt64s(values) {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}

	return unique
}

func sortedInt64s(values []int64) []int64 {
	sorted := append([]int64(nil), values...)
	slices.Sort(sorted)

	return sorted
}
