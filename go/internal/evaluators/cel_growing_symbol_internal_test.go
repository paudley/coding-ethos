// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
)

func growingProposedSymbol() celexpr.ProposedSymbolChangeInput {
	return celexpr.ProposedSymbolChangeInput{
		Action:                    "modify",
		File:                      "go/internal/example/widget.go",
		Language:                  "go",
		LineCountGrows:            true,
		LineDelta:                 12,
		NodeKind:                  "function_declaration",
		NonBlankLineDelta:         9,
		SymbolKind:                "function",
		SymbolName:                "Widget",
		SymbolPath:                "example.Widget",
		CurrentLineCount:          20,
		CurrentNonBlankLineCount:  16,
		ProposedLineCount:         32,
		ProposedNonBlankLineCount: 25,
		ProposedStartLine:         41,
	}
}

func growingChangedSymbol() celexpr.ChangedSymbolInput {
	return celexpr.ChangedSymbolInput{
		Action:                    "modify",
		File:                      "go/internal/example/gadget.go",
		Language:                  "go",
		LineCountGrows:            true,
		LineDelta:                 7,
		NodeKind:                  "method_declaration",
		NonBlankLineDelta:         5,
		SymbolKind:                "method",
		SymbolName:                "Gadget",
		SymbolPath:                "example.Gadget",
		CurrentLineCount:          30,
		CurrentNonBlankLineCount:  24,
		CurrentStartLine:          77,
		OriginalLineCount:         23,
		OriginalNonBlankLineCount: 19,
	}
}

func newGrowingSymbolDiagnostic() *diagnostics.Diagnostic {
	return &diagnostics.Diagnostic{Metadata: map[string]any{}}
}

// A proposed change is the edit the agent is about to make, so it must win
// over the already-staged view of the same repository.
func TestApplyGrowingSymbolDiagnosticPrefersProposedOverStaged(t *testing.T) {
	t.Parallel()

	diagnostic := newGrowingSymbolDiagnostic()
	applyGrowingSymbolDiagnostic(diagnostic, map[string]any{
		"proposed_symbol_changes": proposedSymbolChangeInputs{
			growingProposedSymbol(),
		},
		"changed_symbols": []celexpr.ChangedSymbolInput{growingChangedSymbol()},
	})

	if diagnostic.File != "go/internal/example/widget.go" {
		t.Fatalf("diagnostic file = %q", diagnostic.File)
	}
	if diagnostic.Line != 41 {
		t.Fatalf("diagnostic line = %d, want the proposed start line 41", diagnostic.Line)
	}
	if diagnostic.Metadata["ast_change_source"] != changeSourceProposed {
		t.Fatalf("ast_change_source = %v", diagnostic.Metadata["ast_change_source"])
	}
	if diagnostic.Metadata["proposed_line_count"] != int64(32) {
		t.Fatalf("proposed_line_count = %v", diagnostic.Metadata["proposed_line_count"])
	}
	if diagnostic.Metadata["ast_symbol_path"] != "example.Widget" {
		t.Fatalf("ast_symbol_path = %v", diagnostic.Metadata["ast_symbol_path"])
	}
	if _, reported := diagnostic.Metadata["original_line_count"]; reported {
		t.Fatalf("proposed branch reported staged-only metadata: %#v", diagnostic.Metadata)
	}
}

// Pre-commit runs have no proposed edit, so the staged symbol carries the
// diagnostic and reports the original counts instead of proposed ones.
func TestApplyGrowingSymbolDiagnosticFallsBackToStagedSymbol(t *testing.T) {
	t.Parallel()

	diagnostic := newGrowingSymbolDiagnostic()
	applyGrowingSymbolDiagnostic(diagnostic, map[string]any{
		"changed_symbols": []celexpr.ChangedSymbolInput{growingChangedSymbol()},
	})

	if diagnostic.File != "go/internal/example/gadget.go" {
		t.Fatalf("diagnostic file = %q", diagnostic.File)
	}
	if diagnostic.Line != 77 {
		t.Fatalf("diagnostic line = %d, want the current start line 77", diagnostic.Line)
	}
	if diagnostic.Metadata["ast_change_source"] != changeSourceStaged {
		t.Fatalf("ast_change_source = %v", diagnostic.Metadata["ast_change_source"])
	}
	if diagnostic.Metadata["original_nonblank_line_count"] != int64(19) {
		t.Fatalf(
			"original_nonblank_line_count = %v",
			diagnostic.Metadata["original_nonblank_line_count"],
		)
	}
	if _, reported := diagnostic.Metadata["proposed_line_count"]; reported {
		t.Fatalf("staged branch reported proposed-only metadata: %#v", diagnostic.Metadata)
	}
}

