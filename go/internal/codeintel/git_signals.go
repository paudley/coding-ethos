// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:wsl_v5 // Query and scan code keeps tightly related statements together.
package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

const (
	defaultGitSignalAuthorLimit    = 3
	defaultGitSignalCoChangeLimit  = 5
	defaultGitSignalCommitLimit    = 500
	defaultGitSignalReviewerLimit  = 5
	gitHotspotChurnWeight          = 5
	gitHotspotCommitWeight         = 10
	gitHotspotMultiAuthorWeight    = 3
	gitReviewerAuthorshipLimit     = 10
	gitReviewerRecentDays          = 30
	gitReviewerRecentScore         = 20
	gitReviewerStaleDays           = 180
	gitReviewerStaleScore          = 5
	gitReviewerWarmDays            = 90
	gitReviewerWarmScore           = 10
	gitSignalCommitPrefix          = "\x1e"
	gitSignalFieldSeparator        = "\x1f"
	gitSignalHeaderFieldCount      = 4
	gitSignalHoursPerDay           = 24
	gitSignalOwnershipPercentScale = 100
	gitSignalScoreScale            = 10
)

type GitSignalRefreshOptions struct {
	Now         time.Time
	CommitLimit int
	Force       bool
}

type GitSignalSummary struct {
	HeadCommit   string `json:"head_commit,omitempty"`
	IndexedAtUTC string `json:"indexed_at_utc"`
	Stale        bool   `json:"stale"`
	Refreshed    bool   `json:"refreshed"`
	Commits      int    `json:"commits"`
	Files        int    `json:"files"`
	CoChanges    int    `json:"co_changes"`
}

type GitSignalQuery struct {
	Path  string
	Limit int
}

type GitFileSignal struct {
	Path                 string          `json:"path"`
	PrimaryAuthorName    string          `json:"primary_author_name,omitempty"`
	PrimaryAuthorEmail   string          `json:"primary_author_email,omitempty"`
	FirstSeenUTC         string          `json:"first_seen_utc,omitempty"`
	LastSeenUTC          string          `json:"last_seen_utc,omitempty"`
	TopAuthors           []GitFileAuthor `json:"top_authors,omitempty"`
	CoChanges            []GitCoChange   `json:"co_changes,omitempty"`
	CommitCount          int             `json:"commit_count"`
	Churn                int             `json:"churn"`
	Additions            int             `json:"additions"`
	Deletions            int             `json:"deletions"`
	AuthorCount          int             `json:"author_count"`
	PrimaryAuthorCommits int             `json:"primary_author_commits"`
	HotspotScore         float64         `json:"hotspot_score"`
}

type GitFileAuthor struct {
	Name                string  `json:"name,omitempty"`
	Email               string  `json:"email"`
	LastSeenUTC         string  `json:"last_seen_utc,omitempty"`
	Commits             int     `json:"commits"`
	Additions           int     `json:"additions"`
	Deletions           int     `json:"deletions"`
	OwnershipPercentage float64 `json:"ownership_percentage"`
}

type GitCoChange struct {
	Path           string `json:"path"`
	RelatedPath    string `json:"related_path"`
	LastSeenUTC    string `json:"last_seen_utc,omitempty"`
	Count          int    `json:"count"`
	HiddenCoupling bool   `json:"hidden_coupling"`
}

type GitReviewerSuggestionQuery struct {
	Paths []string
	Limit int
}

type GitReviewerSuggestion struct {
	Name              string   `json:"name,omitempty"`
	Email             string   `json:"email"`
	RecentTouchUTC    string   `json:"recent_touch_utc,omitempty"`
	ScoreExplanation  []string `json:"score_explanation"`
	RepresentativeFor []string `json:"representative_for,omitempty"`
	Score             float64  `json:"score"`
	AuthoredFiles     int      `json:"authored_files"`
	CoChangedFiles    int      `json:"co_changed_files"`
}

type gitCommitChange struct {
	Path      string
	Additions int
	Deletions int
}

type gitCommitSignal struct {
	Hash        string
	AuthorName  string
	AuthorEmail string
	WhenUTC     string
	Changes     []gitCommitChange
}

