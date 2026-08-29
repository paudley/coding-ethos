// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package tokeneconomy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

const (
	historicalAggregationContract = "canonical_source_path_ascending,event_id_unique_v1"
	historicalWindowContract      = "proxy_events.recorded_at_utc:[from,to)"
	percentageScale               = 100.0
)

var errTokenEconomyReport = errors.New("token-economy report error")

type historicalWindow struct {
	from     time.Time
	to       time.Time
	fromText string
	toText   string
}

type preparedHistoricalSource struct {
	info     os.FileInfo
	evidence HistoricalSource
}

type historicalEventIdentity struct {
	eventID   string
	sessionID string
	provider  Provider
}

type historicalIdentityOwner struct {
	sourcePath string
	provider   Provider
}

type historicalAggregate struct {
	providers     map[Provider]struct{}
	eventOwners   map[string]string
	sessionOwners map[string]historicalIdentityOwner
	metrics       HistoricalMetrics
}

// HistoricalReport measures deterministic gross context reduction across
// read-only code-intel stores for an explicit half-open UTC window.
func HistoricalReport(
	ctx context.Context,
	options HistoricalReportOptions,
	now time.Time,
) (Report, error) {
	window, sources, err := prepareHistoricalReport(options)
	if err != nil {
		return Report{}, err
	}

	aggregate := newHistoricalAggregate()

	for _, source := range sources {
		metrics, identities, readErr := readHistoricalSource(
			ctx,
			source.evidence.Path,
			window,
		)
		if readErr != nil {
			return Report{}, readErr
		}

		mergeErr := aggregate.merge(
			source.evidence.Path,
			metrics,
			identities,
		)
		if mergeErr != nil {
			return Report{}, mergeErr
		}
	}

	sourceEvidence, err := verifyHistoricalSources(sources)
	if err != nil {
		return Report{}, err
	}

	metrics, providers := aggregate.finish(window, sourceEvidence)

	conclusion := ConclusionObservational
	if metrics.TransformedEvents == 0 {
		conclusion = ConclusionInconclusive
	}

	return Report{
		Kind:           ReportKind,
		Cohort:         "historical",
		GeneratedAtUTC: reportTimestamp(now),
		Conclusion:     conclusion,
		SchemaVersion:  SchemaVersion,
		Causal:         false,
		Coverage: Coverage{
			Providers: providers,
			TaskCount: 0,
			RunCount:  metrics.ProxySessions,
			Reasons: []string{
				"no disabled or static control arm is present",
				"proxy transform tokens use the Coding Ethos estimator",
				"provider-native end-to-end usage is not joined to accepted outcomes",
			},
		},
		Historical: &metrics,
		Provenance: map[string]string{
			"historical_aggregation": historicalAggregationContract,
			"historical_window":      historicalWindowContract,
		},
	}, nil
}

func prepareHistoricalReport(
	options HistoricalReportOptions,
) (historicalWindow, []preparedHistoricalSource, error) {
	window, err := parseHistoricalWindow(options.FromUTC, options.ToUTC)
	if err != nil {
		return historicalWindow{}, nil, err
	}

	if len(options.DatabasePaths) == 0 {
		return historicalWindow{}, nil, fmt.Errorf(
			"%w: at least one code-intel database path is required",
			errTokenEconomyReport,
		)
	}

	sources := make([]preparedHistoricalSource, 0, len(options.DatabasePaths))
	for _, path := range options.DatabasePaths {
		source, sourceErr := prepareHistoricalSource(path)
		if sourceErr != nil {
			return historicalWindow{}, nil, sourceErr
		}

		sources = append(sources, source)
	}

	slices.SortFunc(sources, func(left, right preparedHistoricalSource) int {
		return strings.Compare(left.evidence.Path, right.evidence.Path)
	})

	err = validateDistinctHistoricalSources(sources)
	if err != nil {
		return historicalWindow{}, nil, err
	}

	for index := range sources {
		digest, hashErr := ledgerFileSHA256(sources[index].evidence.Path)
		if hashErr != nil {
			return historicalWindow{}, nil, fmt.Errorf(
				"hash historical source %q before reporting: %w",
				sources[index].evidence.Path,
				hashErr,
			)
		}

		sources[index].evidence.SHA256Before = digest
	}

	return window, sources, nil
}

