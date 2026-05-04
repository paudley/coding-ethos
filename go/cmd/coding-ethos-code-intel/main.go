// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

var errCommandRequired = errors.New("code intelligence command is required")

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errCommandRequired
	}

	switch args[0] {
	case "ingest-traces":
		return ingestTraces(ctx, args[1:])
	case "ingest-sarif":
		return ingestSARIF(ctx, args[1:])
	case "index-code":
		return indexCode(ctx, args[1:])
	case "record-outcome":
		return recordOutcome(ctx, args[1:])
	case "record-embedding":
		return recordEmbedding(ctx, args[1:])
	case "upsert-vector":
		return upsertVector(ctx, args[1:])
	case "stats":
		return printStats(ctx, args[1:])
	case "repeated-failures":
		return printRepeatedFailures(ctx, args[1:])
	case "sarif-results":
		return printSARIFResults(ctx, args[1:])
	case "remediation-outcomes":
		return printRemediationOutcomes(ctx, args[1:])
	case "remediation-effectiveness":
		return printRemediationEffectiveness(ctx, args[1:])
	case "vector-stats":
		return printVectorStats(ctx, args[1:])
	case "embedding-records":
		return printEmbeddingRecords(ctx, args[1:])
	case "embedding-candidates":
		return printEmbeddingCandidates(ctx, args[1:])
	case "code-chunks":
		return printCodeChunks(ctx, args[1:])
	case "hybrid-search":
		return hybridSearch(ctx, args[1:])
	case "index-status":
		return printIndexStatus(ctx, args[1:])
	case "search":
		return search(ctx, args[1:])
	default:
		return fmt.Errorf("unknown code intelligence command %q", args[0])
	}
}

func upsertVector(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("upsert-vector", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	uri := flags.String("uri", "", "Vector backend URI")
	collection := flags.String("collection", "remediations", "Vector collection")
	modelID := flags.String("model-id", "", "Embedding model ID")
	id := flags.String("id", "", "Vector record ID")
	vectorText := flags.String("vector", "", "Comma-separated float32 embedding values")
	recordKind := flags.String("record-kind", "", "Source record kind")
	recordID := flags.String("record-id", "", "Source record ID")
	policyID := flags.String("policy-id", "", "Policy ID metadata")
	skillID := flags.String("skill-id", "", "Skill ID metadata")
	path := flags.String("path", "", "Path metadata")
	outcome := flags.String("outcome", "", "Outcome metadata")
	message := flags.String("message", "", "Message metadata")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse upsert-vector flags: %w", err)
	}
	vector, err := parseVector(*vectorText)
	if err != nil {
		return err
	}
	if *uri == "" {
		*uri = codeintel.DefaultVectorPath(*root)
	}
	index, err := codeintel.NewVectorIndex(ctx, codeintel.VectorBackendConfig{
		Backend: "sqlite-vec",
		URI:     *uri,
	})
	if err != nil {
		return err
	}
	if closer, ok := index.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	metadata := map[string]string{
		"record_kind": strings.TrimSpace(*recordKind),
		"record_id":   strings.TrimSpace(*recordID),
		"policy_id":   strings.TrimSpace(*policyID),
		"skill_id":    strings.TrimSpace(*skillID),
		"path":        strings.TrimSpace(*path),
		"outcome":     strings.TrimSpace(*outcome),
		"message":     strings.TrimSpace(*message),
	}
	if err := index.UpsertEmbedding(ctx, evidence.VectorRecord{
		ID:         *id,
		Collection: *collection,
		ModelID:    *modelID,
		InputKind:  "text",
		Text:       *message,
		Vector:     vector,
		Dimension:  len(vector),
		Metadata:   metadata,
	}); err != nil {
		return err
	}
	if strings.TrimSpace(*recordKind) != "" && strings.TrimSpace(*recordID) != "" {
		store, err := openStore(ctx, *root, *dbPath)
		if err != nil {
			return err
		}
		defer store.Close()
		if err := store.UpsertEmbeddingRecord(ctx, codeintel.EmbeddingRecord{
			Backend:      "sqlite-vec",
			Collection:   *collection,
			ModelID:      *modelID,
			InputKind:    "text",
			RecordKind:   *recordKind,
			RecordID:     *recordID,
			Dimension:    len(vector),
			PolicyID:     *policyID,
			SkillID:      *skillID,
			Path:         *path,
			BackendRowID: *id,
		}); err != nil {
			return err
		}
	}

	stats, err := index.Stats(ctx)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, stats)
}

