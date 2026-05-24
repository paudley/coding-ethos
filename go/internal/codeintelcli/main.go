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
	defaultResultLimit = 20
	defaultSearchLimit = 10
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
		"embedding-candidates":      printEmbeddingCandidates,
		"embedding-records":         printEmbeddingRecords,
		"enrich-listing":            enrichDirectoryListing,
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
		"remediation-effectiveness": printRemediationEffectiveness,
		"remediation-outcomes":      printRemediationOutcomes,
		"proxy-events":              printProxyEvents,
		"proxy-file-read":           proxyFileRead,
		"proxy-sessions":            printProxySessions,
		"repo-map":                  printRepoMap,
		"repeated-failures":         printRepeatedFailures,
		"sarif-results":             printSARIFResults,
		"search":                    search,
		"stats":                     printStats,
		"upsert-vector":             upsertVector,
		"vector-stats":              printVectorStats,
	}
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
		dbPath: flags.String("db", "", "SQLite code intelligence database path"),
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

	dbPath := flags.String("db", "", "SQLite code intelligence database path")

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
	dbPath := flags.String("db", "", "SQLite code intelligence database path")

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
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
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

	err = store.RecordRemediationOutcome(ctx, codeintel.RemediationOutcome{
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
	})
	if err != nil {
		return fmt.Errorf("record remediation outcome: %w", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return fmt.Errorf("read code intelligence stats: %w", err)
	}

	return encodeJSON(os.Stdout, stats)
}

func recordEmbedding(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("record-embedding", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
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

	err = store.UpsertEmbeddingRecord(ctx, codeintel.EmbeddingRecord{
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
	})
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

	dbPath := flags.String("db", "", "SQLite code intelligence database path")

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
	backend := flags.String("backend", "sqlite-vec", "Vector backend")

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
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
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
		Backend: "sqlite-vec",
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
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
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
		Backend: "sqlite-vec",
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
		Backend:    "sqlite-vec",
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

func resolvedDBPath(root, dbPath string) string {
	if dbPath != "" {
		return dbPath
	}

	return codeintel.DefaultDBPath(root)
}

func encodeJSON(output *os.File, value any) error {
	err := feedback.WriteJSON(output, value)
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}

	return nil
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
