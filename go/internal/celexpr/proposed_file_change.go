// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"bytes"
	"os"
	"path"
	"strings"
)

type ProposedFileChangeInput struct {
	Base                 string `json:"base"`
	Dir                  string `json:"dir"`
	Ext                  string `json:"ext"`
	File                 string `json:"file"`
	CurrentLineCount     int64  `json:"current_line_count"`
	ProposedLineCount    int64  `json:"proposed_line_count"`
	LineDelta            int64  `json:"line_delta"`
	CurrentSizeBytes     int64  `json:"current_size_bytes"`
	ProposedSizeBytes    int64  `json:"proposed_size_bytes"`
	SizeDelta            int64  `json:"size_delta"`
	Exists               bool   `json:"exists"`
	HasProposedContent   bool   `json:"has_proposed_content"`
	IsBinary             bool   `json:"is_binary"`
	IsGenerated          bool   `json:"is_generated"`
	IsTest               bool   `json:"is_test"`
	LineCountGrows       bool   `json:"line_count_grows"`
	LineCountShrinks     bool   `json:"line_count_shrinks"`
	SizeGrows            bool   `json:"size_grows"`
	SizeShrinks          bool   `json:"size_shrinks"`
	ReplacementMatched   bool   `json:"replacement_matched"`
	ReplacementAmbiguous bool   `json:"replacement_ambiguous"`
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
	currentSize := int64(len([]byte(currentContent)))
	proposedSize := int64(len([]byte(proposedContent)))

	return ProposedFileChangeInput{
		Base:                 path.Base(cleanFile),
		Dir:                  path.Dir(cleanFile),
		Ext:                  strings.ToLower(path.Ext(cleanFile)),
		File:                 cleanFile,
		CurrentLineCount:     int64(currentLines),
		ProposedLineCount:    int64(proposedLines),
		LineDelta:            int64(proposedLines - currentLines),
		CurrentSizeBytes:     currentSize,
		ProposedSizeBytes:    proposedSize,
		SizeDelta:            proposedSize - currentSize,
		Exists:               exists,
		HasProposedContent:   true,
		IsGenerated:          isGeneratedPath(cleanFile),
		IsTest:               isTestPath(cleanFile),
		LineCountGrows:       proposedLines > currentLines,
		LineCountShrinks:     proposedLines < currentLines,
		SizeGrows:            proposedSize > currentSize,
		SizeShrinks:          proposedSize < currentSize,
		ReplacementMatched:   matched,
		ReplacementAmbiguous: ambiguous,
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
