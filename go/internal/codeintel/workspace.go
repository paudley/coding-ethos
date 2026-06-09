// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	workspaceDirName        = ".coding-ethos-workspace"
	workspaceConfigFileName = "workspace.json"
	workspaceSchemaVersion  = 1
	workspaceDirMode        = 0o700
	workspaceFileMode       = 0o600
	workspaceGitLogLimit    = 200
	workspaceCoChangeWindow = 2 * time.Minute
	workspaceGitHeaderParts = 4
	workspaceContractLimit  = 200
)

var (
	errWorkspaceRepoRequired = apperror.StaticError(
		"workspace repository path is required",
	)
	errWorkspaceAliasInvalid = apperror.StaticError(
		"workspace repository alias is invalid",
	)
	errWorkspaceAliasExists = apperror.StaticError(
		"workspace repository alias already exists",
	)
	errWorkspaceAliasMissing = apperror.StaticError(
		"workspace repository alias is not registered",
	)
	errWorkspaceNotFound = apperror.StaticError(
		"workspace registry does not exist",
	)
	errWorkspaceSchema = apperror.StaticError(
		"unsupported workspace registry schema version",
	)
	errWorkspaceRepoNotGit = apperror.StaticError(
		"repository is not a git worktree",
	)
	workspaceAliasPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// WorkspaceRegistry records related repositories without merging their
// repo-local code-intel stores.
type WorkspaceRegistry struct {
	UpdatedAtUTC  string          `json:"updated_at_utc,omitempty"`
	WorkspaceRoot string          `json:"workspace_root"`
	Repos         []WorkspaceRepo `json:"repos"`
	SchemaVersion int             `json:"schema_version"`
}

// WorkspaceRepo is a registered repository in a workspace.
type WorkspaceRepo struct {
	Alias            string `json:"alias"`
	Path             string `json:"path"`
	CodeIntelDB      string `json:"code_intel_db"`
	Head             string `json:"head,omitempty"`
	LastIndexedAtUTC string `json:"last_indexed_at_utc,omitempty"`
	StaleWarning     string `json:"stale_warning,omitempty"`
	StoreAvailable   bool   `json:"store_available"`
}

// WorkspaceStatus summarizes registry state and derived cross-repo evidence.
type WorkspaceStatus struct {
	UpdatedAtUTC string                    `json:"updated_at_utc,omitempty"`
	Root         string                    `json:"root"`
	Repos        []WorkspaceRepoStatus     `json:"repos"`
	CoChanges    []WorkspaceCoChange       `json:"cochanges,omitempty"`
	Contracts    []WorkspaceContract       `json:"contracts,omitempty"`
	Warnings     []string                  `json:"warnings,omitempty"`
	Stats        WorkspaceStatusStatistics `json:"stats"`
}

// WorkspaceRepoStatus reports per-repo freshness for workspace queries.
type WorkspaceRepoStatus struct {
	Alias            string `json:"alias"`
	Path             string `json:"path"`
	CodeIntelDB      string `json:"code_intel_db"`
	Head             string `json:"head,omitempty"`
	RecordedHead     string `json:"recorded_head,omitempty"`
	LastIndexedAtUTC string `json:"last_indexed_at_utc,omitempty"`
	StaleWarning     string `json:"stale_warning,omitempty"`
	StoreAvailable   bool   `json:"store_available"`
	Stale            bool   `json:"stale"`
}

// WorkspaceCoChange is a conservative cross-repo co-change candidate.
//
//nolint:govet // Keeps related public JSON evidence fields grouped for review output.
type WorkspaceCoChange struct {
	LeftPaths   []string `json:"left_paths"`
	RightPaths  []string `json:"right_paths"`
	LeftRepo    string   `json:"left_repo"`
	LeftCommit  string   `json:"left_commit"`
	RightRepo   string   `json:"right_repo"`
	RightCommit string   `json:"right_commit"`
	AuthorEmail string   `json:"author_email,omitempty"`
	MatchReason string   `json:"match_reason"`
	Confidence  string   `json:"confidence"`
}

// WorkspaceContract records a conservative cross-repo relationship.
type WorkspaceContract struct {
	ProviderRepo string `json:"provider_repo"`
	ConsumerRepo string `json:"consumer_repo"`
	Kind         string `json:"kind"`
	Path         string `json:"path,omitempty"`
	TargetPath   string `json:"target_path,omitempty"`
	Evidence     string `json:"evidence,omitempty"`
}

// WorkspaceStatusStatistics counts workspace registry and evidence rows.
type WorkspaceStatusStatistics struct {
	Repos        int `json:"repos"`
	Available    int `json:"available"`
	Stale        int `json:"stale"`
	CoChanges    int `json:"cochanges"`
	Contracts    int `json:"contracts"`
	WarningCount int `json:"warning_count"`
}

//nolint:govet // Mirrors parsed git-log record order; this is not allocation-sensitive.
type workspaceGitCommit struct {
	Paths       []string
	AuthorTime  time.Time
	RepoAlias   string
	Hash        string
	AuthorEmail string
	Subject     string
}

// DefaultWorkspaceDir returns the managed workspace state directory.
func DefaultWorkspaceDir(root string) string {
	return filepath.Join(root, workspaceDirName)
}

// DefaultWorkspaceConfigPath returns the workspace registry JSON path.
func DefaultWorkspaceConfigPath(root string) string {
	return filepath.Join(DefaultWorkspaceDir(root), workspaceConfigFileName)
}

// LoadWorkspaceRegistry reads the workspace registry.
func LoadWorkspaceRegistry(root string) (WorkspaceRegistry, error) {
	path := DefaultWorkspaceConfigPath(root)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WorkspaceRegistry{}, fmt.Errorf("%w: %s", errWorkspaceNotFound, path)
		}

		return WorkspaceRegistry{}, fmt.Errorf("read workspace registry: %w", err)
	}

	var registry WorkspaceRegistry

	err = json.Unmarshal(data, &registry)
	if err != nil {
		return WorkspaceRegistry{}, fmt.Errorf("parse workspace registry: %w", err)
	}

	return normalizeWorkspaceRegistry(root, registry)
}