type gitFileAccumulator struct {
	authors   map[string]*gitAuthorAccumulator
	commits   map[string]bool
	path      string
	firstSeen string
	lastSeen  string
	additions int
	deletions int
}

type gitAuthorAccumulator struct {
	name      string
	email     string
	lastSeen  string
	commits   int
	additions int
	deletions int
}

type gitCoChangeAccumulator struct {
	path        string
	relatedPath string
	lastSeen    string
	count       int
}

type gitReviewerAccumulator struct {
	name              string
	email             string
	representativeFor map[string]bool
	authoredFiles     map[string]bool
	coChangedFiles    map[string]bool
	recentTouchUTC    string
	authorshipScore   float64
	cochangeScore     float64
}

func (store *Store) RefreshGitSignals(
	ctx context.Context,
	root string,
	options GitSignalRefreshOptions,
) (GitSignalSummary, error) {
	headCommit, err := currentGitSignalHead(ctx, root)
	if err != nil {
		return GitSignalSummary{}, err
	}

	if !options.Force {
		current, metadataErr := store.gitSignalSummaryForHead(ctx, headCommit)
		if metadataErr != nil {
			return GitSignalSummary{}, metadataErr
		}
		if current.HeadCommit == headCommit && !current.Stale {
			return current, nil
		}
	}

	commits, err := loadGitSignalCommits(ctx, root, options.CommitLimit)
	if err != nil {
		return GitSignalSummary{}, err
	}

	files, cochanges := buildGitSignalAggregates(commits)
	err = store.replaceGitSignals(ctx, files, cochanges, headCommit, options.Now)
	if err != nil {
		return GitSignalSummary{}, err
	}

	return GitSignalSummary{
		HeadCommit:   headCommit,
		IndexedAtUTC: formatGitSignalTime(options.Now),
		Refreshed:    true,
		Commits:      len(commits),
		Files:        len(files),
		CoChanges:    len(cochanges),
	}, nil
}

func (store *Store) GitSignalSummary(
	ctx context.Context,
	root string,
) (GitSignalSummary, error) {
	headCommit, err := currentGitSignalHead(ctx, root)
	if err != nil {
		return GitSignalSummary{}, err
	}

	return store.gitSignalSummaryForHead(ctx, headCommit)
}

func (store *Store) GitSignals(
	ctx context.Context,
	query GitSignalQuery,
) ([]GitFileSignal, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path, commit_count, churn, additions, deletions, author_count,
			COALESCE(primary_author_name, ''), COALESCE(primary_author_email, ''),
			primary_author_commits, COALESCE(first_seen_utc, ''),
			COALESCE(last_seen_utc, ''), hotspot_score
		FROM git_file_signals
		WHERE (? = '' OR path = ?)
		ORDER BY hotspot_score DESC, churn DESC, commit_count DESC, path
		LIMIT ?`,
		query.Path,
		query.Path,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query git file signals: %w", err)
	}
	defer rows.Close()

	signals, err := scanGitFileSignals(rows)
	if err != nil {
		return nil, err
	}

	for index := range signals {
		authors, authorErr := store.GitSignalAuthors(
			ctx,
			signals[index].Path,
			defaultGitSignalAuthorLimit,
		)
		if authorErr != nil {
			return nil, authorErr
		}
		cochanges, cochangeErr := store.GitCoChanges(
			ctx,
			signals[index].Path,
			defaultGitSignalCoChangeLimit,
		)
		if cochangeErr != nil {
			return nil, cochangeErr
		}
		signals[index].TopAuthors = authors
		signals[index].CoChanges = cochanges
	}

	return signals, nil
}

func (store *Store) GitSignalAuthors(
	ctx context.Context,
	path string,
	limit int,
) ([]GitFileAuthor, error) {
	if limit <= 0 {
		limit = defaultGitSignalAuthorLimit
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT author.author_name, author.author_email, author.commit_count,
			author.additions, author.deletions, COALESCE(author.last_seen_utc, ''),
			CASE
				WHEN file.commit_count = 0 THEN 0
				ELSE ROUND((author.commit_count * 100.0 / file.commit_count) * 10) / 10
			END AS ownership_percentage
		FROM git_file_authors author
		JOIN git_file_signals file ON file.path = author.path
		WHERE author.path = ?
		ORDER BY author.commit_count DESC, author.additions + author.deletions DESC,
			author.last_seen_utc DESC
		LIMIT ?`,
		path,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query git signal authors: %w", err)
	}
	defer rows.Close()

	return scanGitFileAuthors(rows)
}

