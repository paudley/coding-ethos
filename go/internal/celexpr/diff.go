// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"strconv"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

const hunkHeaderMinimumFields = 3

func contentDiffHunkInputs(path, before, after string) []DiffHunkInput {
	if path == "" {
		return []DiffHunkInput{}
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(before, after, false)
	patch := dmp.PatchMake(before, diffs)

	// Convert patch into a simplified unified
	// diff that ParseDiffHunks can understand. ParseDiffHunks expects +++ and @@ headers.
	var sb strings.Builder
	sb.WriteString("+++ " + path + "\n")
	sb.WriteString(dmp.PatchToText(patch))

	return ParseDiffHunks(sb.String(), []string{path})
}

func diffHunkInputs(cwd string, files []string) []DiffHunkInput {
	if cwd == "" {
		return []DiffHunkInput{}
	}

	output, err := gitOutput(cwd, "diff", "--cached", "--unified=0", "--no-ext-diff")
	if err != nil {
		return []DiffHunkInput{}
	}

	return parseDiffHunks(output, files)
}

func parseDiffHunks(diff string, files []string) []DiffHunkInput {
	return ParseDiffHunks(diff, files)
}

// ParseDiffHunks parses unified git diff hunks into CEL-ready diff facts.
func ParseDiffHunks(diff string, files []string) []DiffHunkInput {
	selected := selectedDiffFiles(files)
	hunks := []DiffHunkInput{}
	state := diffParseState{currentHunk: -1}

	for rawLine := range strings.SplitSeq(diff, "\n") {
		line := strings.TrimSuffix(rawLine, "\r")
		if strings.HasPrefix(line, "+++ ") {
			state.currentFile = diffPath(line[4:])
			state.currentHunk = -1

			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			nextState, hunk, found := parseDiffHunkHeaderLine(state, selected, line)
			state = nextState

			if found {
				hunks = append(hunks, hunk)
				state.currentHunk = len(hunks) - 1
			}

			continue
		}

		if state.currentHunk != -1 && line != "" {
			state = appendDiffLine(&hunks[state.currentHunk], state, line)
		}
	}

	return hunks
}

type diffParseState struct {
	currentFile string
	oldLine     int64
	newLine     int64
	currentHunk int
}

func parseDiffHunkHeaderLine(
	state diffParseState,
	selected map[string]bool,
	line string,
) (diffParseState, DiffHunkInput, bool) {
	state.currentHunk = -1
	if state.currentFile == "" || !diffFileSelected(state.currentFile, selected) {
		return state, DiffHunkInput{}, false
	}

	hunk, oldLine, newLine, found := parseHunkHeader(state.currentFile, line)
	if !found {
		return state, DiffHunkInput{}, false
	}

	state.oldLine = oldLine
	state.newLine = newLine

	return state, hunk, true
}

func appendDiffLine(
	hunk *DiffHunkInput,
	state diffParseState,
	line string,
) diffParseState {
	switch {
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		hunk.AddedLines = append(hunk.AddedLines, addedDiffLine(state, line))
		state.newLine++
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		hunk.RemovedLines = append(hunk.RemovedLines, removedDiffLine(state, line))
		state.oldLine++
	case strings.HasPrefix(line, " "):
		state.oldLine++
		state.newLine++
	}

	return state
}

func addedDiffLine(state diffParseState, line string) DiffLineInput {
	return DiffLineInput{
		File:    state.currentFile,
		Text:    line[1:],
		Line:    state.newLine,
		NewLine: state.newLine,
		IsBlank: isBlankLine(line[1:]),
	}
}

func removedDiffLine(state diffParseState, line string) DiffLineInput {
	return DiffLineInput{
		File:    state.currentFile,
		Text:    line[1:],
		Line:    state.oldLine,
		OldLine: state.oldLine,
		IsBlank: isBlankLine(line[1:]),
	}
}

func selectedDiffFiles(files []string) map[string]bool {
	selected := map[string]bool{}

	for _, file := range files {
		cleanFile := cleanInputFile(file)
		if cleanFile != "" {
			selected[cleanFile] = true
		}
	}

	return selected
}

func diffFileSelected(file string, selected map[string]bool) bool {
	return len(selected) == 0 || selected[file]
}

func diffPath(raw string) string {
	trimmed := strings.Trim(strings.TrimSpace(raw), `"`)
	if trimmed == "/dev/null" {
		return ""
	}

	if strings.HasPrefix(trimmed, "a/") || strings.HasPrefix(trimmed, "b/") {
		trimmed = trimmed[2:]
	}

	return cleanInputFile(trimmed)
}

func parseHunkHeader(
	file string,
	header string,
) (DiffHunkInput, int64, int64, bool) {
	fields := strings.Fields(header)
	if len(fields) < hunkHeaderMinimumFields {
		return DiffHunkInput{}, 0, 0, false
	}

	oldStart, oldLines, found := parseDiffRange(strings.TrimPrefix(fields[1], "-"))
	if !found {
		return DiffHunkInput{}, 0, 0, false
	}

	newStart, newLines, found := parseDiffRange(strings.TrimPrefix(fields[2], "+"))
	if !found {
		return DiffHunkInput{}, 0, 0, false
	}

	return DiffHunkInput{
		File:         file,
		Header:       header,
		OldStart:     oldStart,
		OldLines:     oldLines,
		NewStart:     newStart,
		NewLines:     newLines,
		AddedLines:   []DiffLineInput{},
		RemovedLines: []DiffLineInput{},
	}, oldStart, newStart, true
}

func parseDiffRange(source string) (int64, int64, bool) {
	startText, lineText, hasLineCount := strings.Cut(source, ",")

	start, err := strconv.ParseInt(startText, 10, 64)
	if err != nil {
		return 0, 0, false
	}

	if !hasLineCount {
		return start, 1, true
	}

	lines, err := strconv.ParseInt(lineText, 10, 64)
	if err != nil {
		return 0, 0, false
	}

	return start, lines, true
}

func diffLines(hunks []DiffHunkInput) ([]DiffLineInput, []DiffLineInput) {
	added := []DiffLineInput{}
	removed := []DiffLineInput{}

	for _, hunk := range hunks {
		added = append(added, hunk.AddedLines...)
		removed = append(removed, hunk.RemovedLines...)
	}

	return added, removed
}
