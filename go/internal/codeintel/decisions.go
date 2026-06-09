// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	DecisionStatusAccepted = "accepted"
	DecisionStatusProposed = "proposed"

	DecisionSourceManual = "manual"
	DecisionSourceInline = "inline_marker"

	DecisionLinkAffects = "affects"
)

const (
	decisionConflictMinimum = 2
	decisionDefaultLimit    = 20
	decisionMaxLimit        = 100
	decisionStaleDays       = 180
	decisionMarkerMatchSize = 3
	decisionSearchBaseParts = 6
	decisionSearchLinkParts = 3
)

var (
	errDecisionLinkPathRequired = errors.New("decision link path is required")
	errDecisionIDRequired       = errors.New("decision id is required")
	errDecisionRecordRequired   = errors.New(
		"decision title and rationale are required",
	)
)

var decisionMarkerRE = regexp.MustCompile(`\b(WHY|DECISION|TRADEOFF):\s*(.+)$`)

type DecisionRecord struct {
	SourcePath      string         `json:"source_path,omitempty"`
	SearchText      string         `json:"search_text"`
	Status          string         `json:"status"`
	Rationale       string         `json:"rationale"`
	Alternatives    string         `json:"alternatives,omitempty"`
	SourceKind      string         `json:"source_kind"`
	ProvenanceClass string         `json:"provenance_class"`
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	Author          string         `json:"author,omitempty"`
	RecordedAtUTC   string         `json:"recorded_at_utc,omitempty"`
	UpdatedAtUTC    string         `json:"updated_at_utc,omitempty"`
	Links           []DecisionLink `json:"links,omitempty"`
	SourceLine      int            `json:"source_line,omitempty"`
}

type DecisionLink struct {
	DecisionID string `json:"decision_id,omitempty"`
	Path       string `json:"path"`
	SymbolPath string `json:"symbol_path,omitempty"`
	Kind       string `json:"kind"`
	Ordinal    int    `json:"ordinal,omitempty"`
}

type DecisionQuery struct {
	Text       string
	Path       string
	SymbolPath string
	Status     string
	Limit      int
}

type DecisionHealth struct {
	Stale       []DecisionRecord      `json:"stale,omitempty"`
	Conflicts   []DecisionConflict    `json:"conflicts,omitempty"`
	Ungoverned  []DecisionUngoverned  `json:"ungoverned,omitempty"`
	Overlapping []DecisionOverlap     `json:"overlapping,omitempty"`
	Summary     DecisionHealthSummary `json:"summary"`
}

type DecisionHealthSummary struct {
	DecisionCount   int `json:"decision_count"`
	StaleCount      int `json:"stale_count"`
	ConflictCount   int `json:"conflict_count"`
	UngovernedCount int `json:"ungoverned_count"`
	OverlapCount    int `json:"overlap_count"`
}

type DecisionConflict struct {
	Path        string   `json:"path"`
	Statuses    []string `json:"statuses"`
	DecisionIDs []string `json:"decision_ids"`
}

type DecisionUngoverned struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type DecisionOverlap struct {
	Path        string   `json:"path"`
	DecisionIDs []string `json:"decision_ids"`
}

