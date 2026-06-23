// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package webguidance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	PackageName       = "modern-web-guidance"
	PackageReference  = PackageName + "@latest"
	DistTagLatest     = "latest"
	CacheDir          = ".coding-ethos/cache/modern-web-guidance"
	cacheRecordV1     = 1
	cacheDirMode      = 0o700
	cacheFileMode     = 0o600
	operationList     = "list"
	operationRetrieve = "retrieve"
	operationSearch   = "search"
)

var (
	errModernWebGuidanceDisabled = errors.New("modern web guidance is disabled")
	errNoCachedModernWebGuidance = errors.New(
		"modern web guidance cache is empty",
	)
	errNetworkRefreshDisabled = errors.New(
		"modern web guidance network refresh is disabled",
	)
	errInvalidUpstreamMetadata = errors.New(
		"invalid modern web guidance upstream metadata",
	)
	errModernWebQueryRequired = errors.New("modern web guidance query is required")
	errModernWebIDsRequired   = errors.New("modern web guidance guide id is required")
	errUnsupportedOperation   = errors.New("unsupported modern web guidance operation")

	guideHeaderPattern = regexp.MustCompile(`(?m)^--- Guide for ([A-Za-z0-9._-]+) ---\s*$`)
)

// CommandRunner executes the upstream npm commands used by the adapter.
type CommandRunner interface {
	Run(ctx context.Context, cwd string, args []string) (CommandOutput, error)
}

// CommandOutput contains captured command output.
type CommandOutput struct {
	Stdout string
	Stderr string
}

type npmRunner struct{}

func (runner npmRunner) Run(
	ctx context.Context,
	cwd string,
	args []string,
) (CommandOutput, error) {
	command := exec.CommandContext(ctx, "npm", args...)
	command.Dir = cwd

	var (
		stdout strings.Builder
		stderr strings.Builder
	)

	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	if err != nil {
		return CommandOutput{
			Stdout: stdout.String(),
			Stderr: stderr.String(),
		}, fmt.Errorf("run npm: %w", err)
	}

	return CommandOutput{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}, nil
}

// Adapter queries Modern Web Guidance and wraps responses with cache and
// provenance metadata.
type Adapter struct {
	Runner CommandRunner
	Now    func() time.Time
	Root   string
}