// Symbols that shrink or stay the same size must not be annotated at all.
func TestApplyGrowingSymbolDiagnosticIgnoresNonGrowingSymbols(t *testing.T) {
	t.Parallel()

	shrinkingProposed := growingProposedSymbol()
	shrinkingProposed.LineCountGrows = false
	shrinkingChanged := growingChangedSymbol()
	shrinkingChanged.LineCountGrows = false

	diagnostic := newGrowingSymbolDiagnostic()
	applyGrowingSymbolDiagnostic(diagnostic, map[string]any{
		"proposed_symbol_changes": proposedSymbolChangeInputs{shrinkingProposed},
		"changed_symbols":         []celexpr.ChangedSymbolInput{shrinkingChanged},
	})

	if diagnostic.File != "" || diagnostic.Line != 0 {
		t.Fatalf(
			"non-growing symbols moved the diagnostic to %s:%d",
			diagnostic.File,
			diagnostic.Line,
		)
	}
	if len(diagnostic.Metadata) != 0 {
		t.Fatalf("non-growing symbols emitted metadata: %#v", diagnostic.Metadata)
	}
}

// An activation without AST facts must leave the diagnostic untouched rather
// than panicking on a missing or mistyped key.
func TestApplyGrowingSymbolDiagnosticToleratesMissingSymbolFacts(t *testing.T) {
	t.Parallel()

	diagnostic := newGrowingSymbolDiagnostic()
	applyGrowingSymbolDiagnostic(diagnostic, map[string]any{
		"proposed_symbol_changes": "not-symbol-inputs",
		"changed_symbols":         42,
	})

	if diagnostic.File != "" || len(diagnostic.Metadata) != 0 {
		t.Fatalf("mistyped activation produced %#v", diagnostic)
	}
}

func TestFirstGrowingProposedSymbolSkipsNonGrowingEntries(t *testing.T) {
	t.Parallel()

	stable := growingProposedSymbol()
	stable.LineCountGrows = false
	stable.SymbolName = "Stable"

	growing := growingProposedSymbol()

	symbol, found := firstGrowingProposedSymbol(map[string]any{
		"proposed_symbol_changes": proposedSymbolChangeInputs{stable, growing},
	})
	if !found {
		t.Fatal("growing proposed symbol not found")
	}
	if symbol.SymbolName != "Widget" {
		t.Fatalf("symbol name = %q, want the growing entry", symbol.SymbolName)
	}

	_, found = firstGrowingProposedSymbol(map[string]any{
		"proposed_symbol_changes": proposedSymbolChangeInputs{stable},
	})
	if found {
		t.Fatal("non-growing proposed symbols reported a growth")
	}
}

func TestFirstGrowingChangedSymbolSkipsNonGrowingEntries(t *testing.T) {
	t.Parallel()

	stable := growingChangedSymbol()
	stable.LineCountGrows = false
	stable.SymbolName = "Stable"

	growing := growingChangedSymbol()

	symbol, found := firstGrowingChangedSymbol(map[string]any{
		"changed_symbols": []celexpr.ChangedSymbolInput{stable, growing},
	})
	if !found {
		t.Fatal("growing changed symbol not found")
	}
	if symbol.SymbolName != "Gadget" {
		t.Fatalf("symbol name = %q, want the growing entry", symbol.SymbolName)
	}

	_, found = firstGrowingChangedSymbol(map[string]any{"changed_symbols": nil})
	if found {
		t.Fatal("absent changed symbols reported a growth")
	}
}

func TestProposedSymbolChangesRequiresTypedInputs(t *testing.T) {
	t.Parallel()

	symbols, found := proposedSymbolChanges(map[string]any{
		"proposed_symbol_changes": proposedSymbolChangeInputs{growingProposedSymbol()},
	})
	if !found || len(symbols) != 1 {
		t.Fatalf("typed activation returned %d symbols (found=%t)", len(symbols), found)
	}

	_, found = proposedSymbolChanges(map[string]any{
		"proposed_symbol_changes": []string{"widget"},
	})
	if found {
		t.Fatal("untyped activation value was accepted")
	}
}
