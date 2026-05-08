// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package astfacts_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/astfacts"
)

func TestAnalyzeIndexesMarkdownHeadingsAndCodeBlocks(t *testing.T) {
	t.Parallel()

	content := []byte(`# Project Title

## Installation

` + "```bash" + `
make install
` + "```" + `

## Usage

Check out the CLI.
`)

	parsedFile, analyzedOK, err := Analyze("README.md", content)
	if err != nil {
		t.Fatalf("analyze markdown: %v", err)
	}

	if !analyzedOK || parsedFile.Language != "markdown" {
		t.Fatalf("language = %q, ok=%v", parsedFile.Language, analyzedOK)
	}

	if !hasSymbol(parsedFile.Symbols, "Project Title", "heading") {
		t.Errorf("missing heading: Project Title")
	}

	if !hasSymbol(parsedFile.Symbols, "Installation", "heading") {
		t.Errorf("missing heading: Installation")
	}

	if !hasSymbol(parsedFile.Symbols, "Usage", "heading") {
		t.Errorf("missing heading: Usage")
	}

	// Goldmark seems to report the content start or different line mapping
	if !hasSymbol(parsedFile.Symbols, "block_6", "code_block") {
		t.Errorf("missing code_block: block_6. Symbols: %#v", parsedFile.Symbols)
	}
}