func parseHistoricalWindow(fromValue, toValue string) (historicalWindow, error) {
	fromValue = strings.TrimSpace(fromValue)

	toValue = strings.TrimSpace(toValue)
	if fromValue == "" || toValue == "" {
		return historicalWindow{}, fmt.Errorf(
			"%w: from_utc and to_utc are required",
			errTokenEconomyReport,
		)
	}

	fromTime, err := time.Parse(time.RFC3339Nano, fromValue)
	if err != nil {
		return historicalWindow{}, fmt.Errorf(
			"%w: parse from_utc %q as RFC3339: %w",
			errTokenEconomyReport,
			fromValue,
			err,
		)
	}

	toTime, err := time.Parse(time.RFC3339Nano, toValue)
	if err != nil {
		return historicalWindow{}, fmt.Errorf(
			"%w: parse to_utc %q as RFC3339: %w",
			errTokenEconomyReport,
			toValue,
			err,
		)
	}

	fromTime = fromTime.UTC()
	toTime = toTime.UTC()

	if !fromTime.Before(toTime) {
		return historicalWindow{}, fmt.Errorf(
			"%w: from_utc must be before to_utc",
			errTokenEconomyReport,
		)
	}

	return historicalWindow{
		from:     fromTime,
		to:       toTime,
		fromText: fromTime.Format(time.RFC3339Nano),
		toText:   toTime.Format(time.RFC3339Nano),
	}, nil
}

func prepareHistoricalSource(path string) (preparedHistoricalSource, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return preparedHistoricalSource{}, fmt.Errorf(
			"%w: code-intel database path is empty",
			errTokenEconomyReport,
		)
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return preparedHistoricalSource{}, fmt.Errorf(
			"resolve historical source %q: %w",
			path,
			err,
		)
	}

	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return preparedHistoricalSource{}, fmt.Errorf(
			"canonicalize historical source %q: %w",
			path,
			err,
		)
	}

	canonical = filepath.Clean(canonical)

	info, err := os.Stat(canonical)
	if err != nil {
		return preparedHistoricalSource{}, fmt.Errorf(
			"stat historical source %q: %w",
			canonical,
			err,
		)
	}

	if !info.Mode().IsRegular() {
		return preparedHistoricalSource{}, fmt.Errorf(
			"%w: historical source is not a regular file: %s",
			errTokenEconomyReport,
			canonical,
		)
	}

	return preparedHistoricalSource{
		evidence: HistoricalSource{Path: canonical},
		info:     info,
	}, nil
}

func validateDistinctHistoricalSources(sources []preparedHistoricalSource) error {
	for index := range sources {
		for prior := range index {
			if sources[index].evidence.Path == sources[prior].evidence.Path ||
				os.SameFile(sources[index].info, sources[prior].info) {
				return fmt.Errorf(
					"%w: historical sources %q and %q identify the same file",
					errTokenEconomyReport,
					sources[prior].evidence.Path,
					sources[index].evidence.Path,
				)
			}
		}
	}

	return nil
}

func readHistoricalSource(
	ctx context.Context,
	path string,
	window historicalWindow,
) (HistoricalMetrics, []historicalEventIdentity, error) {
	database, err := sql.Open("duckdb", path+"?access_mode=READ_ONLY")
	if err != nil {
		return HistoricalMetrics{}, nil, fmt.Errorf(
			"open historical source %q read-only: %w",
			path,
			err,
		)
	}

	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	var identities []historicalEventIdentity

	metrics, readErr := queryHistoricalMetrics(ctx, database, window)
	if readErr == nil {
		identities, readErr = queryHistoricalEventIdentities(ctx, database, window)
	}

	closeErr := database.Close()

	err = errors.Join(readErr, closeErr)
	if err != nil {
		return HistoricalMetrics{}, nil, fmt.Errorf(
			"read historical source %q: %w",
			path,
			err,
		)
	}

	return metrics, identities, nil
}

