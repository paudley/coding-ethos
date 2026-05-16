// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestProxyToolOutputPolicyIDPrefersDirectoryAnatomy(t *testing.T) {
	t.Parallel()

	records := []agentproxy.TransformRecord{
		{
			Name:     "token-budget",
			Decision: proxyDecisionTruncate,
		},
		{
			Name:     codeintel.DirectoryAnatomyTransformName,
			Decision: proxyDecisionInject,
		},
	}

	if got := proxyToolOutputPolicyID(records); got != proxyPolicyDirectoryAnatomy {
		t.Fatalf("policy ID mismatch: got %q, want %q", got, proxyPolicyDirectoryAnatomy)
	}

	if got := proxyToolOutputDecision(records); got != proxyDecisionTruncate {
		t.Fatalf("decision mismatch: got %q, want %q", got, proxyDecisionTruncate)
	}
}

func TestProxyToolOutputPolicyIDPrefersFilePaginationOverTokenBudget(t *testing.T) {
	t.Parallel()

	records := []agentproxy.TransformRecord{
		{
			Name:     "token-budget",
			Decision: proxyDecisionTruncate,
		},
		{
			Name:     agentproxy.FileReadPaginationTransformName,
			Decision: proxyDecisionTruncate,
		},
	}

	if got := proxyToolOutputPolicyID(records); got != proxyPolicyFilePagination {
		t.Fatalf("policy ID mismatch: got %q, want %q", got, proxyPolicyFilePagination)
	}
}