func (store *Store) RecordDecision(
	ctx context.Context,
	decision DecisionRecord,
) (DecisionRecord, error) {
	decision = normalizeDecisionRecord(decision)
	if decision.Title == "" || decision.Rationale == "" {
		return DecisionRecord{}, errDecisionRecordRequired
	}

	raw, err := json.Marshal(decision)
	if err != nil {
		return DecisionRecord{}, fmt.Errorf("marshal decision record: %w", err)
	}

	store.writeMu.Lock()
	defer store.writeMu.Unlock()

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return DecisionRecord{}, fmt.Errorf("begin decision transaction: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	err = upsertDecision(ctx, transaction, decision, raw)
	if err != nil {
		return DecisionRecord{}, err
	}

	err = replaceDecisionLinks(ctx, transaction, decision.ID, decision.Links)
	if err != nil {
		return DecisionRecord{}, err
	}

	err = replaceDecisionFTS(ctx, transaction, decision)
	if err != nil {
		return DecisionRecord{}, err
	}

	err = transaction.Commit()
	if err != nil {
		return DecisionRecord{}, fmt.Errorf("commit decision transaction: %w", err)
	}

	return decision, nil
}

func (store *Store) LinkDecision(
	ctx context.Context,
	decisionID string,
	links []DecisionLink,
) error {
	decisionID = strings.TrimSpace(decisionID)
	if decisionID == "" {
		return errDecisionIDRequired
	}

	newLinks := normalizeDecisionLinks(decisionID, links)
	if len(newLinks) == 0 {
		return errDecisionLinkPathRequired
	}

	store.writeMu.Lock()
	defer store.writeMu.Unlock()

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin decision link transaction: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	existingLinks, err := store.decisionLinks(ctx, decisionID)
	if err != nil {
		return fmt.Errorf("query existing decision links: %w", err)
	}

	err = insertDecisionLinks(
		ctx,
		transaction,
		decisionID,
		len(existingLinks),
		newDecisionLinks(existingLinks, newLinks),
	)
	if err != nil {
		return err
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit decision link transaction: %w", err)
	}

	return nil
}

func (store *Store) Decisions(
	ctx context.Context,
	query DecisionQuery,
) ([]DecisionRecord, error) {
	limit := boundedDecisionLimit(query.Limit)
	status := strings.TrimSpace(query.Status)

	path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(query.Path)))
	if path == "." {
		path = ""
	}

	symbolPath := strings.TrimSpace(query.SymbolPath)
	text := strings.ToLower(strings.TrimSpace(query.Text))
	textPattern := "%" + text + "%"

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT decision_id, title, status, rationale, alternatives, source_kind,
			source_path, source_line, provenance_class, author, recorded_at_utc,
			updated_at_utc, search_text
		FROM decisions decision
		WHERE (? = '' OR decision.status = ?)
			AND (? = '' OR EXISTS (
				SELECT 1 FROM decision_links link
				WHERE link.decision_id = decision.decision_id AND link.path = ?
			))
			AND (? = '' OR EXISTS (
				SELECT 1 FROM decision_links link
				WHERE link.decision_id = decision.decision_id AND link.symbol_path = ?
			))
			AND (? = '' OR LOWER(decision.search_text) LIKE ?)
		ORDER BY updated_at_utc DESC, decision_id
		LIMIT ?`,
		status,
		status,
		path,
		path,
		symbolPath,
		symbolPath,
		text,
		textPattern,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query decisions: %w", err)
	}
	defer rows.Close()

	decisions, err := scanDecisionRows(rows)
	if err != nil {
		return nil, err
	}

	return store.attachDecisionLinks(ctx, decisions)
}

func (store *Store) DecisionHealth(
	ctx context.Context,
	query DecisionQuery,
) (DecisionHealth, error) {
	decisions, err := store.Decisions(ctx, query)
	if err != nil {
		return DecisionHealth{}, err
	}

	stale := staleDecisions(decisions, time.Now().UTC())
	conflicts := decisionConflicts(decisions)
	overlaps := decisionOverlaps(decisions)

	ungoverned, err := store.ungovernedDecisionHotspots(ctx, query)
	if err != nil {
		return DecisionHealth{}, err
	}

	return DecisionHealth{
		Stale:       stale,
		Conflicts:   conflicts,
		Overlapping: overlaps,
		Ungoverned:  ungoverned,
		Summary: DecisionHealthSummary{
			DecisionCount:   len(decisions),
			StaleCount:      len(stale),
			ConflictCount:   len(conflicts),
			UngovernedCount: len(ungoverned),
			OverlapCount:    len(overlaps),
		},
	}, nil
}

func (store *Store) ReplaceIndexedDecisions(
	ctx context.Context,
	path string,
	decisions []DecisionRecord,
) error {
	store.writeMu.Lock()
	defer store.writeMu.Unlock()

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin indexed decision transaction: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	err = deleteSourceDecisions(ctx, transaction, path)
	if err != nil {
		return err
	}

	for _, decision := range decisions {
		decision = normalizeDecisionRecord(decision)

		raw, marshalErr := json.Marshal(decision)
		if marshalErr != nil {
			return fmt.Errorf("marshal indexed decision: %w", marshalErr)
		}

		err = upsertDecision(ctx, transaction, decision, raw)
		if err != nil {
			return err
		}

		err = replaceDecisionLinks(ctx, transaction, decision.ID, decision.Links)
		if err != nil {
			return err
		}

		err = replaceDecisionFTS(ctx, transaction, decision)
		if err != nil {
			return err
		}
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit indexed decision transaction: %w", err)
	}

	return nil
}

func IndexedDecisions(path string, contents []byte) []DecisionRecord {
	decisions := make([]DecisionRecord, 0)
	inFence := false
	lineNum := 0

	for line := range strings.Lines(string(contents)) {
		lineNum++
		line = strings.TrimRight(line, "\r\n")

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence

			continue
		}

		if inFence {
			continue
		}

		match := decisionMarkerRE.FindStringSubmatch(line)
		if len(match) != decisionMarkerMatchSize || decisionMarkerInExample(line) {
			continue
		}

		body := strings.TrimSpace(match[2])
		if body == "" {
			continue
		}

		title := match[1] + ": " + firstDecisionSentence(body)
		decisions = append(decisions, normalizeDecisionRecord(DecisionRecord{
			Title:           title,
			Status:          DecisionStatusAccepted,
			Rationale:       body,
			SourceKind:      DecisionSourceInline,
			SourcePath:      path,
			SourceLine:      lineNum,
			ProvenanceClass: ProvenanceDocDerived,
			Links: []DecisionLink{{
				Path: path,
				Kind: DecisionLinkAffects,
			}},
		}))
	}

	return decisions
}

func normalizeDecisionRecord(decision DecisionRecord) DecisionRecord {
	decision.Title = strings.TrimSpace(decision.Title)

	decision.Status = strings.TrimSpace(decision.Status)
	if decision.Status == "" {
		decision.Status = DecisionStatusAccepted
	}

	decision.Rationale = strings.TrimSpace(decision.Rationale)
	decision.Alternatives = strings.TrimSpace(decision.Alternatives)

	decision.SourceKind = strings.TrimSpace(decision.SourceKind)
	if decision.SourceKind == "" {
		decision.SourceKind = DecisionSourceManual
	}

	decision.SourcePath = filepath.ToSlash(
		filepath.Clean(strings.TrimSpace(decision.SourcePath)),
	)
	if decision.SourcePath == "." {
		decision.SourcePath = ""
	}

	decision.ProvenanceClass = normalizeProvenanceClass(decision.ProvenanceClass)

	decision.Author = strings.TrimSpace(decision.Author)
	if decision.RecordedAtUTC == "" {
		decision.RecordedAtUTC = time.Now().UTC().Format(time.RFC3339)
	}

	if decision.UpdatedAtUTC == "" {
		decision.UpdatedAtUTC = decision.RecordedAtUTC
	}

	if decision.ID == "" {
		decision.ID = stableID(
			"decision",
			decision.Title,
			decision.SourceKind,
			decision.SourcePath,
			strconv.Itoa(decision.SourceLine),
			decision.Rationale,
		)
	}

	decision.Links = normalizeDecisionLinks(decision.ID, decision.Links)
	decision.SearchText = decisionSearchText(decision)

	return decision
}

func normalizeDecisionLinks(decisionID string, links []DecisionLink) []DecisionLink {
	result := make([]DecisionLink, 0, len(links))
	for _, link := range links {
		link.DecisionID = decisionID

		link.Path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(link.Path)))
		if link.Path == "." || link.Path == "" {
			continue
		}

		link.SymbolPath = strings.TrimSpace(link.SymbolPath)

		link.Kind = strings.TrimSpace(link.Kind)
		if link.Kind == "" {
			link.Kind = DecisionLinkAffects
		}

		link.Ordinal = len(result)
		result = append(result, link)
	}

	return result
}

func dedupeDecisionLinks(links []DecisionLink) []DecisionLink {
	result := make([]DecisionLink, 0, len(links))
	seen := map[string]struct{}{}

	for _, link := range links {
		key := decisionLinkKey(link)
		if _, found := seen[key]; found {
			continue
		}

		seen[key] = struct{}{}
		link.Ordinal = len(result)
		result = append(result, link)
	}

	return result
}

func newDecisionLinks(
	existingLinks []DecisionLink,
	candidateLinks []DecisionLink,
) []DecisionLink {
	seen := map[string]struct{}{}
	for _, link := range existingLinks {
		seen[decisionLinkKey(link)] = struct{}{}
	}

	result := make([]DecisionLink, 0, len(candidateLinks))
	for _, link := range dedupeDecisionLinks(candidateLinks) {
		key := decisionLinkKey(link)
		if _, found := seen[key]; found {
			continue
		}

		seen[key] = struct{}{}

		result = append(result, link)
	}

	return result
}

func decisionLinkKey(link DecisionLink) string {
	return strings.Join([]string{link.Path, link.SymbolPath, link.Kind}, "\x00")
}

func upsertDecision(
	ctx context.Context,
	transaction *sql.Tx,
	decision DecisionRecord,
	raw []byte,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO decisions(
			decision_id, title, status, rationale, alternatives, source_kind,
			source_path, source_line, provenance_class, author, recorded_at_utc,
			updated_at_utc, search_text, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		decision.ID,
		decision.Title,
		decision.Status,
		decision.Rationale,
		decision.Alternatives,
		decision.SourceKind,
		decision.SourcePath,
		decision.SourceLine,
		decision.ProvenanceClass,
		decision.Author,
		decision.RecordedAtUTC,
		decision.UpdatedAtUTC,
		decision.SearchText,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("upsert decision: %w", err)
	}

	return nil
}

func replaceDecisionLinks(
	ctx context.Context,
	transaction *sql.Tx,
	decisionID string,
	links []DecisionLink,
) error {
	_, err := transaction.ExecContext(
		ctx,
		"DELETE FROM decision_links WHERE decision_id = ?",
		decisionID,
	)
	if err != nil {
		return fmt.Errorf("delete decision links: %w", err)
	}

	return insertDecisionLinks(
		ctx,
		transaction,
		decisionID,
		0,
		dedupeDecisionLinks(normalizeDecisionLinks(decisionID, links)),
	)
}

func insertDecisionLinks(
	ctx context.Context,
	transaction *sql.Tx,
	decisionID string,
	ordinalOffset int,
	links []DecisionLink,
) error {
	for index, link := range links {
		_, err := transaction.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO decision_links(
				decision_id, ordinal, path, symbol_path, link_kind
			) VALUES (?, ?, ?, ?, ?)`,
			decisionID,
			ordinalOffset+index,
			link.Path,
			link.SymbolPath,
			link.Kind,
		)
		if err != nil {
			return fmt.Errorf("insert decision link: %w", err)
		}
	}

	return nil
}

