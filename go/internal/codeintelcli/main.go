// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/feedback"
)

const (
	defaultGitSignalCommitLimit = 500
	defaultHealthTrendLimit     = 20
	defaultGraphReportSymbols   = 4
	defaultResultLimit          = 20
	defaultSearchLimit          = 10
)

var (
	errCommandRequired = apperror.StaticError(
		"code intelligence command is required",
	)
	errCodeContextTarget    = apperror.StaticError("code context target is required")
	errSARIFFileRequired    = apperror.StaticError("SARIF file is required")
	errSearchTargetRequired = apperror.StaticError("search text or vector is required")
	errSearchTextRequired   = apperror.StaticError("search text is required")
	errVectorValuesRequired = apperror.StaticError(
		"vector must include at least one value",
	)
	errUnknownCodeIntelCommand = apperror.StaticError(
		"unknown code intelligence command",
	)
	errUnknownDownstreamAnalysisFormat = apperror.StaticError(
		"unknown downstream analysis format",
	)
)

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errCommandRequired
	}

	handler, ok := commandHandlers()[args[0]]
	if !ok {
		return fmt.Errorf("%w: %q", errUnknownCodeIntelCommand, args[0])
	}

	return handler(ctx, args[1:])
}

type codeIntelCommand func(context.Context, []string) error

func commandHandlers() map[string]codeIntelCommand {
	return map[string]codeIntelCommand{
		"anatomy-map":               printAnatomyMap,
		"code-chunks":               printCodeChunks,
		"compact-context":           printCompactContext,
		"code-context":              printCodeContext,
		"downstream-analysis":       printDownstreamAnalysis,
		"embedding-candidates":      printEmbeddingCandidates,
		"embedding-records":         printEmbeddingRecords,
		"enrich-listing":            enrichDirectoryListing,
		"git-signals":               gitSignals,
		"graph-report":              printGraphReport,
		"health":                    printHealth,
		"hook-reviews":              printHookReviews,
		"hook-usage":                printHookUsage,
		"repeated-edits":            printRepeatedEdits,
		"hybrid-search":             hybridSearch,
		"index-code":                indexCode,
		"index-status":              printIndexStatus,
		"ingest-sarif":              ingestSARIF,
		"ingest-traces":             ingestTraces,
		"record-embedding":          recordEmbedding,
		"record-hook-review":        recordHookReview,
		"record-proxy-event":        recordProxyEvent,
		"record-outcome":            recordOutcome,
		"rebuild-index":             rebuildIndex,
		"remediation-effectiveness": printRemediationEffectiveness,
		"remediation-outcomes":      printRemediationOutcomes,
		"proxy-events":              printProxyEvents,
		"proxy-file-read":           proxyFileRead,
		"proxy-sessions":            printProxySessions,
		"repo-map":                  printRepoMap,
		"repeated-failures":         printRepeatedFailures,
		"sarif-results":             printSARIFResults,
		"search":                    search,
		"session-snapshot":          printSessionSnapshot,
		"stats":                     printStats,
		"upsert-vector":             upsertVector,
		"vector-stats":              printVectorStats,
	}
}