func queryHistoricalMetrics(
	ctx context.Context,
	database *sql.DB,
	window historicalWindow,
) (HistoricalMetrics, error) {
	err := validateHistoricalEventTimestamps(ctx, database)
	if err != nil {
		return HistoricalMetrics{}, err
	}

	metrics := HistoricalMetrics{}

	err = database.QueryRowContext(
		ctx,
		`WITH first_transform AS (
			SELECT event_id, input_tokens,
				ROW_NUMBER() OVER (PARTITION BY event_id ORDER BY ordinal) AS rank
			FROM proxy_transforms
		)
		SELECT
			COALESCE(SUM(first_transform.input_tokens), 0),
			COALESCE(SUM(event.output_tokens), 0),
			COUNT(*),
			COALESCE(SUM(CASE WHEN first_transform.input_tokens > event.output_tokens
				THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN first_transform.input_tokens < event.output_tokens
				THEN 1 ELSE 0 END), 0)
		FROM first_transform
		JOIN proxy_events AS event USING(event_id)
		WHERE first_transform.rank = 1
			AND CAST(event.recorded_at_utc AS TIMESTAMPTZ) >= CAST(? AS TIMESTAMPTZ)
			AND CAST(event.recorded_at_utc AS TIMESTAMPTZ) < CAST(? AS TIMESTAMPTZ)`,
		window.fromText,
		window.toText,
	).Scan(
		&metrics.RawContextTokens,
		&metrics.DeliveredContextTokens,
		&metrics.TransformedEvents,
		&metrics.ReducedEvents,
		&metrics.ExpandedEvents,
	)
	if err != nil {
		return HistoricalMetrics{}, fmt.Errorf("query historical transforms: %w", err)
	}

	return metrics, nil
}

func validateHistoricalEventTimestamps(ctx context.Context, database *sql.DB) error {
	var eventID, recordedAt string

	err := database.QueryRowContext(
		ctx,
		`SELECT event_id, COALESCE(recorded_at_utc, '')
		FROM proxy_events
		WHERE COALESCE(TRIM(recorded_at_utc), '') = ''
			OR TRY_CAST(recorded_at_utc AS TIMESTAMPTZ) IS NULL
		ORDER BY event_id
		LIMIT 1`,
	).Scan(&eventID, &recordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("validate historical event timestamps: %w", err)
	}

	return fmt.Errorf(
		"%w: event %q has invalid recorded_at_utc %q",
		errTokenEconomyReport,
		eventID,
		recordedAt,
	)
}

func queryHistoricalEventIdentities(
	ctx context.Context,
	database *sql.DB,
	window historicalWindow,
) ([]historicalEventIdentity, error) {
	rows, err := database.QueryContext(
		ctx,
		`SELECT event.event_id, event.session_id,
			COALESCE(NULLIF(TRIM(event.provider), ''),
				NULLIF(TRIM(session.provider), ''), '') AS resolved_provider
		FROM proxy_events AS event
		LEFT JOIN proxy_sessions AS session USING(session_id)
		WHERE CAST(event.recorded_at_utc AS TIMESTAMPTZ) >= CAST(? AS TIMESTAMPTZ)
			AND CAST(event.recorded_at_utc AS TIMESTAMPTZ) < CAST(? AS TIMESTAMPTZ)
		ORDER BY event.event_id, event.session_id, resolved_provider`,
		window.fromText,
		window.toText,
	)
	if err != nil {
		return nil, fmt.Errorf("query historical event identities: %w", err)
	}
	defer rows.Close()

	identities := []historicalEventIdentity{}

	for rows.Next() {
		var identity historicalEventIdentity

		err = rows.Scan(
			&identity.eventID,
			&identity.sessionID,
			&identity.provider,
		)
		if err != nil {
			return nil, fmt.Errorf("scan historical event identity: %w", err)
		}

		if strings.TrimSpace(identity.eventID) == "" ||
			strings.TrimSpace(identity.sessionID) == "" {
			return nil, fmt.Errorf(
				"%w: historical event or session identity is empty",
				errTokenEconomyReport,
			)
		}

		identities = append(identities, identity)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate historical event identities: %w", err)
	}

	return identities, nil
}

