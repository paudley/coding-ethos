// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter

import "blackcat.ca/coding-ethos/go/internal/agentproxy"

// Registry selects the most specific adapter for a request. It holds an ordered
// set of adapters and resolves ties by the highest detection specificity.
type Registry struct {
	adapters []agentproxy.Adapter
}

// NewRegistry builds a registry over the supplied adapters in priority order.
func NewRegistry(adapters ...agentproxy.Adapter) Registry {
	return Registry{adapters: adapters}
}

// DefaultRegistry builds a registry with the OpenAI, Anthropic, and Gemini
// adapters. The order only breaks ties when specificities are identical.
func DefaultRegistry() Registry {
	return NewRegistry(OpenAI{}, Anthropic{}, Gemini{})
}

// Match returns the adapter with the highest detection specificity for the
// request. When no adapter claims the request the second result is false.
func (registry Registry) Match(
	reqCtx agentproxy.RequestContext,
) (agentproxy.Adapter, bool) {
	var (
		best     agentproxy.Adapter
		bestSpec int
		found    bool
	)

	for _, candidate := range registry.adapters {
		result := candidate.Detect(reqCtx)
		if !result.Matched {
			continue
		}

		if !found || result.Specificity > bestSpec {
			best = candidate
			bestSpec = result.Specificity
			found = true
		}
	}

	return best, found
}
