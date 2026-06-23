// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/contextadvisor"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
)

const (
	defaultStartupRepoMapLimit          = 16
	defaultStartupRepoMapSymbolsPerFile = 3
	defaultStartupRepoMapTimeout        = 5 * time.Second
	defaultStartupStorageUpgradeTimeout = 30 * time.Second
	sessionStartHeaderLineCount         = 2
)

func lifecycleOutput(event Event) *HookSpecificOutput {
	context := lifecycleContext(event)
	if context == "" {
		return nil
	}

	return &HookSpecificOutput{
		HookEventName:     event.HookEventName,
		AdditionalContext: hookOutputNormalizer(event.Cwd).preserveLines(context),
	}
}

func lifecycleContext(event Event) string {
	switch event.HookEventName {
	case eventSessionStart:
		return sessionStartContext(event)
	case eventUserPromptSubmit:
		return buildGuidanceContext(
			[]string{
				"Use and maintain a todo list for multi-step work.",
			},
			event.Content(),
		)
	case "PostToolBatch":
		return buildGuidanceContext(
			[]string{
				"Review tool results before continuing.",
				"Update the todo list to reflect completed and remaining work.",
				"Run focused verification after code edits and before broader gates.",
			},
			"",
		)
	case eventStop, "SessionEnd":
		return buildChecklistContext(
			"Before ending:",
			[]string{
				"Do not report completion while planned work remains.",
				"Summarize evidence: files changed, checks run, and unresolved risks.",
				"If hooks or lint failed, keep the failure visible and fix it structurally.",
			},
		)
	case "SubagentStart":
		return buildGuidanceContext(
			[]string{
				"Keep delegated work scoped and concrete.",
				"Do not overwrite edits made by other agents or the user.",
				"Return changed files, verification, and any unresolved risks.",
			},
			event.Content(),
		)
	case "SubagentStop":
		return buildChecklistContext(
			"Before accepting subagent work:",
			[]string{
				"Check the subagent result against the assigned scope.",
				"Integrate only verified changes and preserve unrelated user work.",
				"Record any remaining follow-up explicitly.",
			},
		)
	default:
		return ""
	}
}

func sessionStartContext(event Event) string {
	upgrade := sessionStartStorageUpgradeContext(event.Cwd)

	context := buildSessionStartGuidanceContext(
		[]string{
			"Load repository conventions, managed toolchain rules, " +
				"and generated skills before editing.",
			"Use the repo map to choose focused reads before broad exploration.",
		},
	)
	if upgrade != "" {
		context += "\n\n" + upgrade
	}

	advisor := startupContextAdvice(event.Cwd)
	if advisor != "" {
		context += "\n\n" + advisor
	}

	repoMap := startupRepoMap(event.Cwd)
	if repoMap == "" {
		return context
	}

	return context + "\n\n" + repoMap
}

func startupContextAdvice(cwd string) string {
	root := gitRootFromPath(cwd)
	if root == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultStartupRepoMapTimeout)
	defer cancel()

	store, err := codeintel.OpenReadOnly(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		return ""
	}
	defer store.Close()

	now := time.Now().UTC()

	snapshot, err := store.SessionSnapshot(ctx, codeintel.SessionSnapshotQuery{
		Root:     root,
		Worktree: root,
		Now:      now,
	})
	if err != nil {
		return ""
	}

	surfaces, err := outputsurface.BuildReport(ctx, outputsurface.Options{
		Root:        root,
		IncludeTemp: false,
		Now:         now,
	})
	if err != nil {
		return ""
	}

	thresholds, err := contextadvisor.LoadThresholds(root)
	if err != nil {
		return ""
	}

	return contextadvisor.FormatAdviceTOON(
		contextadvisor.Analyze(snapshot, surfaces, thresholds, now),
	)
}