// SaveWorkspaceRegistry writes the workspace registry.
func SaveWorkspaceRegistry(root string, registry WorkspaceRegistry) error {
	normalized, err := normalizeWorkspaceRegistry(root, registry)
	if err != nil {
		return err
	}

	normalized.UpdatedAtUTC = time.Now().UTC().Format(time.RFC3339)

	err = os.MkdirAll(DefaultWorkspaceDir(root), workspaceDirMode)
	if err != nil {
		return fmt.Errorf("create workspace state directory: %w", err)
	}

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace registry: %w", err)
	}

	data = append(data, '\n')

	err = os.WriteFile(
		DefaultWorkspaceConfigPath(root),
		data,
		workspaceFileMode,
	)
	if err != nil {
		return fmt.Errorf("write workspace registry: %w", err)
	}

	return nil
}

// AddWorkspaceRepo adds or replaces no existing repo alias.
func AddWorkspaceRepo(root, alias, repoPath string) (WorkspaceRegistry, error) {
	if strings.TrimSpace(repoPath) == "" {
		return WorkspaceRegistry{}, errWorkspaceRepoRequired
	}

	registry, err := loadOrCreateWorkspaceRegistry(root)
	if err != nil {
		return WorkspaceRegistry{}, err
	}

	repo, err := newWorkspaceRepo(root, alias, repoPath)
	if err != nil {
		return WorkspaceRegistry{}, err
	}

	for _, existing := range registry.Repos {
		if existing.Alias == repo.Alias {
			return WorkspaceRegistry{}, fmt.Errorf("%w: %s", errWorkspaceAliasExists, repo.Alias)
		}
	}

	registry.Repos = append(registry.Repos, repo)
	sortWorkspaceRepos(registry.Repos)

	err = SaveWorkspaceRegistry(root, registry)
	if err != nil {
		return WorkspaceRegistry{}, err
	}

	return LoadWorkspaceRegistry(root)
}