// SearchInput is the input for a Modern Web Guidance search operation.
type SearchInput struct {
	Query         string `json:"query"`
	BrowserPolicy string `json:"browser_policy,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Refresh       bool   `json:"refresh,omitempty"`
}

// RetrieveInput is the input for a Modern Web Guidance retrieve operation.
type RetrieveInput struct {
	BrowserPolicy string   `json:"browser_policy,omitempty"`
	IDs           []string `json:"ids"`
	Refresh       bool     `json:"refresh,omitempty"`
}

// ListInput is the input for a Modern Web Guidance list operation.
type ListInput struct {
	Refresh bool `json:"refresh,omitempty"`
}

// Response is a structured Modern Web Guidance result.
//
//nolint:govet // Public JSON order mirrors agent-facing output.
type Response struct {
	Kind          string         `json:"kind"`
	Operation     string         `json:"operation"`
	Query         string         `json:"query,omitempty"`
	BrowserPolicy string         `json:"browser_policy,omitempty"`
	Results       []GuideSummary `json:"results,omitempty"`
	Guides        []GuideContent `json:"guides,omitempty"`
	Provenance    Provenance     `json:"provenance"`
	Cache         CacheStatus    `json:"cache"`
	Warnings      []string       `json:"warnings,omitempty"`
	Advisory      bool           `json:"advisory"`
}

// GuideSummary is one list or search result from upstream guidance.
type GuideSummary struct {
	ID           string   `json:"id"`
	Category     string   `json:"category,omitempty"`
	Description  string   `json:"description,omitempty"`
	FeaturesUsed []string `json:"features_used,omitempty"`
	TokenCount   int      `json:"token_count,omitempty"`
	Similarity   float64  `json:"similarity,omitempty"`
}

// UnmarshalJSON accepts the upstream camelCase payload while preserving the
// coding-ethos snake_case output contract.
func (summary *GuideSummary) UnmarshalJSON(data []byte) error {
	//nolint:tagliatelle // Upstream Modern Web Guidance uses camelCase fields.
	var upstream struct {
		ID              string   `json:"id"`
		Category        string   `json:"category"`
		Description     string   `json:"description"`
		FeaturesUsed    []string `json:"featuresUsed"`
		SnakeFeatures   []string `json:"features_used"`
		TokenCount      int      `json:"tokenCount"`
		SnakeTokenCount int      `json:"token_count"`
		Similarity      float64  `json:"similarity"`
	}

	err := json.Unmarshal(data, &upstream)
	if err != nil {
		return fmt.Errorf("parse upstream guide summary: %w", err)
	}

	summary.ID = upstream.ID
	summary.Category = upstream.Category
	summary.Description = upstream.Description

	summary.FeaturesUsed = upstream.FeaturesUsed
	if len(summary.FeaturesUsed) == 0 {
		summary.FeaturesUsed = upstream.SnakeFeatures
	}

	summary.TokenCount = upstream.TokenCount
	if summary.TokenCount == 0 {
		summary.TokenCount = upstream.SnakeTokenCount
	}

	summary.Similarity = upstream.Similarity

	return nil
}

// GuideContent is one retrieved guide.
type GuideContent struct {
	ID          string    `json:"id"`
	Content     string    `json:"content"`
	ContentHash string    `json:"content_hash"`
	Sections    []Section `json:"sections,omitempty"`
}

// Section is a parsed Markdown heading and body fragment.
type Section struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Level   int    `json:"level"`
}

// Provenance describes the upstream package and exact response evidence.
//
//nolint:govet // Public JSON order keeps provenance readable.
type Provenance struct {
	PackageName     string   `json:"package_name"`
	ResolvedVersion string   `json:"resolved_version"`
	DistTag         string   `json:"dist_tag"`
	FetchTimeUTC    string   `json:"fetch_time_utc"`
	GuideIDs        []string `json:"guide_ids,omitempty"`
	SourceURL       string   `json:"source_url,omitempty"`
	ContentHash     string   `json:"content_hash"`
}

// CacheStatus describes the response cache state.
type CacheStatus struct {
	Status     string `json:"status"`
	Path       string `json:"path"`
	AgeSeconds int64  `json:"age_seconds,omitempty"`
	Hit        bool   `json:"hit"`
	Stale      bool   `json:"stale"`
}

//nolint:tagliatelle // npm metadata uses dist-tags.
type upstreamMetadata struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	DistTags   map[string]string `json:"dist-tags"`
	Bin        map[string]string `json:"bin"`
	Repository struct {
		URL string `json:"url"`
	} `json:"repository"`
}

//nolint:govet // Cache JSON keeps metadata grouped after response.
type cacheRecord struct {
	Response   Response `json:"response"`
	Version    int      `json:"version"`
	FetchedAt  string   `json:"fetched_at_utc"`
	CacheKey   string   `json:"cache_key"`
	Expiration string   `json:"expires_at_utc"`
}

type operationRequest struct {
	Operation     string
	Query         string
	BrowserPolicy string
	IDs           []string
	Limit         int
	Refresh       bool
}

// Search queries upstream Modern Web Guidance.
func (adapter Adapter) Search(
	ctx context.Context,
	input SearchInput,
) (Response, error) {
	if strings.TrimSpace(input.Query) == "" {
		return Response{}, errModernWebQueryRequired
	}

	return adapter.execute(ctx, operationRequest{
		Operation:     operationSearch,
		Query:         strings.TrimSpace(input.Query),
		BrowserPolicy: strings.TrimSpace(input.BrowserPolicy),
		Limit:         input.Limit,
		Refresh:       input.Refresh,
	})
}

// Retrieve fetches one or more full upstream guides.
func (adapter Adapter) Retrieve(
	ctx context.Context,
	input RetrieveInput,
) (Response, error) {
	ids := normalizeIDs(input.IDs)
	if len(ids) == 0 {
		return Response{}, errModernWebIDsRequired
	}

	return adapter.execute(ctx, operationRequest{
		Operation:     operationRetrieve,
		IDs:           ids,
		BrowserPolicy: strings.TrimSpace(input.BrowserPolicy),
		Refresh:       input.Refresh,
	})
}

// List returns the upstream guidance catalog.
func (adapter Adapter) List(ctx context.Context, input ListInput) (Response, error) {
	return adapter.execute(ctx, operationRequest{
		Operation: operationList,
		Refresh:   input.Refresh,
	})
}

func (adapter Adapter) execute(
	ctx context.Context,
	request operationRequest,
) (Response, error) {
	root, err := adapter.root()
	if err != nil {
		return Response{}, err
	}

	settings, err := LoadSettings(root)
	if err != nil {
		return Response{}, err
	}

	if !settings.ModernWeb.Enabled {
		return Response{}, errModernWebGuidanceDisabled
	}

	if request.BrowserPolicy == "" {
		request.BrowserPolicy = settings.ModernWeb.BrowserPolicy
	}

	now := adapter.now()

	cachePath, cacheKey, err := adapter.cachePath(root, request)
	if err != nil {
		return Response{}, err
	}

	cached, cacheFound, cacheErr := loadCacheRecord(cachePath)
	if cacheErr != nil {
		return Response{}, cacheErr
	}

	if cacheFound {
		response, usable := cachedResponse(
			cached,
			cachePath,
			request,
			settings.ModernWeb,
			now,
		)
		if usable {
			return response, nil
		}
	}

	if !settings.ModernWeb.AllowNetworkRefresh {
		return Response{}, fmt.Errorf(
			"%w: run with allow_network_refresh enabled to populate %s",
			errNoCachedModernWebGuidance,
			cachePath,
		)
	}

	response, fetchErr := adapter.fetch(ctx, root, request, cachePath, now)
	if fetchErr != nil {
		return fetchFailureResponse(cached, cacheFound, cachePath, fetchErr, now)
	}

	return writeFetchedResponse(cachePath, cacheKey, response, settings.ModernWeb, now)
}

func cachedResponse(
	cached cacheRecord,
	cachePath string,
	request operationRequest,
	settings ModernWebSettings,
	now time.Time,
) (Response, bool) {
	response := cached.Response
	response.Cache = cacheStatus(
		"hit",
		cachePath,
		true,
		cacheIsStale(cached, settings.CacheTTL, now),
		response.Provenance.FetchTimeUTC,
		now,
	)

	if !request.Refresh && !response.Cache.Stale {
		return response, true
	}

	if settings.AllowNetworkRefresh {
		return Response{}, false
	}

	if response.Cache.Stale {
		response.Cache.Status = "stale"
		response.Warnings = append(
			response.Warnings,
			"cached response is stale and network refresh is disabled",
		)
	}

	return response, true
}

func fetchFailureResponse(
	cached cacheRecord,
	cacheFound bool,
	cachePath string,
	fetchErr error,
	now time.Time,
) (Response, error) {
	if cacheFound {
		response := cached.Response
		response.Cache = cacheStatus(
			"stale",
			cachePath,
			true,
			true,
			response.Provenance.FetchTimeUTC,
			now,
		)
		response.Warnings = append(
			response.Warnings,
			"network refresh failed; returning stale cached guidance: "+fetchErr.Error(),
		)

		return response, nil
	}

	return Response{}, fmt.Errorf(
		"%w: %w; no cache exists at %s",
		errNoCachedModernWebGuidance,
		fetchErr,
		cachePath,
	)
}

func writeFetchedResponse(
	cachePath string,
	cacheKey string,
	response Response,
	settings ModernWebSettings,
	now time.Time,
) (Response, error) {
	record := cacheRecord{
		Version:    cacheRecordV1,
		CacheKey:   cacheKey,
		FetchedAt:  response.Provenance.FetchTimeUTC,
		Expiration: now.Add(settings.CacheTTL).UTC().Format(time.RFC3339),
		Response:   response,
	}

	err := writeCacheRecord(cachePath, record)
	if err != nil {
		return Response{}, err
	}

	return response, nil
}

func (adapter Adapter) fetch(
	ctx context.Context,
	root string,
	request operationRequest,
	cachePath string,
	now time.Time,
) (Response, error) {
	metadata, err := adapter.fetchMetadata(ctx, root)
	if err != nil {
		return Response{}, err
	}

	output, err := adapter.runUpstream(ctx, root, request)
	if err != nil {
		return Response{}, err
	}

	response := Response{
		Kind:          "modern_web_guidance",
		Operation:     request.Operation,
		Query:         request.Query,
		BrowserPolicy: request.BrowserPolicy,
		Advisory:      true,
		Cache: cacheStatus(
			"refreshed",
			cachePath,
			false,
			false,
			now.Format(time.RFC3339),
			now,
		),
		Provenance: Provenance{
			PackageName:     metadata.Name,
			ResolvedVersion: metadata.Version,
			DistTag:         DistTagLatest,
			FetchTimeUTC:    now.UTC().Format(time.RFC3339),
			SourceURL:       normalizeSourceURL(metadata.Repository.URL),
			ContentHash:     sha256Text(output.Stdout),
		},
	}

	switch request.Operation {
	case operationList, operationSearch:
		results, err := parseSummaries(output.Stdout)
		if err != nil {
			return Response{}, err
		}

		if request.Operation == operationSearch && request.Limit > 0 &&
			len(results) > request.Limit {
			results = results[:request.Limit]
		}

		response.Results = results
		response.Provenance.GuideIDs = summaryIDs(results)
	case operationRetrieve:
		guides := parseRetrievedGuides(output.Stdout, request.IDs)
		response.Guides = guides
		response.Provenance.GuideIDs = guideContentIDs(guides)
	default:
		return Response{}, fmt.Errorf(
			"%w: %q",
			errUnsupportedOperation,
			request.Operation,
		)
	}

	if request.BrowserPolicy != "" {
		response.Warnings = append(
			response.Warnings,
			"browser_policy is recorded for downstream context; "+
				"upstream guidance remains advisory",
		)
	}

	return response, nil
}

func (adapter Adapter) fetchMetadata(
	ctx context.Context,
	root string,
) (upstreamMetadata, error) {
	output, err := adapter.runner().Run(ctx, root, []string{
		"--cache",
		adapter.npmCacheDir(root),
		"view",
		PackageReference,
		"name",
		"version",
		"dist-tags",
		"bin",
		"repository",
		"--json",
	})
	if err != nil {
		return upstreamMetadata{}, commandError("fetch upstream metadata", output, err)
	}

	var metadata upstreamMetadata

	err = json.Unmarshal([]byte(output.Stdout), &metadata)
	if err != nil {
		return upstreamMetadata{}, fmt.Errorf("parse upstream metadata: %w", err)
	}

	if metadata.Name != PackageName ||
		metadata.Version == "" ||
		metadata.DistTags[DistTagLatest] == "" ||
		metadata.Bin["modern-web-guidance"] == "" {
		return upstreamMetadata{}, fmt.Errorf("%w: %#v", errInvalidUpstreamMetadata, metadata)
	}

	return metadata, nil
}

func (adapter Adapter) runUpstream(
	ctx context.Context,
	root string,
	request operationRequest,
) (CommandOutput, error) {
	args := []string{
		"--cache",
		adapter.npmCacheDir(root),
		"exec",
		"--yes",
		PackageReference,
		"--",
	}

	switch request.Operation {
	case operationList:
		args = append(args, operationList)
	case operationSearch:
		args = append(args, operationSearch, request.Query)
	case operationRetrieve:
		args = append(args, operationRetrieve, strings.Join(request.IDs, ","))
	default:
		return CommandOutput{}, fmt.Errorf(
			"%w: %q",
			errUnsupportedOperation,
			request.Operation,
		)
	}

	output, err := adapter.runner().Run(ctx, root, args)
	if err != nil {
		return CommandOutput{}, commandError("run upstream "+request.Operation, output, err)
	}

	return output, nil
}

func commandError(action string, output CommandOutput, err error) error {
	message := strings.TrimSpace(output.Stderr)
	if message == "" {
		message = strings.TrimSpace(output.Stdout)
	}

	if message == "" {
		message = err.Error()
	}

	return fmt.Errorf("%s: %w: %s", action, err, message)
}

func parseSummaries(raw string) ([]GuideSummary, error) {
	var summaries []GuideSummary

	err := json.Unmarshal([]byte(raw), &summaries)
	if err != nil {
		return nil, fmt.Errorf("parse modern web guidance summaries: %w", err)
	}

	return summaries, nil
}

func parseRetrievedGuides(raw string, requestedIDs []string) []GuideContent {
	matches := guideHeaderPattern.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		id := ""
		if len(requestedIDs) > 0 {
			id = requestedIDs[0]
		}

		return []GuideContent{guideContent(id, strings.TrimSpace(raw))}
	}

	guides := make([]GuideContent, 0, len(matches))
	for index, match := range matches {
		start := match[1]

		end := len(raw)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}

		id := raw[match[2]:match[3]]
		content := strings.TrimSpace(raw[start:end])
		guides = append(guides, guideContent(id, content))
	}

	return guides
}

func guideContent(id, content string) GuideContent {
	return GuideContent{
		ID:          strings.TrimSpace(id),
		Content:     content,
		ContentHash: sha256Text(content),
		Sections:    parseSections(content),
	}
}

func parseSections(markdown string) []Section {
	sections := []Section{}
	current := Section{}
	body := []string{}

	flush := func() {
		if current.Title == "" {
			body = []string{}

			return
		}

		current.Content = strings.TrimSpace(strings.Join(body, "\n"))
		sections = append(sections, current)
		body = []string{}
	}

	for line := range strings.SplitSeq(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		level := headingLevel(trimmed)

		if level > 0 {
			flush()

			current = Section{
				Level: level,
				Title: strings.TrimSpace(strings.TrimLeft(trimmed, "#")),
			}

			continue
		}

		body = append(body, line)
	}

	flush()

	return sections
}

func headingLevel(line string) int {
	if !strings.HasPrefix(line, "#") {
		return 0
	}

	level := 0

	for _, char := range line {
		if char != '#' {
			break
		}

		level++
	}

	if level == 0 || level > 6 || len(line) <= level || line[level] != ' ' {
		return 0
	}

	return level
}

func normalizeIDs(ids []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(ids))

	for _, id := range ids {
		for part := range strings.SplitSeq(id, ",") {
			value := strings.TrimSpace(part)
			if value == "" || seen[value] {
				continue
			}

			seen[value] = true
			normalized = append(normalized, value)
		}
	}

	sort.Strings(normalized)

	return normalized
}

func summaryIDs(summaries []GuideSummary) []string {
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		if strings.TrimSpace(summary.ID) != "" {
			ids = append(ids, strings.TrimSpace(summary.ID))
		}
	}

	return ids
}

func guideContentIDs(guides []GuideContent) []string {
	ids := make([]string, 0, len(guides))
	for _, guide := range guides {
		if strings.TrimSpace(guide.ID) != "" {
			ids = append(ids, strings.TrimSpace(guide.ID))
		}
	}

	return ids
}

func (adapter Adapter) cachePath(
	root string,
	request operationRequest,
) (string, string, error) {
	payload, err := json.Marshal(map[string]any{
		"operation":      request.Operation,
		"query":          request.Query,
		"ids":            request.IDs,
		"limit":          request.Limit,
		"browser_policy": request.BrowserPolicy,
		"package":        PackageReference,
	})
	if err != nil {
		return "", "", fmt.Errorf("encode modern web guidance cache key: %w", err)
	}

	hash := sha256.Sum256(payload)
	key := hex.EncodeToString(hash[:])

	return filepath.Join(root, CacheDir, "responses", key+".json"), key, nil
}

func (adapter Adapter) npmCacheDir(root string) string {
	return filepath.Join(root, CacheDir, "npm-cache")
}

func (adapter Adapter) runner() CommandRunner {
	if adapter.Runner == nil {
		return npmRunner{}
	}

	return adapter.Runner
}

func (adapter Adapter) now() time.Time {
	if adapter.Now != nil {
		return adapter.Now().UTC()
	}

	return time.Now().UTC()
}

func (adapter Adapter) root() (string, error) {
	root := strings.TrimSpace(adapter.Root)
	if root == "" {
		root = "."
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve modern web guidance root: %w", err)
	}

	return absoluteRoot, nil
}

func loadCacheRecord(path string) (cacheRecord, bool, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cacheRecord{}, false, nil
		}

		return cacheRecord{}, false, fmt.Errorf(
			"read modern web guidance cache %s: %w",
			path,
			err,
		)
	}

	var record cacheRecord

	err = json.Unmarshal(data, &record)
	if err != nil {
		return cacheRecord{}, false, fmt.Errorf(
			"parse modern web guidance cache %s: %w",
			path,
			err,
		)
	}

	return record, true, nil
}

func writeCacheRecord(path string, record cacheRecord) error {
	err := os.MkdirAll(filepath.Dir(path), cacheDirMode)
	if err != nil {
		return fmt.Errorf("create modern web guidance cache directory: %w", err)
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode modern web guidance cache: %w", err)
	}

	err = os.WriteFile(filepath.Clean(path), append(data, '\n'), cacheFileMode)
	if err != nil {
		return fmt.Errorf("write modern web guidance cache %s: %w", path, err)
	}

	return nil
}

func cacheIsStale(record cacheRecord, ttl time.Duration, now time.Time) bool {
	fetchedAt, err := time.Parse(time.RFC3339, record.Response.Provenance.FetchTimeUTC)
	if err != nil {
		return true
	}

	return ttl > 0 && now.Sub(fetchedAt) > ttl
}

func cacheStatus(
	status string,
	path string,
	hit bool,
	stale bool,
	fetchedAt string,
	now time.Time,
) CacheStatus {
	ageSeconds := int64(0)

	if fetchedAt != "" {
		parsed, err := time.Parse(time.RFC3339, fetchedAt)
		if err == nil {
			ageSeconds = max(int64(now.Sub(parsed).Seconds()), 0)
		}
	}

	return CacheStatus{
		Status:     status,
		Path:       path,
		Hit:        hit,
		Stale:      stale,
		AgeSeconds: ageSeconds,
	}
}

func sha256Text(value string) string {
	hash := sha256.Sum256([]byte(value))

	return "sha256:" + hex.EncodeToString(hash[:])
}

func normalizeSourceURL(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "git+")
	value = strings.TrimSuffix(value, ".git")

	return value
}