func (store *Store) GitReviewerSuggestions(
	ctx context.Context,
	query GitReviewerSuggestionQuery,
) ([]GitReviewerSuggestion, error) {
	paths := normalizedGitSignalQueryPaths(query.Paths)
	if len(paths) == 0 {
		return nil, nil
	}

	limit := query.Limit
	if limit <= 0 {
		limit = defaultGitSignalReviewerLimit
	}

	reviewers := map[string]*gitReviewerAccumulator{}
	for _, path := range paths {
		err := store.addDirectReviewerSignals(ctx, reviewers, path)
		if err != nil {
			return nil, err
		}

		err = store.addCoChangeReviewerSignals(ctx, reviewers, path)
		if err != nil {
			return nil, err
		}
	}

	suggestions := sortedGitReviewerSuggestions(reviewers)
	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}

	return suggestions, nil
}

func (store *Store) addDirectReviewerSignals(
	ctx context.Context,
	reviewers map[string]*gitReviewerAccumulator,
	path string,
) error {
	authors, err := store.GitSignalAuthors(ctx, path, gitReviewerAuthorshipLimit)
	if err != nil {
		return err
	}

	for _, author := range authors {
		reviewer := gitReviewer(reviewers, author.Email, author.Name)
		reviewer.representativeFor[path] = true
		reviewer.authoredFiles[path] = true
		reviewer.authorshipScore += author.OwnershipPercentage
		reviewer.recentTouchUTC = maxNonEmptyTime(reviewer.recentTouchUTC, author.LastSeenUTC)
	}

	return nil
}

func (store *Store) addCoChangeReviewerSignals(
	ctx context.Context,
	reviewers map[string]*gitReviewerAccumulator,
	path string,
) error {
	cochanges, err := store.GitCoChanges(ctx, path, defaultGitSignalCoChangeLimit)
	if err != nil {
		return err
	}

	for _, cochange := range cochanges {
		authors, authorErr := store.GitSignalAuthors(
			ctx,
			cochange.RelatedPath,
			defaultGitSignalAuthorLimit,
		)
		if authorErr != nil {
			return authorErr
		}
		for _, author := range authors {
			reviewer := gitReviewer(reviewers, author.Email, author.Name)
			reviewer.representativeFor[path] = true
			reviewer.coChangedFiles[cochange.RelatedPath] = true
			reviewer.cochangeScore += float64(cochange.Count) *
				author.OwnershipPercentage / gitSignalOwnershipPercentScale
			reviewer.recentTouchUTC = maxNonEmptyTime(
				reviewer.recentTouchUTC,
				author.LastSeenUTC,
			)
		}
	}

	return nil
}