// RemoveWorkspaceRepo removes a registered repo alias.
func RemoveWorkspaceRepo(root, alias string) (WorkspaceRegistry, error) {
	registry, err := LoadWorkspaceRegistry(root)
	if err != nil {
		return WorkspaceRegistry{}, err
	}

	alias = strings.TrimSpace(alias)
	next := registry.Repos[:0]
	removed := false

	for _, repo := range registry.Repos {
		if repo.Alias == alias {
			removed = true

			continue
		}

		next = append(next, repo)
	}

	if !removed {
		return WorkspaceRegistry{}, fmt.Errorf("%w: %s", errWorkspaceAliasMissing, alias)
	}

	registry.Repos = next

	err = SaveWorkspaceRegistry(root, registry)
	if err != nil {
		return WorkspaceRegistry{}, err
	}

	return LoadWorkspaceRegistry(root)
}

// ScanWorkspaceRepos discovers git repositories under root and writes aliases
// only when they are unique.
func ScanWorkspaceRepos(root string) (WorkspaceRegistry, []string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return WorkspaceRegistry{}, nil, fmt.Errorf("read workspace root: %w", err)
	}

	registry, err := loadOrCreateWorkspaceRegistry(root)
	if err != nil {
		return WorkspaceRegistry{}, nil, err
	}

	seenAliases := map[string]bool{}
	seenPaths := map[string]bool{}

	for _, repo := range registry.Repos {
		seenAliases[repo.Alias] = true
		seenPaths[repo.Path] = true
	}

	warnings := []string{}

	for _, entry := range entries {
		repo, warning, registered, scanErr := scanWorkspaceEntry(
			root,
			entry,
			seenAliases,
			seenPaths,
		)
		if scanErr != nil {
			return WorkspaceRegistry{}, nil, scanErr
		}

		if warning != "" {
			warnings = append(warnings, warning)
		}

		if !registered {
			continue
		}

		registry.Repos = append(registry.Repos, repo)
		seenAliases[repo.Alias] = true
		seenPaths[repo.Path] = true
	}

	sortWorkspaceRepos(registry.Repos)

	err = SaveWorkspaceRegistry(root, registry)
	if err != nil {
		return WorkspaceRegistry{}, nil, err
	}

	updated, err := LoadWorkspaceRegistry(root)
	if err != nil {
		return WorkspaceRegistry{}, nil, err
	}

	return updated, warnings, nil
}

func scanWorkspaceEntry(
	root string,
	entry os.DirEntry,
	seenAliases map[string]bool,
	seenPaths map[string]bool,
) (WorkspaceRepo, string, bool, error) {
	if !entry.IsDir() || workspaceSkipDir(entry.Name()) {
		return WorkspaceRepo{}, "", false, nil
	}

	path := filepath.Join(root, entry.Name())
	if !isGitRepo(path) {
		return WorkspaceRepo{}, "", false, nil
	}

	canonical, err := filepath.Abs(path)
	if err != nil {
		return WorkspaceRepo{}, "", false, fmt.Errorf("canonicalize discovered repo: %w", err)
	}

	if seenPaths[canonical] {
		return WorkspaceRepo{}, "", false, nil
	}

	alias := sanitizeWorkspaceAlias(entry.Name())
	if alias == "" || seenAliases[alias] {
		return WorkspaceRepo{}, "skipped " + canonical + ": alias collision", false, nil
	}

	repo, repoErr := newWorkspaceRepo(root, alias, canonical)
	if repoErr == nil {
		return repo, "", true, nil
	}

	return WorkspaceRepo{}, "skipped " + canonical + ": " + repoErr.Error(), false, nil
}