func replaceDecisionFTS(
	ctx context.Context,
	transaction *sql.Tx,
	decision DecisionRecord,
) error {
	row := ftsRow{
		Kind:       "decision",
		RecordID:   decision.ID,
		Path:       decision.SourcePath,
		Message:    decision.Title,
		SearchText: decision.SearchText,
	}

	err := deleteFTSRow(ctx, transaction, ftsRowID(row))
	if err != nil {
		return err
	}

	return insertFTS(ctx, transaction, row)
}

func deleteSourceDecisions(
	ctx context.Context,
	transaction *sql.Tx,
	path string,
) error {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT decision_id FROM decisions
		WHERE source_kind = ? AND source_path = ?`,
		DecisionSourceInline,
		path,
	)
	if err != nil {
		return fmt.Errorf("query indexed decisions for replacement: %w", err)
	}
	defer rows.Close()

	ids := []any{}

	for rows.Next() {
		var decisionID string

		scanErr := rows.Scan(&decisionID)
		if scanErr != nil {
			return fmt.Errorf("scan indexed decision id: %w", scanErr)
		}

		ids = append(ids, decisionID)
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate indexed decision ids: %w", err)
	}

	if len(ids) == 0 {
		return nil
	}

	err = deleteDecisionFTSRows(ctx, transaction, ids)
	if err != nil {
		return err
	}

	err = batchDeleteEntities(ctx, transaction, "decision_links", "decision_id", ids)
	if err != nil {
		return err
	}

	return batchDeleteEntities(ctx, transaction, "decisions", "decision_id", ids)
}

func deleteDecisionFTSRows(ctx context.Context, transaction *sql.Tx, ids []any) error {
	for _, id := range ids {
		err := deleteFTSRow(ctx, transaction, ftsRowID(ftsRow{
			Kind:     "decision",
			RecordID: fmt.Sprint(id),
		}))
		if err != nil {
			return err
		}
	}

	return nil
}

func deleteFTSRow(ctx context.Context, transaction *sql.Tx, rowID string) error {
	_, err := transaction.ExecContext(
		ctx,
		"DELETE FROM code_intel_search_terms WHERE fts_id = ?",
		rowID,
	)
	if err != nil {
		return fmt.Errorf("delete decision search terms: %w", err)
	}

	_, err = transaction.ExecContext(
		ctx,
		"DELETE FROM code_intel_fts WHERE fts_id = ?",
		rowID,
	)
	if err != nil {
		return fmt.Errorf("delete decision FTS rows: %w", err)
	}

	return nil
}

func scanDecisionRows(rows *sql.Rows) ([]DecisionRecord, error) {
	decisions := []DecisionRecord{}

	for rows.Next() {
		var decision DecisionRecord

		err := rows.Scan(
			&decision.ID,
			&decision.Title,
			&decision.Status,
			&decision.Rationale,
			&decision.Alternatives,
			&decision.SourceKind,
			&decision.SourcePath,
			&decision.SourceLine,
			&decision.ProvenanceClass,
			&decision.Author,
			&decision.RecordedAtUTC,
			&decision.UpdatedAtUTC,
			&decision.SearchText,
		)
		if err != nil {
			return nil, fmt.Errorf("scan decision: %w", err)
		}

		decisions = append(decisions, decision)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate decisions: %w", err)
	}

	return decisions, nil
}

func (store *Store) attachDecisionLinks(
	ctx context.Context,
	decisions []DecisionRecord,
) ([]DecisionRecord, error) {
	for index := range decisions {
		links, err := store.decisionLinks(ctx, decisions[index].ID)
		if err != nil {
			return nil, err
		}

		decisions[index].Links = links
	}

	return decisions, nil
}

func (store *Store) decisionLinks(
	ctx context.Context,
	decisionID string,
) ([]DecisionLink, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT decision_id, ordinal, path, symbol_path, link_kind
		FROM decision_links
		WHERE decision_id = ?
		ORDER BY ordinal`,
		decisionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query decision links: %w", err)
	}
	defer rows.Close()

	links := []DecisionLink{}

	for rows.Next() {
		var link DecisionLink

		err = rows.Scan(
			&link.DecisionID,
			&link.Ordinal,
			&link.Path,
			&link.SymbolPath,
			&link.Kind,
		)
		if err != nil {
			return nil, fmt.Errorf("scan decision link: %w", err)
		}

		links = append(links, link)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate decision links: %w", err)
	}

	return links, nil
}

