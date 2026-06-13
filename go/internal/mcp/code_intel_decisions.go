// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func (server Server) codeIntelWhy(args json.RawMessage) (any, error) {
	var input codeIntelWhyInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf("parse code intelligence why arguments: %w", inlineErr0)
	}

	text := strings.TrimSpace(firstNonEmpty(input.Text, input.Query))
	path := strings.TrimSpace(input.Path)
	symbolPath := strings.TrimSpace(input.SymbolPath)

	status := strings.TrimSpace(input.Status)
	if text == "" && path == "" && symbolPath == "" && status == "" {
		return nil, apperror.StaticError("query, path, symbol_path, or status is required")
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	ctx := argsContext()
	limit := boundedCodeIntelLimit(input.Limit)
	query := codeintel.DecisionQuery{
		Text:       text,
		Path:       path,
		SymbolPath: symbolPath,
		Status:     status,
		Limit:      limit,
	}

	decisions, err := store.Decisions(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query architectural decisions: %w", err)
	}

	health, err := store.DecisionHealth(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query architectural decision health: %w", err)
	}

	meta, err := server.codeIntelTaskMeta(ctx, store, index, []string{
		"decisions",
		"decision_links",
		"code_files",
		"code_chunks",
	})
	if err != nil {
		return nil, fmt.Errorf("read code intelligence task metadata: %w", err)
	}

	result := map[string]any{
		"kind":           "code_intel_why",
		"_meta":          meta,
		"decisions":      decisions,
		"health":         health,
		"next_mcp_calls": codeIntelWhyNextCalls(path, symbolPath),
	}
	if strings.EqualFold(strings.TrimSpace(input.Format), "toon") {
		result["content"] = renderCodeIntelWhyTOON(decisions, health)
	}

	return result, nil
}

func codeIntelWhyNextCalls(path, symbolPath string) []map[string]any {
	if strings.TrimSpace(path) == "" {
		return []map[string]any{{
			"tool":      "code_intel_overview",
			"arguments": map[string]any{"limit": codeIntelDefaultTaskLimit},
		}}
	}

	contextArgs := map[string]any{"path": path}
	if strings.TrimSpace(symbolPath) != "" {
		contextArgs["symbol_path"] = symbolPath
	}

	return []map[string]any{
		{
			"tool":      "code_intel_context_card",
			"arguments": contextArgs,
		},
		{
			"tool":      "code_intel_change_risk",
			"arguments": map[string]any{"paths": []string{path}},
		},
	}
}

func renderCodeIntelWhyTOON(
	decisions []codeintel.DecisionRecord,
	health codeintel.DecisionHealth,
) string {
	var builder strings.Builder
	builder.WriteString("kind: code_intel_why\n")
	builder.WriteString("decisions: ")
	builder.WriteString(strconv.Itoa(len(decisions)))
	builder.WriteString("\nstale: ")
	builder.WriteString(strconv.Itoa(health.Summary.StaleCount))
	builder.WriteString("\nconflicts: ")
	builder.WriteString(strconv.Itoa(health.Summary.ConflictCount))
	builder.WriteString("\nungoverned: ")
	builder.WriteString(strconv.Itoa(health.Summary.UngovernedCount))
	builder.WriteString("\n")

	for _, decision := range decisions {
		builder.WriteString("- id: ")
		builder.WriteString(decision.ID)
		builder.WriteString("\n  status: ")
		builder.WriteString(decision.Status)
		builder.WriteString("\n  title: ")
		builder.WriteString(decision.Title)
		builder.WriteString("\n  source: ")
		builder.WriteString(decision.SourceKind)

		if decision.SourcePath != "" {
			builder.WriteString(" ")
			builder.WriteString(decision.SourcePath)
		}

		builder.WriteString("\n  provenance: ")
		builder.WriteString(decision.ProvenanceClass)
		builder.WriteString("\n")
	}

	return builder.String()
}

func annotateContextTargetWithDecisions(
	ctx context.Context,
	store *codeintel.Store,
	target *codeIntelContextTarget,
	limit int,
) error {
	decisions, err := store.Decisions(ctx, codeintel.DecisionQuery{
		Path:  target.Path,
		Limit: limit,
	})
	if err != nil {
		return fmt.Errorf("query context-card decisions for %s: %w", target.Path, err)
	}

	health, err := store.DecisionHealth(ctx, codeintel.DecisionQuery{
		Path:  target.Path,
		Limit: limit,
	})
	if err != nil {
		return fmt.Errorf("query context-card decision health for %s: %w", target.Path, err)
	}

	target.Decisions = decisions
	target.DecisionHealth = &health

	return nil
}

func changeRiskDecisions(
	ctx context.Context,
	store *codeintel.Store,
	path string,
	limit int,
) ([]codeintel.DecisionRecord, codeintel.DecisionHealth, error) {
	decisions, err := store.Decisions(ctx, codeintel.DecisionQuery{
		Path:  path,
		Limit: limit,
	})
	if err != nil {
		return nil, codeintel.DecisionHealth{}, fmt.Errorf(
			"query change-risk decisions for %s: %w",
			path,
			err,
		)
	}

	health, err := store.DecisionHealth(ctx, codeintel.DecisionQuery{
		Path:  path,
		Limit: limit,
	})
	if err != nil {
		return nil, codeintel.DecisionHealth{}, fmt.Errorf(
			"query change-risk decision health for %s: %w",
			path,
			err,
		)
	}

	return decisions, health, nil
}