func newHistoricalAggregate() historicalAggregate {
	return historicalAggregate{
		providers:     map[Provider]struct{}{},
		eventOwners:   map[string]string{},
		sessionOwners: map[string]historicalIdentityOwner{},
	}
}

func (aggregate *historicalAggregate) merge(
	sourcePath string,
	metrics HistoricalMetrics,
	identities []historicalEventIdentity,
) error {
	for _, identity := range identities {
		if owner, found := aggregate.eventOwners[identity.eventID]; found {
			return fmt.Errorf(
				"%w: event_id %q appears in both historical sources %q and %q",
				errTokenEconomyReport,
				identity.eventID,
				owner,
				sourcePath,
			)
		}

		aggregate.eventOwners[identity.eventID] = sourcePath

		mergeErr := aggregate.mergeSession(sourcePath, identity)
		if mergeErr != nil {
			return mergeErr
		}
	}

	aggregate.metrics.RawContextTokens += metrics.RawContextTokens
	aggregate.metrics.DeliveredContextTokens += metrics.DeliveredContextTokens
	aggregate.metrics.TransformedEvents += metrics.TransformedEvents
	aggregate.metrics.ReducedEvents += metrics.ReducedEvents
	aggregate.metrics.ExpandedEvents += metrics.ExpandedEvents

	return nil
}

func (aggregate *historicalAggregate) mergeSession(
	sourcePath string,
	identity historicalEventIdentity,
) error {
	owner, found := aggregate.sessionOwners[identity.sessionID]
	if found && owner.provider != "" && identity.provider != "" &&
		owner.provider != identity.provider {
		return fmt.Errorf(
			"%w: session_id %q has providers %q in %q and %q in %q",
			errTokenEconomyReport,
			identity.sessionID,
			owner.provider,
			owner.sourcePath,
			identity.provider,
			sourcePath,
		)
	}

	if !found || owner.provider == "" {
		aggregate.sessionOwners[identity.sessionID] = historicalIdentityOwner{
			sourcePath: sourcePath,
			provider:   identity.provider,
		}
	}

	if identity.provider != "" {
		aggregate.providers[identity.provider] = struct{}{}
	}

	return nil
}

func (aggregate *historicalAggregate) finish(
	window historicalWindow,
	sources []HistoricalSource,
) (HistoricalMetrics, []Provider) {
	aggregate.metrics.FromUTC = window.fromText
	aggregate.metrics.ToUTC = window.toText
	aggregate.metrics.Sources = sources
	aggregate.metrics.ProxySessions = len(aggregate.sessionOwners)

	aggregate.metrics.AvoidedContextTokens = aggregate.metrics.RawContextTokens -
		aggregate.metrics.DeliveredContextTokens

	if aggregate.metrics.RawContextTokens > 0 {
		aggregate.metrics.GrossReductionPercent = percentageScale *
			float64(aggregate.metrics.AvoidedContextTokens) /
			float64(aggregate.metrics.RawContextTokens)
	}

	providers := make([]Provider, 0, len(aggregate.providers))
	for provider := range aggregate.providers {
		providers = append(providers, provider)
	}

	slices.Sort(providers)

	return aggregate.metrics, providers
}

func verifyHistoricalSources(
	sources []preparedHistoricalSource,
) ([]HistoricalSource, error) {
	evidence := make([]HistoricalSource, 0, len(sources))
	for _, source := range sources {
		after, err := ledgerFileSHA256(source.evidence.Path)
		if err != nil {
			return nil, fmt.Errorf(
				"hash historical source %q after reporting: %w",
				source.evidence.Path,
				err,
			)
		}

		source.evidence.SHA256After = after
		source.evidence.SourceUnchanged = source.evidence.SHA256Before == after

		if !source.evidence.SourceUnchanged {
			return nil, fmt.Errorf(
				"%w: historical source %q changed while reporting: before %s, after %s",
				errTokenEconomyReport,
				source.evidence.Path,
				source.evidence.SHA256Before,
				after,
			)
		}

		evidence = append(evidence, source.evidence)
	}

	return evidence, nil
}