// RefreshWorkspaceStatus reads current git/store state without changing
// per-repo code-intel stores.
func RefreshWorkspaceStatus(ctx context.Context, root string) (WorkspaceStatus, error) {
	registry, err := LoadWorkspaceRegistry(root)
	if err != nil {
		return WorkspaceStatus{}, err
	}

	status, err := workspaceStatus(ctx, registry)
	if err != nil {
		return WorkspaceStatus{}, err
	}

	for index, repoStatus := range status.Repos {
		registry.Repos[index].Head = repoStatus.Head
		registry.Repos[index].LastIndexedAtUTC = repoStatus.LastIndexedAtUTC
		registry.Repos[index].StaleWarning = repoStatus.StaleWarning
		registry.Repos[index].StoreAvailable = repoStatus.StoreAvailable
	}

	err = SaveWorkspaceRegistry(root, registry)
	if err != nil {
		return WorkspaceStatus{}, err
	}

	return workspaceStatus(ctx, registry)
}

// WorkspaceStatusForRegistry reports current workspace state.
func WorkspaceStatusForRegistry(
	ctx context.Context,
	registry WorkspaceRegistry,
) (WorkspaceStatus, error) {
	return workspaceStatus(ctx, registry)
}

// WorkspaceRepoByAlias resolves a repo alias from the registry.
func WorkspaceRepoByAlias(
	registry WorkspaceRegistry,
	alias string,
) (WorkspaceRepo, bool) {
	alias = strings.TrimSpace(alias)
	for _, repo := range registry.Repos {
		if repo.Alias == alias {
			return repo, true
		}
	}

	return WorkspaceRepo{}, false
}

func workspaceStatus(
	ctx context.Context,
	registry WorkspaceRegistry,
) (WorkspaceStatus, error) {
	status := WorkspaceStatus{
		Root:         registry.WorkspaceRoot,
		UpdatedAtUTC: registry.UpdatedAtUTC,
		Repos:        []WorkspaceRepoStatus{},
		Warnings:     []string{},
	}

	commits := []workspaceGitCommit{}

	for _, repo := range registry.Repos {
		repoStatus, err := workspaceRepoStatus(ctx, repo)
		if err != nil {
			status.Warnings = append(status.Warnings, repo.Alias+": "+err.Error())
			repoStatus = WorkspaceRepoStatus{
				Alias:          repo.Alias,
				Path:           repo.Path,
				CodeIntelDB:    repo.CodeIntelDB,
				RecordedHead:   repo.Head,
				StaleWarning:   err.Error(),
				StoreAvailable: workspaceFileExists(repo.CodeIntelDB),
				Stale:          true,
			}
		}

		status.Repos = append(status.Repos, repoStatus)

		status.Stats.Repos++
		if repoStatus.StoreAvailable {
			status.Stats.Available++
		}

		if repoStatus.Stale {
			status.Stats.Stale++
		}

		repoCommits, err := workspaceGitCommits(ctx, repo)
		if err != nil {
			status.Warnings = append(status.Warnings, repo.Alias+" git history: "+err.Error())

			continue
		}

		commits = append(commits, repoCommits...)
	}

	status.CoChanges = workspaceCoChanges(commits)
	contracts, warnings := workspaceContracts(ctx, registry)
	status.Contracts = contracts
	status.Warnings = append(status.Warnings, warnings...)
	status.Stats.CoChanges = len(status.CoChanges)
	status.Stats.Contracts = len(status.Contracts)
	status.Stats.WarningCount = len(status.Warnings)

	return status, nil
}

func workspaceRepoStatus(
	ctx context.Context,
	repo WorkspaceRepo,
) (WorkspaceRepoStatus, error) {
	head, err := workspaceGitOutput(ctx, repo.Path, "rev-parse", "HEAD")
	if err != nil {
		return WorkspaceRepoStatus{}, fmt.Errorf("read HEAD: %w", err)
	}

	repoStatus := WorkspaceRepoStatus{
		Alias:          repo.Alias,
		Path:           repo.Path,
		CodeIntelDB:    repo.CodeIntelDB,
		Head:           head,
		RecordedHead:   repo.Head,
		StoreAvailable: workspaceFileExists(repo.CodeIntelDB),
	}

	if repo.Head != "" && repo.Head != head {
		repoStatus.Stale = true
		repoStatus.StaleWarning = "registered HEAD differs from current repo HEAD"
	}

	if repoStatus.StoreAvailable {
		return workspaceRepoStoreStatus(ctx, repo, repoStatus)
	}

	repoStatus.Stale = true
	repoStatus.StaleWarning = firstNonEmptyString(
		repoStatus.StaleWarning,
		"code-intel store is missing",
	)

	return repoStatus, nil
}

