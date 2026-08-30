// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:funcorder,lll,noinlineerr,varnamelen,wsl_v5 // SQL lifecycle helpers remain near callers.
package tokeneconomy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
)

const (
	storeDirMode  = 0o700
	storeFileMode = 0o600
)

var (
	errInvalidRecord  = errors.New("invalid token-economy record")
	errRecordConflict = errors.New("token-economy record conflicts with stored evidence")
)

// Store is the durable token-economy evidence store.
type Store struct {
	database *sql.DB
}

// DefaultDBPath returns the private token-economy database path for a state root.
func DefaultDBPath(stateRoot string) string {
	return filepath.Join(stateRoot, ".coding-ethos", "token-economy.duckdb")
}

// Open creates or upgrades a token-economy evidence store.
func Open(ctx context.Context, path string) (*Store, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return nil, fmt.Errorf("%w: database path is required", errInvalidRecord)
	}

	err := os.MkdirAll(filepath.Dir(path), storeDirMode)
	if err != nil {
		return nil, fmt.Errorf("create token-economy store directory: %w", err)
	}

	database, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open token-economy store: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	store := &Store{database: database}
	if err = store.migrate(ctx); err != nil {
		_ = database.Close()

		return nil, err
	}

	err = os.Chmod(path, storeFileMode)
	if err != nil {
		_ = database.Close()

		return nil, fmt.Errorf("secure token-economy store: %w", err)
	}

	return store, nil
}

// Close closes the token-economy store.
func (store *Store) Close() error {
	err := store.database.Close()
	if err != nil {
		return fmt.Errorf("close token-economy store: %w", err)
	}

	return nil
}

func (store *Store) migrate(ctx context.Context) error {
	for _, statement := range tokenEconomySchemaStatements() {
		_, err := store.database.ExecContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("migrate token-economy store: %w", err)
		}
	}

	_, err := store.database.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO token_economy_metadata(key, value)
		VALUES ('schema_version', ?)`,
		SchemaVersion,
	)
	if err != nil {
		return fmt.Errorf("record token-economy schema version: %w", err)
	}

	return nil
}

func tokenEconomySchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS token_economy_metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS token_economy_experiments (
			experiment_id TEXT PRIMARY KEY,
			record_sha256 TEXT NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			created_at_utc TEXT NOT NULL,
			raw_json TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS token_economy_tasks (
			experiment_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			record_sha256 TEXT NOT NULL,
			task_kind TEXT NOT NULL,
			raw_json TEXT NOT NULL,
			PRIMARY KEY(experiment_id, task_id)
		)`,
		`CREATE TABLE IF NOT EXISTS token_economy_runs (
			run_id TEXT PRIMARY KEY,
			experiment_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			arm TEXT NOT NULL,
			replicate INTEGER NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			status TEXT NOT NULL,
			accepted INTEGER NOT NULL,
			severe_governance_violation INTEGER NOT NULL,
			total_tokens BIGINT NOT NULL,
			record_sha256 TEXT NOT NULL,
			raw_json TEXT NOT NULL,
			UNIQUE(experiment_id, task_id, arm, replicate)
		)`,
		`CREATE TABLE IF NOT EXISTS token_economy_usage_events (
			run_id TEXT NOT NULL,
			ordinal INTEGER NOT NULL,
			provider_message_id TEXT,
			usage_kind TEXT NOT NULL,
			input_tokens BIGINT NOT NULL,
			cached_input_tokens BIGINT NOT NULL,
			cache_creation_input_tokens BIGINT NOT NULL,
			cache_read_input_tokens BIGINT NOT NULL,
			output_tokens BIGINT NOT NULL,
			reasoning_output_tokens BIGINT NOT NULL,
			total_tokens BIGINT NOT NULL,
			PRIMARY KEY(run_id, ordinal)
		)`,
		`CREATE TABLE IF NOT EXISTS token_economy_mechanisms (
			run_id TEXT PRIMARY KEY,
			raw_context_tokens BIGINT NOT NULL,
			delivered_context_tokens BIGINT NOT NULL,
			avoided_context_tokens BIGINT NOT NULL,
			injected_guidance_tokens BIGINT NOT NULL,
			repeated_advice_count INTEGER NOT NULL,
			transform_event_count INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_token_economy_runs_experiment
		ON token_economy_runs(experiment_id, task_id, arm, replicate)`,
	}
}