func ingestTraces(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("ingest-traces", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos traces")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse ingest-traces flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	summary, err := codeintel.NewTraceIngester(store).IngestTraceDirs(ctx, *root)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, summary)
}

func ingestSARIF(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("ingest-sarif", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	file := flags.String("file", "", "SARIF file to ingest")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse ingest-sarif flags: %w", err)
	}
	if *file == "" {
		return errors.New("--file is required")
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

	if err := codeintel.NewTraceIngester(store).IngestSARIF(ctx, *file, payload); err != nil {
		return err
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, stats)
}

func recordOutcome(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("record-outcome", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	remediationID := flags.String("remediation-id", "", "Remediation ID")
	findingID := flags.String("finding-id", "", "Finding ID")
	sourceTraceID := flags.String("source-trace-id", "", "Trace that emitted the remediation")
	followupTraceID := flags.String("followup-trace-id", "", "Trace from the follow-up attempt")
	policyID := flags.String("policy-id", "", "Policy ID")
	skillID := flags.String("skill-id", "", "Skill ID")
	path := flags.String("path", "", "File/path context")
	provider := flags.String("provider", "", "Agent provider")
	tool := flags.String("tool", "", "Agent tool")
	outcome := flags.String("outcome", "", "Outcome: attempted, fixed, repeated, superseded, or unknown")
	attempt := flags.Int("attempt", 0, "Attempt ordinal")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse record-outcome flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.RecordRemediationOutcome(ctx, codeintel.RemediationOutcome{
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
	}); err != nil {
		return err
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return err
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
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse record-embedding flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.UpsertEmbeddingRecord(ctx, codeintel.EmbeddingRecord{
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
	}); err != nil {
		return err
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, stats)
}

func printStats(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("stats", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse stats flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	stats, err := store.Stats(ctx)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, stats)
}

func printRepeatedFailures(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("repeated-failures", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	policyID := flags.String("policy-id", "", "Filter by policy ID")
	skillID := flags.String("skill-id", "", "Filter by skill ID")
	path := flags.String("path", "", "Filter by normalized source path")
	limit := flags.Int("limit", 20, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse repeated-failures flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.RepeatedFailures(ctx, codeintel.RepeatedFailureQuery{
		PolicyID: *policyID,
		SkillID:  *skillID,
		Path:     *path,
		Limit:    *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func printSARIFResults(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("sarif-results", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	runID := flags.String("run-id", "", "Filter by SARIF run ID")
	traceID := flags.String("trace-id", "", "Filter by linked trace ID")
	policyID := flags.String("policy-id", "", "Filter by policy ID")
	skillID := flags.String("skill-id", "", "Filter by skill ID")
	path := flags.String("path", "", "Filter by source path")
	limit := flags.Int("limit", 20, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse sarif-results flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.SARIFResults(ctx, codeintel.SARIFResultQuery{
		RunID:    *runID,
		TraceID:  *traceID,
		PolicyID: *policyID,
		SkillID:  *skillID,
		Path:     *path,
		Limit:    *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func printRemediationOutcomes(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("remediation-outcomes", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	policyID := flags.String("policy-id", "", "Filter by policy ID")
	skillID := flags.String("skill-id", "", "Filter by skill ID")
	outcome := flags.String("outcome", "", "Filter by outcome")
	path := flags.String("path", "", "Filter by source path")
	limit := flags.Int("limit", 20, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse remediation-outcomes flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.RemediationOutcomes(ctx, codeintel.RemediationOutcomeQuery{
		PolicyID: *policyID,
		SkillID:  *skillID,
		Outcome:  *outcome,
		Path:     *path,
		Limit:    *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func printRemediationEffectiveness(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("remediation-effectiveness", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	policyID := flags.String("policy-id", "", "Filter by policy ID")
	skillID := flags.String("skill-id", "", "Filter by skill ID")
	path := flags.String("path", "", "Filter by source path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse remediation-effectiveness flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.RemediationEffectiveness(ctx, codeintel.RemediationOutcomeQuery{
		PolicyID: *policyID,
		SkillID:  *skillID,
		Path:     *path,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func printVectorStats(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("vector-stats", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	backend := flags.String("backend", "sqlite-vec", "Vector backend")
	uri := flags.String("uri", "", "Vector backend URI")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse vector-stats flags: %w", err)
	}
	if *uri == "" {
		*uri = codeintel.DefaultVectorPath(*root)
	}

	index, err := codeintel.NewVectorIndex(ctx, codeintel.VectorBackendConfig{
		Backend: *backend,
		URI:     *uri,
	})
	if err != nil {
		return err
	}
	if closer, ok := index.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	stats, err := index.Stats(ctx)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, stats)
}

func printEmbeddingRecords(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("embedding-records", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	backend := flags.String("backend", "", "Filter by vector backend")
	collection := flags.String("collection", "", "Filter by collection")
	modelID := flags.String("model-id", "", "Filter by embedding model ID")
	recordKind := flags.String("record-kind", "", "Filter by source record kind")
	recordID := flags.String("record-id", "", "Filter by source record ID")
	limit := flags.Int("limit", 20, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse embedding-records flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.EmbeddingRecords(ctx, codeintel.EmbeddingRecordQuery{
		Backend:    *backend,
		Collection: *collection,
		ModelID:    *modelID,
		RecordKind: *recordKind,
		RecordID:   *recordID,
		Limit:      *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func printEmbeddingCandidates(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("embedding-candidates", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	recordKind := flags.String("record-kind", "", "Filter by source record kind")
	policyID := flags.String("policy-id", "", "Filter by policy ID")
	skillID := flags.String("skill-id", "", "Filter by skill ID")
	path := flags.String("path", "", "Filter by path")
	limit := flags.Int("limit", 20, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse embedding-candidates flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.EmbeddingCandidates(ctx, codeintel.EmbeddingCandidateQuery{
		RecordKind: *recordKind,
		PolicyID:   *policyID,
		SkillID:    *skillID,
		Path:       *path,
		Limit:      *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
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
	limit := flags.Int("limit", 10, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse hybrid-search flags: %w", err)
	}
	vector, err := parseOptionalVector(*vectorText)
	if err != nil {
		return err
	}
	if *text == "" && len(vector) == 0 {
		return errors.New("--text or --vector is required")
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
		return err
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
		return err
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
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse index-status flags: %w", err)
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
		return err
	}
	if closer, ok := index.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	vectorStats, err := index.Stats(ctx)
	if err != nil {
		return err
	}
	status, err := store.IndexStatus(ctx, vectorStats, codeintel.EmbeddingRecordQuery{
		Backend:    "sqlite-vec",
		Collection: *collection,
		ModelID:    *modelID,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, status)
}

func search(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("search", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	text := flags.String("text", "", "FTS query text")
	limit := flags.Int("limit", 10, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse search flags: %w", err)
	}
	if *text == "" {
		return errors.New("--text is required")
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.Search(ctx, codeintel.SearchQuery{
		Text:  *text,
		Limit: *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func openStore(ctx context.Context, root string, dbPath string) (*codeintel.Store, error) {
	return codeintel.Open(ctx, resolvedDBPath(root, dbPath))
}

func resolvedDBPath(root string, dbPath string) string {
	if dbPath != "" {
		return dbPath
	}

	return codeintel.DefaultDBPath(root)
}

func encodeJSON(output *os.File, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
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
		return nil, errors.New("vector must include at least one value")
	}

	return vector, nil
}