func workspaceRepoStoreStatus(
	ctx context.Context,
	repo WorkspaceRepo,
	repoStatus WorkspaceRepoStatus,
) (WorkspaceRepoStatus, error) {
	store, openErr := OpenReadOnly(ctx, repo.CodeIntelDB)
	if openErr == nil {
		return workspaceOpenRepoStoreStatus(ctx, store, repoStatus)
	}

	repoStatus.Stale = true
	repoStatus.StaleWarning = firstNonEmptyString(
		repoStatus.StaleWarning,
		"open code-intel store: "+openErr.Error(),
	)

	return repoStatus, nil
}

func workspaceOpenRepoStoreStatus(
	ctx context.Context,
	store *Store,
	repoStatus WorkspaceRepoStatus,
) (WorkspaceRepoStatus, error) {
	stats, statsErr := store.CodeFileIndexStats(ctx)
	closeErr := store.Close()

	if statsErr != nil {
		return WorkspaceRepoStatus{}, fmt.Errorf("read code file index stats: %w", statsErr)
	}

	if closeErr != nil {
		return WorkspaceRepoStatus{}, fmt.Errorf("close code-intel store: %w", closeErr)
	}

	repoStatus.LastIndexedAtUTC = stats.LatestIndexedAtUTC
	if stats.ActiveFiles == 0 {
		repoStatus.Stale = true
		repoStatus.StaleWarning = firstNonEmptyString(
			repoStatus.StaleWarning,
			"code-intel store has no active indexed files",
		)
	}

	return repoStatus, nil
}

func workspaceGitCommits(
	ctx context.Context,
	repo WorkspaceRepo,
) ([]workspaceGitCommit, error) {
	output, err := workspaceGitOutput(
		ctx,
		repo.Path,
		"log",
		"-"+strconv.Itoa(workspaceGitLogLimit),
		"--date=iso-strict",
		"--pretty=format:%H%x1f%ae%x1f%aI%x1f%s%x1e",
		"--name-only",
	)
	if err != nil {
		return nil, err
	}

	records := strings.Split(output, "\x1e")
	commits := []workspaceGitCommit{}

	for _, record := range records {
		commit, ok := parseWorkspaceGitCommit(repo.Alias, record)
		if ok {
			commits = append(commits, commit)
		}
	}

	return commits, nil
}

func parseWorkspaceGitCommit(alias, record string) (workspaceGitCommit, bool) {
	record = strings.TrimSpace(record)
	if record == "" {
		return workspaceGitCommit{}, false
	}

	lines := strings.Split(record, "\n")

	header := strings.SplitN(lines[0], "\x1f", workspaceGitHeaderParts)
	if len(header) != workspaceGitHeaderParts {
		return workspaceGitCommit{}, false
	}

	authorTime, err := time.Parse(time.RFC3339, strings.TrimSpace(header[2]))
	if err != nil {
		return workspaceGitCommit{}, false
	}

	paths := []string{}

	for _, line := range lines[1:] {
		path := strings.TrimSpace(line)
		if path != "" {
			paths = append(paths, path)
		}
	}

	return workspaceGitCommit{
		RepoAlias:   alias,
		Hash:        strings.TrimSpace(header[0]),
		AuthorEmail: strings.TrimSpace(header[1]),
		AuthorTime:  authorTime,
		Subject:     strings.TrimSpace(header[3]),
		Paths:       paths,
	}, true
}

func workspaceCoChanges(commits []workspaceGitCommit) []WorkspaceCoChange {
	cochanges := []WorkspaceCoChange{}

	for leftIndex, left := range commits {
		for _, right := range commits[leftIndex+1:] {
			if left.RepoAlias == right.RepoAlias {
				continue
			}

			reason := workspaceCoChangeReason(left, right)
			if reason == "" {
				continue
			}

			cochanges = append(cochanges, WorkspaceCoChange{
				LeftRepo:    left.RepoAlias,
				LeftCommit:  left.Hash,
				LeftPaths:   left.Paths,
				RightRepo:   right.RepoAlias,
				RightCommit: right.Hash,
				RightPaths:  right.Paths,
				AuthorEmail: left.AuthorEmail,
				MatchReason: reason,
				Confidence:  "candidate",
			})
		}
	}

	return cochanges
}