// RecordExperiment stores immutable experiment provenance idempotently.
func (store *Store) RecordExperiment(ctx context.Context, record Experiment) error {
	if strings.TrimSpace(record.ExperimentID) == "" ||
		strings.TrimSpace(string(record.Provider)) == "" ||
		strings.TrimSpace(record.Model) == "" ||
		strings.TrimSpace(record.ManifestSHA256) == "" ||
		strings.TrimSpace(record.ProtocolSHA256) == "" {
		return fmt.Errorf("%w: experiment identity is incomplete", errInvalidRecord)
	}

	payload, digest, err := immutablePayload(record)
	if err != nil {
		return err
	}

	var storedDigest string
	err = store.database.QueryRowContext(
		ctx,
		"SELECT record_sha256 FROM token_economy_experiments WHERE experiment_id = ?",
		record.ExperimentID,
	).Scan(&storedDigest)
	if err == nil {
		return compareImmutableDigest("experiment", record.ExperimentID, storedDigest, digest)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"query token-economy experiment %q: %w",
			record.ExperimentID,
			err,
		)
	}

	_, err = store.database.ExecContext(
		ctx,
		`INSERT INTO token_economy_experiments(
			experiment_id, record_sha256, provider, model, created_at_utc, raw_json
		) VALUES (?, ?, ?, ?, ?, ?)`,
		record.ExperimentID,
		digest,
		record.Provider,
		record.Model,
		record.CreatedAtUTC,
		string(payload),
	)
	if err != nil {
		return fmt.Errorf(
			"insert token-economy experiment %q: %w",
			record.ExperimentID,
			err,
		)
	}

	return nil
}

// RecordTask stores immutable task provenance idempotently.
func (store *Store) RecordTask(ctx context.Context, record Task) error {
	if strings.TrimSpace(record.ExperimentID) == "" ||
		strings.TrimSpace(record.TaskID) == "" ||
		strings.TrimSpace(record.SourceSHA256) == "" ||
		strings.TrimSpace(record.PromptSHA256) == "" ||
		strings.TrimSpace(record.ValidatorSHA256) == "" {
		return fmt.Errorf("%w: task identity is incomplete", errInvalidRecord)
	}

	payload, digest, err := immutablePayload(record)
	if err != nil {
		return err
	}

	var storedDigest string

	err = store.database.QueryRowContext(
		ctx,
		`SELECT record_sha256 FROM token_economy_tasks
		WHERE experiment_id = ? AND task_id = ?`,
		record.ExperimentID,
		record.TaskID,
	).Scan(&storedDigest)
	if err == nil {
		return compareImmutableDigest("task", record.TaskID, storedDigest, digest)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("query token-economy task %q: %w", record.TaskID, err)
	}

	_, err = store.database.ExecContext(
		ctx,
		`INSERT INTO token_economy_tasks(
			experiment_id, task_id, record_sha256, task_kind, raw_json
		) VALUES (?, ?, ?, ?, ?)`,
		record.ExperimentID,
		record.TaskID,
		digest,
		record.Kind,
		string(payload),
	)
	if err != nil {
		return fmt.Errorf("insert token-economy task %q: %w", record.TaskID, err)
	}

	return nil
}

// RecordRun stores a terminal run and its sanitized usage evidence atomically.
func (store *Store) RecordRun(ctx context.Context, record Run) error {
	err := validateRun(record)
	if err != nil {
		return err
	}

	payload, digest, err := immutablePayload(record)
	if err != nil {
		return err
	}

	storedDigest, found, err := store.runDigest(ctx, record.RunID)
	if err != nil {
		return err
	}
	if found {
		return compareImmutableDigest("run", record.RunID, storedDigest, digest)
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin token-economy run insert: %w", err)
	}
	defer func() {
		_ = transaction.Rollback() //nolint:errcheck // Committed and rolled-back transactions are terminal.
	}()

	err = validateRunParents(ctx, transaction, record)
	if err != nil {
		return err
	}

	err = insertRun(ctx, transaction, record, payload, digest)
	if err != nil {
		return err
	}

	err = insertUsageEvents(ctx, transaction, record.RunID, record.UsageEvents)
	if err != nil {
		return err
	}

	err = insertMechanisms(ctx, transaction, record.RunID, record.Mechanisms)
	if err != nil {
		return err
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit token-economy run %q: %w", record.RunID, err)
	}

	return nil
}

func validateRun(record Run) error {
	if strings.TrimSpace(record.RunID) == "" ||
		strings.TrimSpace(record.ExperimentID) == "" ||
		strings.TrimSpace(record.TaskID) == "" ||
		strings.TrimSpace(string(record.Provider)) == "" ||
		strings.TrimSpace(record.Status) == "" ||
		record.Replicate < 1 {
		return fmt.Errorf("%w: run identity is incomplete", errInvalidRecord)
	}

	switch record.Arm {
	case ArmFull, ArmStatic, ArmOff:
	default:
		return fmt.Errorf("%w: unknown arm %q", errInvalidRecord, record.Arm)
	}

	if record.Usage.TotalTokens < 0 {
		return fmt.Errorf("%w: total tokens cannot be negative", errInvalidRecord)
	}

	return nil
}