func printSessionSnapshot(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("session-snapshot", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	provider := flags.String("provider", "", "Filter by agent provider")
	sessionID := flags.String("session-id", "", "Filter by agent session ID")
	format := flags.String("format", outputFormatJSON, "Output format: json or toon")
	limit := addResultLimit(flags)

	err := parseCommandFlags(flags, args, "session-snapshot")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	snapshot, err := store.SessionSnapshot(ctx, codeintel.SessionSnapshotQuery{
		Root:      *storeFlags.root,
		Worktree:  *storeFlags.root,
		Provider:  *provider,
		SessionID: *sessionID,
		Limit:     *limit,
	})
	if err != nil {
		return fmt.Errorf("query session snapshot: %w", err)
	}

	switch strings.TrimSpace(*format) {
	case "", outputFormatJSON:
		return encodeJSON(os.Stdout, snapshot)
	case outputFormatTOON:
		err = feedback.WriteRendered(
			os.Stdout,
			codeintel.FormatSessionSnapshotTOON(snapshot),
			feedback.FormatTOON,
		)
		if err != nil {
			return fmt.Errorf("write session snapshot TOON: %w", err)
		}

		return nil
	default:
		return fmt.Errorf(
			"%w: %q",
			errUnknownDownstreamAnalysisFormat,
			*format,
		)
	}
}

func printHealth(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("health", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	path := flags.String("path", "", "Filter by source path or directory")
	limit := addResultLimit(flags)
	trend := flags.Int(
		"trend",
		defaultHealthTrendLimit,
		"Maximum trend snapshots to return",
	)
	refresh := flags.Bool("refresh", false, "Recompute and persist a health snapshot")
	gitHead := flags.String("git-head", "", "Git commit associated with the snapshot")
	lcovPath := flags.String("lcov", "", "LCOV file to import before scoring")

	err := parseCommandFlags(flags, args, "health")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	health, err := store.CodeHealth(ctx, codeintel.CodeHealthQuery{
		Root:     *storeFlags.root,
		Path:     *path,
		Limit:    *limit,
		Refresh:  *refresh || *lcovPath != "",
		Trend:    *trend,
		GitHead:  *gitHead,
		LCOVPath: *lcovPath,
	})
	if err != nil {
		return fmt.Errorf("query code health: %w", err)
	}

	return encodeJSON(os.Stdout, map[string]any{
		"kind":   "code_intel_health",
		"health": health,
	})
}

type storeFlags struct {
	root   *string
	dbPath *string
}

type policyPathFlags struct {
	policyID *string
	skillID  *string
	path     *string
}

type policySkillFlags struct {
	policyID *string
	skillID  *string
}

func addStoreFlags(flags *flag.FlagSet, rootUsage string) storeFlags {
	return storeFlags{
		root:   flags.String("root", ".", rootUsage),
		dbPath: flags.String("db", "", "DuckDB code intelligence database path"),
	}
}

func addPolicyPathFlags(flags *flag.FlagSet, pathUsage string) policyPathFlags {
	policySkill := addPolicySkillFlags(flags)

	return policyPathFlags{
		policyID: policySkill.policyID,
		skillID:  policySkill.skillID,
		path:     flags.String("path", "", pathUsage),
	}
}

func addPolicySkillFlags(flags *flag.FlagSet) policySkillFlags {
	return policySkillFlags{
		policyID: flags.String("policy-id", "", "Filter by policy ID"),
		skillID:  flags.String("skill-id", "", "Filter by skill ID"),
	}
}

func addResultLimit(flags *flag.FlagSet) *int {
	return flags.Int("limit", defaultResultLimit, "Maximum result count")
}

func addRelatedLimit(flags *flag.FlagSet) *int {
	return flags.Int("limit", defaultResultLimit, "Maximum related item count")
}

func addSearchLimit(flags *flag.FlagSet) *int {
	return flags.Int("limit", defaultSearchLimit, "Maximum result count")
}

func parseAndPrintStoreJSON(
	ctx context.Context,
	args []string,
	command string,
	flags *flag.FlagSet,
	storeFlags storeFlags,
	query func(*codeintel.Store) (any, error),
) error {
	err := parseCommandFlags(flags, args, command)
	if err != nil {
		return err
	}

	return printStoreJSON(ctx, *storeFlags.root, *storeFlags.dbPath, query)
}

func parseCommandFlags(flags *flag.FlagSet, args []string, command string) error {
	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse %s flags: %w", command, err)
	}

	return nil
}

func ingestTraces(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("ingest-traces", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos traces")

	dbPath := flags.String("db", "", "DuckDB code intelligence database path")

	err := parseCommandFlags(flags, args, "ingest-traces")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	summary, err := codeintel.NewTraceIngester(store).IngestTraceDirs(ctx, *root)
	if err != nil {
		return fmt.Errorf("ingest trace directories: %w", err)
	}

	return encodeJSON(os.Stdout, summary)
}

func ingestSARIF(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("ingest-sarif", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "DuckDB code intelligence database path")

	file := flags.String("file", "", "SARIF file to ingest")

	err := parseCommandFlags(flags, args, "ingest-sarif")
	if err != nil {
		return err
	}

	if *file == "" {
		return fmt.Errorf("%w: --file", errSARIFFileRequired)
	}

	payload, err := os.ReadFile(*file)
	if err != nil {
		return fmt.Errorf("read SARIF file %q: %w", *file, err)
	}

	err = appendCLIEvent(*root, "sarif", codeintel.EventRecord{
		Kind:    "sarif",
		Path:    *file,
		Payload: payload,
	})
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	err = codeintel.NewTraceIngester(store).IngestSARIF(ctx, *file, payload)
	if err != nil {
		return fmt.Errorf("ingest SARIF: %w", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return fmt.Errorf("read code intelligence stats: %w", err)
	}

	return encodeJSON(os.Stdout, stats)
}

func recordOutcome(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("record-outcome", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "DuckDB code intelligence database path")
	remediationID := flags.String("remediation-id", "", "Remediation ID")
	findingID := flags.String("finding-id", "", "Finding ID")
	sourceTraceID := flags.String(
		"source-trace-id",
		"",
		"Trace that emitted the remediation",
	)
	followupTraceID := flags.String(
		"followup-trace-id",
		"",
		"Trace from the follow-up attempt",
	)
	policyID := flags.String("policy-id", "", "Policy ID")
	skillID := flags.String("skill-id", "", "Skill ID")
	path := flags.String("path", "", "File/path context")
	provider := flags.String("provider", "", "Agent provider")
	tool := flags.String("tool", "", "Agent tool")
	outcome := flags.String(
		"outcome",
		"",
		"Outcome: attempted, fixed, repeated, superseded, or unknown",
	)

	attempt := flags.Int("attempt", 0, "Attempt ordinal")

	err := parseCommandFlags(flags, args, "record-outcome")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	outcomeRecord := codeintel.RemediationOutcome{
		RemediationID:   *remediationID,
		FindingID:       *findingID,
		SourceTraceID:   *sourceTraceID,
		FollowupTraceID: *followupTraceID,
		PolicyID:        *policyID,
		SkillID:         *skillID,
		Path:            *path,
		Provider:        *provider,
		Tool:            *tool,
		Outcome:         *outcome,
		AttemptOrdinal:  *attempt,
	}

	err = appendRemediationOutcomeEvent(*root, outcomeRecord)
	if err != nil {
		return err
	}

	err = store.RecordRemediationOutcome(ctx, outcomeRecord)
	if err != nil {
		return fmt.Errorf("record remediation outcome: %w", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return fmt.Errorf("read code intelligence stats: %w", err)
	}

	return encodeJSON(os.Stdout, stats)
}

func appendRemediationOutcomeEvent(
	root string,
	outcome codeintel.RemediationOutcome,
) error {
	payload, err := rawEventPayload(outcome)
	if err != nil {
		return err
	}

	return appendCLIEvent(root, "remediation-outcome", codeintel.EventRecord{
		Kind:     "remediation_outcome",
		TraceID:  outcome.SourceTraceID,
		Provider: outcome.Provider,
		Tool:     outcome.Tool,
		PolicyID: outcome.PolicyID,
		SkillID:  outcome.SkillID,
		Path:     outcome.Path,
		Payload:  payload,
	})
}

func recordEmbedding(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("record-embedding", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "DuckDB code intelligence database path")
	backend := flags.String("backend", "", "Vector backend")
	collection := flags.String("collection", "", "Vector collection")
	modelID := flags.String("model-id", "", "Embedding model ID")
	recordKind := flags.String("record-kind", "", "Source record kind")
	recordID := flags.String("record-id", "", "Source record ID")
	dimension := flags.Int("dimension", 0, "Embedding dimension")
	path := flags.String("path", "", "Path context")
	policyID := flags.String("policy-id", "", "Policy ID")
	skillID := flags.String("skill-id", "", "Skill ID")

	backendRowID := flags.String("backend-row-id", "", "Backend row ID")

	err := parseCommandFlags(flags, args, "record-embedding")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	embeddingRecord := codeintel.EmbeddingRecord{
		Backend:      *backend,
		Collection:   *collection,
		ModelID:      *modelID,
		RecordKind:   *recordKind,
		RecordID:     *recordID,
		Dimension:    *dimension,
		Path:         *path,
		PolicyID:     *policyID,
		SkillID:      *skillID,
		BackendRowID: *backendRowID,
	}

	payload, err := rawEventPayload(embeddingRecord)
	if err != nil {
		return err
	}

	err = appendCLIEvent(*root, "embedding-record", codeintel.EventRecord{
		Kind:     "embedding_record",
		PolicyID: *policyID,
		SkillID:  *skillID,
		Path:     *path,
		Payload:  payload,
	})
	if err != nil {
		return err
	}

	err = store.UpsertEmbeddingRecord(ctx, embeddingRecord)
	if err != nil {
		return fmt.Errorf("record embedding metadata: %w", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return fmt.Errorf("read code intelligence stats: %w", err)
	}

	return encodeJSON(os.Stdout, stats)
}

func printStats(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("stats", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")

	dbPath := flags.String("db", "", "DuckDB code intelligence database path")

	err := parseCommandFlags(flags, args, "stats")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	stats, err := store.Stats(ctx)
	if err != nil {
		return fmt.Errorf("read code intelligence stats: %w", err)
	}

	return encodeJSON(os.Stdout, stats)
}

func gitSignals(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("git-signals", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	path := flags.String("path", "", "Filter by source path")
	paths := flags.String(
		"paths",
		"",
		"Comma-separated source paths for reviewer suggestions",
	)
	limit := addResultLimit(flags)
	refresh := flags.Bool("refresh", true, "Refresh git signals before querying")
	force := flags.Bool("force", false, "Force refresh even when HEAD is already indexed")
	commits := flags.Int(
		"commits",
		defaultGitSignalCommitLimit,
		"Maximum recent commits to index when refreshing",
	)

	err := parseCommandFlags(flags, args, "git-signals")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	var summary codeintel.GitSignalSummary
	if *refresh {
		summary, err = store.RefreshGitSignals(
			ctx,
			*storeFlags.root,
			codeintel.GitSignalRefreshOptions{CommitLimit: *commits, Force: *force},
		)
		if err != nil {
			return fmt.Errorf("refresh git signals: %w", err)
		}
	}

	signals, err := store.GitSignals(ctx, codeintel.GitSignalQuery{
		Path:  *path,
		Limit: *limit,
	})
	if err != nil {
		return fmt.Errorf("query git signals: %w", err)
	}

	reviewPaths := codeIntelCSVPaths(*paths)
	if *path != "" {
		reviewPaths = append(reviewPaths, *path)
	}

	reviewers, err := store.GitReviewerSuggestions(
		ctx,
		codeintel.GitReviewerSuggestionQuery{
			Paths: reviewPaths,
			Limit: *limit,
		},
	)
	if err != nil {
		return fmt.Errorf("query git reviewer suggestions: %w", err)
	}

	return encodeJSON(os.Stdout, map[string]any{
		"kind":      "git_signals",
		"summary":   summary,
		"signals":   signals,
		"reviewers": reviewers,
	})
}

func printDownstreamAnalysis(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("downstream-analysis", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "Legacy DuckDB code intelligence database path")
	duckDBPath := flags.String("duckdb", "", "DuckDB code intelligence database path")
	format := flags.String(
		"format",
		outputFormatJSON,
		"Output format: json, toon, or human",
	)
	limit := addResultLimit(flags)

	err := parseCommandFlags(flags, args, "downstream-analysis")
	if err != nil {
		return err
	}

	analysis, err := downstreamAnalysisForStores(
		ctx,
		*root,
		*dbPath,
		*duckDBPath,
		*limit,
	)
	if err != nil {
		return err
	}

	return printDownstreamAnalysisFormat(os.Stdout, analysis, *format)
}

func downstreamAnalysisForStores(
	ctx context.Context,
	root string,
	dbPath string,
	duckDBPath string,
	limit int,
) (codeintel.DownstreamAnalysis, error) {
	duckStore, duckOpenErr := codeintel.OpenDuckDBReadOnly(
		ctx,
		resolvedDuckDBPath(root, duckDBPath),
	)
	if duckOpenErr == nil {
		defer duckStore.Close()

		analysis, err := codeintel.AnalyzeDownstreamDuckDB(ctx, root, duckStore, limit)
		if err != nil {
			return codeintel.DownstreamAnalysis{}, fmt.Errorf(
				"analyze downstream DuckDB code intelligence: %w",
				err,
			)
		}

		return analysis, nil
	}

	return legacyDownstreamAnalysis(ctx, root, dbPath, limit, duckOpenErr)
}

func printDownstreamAnalysisFormat(
	output *os.File,
	analysis codeintel.DownstreamAnalysis,
	format string,
) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", outputFormatJSON:
		return encodeJSON(output, analysis)
	case outputFormatTOON:
		err := feedback.WriteRendered(
			output,
			codeintel.FormatDownstreamAnalysisTOON(analysis),
			feedback.FormatTOON,
		)
		if err != nil {
			return fmt.Errorf("write downstream analysis TOON: %w", err)
		}

		return nil
	case "human":
		err := feedback.WriteRendered(
			output,
			codeintel.FormatDownstreamAnalysisHuman(analysis),
			feedback.FormatHuman,
		)
		if err != nil {
			return fmt.Errorf("write downstream analysis text: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %q", errUnknownDownstreamAnalysisFormat, format)
	}
}

func legacyDownstreamAnalysis(
	ctx context.Context,
	root string,
	dbPath string,
	limit int,
	duckOpenErr error,
) (codeintel.DownstreamAnalysis, error) {
	store, openErr := openReadOnlyStore(ctx, root, dbPath)
	if openErr != nil {
		analysis, err := codeintel.AnalyzeDownstream(ctx, root, nil, limit)
		if err != nil {
			return codeintel.DownstreamAnalysis{}, fmt.Errorf(
				"analyze downstream logs: %w",
				err,
			)
		}

		analysis.StorageStrategy.OpenError = openErr.Error()
		analysis.StorageHealth.OpenError = duckOpenErr.Error()

		return analysis, nil
	}
	defer store.Close()

	analysis, err := codeintel.AnalyzeDownstream(ctx, root, store, limit)
	if err != nil {
		return codeintel.DownstreamAnalysis{}, fmt.Errorf(
			"analyze downstream code intelligence: %w",
			err,
		)
	}

	return analysis, nil
}

func rebuildIndex(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("rebuild-index", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "Legacy DuckDB code intelligence database path")
	duckDBPath := flags.String("duckdb", "", "DuckDB code intelligence database path")

	err := parseCommandFlags(flags, args, "rebuild-index")
	if err != nil {
		return err
	}

	summary, err := codeintel.RebuildDuckDBIndex(
		ctx,
		*root,
		resolvedDuckDBPath(*root, *duckDBPath),
		resolvedDBPath(*root, *dbPath),
	)
	if err != nil {
		return fmt.Errorf("rebuild DuckDB code intelligence index: %w", err)
	}

	return encodeJSON(os.Stdout, summary)
}

func printRepeatedFailures(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("repeated-failures", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	filters := addPolicyPathFlags(flags, "Filter by normalized source path")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"repeated-failures",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.RepeatedFailures(ctx, codeintel.RepeatedFailureQuery{
				PolicyID: *filters.policyID,
				SkillID:  *filters.skillID,
				Path:     *filters.path,
				Limit:    *limit,
			})
		},
	)
}

func printSARIFResults(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("sarif-results", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	runID := flags.String("run-id", "", "Filter by SARIF run ID")
	traceID := flags.String("trace-id", "", "Filter by linked trace ID")
	filters := addPolicyPathFlags(flags, "Filter by source path")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"sarif-results",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.SARIFResults(ctx, codeintel.SARIFResultQuery{
				RunID:    *runID,
				TraceID:  *traceID,
				PolicyID: *filters.policyID,
				SkillID:  *filters.skillID,
				Path:     *filters.path,
				Limit:    *limit,
			})
		},
	)
}

func printRemediationOutcomes(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("remediation-outcomes", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	filters := addPolicyPathFlags(flags, "Filter by source path")
	outcome := flags.String("outcome", "", "Filter by outcome")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"remediation-outcomes",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.RemediationOutcomes(ctx, codeintel.RemediationOutcomeQuery{
				PolicyID: *filters.policyID,
				SkillID:  *filters.skillID,
				Outcome:  *outcome,
				Path:     *filters.path,
				Limit:    *limit,
			})
		},
	)
}

func printRemediationEffectiveness(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("remediation-effectiveness", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	filters := addPolicyPathFlags(flags, "Filter by source path")

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"remediation-effectiveness",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.RemediationEffectiveness(
				ctx,
				codeintel.RemediationOutcomeQuery{
					PolicyID: *filters.policyID,
					SkillID:  *filters.skillID,
					Path:     *filters.path,
				},
			)
		},
	)
}

func printVectorStats(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("vector-stats", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	backend := flags.String("backend", codeintel.VectorBackendDuckDBVSS, "Vector backend")

	uri := flags.String("uri", "", "Vector backend URI")

	err := parseCommandFlags(flags, args, "vector-stats")
	if err != nil {
		return err
	}

	if *uri == "" {
		*uri = codeintel.DefaultVectorPath(*root)
	}

	index, err := codeintel.NewVectorIndex(ctx, codeintel.VectorBackendConfig{
		Backend: *backend,
		URI:     *uri,
	})
	if err != nil {
		return fmt.Errorf("open vector index: %w", err)
	}

	if closer, ok := index.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	stats, err := index.Stats(ctx)
	if err != nil {
		return fmt.Errorf("read vector stats: %w", err)
	}

	return encodeJSON(os.Stdout, stats)
}

func printEmbeddingRecords(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("embedding-records", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	backend := flags.String("backend", "", "Filter by vector backend")
	collection := flags.String("collection", "", "Filter by collection")
	modelID := flags.String("model-id", "", "Filter by embedding model ID")
	recordKind := flags.String("record-kind", "", "Filter by source record kind")
	recordID := flags.String("record-id", "", "Filter by source record ID")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"embedding-records",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.EmbeddingRecords(ctx, codeintel.EmbeddingRecordQuery{
				Backend:    *backend,
				Collection: *collection,
				ModelID:    *modelID,
				RecordKind: *recordKind,
				RecordID:   *recordID,
				Limit:      *limit,
			})
		},
	)
}

func printEmbeddingCandidates(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("embedding-candidates", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	recordKind := flags.String("record-kind", "", "Filter by source record kind")
	filters := addPolicyPathFlags(flags, "Filter by path")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"embedding-candidates",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.EmbeddingCandidates(ctx, codeintel.EmbeddingCandidateQuery{
				RecordKind: *recordKind,
				PolicyID:   *filters.policyID,
				SkillID:    *filters.skillID,
				Path:       *filters.path,
				Limit:      *limit,
			})
		},
	)
}

func hybridSearch(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("hybrid-search", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "DuckDB code intelligence database path")
	uri := flags.String("uri", "", "Vector backend URI")
	text := flags.String("text", "", "FTS query text")
	vectorText := flags.String("vector", "", "Comma-separated float32 query embedding")
	collection := flags.String("collection", "remediations", "Vector collection")
	modelID := flags.String("model-id", "", "Embedding model ID")
	policyID := flags.String("policy-id", "", "Filter by policy ID")
	skillID := flags.String("skill-id", "", "Filter by skill ID")
	path := flags.String("path", "", "Filter by path")

	limit := addSearchLimit(flags)

	err := parseCommandFlags(flags, args, "hybrid-search")
	if err != nil {
		return err
	}

	vector, err := parseOptionalVector(*vectorText)
	if err != nil {
		return err
	}

	if *text == "" && len(vector) == 0 {
		return fmt.Errorf("%w: --text or --vector", errSearchTargetRequired)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if *uri == "" {
		*uri = codeintel.DefaultVectorPath(*root)
	}

	index, err := codeintel.NewVectorIndex(ctx, codeintel.VectorBackendConfig{
		Backend: codeintel.VectorBackendDuckDBVSS,
		URI:     *uri,
	})
	if err != nil {
		return fmt.Errorf("open vector index: %w", err)
	}

	if closer, ok := index.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	results, err := store.HybridSearch(ctx, index, codeintel.HybridSearchQuery{
		Text:       *text,
		Collection: *collection,
		ModelID:    *modelID,
		PolicyID:   *policyID,
		SkillID:    *skillID,
		Path:       *path,
		Vector:     vector,
		Limit:      *limit,
	})
	if err != nil {
		return fmt.Errorf("run hybrid search: %w", err)
	}

	return encodeJSON(os.Stdout, results)
}

func printIndexStatus(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("index-status", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "DuckDB code intelligence database path")
	uri := flags.String("uri", "", "Vector backend URI")
	collection := flags.String("collection", "remediations", "Vector collection")

	modelID := flags.String("model-id", "", "Embedding model ID")

	err := parseCommandFlags(flags, args, "index-status")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if *uri == "" {
		*uri = codeintel.DefaultVectorPath(*root)
	}

	index, err := codeintel.NewVectorIndex(ctx, codeintel.VectorBackendConfig{
		Backend: codeintel.VectorBackendDuckDBVSS,
		URI:     *uri,
	})
	if err != nil {
		return fmt.Errorf("open vector index: %w", err)
	}

	if closer, ok := index.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	vectorStats, err := index.Stats(ctx)
	if err != nil {
		return fmt.Errorf("read vector stats: %w", err)
	}

	status, err := store.IndexStatus(ctx, vectorStats, codeintel.EmbeddingRecordQuery{
		Backend:    codeintel.VectorBackendDuckDBVSS,
		Collection: *collection,
		ModelID:    *modelID,
	})
	if err != nil {
		return fmt.Errorf("read index status: %w", err)
	}

	return encodeJSON(os.Stdout, status)
}

func search(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("search", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	text := flags.String("text", "", "FTS query text")
	limit := addSearchLimit(flags)

	err := parseCommandFlags(flags, args, "search")
	if err != nil {
		return err
	}

	if *text == "" {
		return fmt.Errorf("%w: --text", errSearchTextRequired)
	}

	return printStoreJSON(
		ctx,
		*storeFlags.root,
		*storeFlags.dbPath,
		func(store *codeintel.Store) (any, error) {
			return store.Search(ctx, codeintel.SearchQuery{
				Text:  *text,
				Limit: *limit,
			})
		},
	)
}

func printStoreJSON(
	ctx context.Context,
	root string,
	dbPath string,
	query func(*codeintel.Store) (any, error),
) error {
	store, err := openStore(ctx, root, dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := query(store)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func openStore(ctx context.Context, root, dbPath string) (*codeintel.Store, error) {
	store, err := codeintel.Open(ctx, resolvedDBPath(root, dbPath))
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}

	return store, nil
}

func openReadOnlyStore(
	ctx context.Context,
	root,
	dbPath string,
) (*codeintel.Store, error) {
	store, err := codeintel.OpenReadOnly(ctx, resolvedDBPath(root, dbPath))
	if err != nil {
		return nil, fmt.Errorf("open read-only code intelligence store: %w", err)
	}

	return store, nil
}

func resolvedDBPath(root, dbPath string) string {
	if dbPath != "" {
		return dbPath
	}

	return codeintel.DefaultDBPath(root)
}

func resolvedDuckDBPath(root, dbPath string) string {
	if dbPath != "" {
		return dbPath
	}

	return codeintel.DefaultDuckDBPath(root)
}

func encodeJSON(output *os.File, value any) error {
	err := feedback.WriteJSON(output, value)
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}

	return nil
}

func codeIntelCSVPaths(value string) []string {
	parts := strings.Split(strings.TrimSpace(value), ",")
	paths := []string{}

	for _, part := range parts {
		path := strings.TrimSpace(part)
		if path != "" {
			paths = append(paths, path)
		}
	}

	return paths
}

func parseOptionalVector(value string) ([]float32, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	return parseVector(value)
}

func parseVector(value string) ([]float32, error) {
	parts := strings.Split(strings.TrimSpace(value), ",")

	vector := make([]float32, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		parsed, err := strconv.ParseFloat(part, 32)
		if err != nil {
			return nil, fmt.Errorf("parse vector value %q: %w", part, err)
		}

		vector = append(vector, float32(parsed))
	}

	if len(vector) == 0 {
		return nil, errVectorValuesRequired
	}

	return vector, nil
}