func workspaceCoChangeReason(left, right workspaceGitCommit) string {
	if left.Subject != "" && left.Subject == right.Subject {
		return "matching commit subject"
	}

	if left.AuthorEmail == "" || left.AuthorEmail != right.AuthorEmail {
		return ""
	}

	delta := left.AuthorTime.Sub(right.AuthorTime)
	if delta < 0 {
		delta = -delta
	}

	if delta <= workspaceCoChangeWindow {
		return "matching author and close commit time"
	}

	return ""
}

func workspaceContracts(
	ctx context.Context,
	registry WorkspaceRegistry,
) ([]WorkspaceContract, []string) {
	contracts := []WorkspaceContract{}
	warnings := []string{}

	for _, repo := range registry.Repos {
		if !workspaceFileExists(repo.CodeIntelDB) {
			continue
		}

		store, err := OpenReadOnly(ctx, repo.CodeIntelDB)
		if err != nil {
			warnings = append(warnings, repo.Alias+" contracts: "+err.Error())

			continue
		}

		edges, err := store.CodeEdges(ctx, CodeEdgeQuery{Limit: workspaceContractLimit})
		closeErr := store.Close()

		if err != nil {
			warnings = append(warnings, repo.Alias+" contracts: "+err.Error())

			continue
		}

		if closeErr != nil {
			warnings = append(warnings, repo.Alias+" contracts close: "+closeErr.Error())
		}

		contracts = append(contracts, workspaceContractsFromEdges(repo, registry, edges)...)
	}

	return contracts, warnings
}

func workspaceContractsFromEdges(
	repo WorkspaceRepo,
	registry WorkspaceRegistry,
	edges []CodeEdge,
) []WorkspaceContract {
	contracts := []WorkspaceContract{}

	for _, edge := range edges {
		if !workspaceContractEdgeKind(edge.Kind) || edge.TargetPath == "" {
			continue
		}

		provider, ok := workspaceRepoForTargetPath(registry, repo, edge.TargetPath)
		if !ok || provider.Alias == repo.Alias {
			continue
		}

		contracts = append(contracts, WorkspaceContract{
			ProviderRepo: provider.Alias,
			ConsumerRepo: repo.Alias,
			Kind:         edge.Kind,
			Path:         edge.Path,
			TargetPath:   edge.TargetPath,
			Evidence:     edge.RawText,
		})
	}

	return contracts
}

func workspaceRepoForTargetPath(
	registry WorkspaceRegistry,
	current WorkspaceRepo,
	targetPath string,
) (WorkspaceRepo, bool) {
	if filepath.IsAbs(targetPath) {
		for _, repo := range registry.Repos {
			if pathInside(repo.Path, targetPath) {
				return repo, true
			}
		}
	}

	for _, repo := range registry.Repos {
		if repo.Alias == current.Alias {
			continue
		}

		if strings.HasPrefix(targetPath, repo.Alias+"/") {
			return repo, true
		}
	}

	return WorkspaceRepo{}, false
}

func workspaceContractEdgeKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "imports",
		"imported_by",
		"http_route",
		"http_client",
		"grpc_service",
		"message_topic":
		return true
	default:
		return false
	}
}

func loadOrCreateWorkspaceRegistry(root string) (WorkspaceRegistry, error) {
	registry, err := LoadWorkspaceRegistry(root)
	if err == nil {
		return registry, nil
	}

	if !strings.Contains(err.Error(), errWorkspaceNotFound.Error()) {
		return WorkspaceRegistry{}, err
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return WorkspaceRegistry{}, fmt.Errorf("canonicalize workspace root: %w", err)
	}

	return WorkspaceRegistry{
		SchemaVersion: workspaceSchemaVersion,
		WorkspaceRoot: absRoot,
		Repos:         []WorkspaceRepo{},
	}, nil
}