func (store *Store) ungovernedDecisionHotspots(
	ctx context.Context,
	query DecisionQuery,
) ([]DecisionUngoverned, error) {
	limit := boundedDecisionLimit(query.Limit)
	path := normalizeDecisionPathFilter(query.Path)

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT file.path
		FROM code_files file
		LEFT JOIN decision_links link ON link.path = file.path
		WHERE COALESCE(file.deleted_at_utc, '') = ''
			AND link.decision_id IS NULL
			AND (? = '' OR file.path = ? OR file.path LIKE ?)
		ORDER BY file.line_count DESC, file.path
		LIMIT ?`,
		path,
		path,
		strings.TrimSuffix(path, "/")+"/%",
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query ungoverned decision hotspots: %w", err)
	}
	defer rows.Close()

	results := []DecisionUngoverned{}

	for rows.Next() {
		var path string

		scanErr := rows.Scan(&path)
		if scanErr != nil {
			return nil, fmt.Errorf("scan ungoverned decision hotspot: %w", scanErr)
		}

		results = append(results, DecisionUngoverned{
			Path:   path,
			Reason: "indexed file has no linked decision",
		})
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate ungoverned decision hotspots: %w", err)
	}

	return results, nil
}

func staleDecisions(decisions []DecisionRecord, now time.Time) []DecisionRecord {
	stale := []DecisionRecord{}

	for _, decision := range decisions {
		updated, err := time.Parse(time.RFC3339, decision.UpdatedAtUTC)
		if err == nil && now.Sub(updated) > decisionStaleDays*24*time.Hour {
			stale = append(stale, decision)
		}
	}

	return stale
}

func decisionConflicts(decisions []DecisionRecord) []DecisionConflict {
	byPath := map[string]map[string][]string{}

	for _, decision := range decisions {
		for _, link := range decision.Links {
			if byPath[link.Path] == nil {
				byPath[link.Path] = map[string][]string{}
			}

			byPath[link.Path][decision.Status] = append(
				byPath[link.Path][decision.Status],
				decision.ID,
			)
		}
	}

	conflicts := []DecisionConflict{}

	for path, statuses := range byPath {
		if len(statuses) < decisionConflictMinimum {
			continue
		}

		names := make([]string, 0, len(statuses))
		ids := []string{}

		for status, statusIDs := range statuses {
			names = append(names, status)
			ids = append(ids, statusIDs...)
		}

		slices.Sort(names)
		slices.Sort(ids)
		conflicts = append(
			conflicts,
			DecisionConflict{Path: path, Statuses: names, DecisionIDs: ids},
		)
	}

	slices.SortFunc(conflicts, func(left, right DecisionConflict) int {
		return strings.Compare(left.Path, right.Path)
	})

	return conflicts
}

func decisionOverlaps(decisions []DecisionRecord) []DecisionOverlap {
	byPath := map[string]map[string]struct{}{}

	for _, decision := range decisions {
		for _, link := range decision.Links {
			if byPath[link.Path] == nil {
				byPath[link.Path] = map[string]struct{}{}
			}

			byPath[link.Path][decision.ID] = struct{}{}
		}
	}

	overlaps := []DecisionOverlap{}

	for path, idSet := range byPath {
		if len(idSet) < decisionConflictMinimum {
			continue
		}

		ids := make([]string, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}

		slices.Sort(ids)
		overlaps = append(overlaps, DecisionOverlap{Path: path, DecisionIDs: ids})
	}

	slices.SortFunc(overlaps, func(left, right DecisionOverlap) int {
		return strings.Compare(left.Path, right.Path)
	})

	return overlaps
}

func decisionSearchText(decision DecisionRecord) string {
	parts := make(
		[]string,
		0,
		decisionSearchBaseParts+decisionSearchLinkParts*len(decision.Links),
	)

	parts = append(parts,
		decision.Title,
		decision.Status,
		decision.Rationale,
		decision.Alternatives,
		decision.SourceKind,
		decision.SourcePath,
	)
	for _, link := range decision.Links {
		parts = append(parts, link.Path, link.SymbolPath, link.Kind)
	}

	return strings.Join(compactStrings(parts), "\n")
}

func decisionMarkerInExample(line string) bool {
	trimmed := strings.TrimSpace(line)

	return strings.HasPrefix(trimmed, "`") ||
		strings.HasPrefix(trimmed, ">") ||
		strings.HasPrefix(trimmed, "- `") ||
		strings.HasPrefix(trimmed, "* `")
}

func firstDecisionSentence(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexAny(text, ".;"); index != -1 {
		return strings.TrimSpace(text[:index])
	}

	return text
}

func normalizeDecisionPathFilter(path string) string {
	path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if path == "." {
		return ""
	}

	return path
}

func boundedDecisionLimit(limit int) int {
	if limit <= 0 {
		return decisionDefaultLimit
	}

	if limit > decisionMaxLimit {
		return decisionMaxLimit
	}

	return limit
}
