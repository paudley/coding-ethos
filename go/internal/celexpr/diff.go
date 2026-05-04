// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"strconv"
	"strings"
)

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
	selected := selectedDiffFiles(files)
	hunks := []DiffHunkInput{}
	currentFile := ""
	oldLine := int64(0)
	newLine := int64(0)
	currentHunk := -1

	for _, rawLine := range strings.Split(diff, "\n") {
		line := strings.TrimSuffix(rawLine, "\r")
		if strings.HasPrefix(line, "+++ ") {
			currentFile = diffPath(line[4:])
			currentHunk = -1

			continue
		}
		if strings.HasPrefix(line, "@@ ") {
			if currentFile == "" || !diffFileSelected(currentFile, selected) {
				currentHunk = -1

				continue
			}

			hunk, parsedOldLine, parsedNewLine, ok := parseHunkHeader(currentFile, line)
			if !ok {
				currentHunk = -1

				continue
			}
			hunks = append(hunks, hunk)
			currentHunk = len(hunks) - 1
			oldLine = parsedOldLine
			newLine = parsedNewLine

			continue
		}
		if currentHunk == -1 || line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added := DiffLineInput{
				File:    currentFile,
				Text:    line[1:],
				Line:    newLine,
				NewLine: newLine,
				IsBlank: isBlankLine(line[1:]),
			}
			hunks[currentHunk].AddedLines = append(hunks[currentHunk].AddedLines, added)
			newLine++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			removed := DiffLineInput{
				File:    currentFile,
				Text:    line[1:],
				Line:    oldLine,
				OldLine: oldLine,
				IsBlank: isBlankLine(line[1:]),
			}
			hunks[currentHunk].RemovedLines = append(
				hunks[currentHunk].RemovedLines,
				removed,
			)
			oldLine++
		case strings.HasPrefix(line, " "):
			oldLine++
			newLine++
		}
	}

	return hunks
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
	if len(fields) < 3 {
		return DiffHunkInput{}, 0, 0, false
	}

	oldStart, oldLines, ok := parseDiffRange(strings.TrimPrefix(fields[1], "-"))
	if !ok {
		return DiffHunkInput{}, 0, 0, false
	}
	newStart, newLines, ok := parseDiffRange(strings.TrimPrefix(fields[2], "+"))
	if !ok {
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