func normalizeWorkspaceRegistry(
	root string,
	registry WorkspaceRegistry,
) (WorkspaceRegistry, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return WorkspaceRegistry{}, fmt.Errorf("canonicalize workspace root: %w", err)
	}

	if registry.SchemaVersion == 0 {
		registry.SchemaVersion = workspaceSchemaVersion
	}

	if registry.SchemaVersion != workspaceSchemaVersion {
		return WorkspaceRegistry{}, fmt.Errorf(
			"%w: %d",
			errWorkspaceSchema,
			registry.SchemaVersion,
		)
	}

	registry.WorkspaceRoot = firstNonEmptyString(registry.WorkspaceRoot, absRoot)

	registry.Repos = append([]WorkspaceRepo{}, registry.Repos...)
	for index, repo := range registry.Repos {
		normalized, err := normalizeWorkspaceRepo(absRoot, repo)
		if err != nil {
			return WorkspaceRegistry{}, err
		}

		registry.Repos[index] = normalized
	}

	sortWorkspaceRepos(registry.Repos)

	return registry, nil
}

func newWorkspaceRepo(root, alias, repoPath string) (WorkspaceRepo, error) {
	canonical, err := filepath.Abs(repoPath)
	if err != nil {
		return WorkspaceRepo{}, fmt.Errorf("canonicalize repository path: %w", err)
	}

	if !isGitRepo(canonical) {
		return WorkspaceRepo{}, fmt.Errorf("%w: %s", errWorkspaceRepoNotGit, canonical)
	}

	if strings.TrimSpace(alias) == "" {
		alias = sanitizeWorkspaceAlias(filepath.Base(canonical))
	}

	repo := WorkspaceRepo{Alias: alias, Path: canonical}

	return normalizeWorkspaceRepo(root, repo)
}

func normalizeWorkspaceRepo(root string, repo WorkspaceRepo) (WorkspaceRepo, error) {
	alias := strings.TrimSpace(repo.Alias)
	if !workspaceAliasPattern.MatchString(alias) {
		return WorkspaceRepo{}, fmt.Errorf("%w: %q", errWorkspaceAliasInvalid, alias)
	}

	canonical, err := filepath.Abs(repo.Path)
	if err != nil {
		return WorkspaceRepo{}, fmt.Errorf("canonicalize repository path: %w", err)
	}

	repo.Alias = alias
	repo.Path = canonical
	repo.CodeIntelDB = firstNonEmptyString(repo.CodeIntelDB, DefaultDBPath(canonical))
	_ = root

	return repo, nil
}

func sanitizeWorkspaceAlias(value string) string {
	value = strings.TrimSpace(value)
	builder := strings.Builder{}

	for _, char := range value {
		if char >= 'A' && char <= 'Z' ||
			char >= 'a' && char <= 'z' ||
			char >= '0' && char <= '9' ||
			char == '_' || char == '-' || char == '.' {
			builder.WriteRune(char)
		}
	}

	return builder.String()
}

func sortWorkspaceRepos(repos []WorkspaceRepo) {
	slices.SortFunc(repos, func(left, right WorkspaceRepo) int {
		return strings.Compare(left.Alias, right.Alias)
	})
}

func workspaceSkipDir(name string) bool {
	switch name {
	case ".git", ".coding-ethos", workspaceDirName, "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func isGitRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))

	return err == nil
}

func workspaceFileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
}

func workspaceGitOutput(
	ctx context.Context,
	repoPath string,
	args ...string,
) (string, error) {
	git, err := realgit.Resolve(ctx, "git")
	if err != nil {
		return "", fmt.Errorf("resolve real git: %w", err)
	}

	commandArgs := append([]string{"-C", repoPath}, args...)
	command := exec.CommandContext(ctx, git, commandArgs...)
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}

	return strings.TrimSpace(string(output)), nil
}

func pathInside(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}

	return relative != "." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		relative != ".."
}
