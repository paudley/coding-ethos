// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

type SimilarityFactInput struct {
	File            string  `json:"file"`
	SymbolName      string  `json:"symbol_name"`
	SymbolKind      string  `json:"symbol_kind"`
	SymbolPath      string  `json:"symbol_path"`
	Language        string  `json:"language"`
	MatchPath       string  `json:"match_path"`
	MatchSymbolName string  `json:"match_symbol_name"`
	MatchSymbolKind string  `json:"match_symbol_kind"`
	MatchStartLine  int64   `json:"match_start_line"`
	Similarity      float64 `json:"similarity"`
	ExactNormalized bool    `json:"exact_normalized"`
}