func validateRunParents(ctx context.Context, transaction *sql.Tx, record Run) error {
	var count int

	err := transaction.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM token_economy_tasks
		WHERE experiment_id = ? AND task_id = ?`,
		record.ExperimentID,
		record.TaskID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("query token-economy run parents: %w", err)
	}
	if count != 1 {
		return fmt.Errorf(
			"%w: task %q is not registered for experiment %q",
			errInvalidRecord,
			record.TaskID,
			record.ExperimentID,
		)
	}

	return nil
}

func insertRun(
	ctx context.Context,
	transaction *sql.Tx,
	record Run,
	payload []byte,
	digest string,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO token_economy_runs(
			run_id, experiment_id, task_id, arm, replicate, provider, model,
			status, accepted, severe_governance_violation, total_tokens,
			record_sha256, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RunID,
		record.ExperimentID,
		record.TaskID,
		record.Arm,
		record.Replicate,
		record.Provider,
		record.Model,
		record.Status,
		record.Accepted,
		record.SevereGovernanceViolation,
		record.Usage.TotalTokens,
		digest,
		string(payload),
	)
	if err != nil {
		return fmt.Errorf("insert token-economy run %q: %w", record.RunID, err)
	}

	return nil
}

func insertUsageEvents(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	events []UsageEvent,
) error {
	for _, event := range events {
		_, err := transaction.ExecContext(
			ctx,
			`INSERT INTO token_economy_usage_events(
				run_id, ordinal, provider_message_id, usage_kind, input_tokens,
				cached_input_tokens, cache_creation_input_tokens,
				cache_read_input_tokens, output_tokens, reasoning_output_tokens,
				total_tokens
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			runID,
			event.Ordinal,
			event.ProviderMessageID,
			event.UsageKind,
			event.Usage.InputTokens,
			event.Usage.CachedInputTokens,
			event.Usage.CacheCreationInputTokens,
			event.Usage.CacheReadInputTokens,
			event.Usage.OutputTokens,
			event.Usage.ReasoningOutputTokens,
			event.Usage.TotalTokens,
		)
		if err != nil {
			return fmt.Errorf(
				"insert token-economy usage event %q:%d: %w",
				runID,
				event.Ordinal,
				err,
			)
		}
	}

	return nil
}

func insertMechanisms(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	metrics MechanismMetrics,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO token_economy_mechanisms(
			run_id, raw_context_tokens, delivered_context_tokens,
			avoided_context_tokens, injected_guidance_tokens,
			repeated_advice_count, transform_event_count
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		runID,
		metrics.RawContextTokens,
		metrics.DeliveredContextTokens,
		metrics.AvoidedContextTokens,
		metrics.InjectedGuidanceTokens,
		metrics.RepeatedAdviceCount,
		metrics.TransformEventCount,
	)
	if err != nil {
		return fmt.Errorf("insert token-economy mechanisms %q: %w", runID, err)
	}

	return nil
}

func (store *Store) runDigest(ctx context.Context, runID string) (string, bool, error) {
	var digest string

	err := store.database.QueryRowContext(
		ctx,
		"SELECT record_sha256 FROM token_economy_runs WHERE run_id = ?",
		runID,
	).Scan(&digest)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query token-economy run %q: %w", runID, err)
	}

	return digest, true, nil
}

func compareImmutableDigest(kind, id, stored, incoming string) error {
	if stored == incoming {
		return nil
	}

	return fmt.Errorf(
		"%w: %s %q changed from %s to %s",
		errRecordConflict,
		kind,
		id,
		stored,
		incoming,
	)
}

func immutablePayload(value any) ([]byte, string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, "", fmt.Errorf("encode token-economy evidence: %w", err)
	}

	digest := sha256.Sum256(payload)

	return payload, hex.EncodeToString(digest[:]), nil
}

// Runs returns all stored runs for an experiment in stable task order.
func (store *Store) Runs(ctx context.Context, experimentID string) ([]Run, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT raw_json FROM token_economy_runs
		WHERE experiment_id = ?
		ORDER BY task_id, replicate, arm`,
		experimentID,
	)
	if err != nil {
		return nil, fmt.Errorf("query token-economy runs: %w", err)
	}
	defer rows.Close()

	runs := []Run{}
	for rows.Next() {
		var raw string

		err = rows.Scan(&raw)
		if err != nil {
			return nil, fmt.Errorf("scan token-economy run: %w", err)
		}

		var run Run
		err = json.Unmarshal([]byte(raw), &run)
		if err != nil {
			return nil, fmt.Errorf("decode token-economy run: %w", err)
		}

		runs = append(runs, run)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate token-economy runs: %w", err)
	}

	return runs, nil
}

// Experiment returns immutable experiment provenance.
func (store *Store) Experiment(
	ctx context.Context,
	experimentID string,
) (Experiment, error) {
	var raw string

	err := store.database.QueryRowContext(
		ctx,
		`SELECT raw_json FROM token_economy_experiments WHERE experiment_id = ?`,
		experimentID,
	).Scan(&raw)
	if err != nil {
		return Experiment{}, fmt.Errorf("query token-economy experiment: %w", err)
	}

	var experiment Experiment
	err = json.Unmarshal([]byte(raw), &experiment)
	if err != nil {
		return Experiment{}, fmt.Errorf("decode token-economy experiment: %w", err)
	}

	return experiment, nil
}

// RunRecorded reports whether an immutable run ID already exists.
func (store *Store) RunRecorded(ctx context.Context, runID string) (bool, error) {
	_, found, err := store.runDigest(ctx, runID)

	return found, err
}
