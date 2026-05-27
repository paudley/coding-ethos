// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

type upsertVectorOptions struct {
	root       string
	dbPath     string
	uri        string
	collection string
	modelID    string
	vectorID   string
	vectorText string
	recordKind string
	recordID   string
	policyID   string
	skillID    string
	path       string
	outcome    string
	message    string
}

func upsertVector(ctx context.Context, args []string) error {
	options, err := parseUpsertVectorOptions(args)
	if err != nil {
		return err
	}

	vector, err := parseVector(options.vectorText)
	if err != nil {
		return err
	}

	index, err := openDuckDBVectorIndex(ctx, options.root, options.uri)
	if err != nil {
		return err
	}

	if closer, ok := index.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	err = index.UpsertEmbedding(ctx, options.vectorRecord(vector))
	if err != nil {
		return fmt.Errorf("upsert vector embedding: %w", err)
	}

	err = recordVectorEmbedding(ctx, options, len(vector))
	if err != nil {
		return err
	}

	stats, err := index.Stats(ctx)
	if err != nil {
		return fmt.Errorf("read vector stats: %w", err)
	}

	return encodeJSON(os.Stdout, stats)
}

func parseUpsertVectorOptions(args []string) (upsertVectorOptions, error) {
	flags := flag.NewFlagSet("upsert-vector", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	uri := flags.String("uri", "", "Vector backend URI")
	collection := flags.String("collection", "remediations", "Vector collection")
	modelID := flags.String("model-id", "", "Embedding model ID")
	vectorID := flags.String("id", "", "Vector record ID")
	vectorText := flags.String("vector", "", "Comma-separated float32 embedding values")
	recordKind := flags.String("record-kind", "", "Source record kind")
	recordID := flags.String("record-id", "", "Source record ID")
	policyID := flags.String("policy-id", "", "Policy ID metadata")
	skillID := flags.String("skill-id", "", "Skill ID metadata")
	path := flags.String("path", "", "Path metadata")
	outcome := flags.String("outcome", "", "Outcome metadata")
	message := flags.String("message", "", "Message metadata")

	err := parseCommandFlags(flags, args, "upsert-vector")
	if err != nil {
		return upsertVectorOptions{}, err
	}

	return upsertVectorOptions{
		root:       *storeFlags.root,
		dbPath:     *storeFlags.dbPath,
		uri:        *uri,
		collection: *collection,
		modelID:    *modelID,
		vectorID:   *vectorID,
		vectorText: *vectorText,
		recordKind: *recordKind,
		recordID:   *recordID,
		policyID:   *policyID,
		skillID:    *skillID,
		path:       *path,
		outcome:    *outcome,
		message:    *message,
	}, nil
}

func openDuckDBVectorIndex(
	ctx context.Context,
	root string,
	uri string,
) (evidence.VectorIndex, error) {
	if uri == "" {
		uri = codeintel.DefaultVectorPath(root)
	}

	index, err := codeintel.NewVectorIndex(ctx, codeintel.VectorBackendConfig{
		Backend: codeintel.VectorBackendDuckDBVSS,
		URI:     uri,
	})
	if err != nil {
		return nil, fmt.Errorf("open DuckDB vector index: %w", err)
	}

	return index, nil
}

func (options upsertVectorOptions) vectorRecord(
	vector []float32,
) evidence.VectorRecord {
	return evidence.VectorRecord{
		ID:         options.vectorID,
		Collection: options.collection,
		ModelID:    options.modelID,
		InputKind:  "text",
		Text:       options.message,
		Vector:     vector,
		Dimension:  len(vector),
		Metadata:   options.metadata(),
	}
}

func (options upsertVectorOptions) metadata() map[string]string {
	return map[string]string{
		"record_kind": strings.TrimSpace(options.recordKind),
		"record_id":   strings.TrimSpace(options.recordID),
		"policy_id":   strings.TrimSpace(options.policyID),
		"skill_id":    strings.TrimSpace(options.skillID),
		"path":        strings.TrimSpace(options.path),
		"outcome":     strings.TrimSpace(options.outcome),
		"message":     strings.TrimSpace(options.message),
	}
}

func recordVectorEmbedding(
	ctx context.Context,
	options upsertVectorOptions,
	dimension int,
) error {
	if strings.TrimSpace(options.recordKind) == "" ||
		strings.TrimSpace(options.recordID) == "" {
		return nil
	}

	store, err := openStore(ctx, options.root, options.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	err = store.UpsertEmbeddingRecord(ctx, codeintel.EmbeddingRecord{
		Backend:      codeintel.VectorBackendDuckDBVSS,
		Collection:   options.collection,
		ModelID:      options.modelID,
		InputKind:    "text",
		RecordKind:   options.recordKind,
		RecordID:     options.recordID,
		Dimension:    dimension,
		PolicyID:     options.policyID,
		SkillID:      options.skillID,
		Path:         options.path,
		BackendRowID: options.vectorID,
	})
	if err != nil {
		return fmt.Errorf("record vector embedding metadata: %w", err)
	}

	return nil
}
