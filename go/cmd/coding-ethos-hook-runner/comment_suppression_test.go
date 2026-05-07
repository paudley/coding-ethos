// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestFindPythonCommentsIgnoresEscapedQuotesAndTracksLines(t *testing.T) {
	t.Parallel()

	source := strings.Join([]string{
		"value = 'not \\' # comment'\\",
		"continued = \"not # comment\"",
		"# real suppression",
		"",
	}, "\n")
	comments := findPythonComments(source)

	if len(comments) != 1 {
		t.Fatalf("comments = %#v, want one real comment", comments)
	}

	if comments[0].Line != 3 || comments[0].Comment != "# real suppression" {
		t.Fatalf("comment = %#v", comments[0])
	}
}

func TestClassifyCommentSuppressionUsesFirstMatchingPattern(t *testing.T) {
	t.Parallel()

	patterns, err := compileCommentSuppressionPatterns(commentSuppressionSettings{
		Patterns: []commentSuppressionPattern{
			{Regex: `#\s*type:\s*ignore`, Label: "type ignore"},
			{Regex: `#`, Label: "generic"},
			{Regex: ``, Label: "skipped"},
		},
	})
	if err != nil {
		t.Fatalf("compile patterns: %v", err)
	}

	got := classifyCommentSuppression("# type: ignore[arg-type]", patterns)
	if got != "type ignore" {
		t.Fatalf("classifyCommentSuppression = %q", got)
	}

	got = classifyCommentSuppression("# unrelated", patterns)
	if got != "generic" {
		t.Fatalf("classifyCommentSuppression generic = %q", got)
	}
}
