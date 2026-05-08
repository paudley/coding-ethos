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

	// SymbolPath encodes level + start-line + name to ensure uniqueness across
	// duplicate headings; SymbolName stays as the human-readable heading text.
	if !hasSymbol(parsedFile.Symbols, "h1:1:Project Title", "heading") {
		t.Errorf("missing heading: h1:1:Project Title. Symbols: %#v", parsedFile.Symbols)
	}

	if !hasSymbol(parsedFile.Symbols, "h2:3:Installation", "heading") {
		t.Errorf("missing heading: h2:3:Installation. Symbols: %#v", parsedFile.Symbols)
	}

	if !hasSymbol(parsedFile.Symbols, "h2:9:Usage", "heading") {
		t.Errorf("missing heading: h2:9:Usage. Symbols: %#v", parsedFile.Symbols)
	}

	// Goldmark reports the content start for code blocks (first line of body).
	if !hasSymbol(parsedFile.Symbols, "block_6", "code_block") {
		t.Errorf("missing code_block: block_6. Symbols: %#v", parsedFile.Symbols)
	}
}

func TestDuplicateMarkdownHeadingsProduceUniqueSymbolPaths(t *testing.T) {
	t.Parallel()

	// Duplicate headings at the same level are valid Markdown.  They must not
	// produce identical SymbolPaths, which would collide in stableID.
	content := []byte(`# Overview

Some intro text.

# Overview

A second overview section.
`)

	parsedFile, analyzedOK, err := Analyze("README.md", content)
	if err != nil {
		t.Fatalf("analyze markdown: %v", err)
	}

	if !analyzedOK {
		t.Fatalf("analyze returned ok=false")
	}

	seen := make(map[string]int)
	for _, sym := range parsedFile.Symbols {
		if sym.SymbolKind == "heading" {
			seen[sym.SymbolPath]++
		}
	}

	for path, count := range seen {
		if count > 1 {
			t.Errorf("duplicate SymbolPath %q appears %d times; paths must be unique", path, count)
		}
	}

	// Both sections share the name "Overview" but must differ by start line.
	if !hasSymbol(parsedFile.Symbols, "h1:1:Overview", "heading") {
		t.Errorf("missing h1:1:Overview. Symbols: %#v", parsedFile.Symbols)
	}

	if !hasSymbol(parsedFile.Symbols, "h1:5:Overview", "heading") {
		t.Errorf("missing h1:5:Overview. Symbols: %#v", parsedFile.Symbols)
	}
}