func sessionStartStorageUpgradeContext(cwd string) string {
	root := gitRootFromPath(cwd)
	if root == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		defaultStartupStorageUpgradeTimeout,
	)
	defer cancel()

	summary, err := codeintel.UpgradeStorageIfNeeded(ctx, root)
	if err != nil {
		startupRepoMapWarning("storage-upgrade", err)

		return feedback.MustRender(feedback.Message{
			Scalars: []feedback.Scalar{
				feedback.S("storage", "code-intel"),
				feedback.S("status", "upgrade_failed"),
				feedback.S("reason", err.Error()),
				feedback.S("repair", "Run coding-ethos-code-intel rebuild-index."),
			},
		}, feedback.FormatTOON)
	}

	if !summary.Needed {
		return ""
	}

	return feedback.MustRender(feedback.Message{
		Scalars: []feedback.Scalar{
			feedback.S("storage", "code-intel"),
			feedback.S("status", "obsolete_artifacts_removed"),
			feedback.S("to", "duckdb"),
			feedback.S(
				"removed",
				strings.Join(summary.RebuildSummary.RemovedObsoleteArtifacts, ","),
			),
		},
	}, feedback.FormatTOON)
}

func buildSessionStartGuidanceContext(guidance []string) string {
	lines := make([]string, 0, sessionStartHeaderLineCount+len(guidance))
	lines = append(
		lines,
		"event: SessionStart",
		fmt.Sprintf("guidance[%d]{message}:", len(guidance)),
	)

	for _, item := range guidance {
		lines = append(lines, "  "+toonCell(item))
	}

	return strings.Join(lines, "\n")
}

func startupRepoMap(cwd string) string {
	root := gitRootFromPath(cwd)
	if root == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		defaultStartupRepoMapTimeout,
	)
	defer cancel()

	store, err := codeintel.OpenDuckDBReadOnly(ctx, codeintel.DefaultDuckDBPath(root))
	if err != nil {
		startupRepoMapWarning("open", err)

		return startupLegacyRepoMap(ctx, root)
	}
	defer store.Close()

	repoMap, err := store.GlobalRepoMap(ctx, codeintel.RepoMapQuery{
		Root:           root,
		Limit:          defaultStartupRepoMapLimit,
		SymbolsPerFile: defaultStartupRepoMapSymbolsPerFile,
	})
	if err != nil {
		startupRepoMapWarning("query", err)

		return startupLegacyRepoMap(ctx, root)
	}

	return renderStartupRepoMap(repoMap)
}

func startupLegacyRepoMap(ctx context.Context, root string) string {
	store, err := codeintel.OpenReadOnly(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		startupRepoMapWarning("legacy-open", err)

		return ""
	}
	defer store.Close()

	repoMap, err := store.GlobalRepoMap(ctx, codeintel.RepoMapQuery{
		Root:           root,
		Limit:          defaultStartupRepoMapLimit,
		SymbolsPerFile: defaultStartupRepoMapSymbolsPerFile,
	})
	if err != nil {
		startupRepoMapWarning("legacy-query", err)

		return ""
	}

	return renderStartupRepoMap(repoMap)
}

func renderStartupRepoMap(repoMap codeintel.RepoMap) string {
	rendered := codeintel.RenderRepoMapTOON(repoMap)
	if rendered == "" {
		return ""
	}

	return strings.Join([]string{
		rendered,
		fmt.Sprintf(
			`repo_map_mcp: code_intel_repo_map {"limit":%d,"symbols_per_file":%d}`,
			defaultStartupRepoMapLimit,
			defaultStartupRepoMapSymbolsPerFile,
		),
	}, "\n")
}

func startupRepoMapWarning(stage string, err error) {
	feedback.Emit(
		os.Stderr,
		feedback.Text{
			Text: fmt.Sprintf("warning: startup repo map %s failed: %v", stage, err),
		},
		feedback.FormatTOON,
	)
}

func buildChecklistContext(heading string, guidance []string) string {
	lines := make([]string, 0, 1+len(guidance))

	lines = append(lines, heading)
	for _, item := range guidance {
		lines = append(lines, "- "+item)
	}

	return strings.Join(lines, "\n")
}

func buildGuidanceContext(guidance []string, prompt string) string {
	lines := []string{}
	if trimmed := strings.TrimSpace(prompt); trimmed != "" {
		lines = append(lines, "prompt:", trimmed, "")
	}

	lines = append(lines, "guidance:")
	for _, item := range guidance {
		lines = append(lines, "- "+item)
	}

	return strings.Join(lines, "\n")
}
