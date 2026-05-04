// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"bytes"
	"os"
	"path"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

type ProposedFileChangeInput struct {
	Base                      string `json:"base"`
	Dir                       string `json:"dir"`
	Ext                       string `json:"ext"`
	File                      string `json:"file"`
	CurrentLineCount          int64  `json:"current_line_count"`
	ProposedLineCount         int64  `json:"proposed_line_count"`
	LineDelta                 int64  `json:"line_delta"`
	CurrentNonBlankLineCount  int64  `json:"current_nonblank_line_count"`
	ProposedNonBlankLineCount int64  `json:"proposed_nonblank_line_count"`
	NonBlankLineDelta         int64  `json:"nonblank_line_delta"`
	CurrentSizeBytes          int64  `json:"current_size_bytes"`
	ProposedSizeBytes         int64  `json:"proposed_size_bytes"`
	SizeDelta                 int64  `json:"size_delta"`
	Exists                    bool   `json:"exists"`
	HasProposedContent        bool   `json:"has_proposed_content"`
	IsBinary                  bool   `json:"is_binary"`
	IsGenerated               bool   `json:"is_generated"`
	IsTest                    bool   `json:"is_test"`
	LineCountGrows            bool   `json:"line_count_grows"`
	LineCountShrinks          bool   `json:"line_count_shrinks"`
	NonBlankLineCountGrows    bool   `json:"nonblank_line_count_grows"`
	NonBlankLineCountShrinks  bool   `json:"nonblank_line_count_shrinks"`
	SizeGrows                 bool   `json:"size_grows"`
	SizeShrinks               bool   `json:"size_shrinks"`
	ReplacementMatched        bool   `json:"replacement_matched"`
	ReplacementAmbiguous      bool   `json:"replacement_ambiguous"`
}

type ProposedSymbolChangeInput struct {
	Base                      string `json:"base"`
	CurrentContentHash        string `json:"current_content_hash"`
	Dir                       string `json:"dir"`
	Ext                       string `json:"ext"`
	File                      string `json:"file"`
	Language                  string `json:"language"`
	NodeKind                  string `json:"node_kind"`
	ProposedContentHash       string `json:"proposed_content_hash"`
	SymbolKind                string `json:"symbol_kind"`
	SymbolName                string `json:"symbol_name"`
	SymbolPath                string `json:"symbol_path"`
	Action                    string `json:"action"`
	CurrentEndLine            int64  `json:"current_end_line"`
	CurrentLineCount          int64  `json:"current_line_count"`
	CurrentNonBlankLineCount  int64  `json:"current_nonblank_line_count"`
	CurrentStartLine          int64  `json:"current_start_line"`
	LineDelta                 int64  `json:"line_delta"`
	NonBlankLineDelta         int64  `json:"nonblank_line_delta"`
	ProposedEndLine           int64  `json:"proposed_end_line"`
	ProposedLineCount         int64  `json:"proposed_line_count"`
	ProposedNonBlankLineCount int64  `json:"proposed_nonblank_line_count"`
	ProposedStartLine         int64  `json:"proposed_start_line"`
	IsGenerated               bool   `json:"is_generated"`
	IsTest                    bool   `json:"is_test"`
	LineCountGrows            bool   `json:"line_count_grows"`
	LineCountShrinks          bool   `json:"line_count_shrinks"`
	NonBlankLineCountGrows    bool   `json:"nonblank_line_count_grows"`
	NonBlankLineCountShrinks  bool   `json:"nonblank_line_count_shrinks"`
}

func proposedFileChangeInputs(input ActivationInput) []ProposedFileChangeInput {
	files := cleanStringSlice(input.Files)
	if len(files) == 0 {
		return nil
	}

	changes := make([]ProposedFileChangeInput, 0, len(files))
	for _, file := range files {
		change, ok := proposedFileChangeInput(input, file)
		if ok {
			changes = append(changes, change)
		}
	}

	return changes
}