func (store *Store) GitCoChanges(
	ctx context.Context,
	path string,
	limit int,
) ([]GitCoChange, error) {
	if limit <= 0 {
		limit = defaultGitSignalCoChangeLimit
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path, related_path, cochange_count, COALESCE(last_seen_utc, ''),
			hidden_coupling
		FROM git_cochanges
		WHERE path = ?
		ORDER BY cochange_count DESC, hidden_coupling DESC, related_path
		LIMIT ?`,
		path,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query git co-changes: %w", err)
	}
	defer rows.Close()

	return scanGitCoChanges(rows)
}

func (store *Store) replaceGitSignals(
	ctx context.Context,
	files map[string]*gitFileAccumulator,
	cochanges map[string]*gitCoChangeAccumulator,
	headCommit string,
	now time.Time,
) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin git signal refresh: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	for _, statement := range []string{
		"DELETE FROM git_signal_metadata",
		"DELETE FROM git_cochanges",
		"DELETE FROM git_file_authors",
		"DELETE FROM git_file_signals",
	} {
		_, execErr := transaction.ExecContext(ctx, statement)
		if execErr != nil {
			return fmt.Errorf("clear git signals: %w", execErr)
		}
	}

	err = insertGitSignalFiles(ctx, transaction, files)
	if err != nil {
		return err
	}

	err = insertGitSignalCoChanges(ctx, transaction, cochanges)
	if err != nil {
		return err
	}

	for key, value := range map[string]string{
		"head_commit":    headCommit,
		"indexed_at_utc": formatGitSignalTime(now),
	} {
		_, execErr := transaction.ExecContext(
			ctx,
			"INSERT INTO git_signal_metadata(key, value) VALUES(?, ?)",
			key,
			value,
		)
		if execErr != nil {
			return fmt.Errorf("insert git signal metadata: %w", execErr)
		}
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit git signal refresh: %w", err)
	}

	return nil
}

func insertGitSignalFiles(
	ctx context.Context,
	transaction *sql.Tx,
	files map[string]*gitFileAccumulator,
) error {
	paths := sortedGitSignalKeys(files)
	for _, path := range paths {
		file := files[path]
		authors := sortedAuthorAccumulators(file.authors)
		primary := gitAuthorAccumulator{}
		if len(authors) > 0 {
			primary = *authors[0]
		}

		commitCount := len(file.commits)
		churn := file.additions + file.deletions
		hotspotScore := gitHotspotScore(commitCount, churn, len(file.authors))

		_, err := transaction.ExecContext(
			ctx,
			`INSERT INTO git_file_signals(
				path, commit_count, churn, additions, deletions, author_count,
				primary_author_name, primary_author_email,
				primary_author_commits, first_seen_utc, last_seen_utc,
				hotspot_score
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			file.path,
			commitCount,
			churn,
			file.additions,
			file.deletions,
			len(file.authors),
			primary.name,
			primary.email,
			primary.commits,
			file.firstSeen,
			file.lastSeen,
			hotspotScore,
		)
		if err != nil {
			return fmt.Errorf("insert git file signal %q: %w", file.path, err)
		}

		for _, author := range authors {
			_, err = transaction.ExecContext(
				ctx,
				`INSERT INTO git_file_authors(
					path, author_email, author_name, commit_count,
					additions, deletions, last_seen_utc
				) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				file.path,
				author.email,
				author.name,
				author.commits,
				author.additions,
				author.deletions,
				author.lastSeen,
			)
			if err != nil {
				return fmt.Errorf("insert git file author %q: %w", file.path, err)
			}
		}
	}

	return nil
}

func insertGitSignalCoChanges(
	ctx context.Context,
	transaction *sql.Tx,
	cochanges map[string]*gitCoChangeAccumulator,
) error {
	keys := sortedGitCoChangeKeys(cochanges)
	for _, key := range keys {
		cochange := cochanges[key]
		hidden, err := gitCoChangeHiddenCoupling(
			ctx,
			transaction,
			cochange.path,
			cochange.relatedPath,
		)
		if err != nil {
			return err
		}

		_, err = transaction.ExecContext(
			ctx,
			`INSERT INTO git_cochanges(
				path, related_path, cochange_count, last_seen_utc, hidden_coupling
			) VALUES (?, ?, ?, ?, ?)`,
			cochange.path,
			cochange.relatedPath,
			cochange.count,
			cochange.lastSeen,
			boolToInt(hidden),
		)
		if err != nil {
			return fmt.Errorf("insert git co-change %q: %w", key, err)
		}
	}

	return nil
}

func (store *Store) gitSignalSummaryForHead(
	ctx context.Context,
	headCommit string,
) (GitSignalSummary, error) {
	metadata, err := store.gitSignalMetadata(ctx)
	if err != nil {
		return GitSignalSummary{}, err
	}

	var fileCount int
	err = store.database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM git_file_signals",
	).Scan(&fileCount)
	if err != nil {
		return GitSignalSummary{}, fmt.Errorf("count git file signals: %w", err)
	}

	var cochangeCount int
	err = store.database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM git_cochanges",
	).Scan(&cochangeCount)
	if err != nil {
		return GitSignalSummary{}, fmt.Errorf("count git co-change signals: %w", err)
	}

	indexedHead := metadata["head_commit"]

	return GitSignalSummary{
		HeadCommit:   indexedHead,
		IndexedAtUTC: metadata["indexed_at_utc"],
		Stale:        indexedHead == "" || indexedHead != headCommit,
		Files:        fileCount,
		CoChanges:    cochangeCount,
	}, nil
}

func (store *Store) gitSignalMetadata(ctx context.Context) (map[string]string, error) {
	rows, err := store.database.QueryContext(
		ctx,
		"SELECT key, value FROM git_signal_metadata",
	)
	if err != nil {
		return nil, fmt.Errorf("query git signal metadata: %w", err)
	}
	defer rows.Close()

	metadata := map[string]string{}
	for rows.Next() {
		var key, value string
		scanErr := rows.Scan(&key, &value)
		if scanErr != nil {
			return nil, fmt.Errorf("scan git signal metadata: %w", scanErr)
		}

		metadata[key] = value
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate git signal metadata: %w", rowsErr)
	}

	return metadata, nil
}

func gitCoChangeHiddenCoupling(
	ctx context.Context,
	transaction *sql.Tx,
	path string,
	relatedPath string,
) (bool, error) {
	var count int
	err := transaction.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		FROM code_edges
		WHERE (path = ? AND target_path = ?)
			OR (path = ? AND target_path = ?)`,
		path,
		relatedPath,
		relatedPath,
		path,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query static edge for git co-change: %w", err)
	}

	return count == 0, nil
}

func gitReviewer(
	reviewers map[string]*gitReviewerAccumulator,
	email string,
	name string,
) *gitReviewerAccumulator {
	if email == "" {
		email = "unknown"
	}

	reviewer := reviewers[email]
	if reviewer == nil {
		reviewer = &gitReviewerAccumulator{
			name:              name,
			email:             email,
			representativeFor: map[string]bool{},
			authoredFiles:     map[string]bool{},
			coChangedFiles:    map[string]bool{},
		}
		reviewers[email] = reviewer
	}
	if reviewer.name == "" {
		reviewer.name = name
	}

	return reviewer
}

func normalizedGitSignalQueryPaths(paths []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, path := range paths {
		normalized := normalizeGitSignalPath(path)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	slices.Sort(result)

	return result
}

func sortedGitReviewerSuggestions(
	reviewers map[string]*gitReviewerAccumulator,
) []GitReviewerSuggestion {
	suggestions := make([]GitReviewerSuggestion, 0, len(reviewers))
	latestTouchUTC := latestReviewerTouchUTC(reviewers)
	for _, reviewer := range reviewers {
		recencyScore := gitReviewerRecencyScore(reviewer.recentTouchUTC, latestTouchUTC)
		score := reviewer.authorshipScore + reviewer.cochangeScore +
			recencyScore
		suggestions = append(suggestions, GitReviewerSuggestion{
			Name:              reviewer.name,
			Email:             reviewer.email,
			Score:             roundGitSignalScore(score),
			AuthoredFiles:     len(reviewer.authoredFiles),
			CoChangedFiles:    len(reviewer.coChangedFiles),
			RecentTouchUTC:    reviewer.recentTouchUTC,
			ScoreExplanation:  gitReviewerScoreExplanation(reviewer, recencyScore),
			RepresentativeFor: sortedBoolMapKeys(reviewer.representativeFor),
		})
	}

	slices.SortFunc(suggestions, func(left, right GitReviewerSuggestion) int {
		if left.Score != right.Score {
			if left.Score > right.Score {
				return -1
			}

			return 1
		}

		return strings.Compare(left.Email, right.Email)
	})

	return suggestions
}

func latestReviewerTouchUTC(reviewers map[string]*gitReviewerAccumulator) string {
	latest := ""
	for _, reviewer := range reviewers {
		latest = maxNonEmptyTime(latest, reviewer.recentTouchUTC)
	}

	return latest
}

func gitReviewerScoreExplanation(
	reviewer *gitReviewerAccumulator,
	recencyScore float64,
) []string {
	return []string{
		fmt.Sprintf("authorship=%.1f", reviewer.authorshipScore),
		fmt.Sprintf("cochange=%.1f", reviewer.cochangeScore),
		fmt.Sprintf("recency=%.1f", recencyScore),
	}
}

func gitReviewerRecencyScore(value, latest string) float64 {
	if value == "" || latest == "" {
		return 0
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0
	}
	latestParsed, err := time.Parse(time.RFC3339, latest)
	if err != nil {
		return 0
	}

	days := latestParsed.Sub(parsed).Hours() / gitSignalHoursPerDay
	switch {
	case days <= gitReviewerRecentDays:
		return gitReviewerRecentScore
	case days <= gitReviewerWarmDays:
		return gitReviewerWarmScore
	case days <= gitReviewerStaleDays:
		return gitReviewerStaleScore
	default:
		return 1
	}
}

func sortedBoolMapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

func scanGitFileSignals(rows *sql.Rows) ([]GitFileSignal, error) {
	signals := []GitFileSignal{}
	for rows.Next() {
		var signal GitFileSignal
		err := rows.Scan(
			&signal.Path,
			&signal.CommitCount,
			&signal.Churn,
			&signal.Additions,
			&signal.Deletions,
			&signal.AuthorCount,
			&signal.PrimaryAuthorName,
			&signal.PrimaryAuthorEmail,
			&signal.PrimaryAuthorCommits,
			&signal.FirstSeenUTC,
			&signal.LastSeenUTC,
			&signal.HotspotScore,
		)
		if err != nil {
			return nil, fmt.Errorf("scan git file signal: %w", err)
		}
		signals = append(signals, signal)
	}
	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate git file signals: %w", err)
	}

	return signals, nil
}

func scanGitFileAuthors(rows *sql.Rows) ([]GitFileAuthor, error) {
	authors := []GitFileAuthor{}
	for rows.Next() {
		var author GitFileAuthor
		err := rows.Scan(
			&author.Name,
			&author.Email,
			&author.Commits,
			&author.Additions,
			&author.Deletions,
			&author.LastSeenUTC,
			&author.OwnershipPercentage,
		)
		if err != nil {
			return nil, fmt.Errorf("scan git file author: %w", err)
		}
		authors = append(authors, author)
	}
	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate git file authors: %w", err)
	}

	return authors, nil
}

func scanGitCoChanges(rows *sql.Rows) ([]GitCoChange, error) {
	cochanges := []GitCoChange{}
	for rows.Next() {
		var cochange GitCoChange
		var hidden int
		err := rows.Scan(
			&cochange.Path,
			&cochange.RelatedPath,
			&cochange.Count,
			&cochange.LastSeenUTC,
			&hidden,
		)
		if err != nil {
			return nil, fmt.Errorf("scan git co-change: %w", err)
		}
		cochange.HiddenCoupling = hidden != 0
		cochanges = append(cochanges, cochange)
	}
	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate git co-changes: %w", err)
	}

	return cochanges, nil
}

func gitHotspotScore(commitCount, churn, authorCount int) float64 {
	if commitCount == 0 && churn == 0 {
		return 0
	}

	score := float64(commitCount*gitHotspotCommitWeight) +
		math.Log1p(float64(churn))*gitHotspotChurnWeight
	if authorCount > 1 {
		score += float64(authorCount-1) * gitHotspotMultiAuthorWeight
	}

	return roundGitSignalScore(score)
}

func roundGitSignalScore(score float64) float64 {
	return math.Round(score*gitSignalScoreScale) / gitSignalScoreScale
}

func minNonEmptyTime(left, right string) string {
	if left == "" {
		return right
	}
	if right == "" || left < right {
		return left
	}

	return right
}

func maxNonEmptyTime(left, right string) string {
	if left == "" || right > left {
		return right
	}

	return left
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func formatGitSignalTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now()
	}

	return value.UTC().Format(time.RFC3339)
}
