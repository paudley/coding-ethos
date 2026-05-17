// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
)

func diagnosticFromInput(input lintAdviceInput) diagnostics.Diagnostic {
	return diagnostics.Diagnostic{
		Code:     strings.TrimSpace(input.Code),
		File:     strings.TrimSpace(input.File),
		Message:  strings.TrimSpace(input.Message),
		Severity: strings.TrimSpace(input.Severity),
		Tool:     strings.TrimSpace(input.Tool),
		Column:   input.Column,
		Line:     input.Line,
	}
}

func (server Server) skillIDForDiagnostic(input lintAdviceInput) string {
	return strings.TrimSpace(server.enrichedDiagnostic(input).SkillID)
}

func (server Server) enrichedDiagnostic(input lintAdviceInput) diagnostics.Diagnostic {
	if strings.TrimSpace(input.Tool) == "" || strings.TrimSpace(input.Message) == "" {
		return diagnostics.Diagnostic{}
	}

	enriched := diagnostics.Enrich(
		[]diagnostics.Diagnostic{diagnosticFromInput(input)},
		server.bundle.EvidenceMaps,
	)
	if len(enriched) == 0 {
		return diagnostics.Diagnostic{}
	}

	return enriched[0]
}