func proposedSymbolChangeInputs(input ActivationInput) []ProposedSymbolChangeInput {
	files := cleanStringSlice(input.Files)
	if len(files) == 0 {
		return nil
	}

	changes := []ProposedSymbolChangeInput{}
	for _, file := range files {
		change, ok := proposedFileChangeInput(input, file)
		if !ok || !change.HasProposedContent || change.IsBinary {
			continue
		}
		currentContent, _, binary := readTextFile(input.Cwd, change.File)
		if binary {
			continue
		}
		proposedContent, _, _, ok := proposedContentForTool(
			input.Tool,
			currentContent,
			input.OldContent,
			input.Content,
			change.Exists,
		)
		if !ok {
			continue
		}
		changes = append(changes, symbolChangesForContent(change.File, currentContent, proposedContent)...)
	}

	return changes
}

func symbolChangesForContent(
	file string,
	currentContent string,
	proposedContent string,
) []ProposedSymbolChangeInput {
	currentFile, currentOK, currentErr := astfacts.Analyze(file, []byte(currentContent))
	proposedFile, proposedOK, proposedErr := astfacts.Analyze(file, []byte(proposedContent))
	if currentErr != nil || proposedErr != nil || !currentOK || !proposedOK {
		return nil
	}
	current := symbolsByKey(currentFile.Symbols)
	proposed := symbolsByKey(proposedFile.Symbols)
	keys := sortedSymbolKeys(current, proposed)
	changes := make([]ProposedSymbolChangeInput, 0, len(keys))
	for _, key := range keys {
		currentSymbol, hasCurrent := current[key]
		proposedSymbol, hasProposed := proposed[key]
		change := proposedSymbolChange(file, currentSymbol, hasCurrent, proposedSymbol, hasProposed)
		if change.Action != "unchanged" {
			changes = append(changes, change)
		}
	}

	return changes
}

func symbolsByKey(symbols []astfacts.Symbol) map[string]astfacts.Symbol {
	result := map[string]astfacts.Symbol{}
	for _, symbol := range symbols {
		result[symbolKey(symbol)] = symbol
	}

	return result
}

