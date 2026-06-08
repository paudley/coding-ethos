// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

const (
	// ProvenanceExtracted marks direct parser or static-analysis facts.
	ProvenanceExtracted = "EXTRACTED"
	// ProvenancePolicyDerived marks facts derived from CEL, SARIF, or policy evidence.
	ProvenancePolicyDerived = "POLICY_DERIVED"
	// ProvenanceTraceDerived marks facts derived from hook, lint, or proxy traces.
	ProvenanceTraceDerived = "TRACE_DERIVED"
	// ProvenanceGitDerived marks facts derived from git history and co-change data.
	ProvenanceGitDerived = "GIT_DERIVED"
	// ProvenanceDocDerived marks facts derived from documentation or comments.
	ProvenanceDocDerived = "DOC_DERIVED"
	// ProvenanceInferred marks advisory semantic links that are not enforcement-grade.
	ProvenanceInferred = "INFERRED"
	// ProvenanceAmbiguous marks retained context that requires inspection.
	ProvenanceAmbiguous = "AMBIGUOUS"
)

func normalizeProvenanceClass(value string) string {
	switch value {
	case ProvenanceExtracted,
		ProvenancePolicyDerived,
		ProvenanceTraceDerived,
		ProvenanceGitDerived,
		ProvenanceDocDerived,
		ProvenanceInferred,
		ProvenanceAmbiguous:
		return value
	default:
		return ProvenanceExtracted
	}
}

func extractedProvenanceClasses() []string {
	return []string{ProvenanceExtracted}
}