func sortedSymbolKeys(left map[string]astfacts.Symbol, right map[string]astfacts.Symbol) []string {
	keys := make([]string, 0, len(left)+len(right))
	for key := range left {
		keys = append(keys, key)
	}
	for key := range right {
		if _, ok := left[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)

	return keys
}

func symbolKey(symbol astfacts.Symbol) string {
	return strings.Join([]string{
		symbol.Language,
		symbol.NodeKind,
		symbol.SymbolKind,
		symbol.SymbolPath,
	}, "\x00")
}

func proposedSymbolChange(
	file string,
	current astfacts.Symbol,
	hasCurrent bool,
	proposed astfacts.Symbol,
	hasProposed bool,
) ProposedSymbolChangeInput {
	symbol := proposed
	if !hasProposed {
		symbol = current
	}
	currentLines := 0
	proposedLines := 0
	currentNonBlankLines := 0
	proposedNonBlankLines := 0
	if hasCurrent {
		currentLines = current.LineCount
		currentNonBlankLines = countNonBlankLines(current.RawText)
	}
	if hasProposed {
		proposedLines = proposed.LineCount
		proposedNonBlankLines = countNonBlankLines(proposed.RawText)
	}
	action := "unchanged"
	switch {
	case !hasCurrent && hasProposed:
		action = "added"
	case hasCurrent && !hasProposed:
		action = "deleted"
	case current.ContentHash != proposed.ContentHash:
		action = "modified"
	}

	return ProposedSymbolChangeInput{
		Base:                      path.Base(file),
		CurrentContentHash:        current.ContentHash,
		Dir:                       path.Dir(file),
		Ext:                       strings.ToLower(path.Ext(file)),
		File:                      file,
		Language:                  symbol.Language,
		NodeKind:                  symbol.NodeKind,
		ProposedContentHash:       proposed.ContentHash,
		SymbolKind:                symbol.SymbolKind,
		SymbolName:                symbol.SymbolName,
		SymbolPath:                symbol.SymbolPath,
		Action:                    action,
		CurrentEndLine:            int64(current.EndLine),
		CurrentLineCount:          int64(currentLines),
		CurrentNonBlankLineCount:  int64(currentNonBlankLines),
		CurrentStartLine:          int64(current.StartLine),
		LineDelta:                 int64(proposedLines - currentLines),
		NonBlankLineDelta:         int64(proposedNonBlankLines - currentNonBlankLines),
		ProposedEndLine:           int64(proposed.EndLine),
		ProposedLineCount:         int64(proposedLines),
		ProposedNonBlankLineCount: int64(proposedNonBlankLines),
		ProposedStartLine:         int64(proposed.StartLine),
		IsGenerated:               isGeneratedPath(file),
		IsTest:                    isTestPath(file),
		LineCountGrows:            proposedLines > currentLines,
		LineCountShrinks:          proposedLines < currentLines,
		NonBlankLineCountGrows:    proposedNonBlankLines > currentNonBlankLines,
		NonBlankLineCountShrinks:  proposedNonBlankLines < currentNonBlankLines,
	}
}

func proposedFileChangeInput(
	input ActivationInput,
	file string,
) (ProposedFileChangeInput, bool) {
	cleanFile := cleanInputFile(file)
	if cleanFile == "" {
		return ProposedFileChangeInput{}, false
	}

	currentContent, exists, binary := readTextFile(input.Cwd, cleanFile)
	if binary {
		return ProposedFileChangeInput{
			Base:     path.Base(cleanFile),
			Dir:      path.Dir(cleanFile),
			Ext:      strings.ToLower(path.Ext(cleanFile)),
			File:     cleanFile,
			Exists:   exists,
			IsBinary: true,
		}, true
	}

	proposedContent, matched, ambiguous, ok := proposedContentForTool(
		input.Tool,
		currentContent,
		input.OldContent,
		input.Content,
		exists,
	)
	if !ok {
		return ProposedFileChangeInput{}, false
	}

	currentLines := countLines(currentContent)
	proposedLines := countLines(proposedContent)
	currentNonBlankLines := countNonBlankLines(currentContent)
	proposedNonBlankLines := countNonBlankLines(proposedContent)
	currentSize := int64(len([]byte(currentContent)))
	proposedSize := int64(len([]byte(proposedContent)))

	return ProposedFileChangeInput{
		Base:                      path.Base(cleanFile),
		Dir:                       path.Dir(cleanFile),
		Ext:                       strings.ToLower(path.Ext(cleanFile)),
		File:                      cleanFile,
		CurrentLineCount:          int64(currentLines),
		ProposedLineCount:         int64(proposedLines),
		LineDelta:                 int64(proposedLines - currentLines),
		CurrentNonBlankLineCount:  int64(currentNonBlankLines),
		ProposedNonBlankLineCount: int64(proposedNonBlankLines),
		NonBlankLineDelta:         int64(proposedNonBlankLines - currentNonBlankLines),
		CurrentSizeBytes:          currentSize,
		ProposedSizeBytes:         proposedSize,
		SizeDelta:                 proposedSize - currentSize,
		Exists:                    exists,
		HasProposedContent:        true,
		IsGenerated:               isGeneratedPath(cleanFile),
		IsTest:                    isTestPath(cleanFile),
		LineCountGrows:            proposedLines > currentLines,
		LineCountShrinks:          proposedLines < currentLines,
		NonBlankLineCountGrows:    proposedNonBlankLines > currentNonBlankLines,
		NonBlankLineCountShrinks:  proposedNonBlankLines < currentNonBlankLines,
		SizeGrows:                 proposedSize > currentSize,
		SizeShrinks:               proposedSize < currentSize,
		ReplacementMatched:        matched,
		ReplacementAmbiguous:      ambiguous,
	}, true
}

func proposedContentForTool(
	tool string,
	currentContent string,
	oldContent string,
	newContent string,
	exists bool,
) (string, bool, bool, bool) {
	switch tool {
	case "Write":
		return newContent, exists, false, true
	case "Edit", "MultiEdit":
		if oldContent == "" {
			return "", false, false, false
		}
		count := strings.Count(currentContent, oldContent)
		if count == 0 {
			return currentContent, false, false, true
		}
		if count > 1 {
			return strings.ReplaceAll(currentContent, oldContent, newContent), true, true, true
		}

		return strings.Replace(currentContent, oldContent, newContent, 1), true, false, true
	default:
		return "", false, false, false
	}
}

func readTextFile(cwd string, file string) (string, bool, bool) {
	content, err := os.ReadFile(resolveFilePath(cwd, file))
	if err != nil {
		return "", false, false
	}
	if bytes.Contains(content, []byte{0}) {
		return "", true, true
	}

	return string(content), true, false
}
