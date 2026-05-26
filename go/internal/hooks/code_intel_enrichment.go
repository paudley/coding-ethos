// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const (
	defaultCodeIntelEnrichmentTimeout        = 2 * time.Second
	defaultCodeIntelEnrichmentMaxPaths       = 3
	defaultCodeIntelEnrichmentMaxSymbols     = 6
	defaultCodeIntelEnrichmentMaxEdges       = 4
	defaultCodeIntelEnrichmentMaxFailures    = 4
	defaultCodeIntelEnrichmentMaxOutputPaths = 12
	codeIntelEnrichmentRefreshCommand        = "coding-ethos-code-intel rebuild-index"
	codeIntelTimestampSQLiteLayout           = "2006-01-02 15:04:05"
	codeIntelFreshnessUnknownHead            = "unknown"
	codeIntelFreshnessStateDirMode           = 0o700
	codeIntelFreshnessStateFileMode          = 0o600
	codeIntelLargeFileLineThreshold          = 1000
	codeIntelSymbolHotspotThreshold          = 50
	codeIntelStatusMissingIndex              = "missing_index"
	codeIntelStatusReady                     = "ready"
	codeIntelStatusStale                     = "stale"
)

var errEmptyCodeIntelTimestamp = errors.New("empty code-intel timestamp")

type codeIntelEnrichmentOptions struct {
	Enabled     bool
	MaxPaths    int
	MaxSymbols  int
	MaxEdges    int
	MaxFailures int
}

type codeIntelEnrichment struct {
	Status       string
	Refresh      string
	Reason       string
	Paths        []codeIntelEnrichmentPath
	Symbols      []codeIntelEnrichmentSymbol
	Related      []codeIntelEnrichmentRelated
	Evidence     []codeIntelEnrichmentEvidence
	NextMCPCalls []string
}

type codeIntelEnrichmentPath struct {
	Path     string
	Language string
	Risk     string
	Lines    int
	Symbols  int
}

type codeIntelEnrichmentSymbol struct {
	Path      string
	Symbol    string
	Signature string
	Line      int
}

type codeIntelEnrichmentRelated struct {
	Path       string
	Kind       string
	Target     string
	EvidenceID string
}

type codeIntelEnrichmentEvidence struct {
	Path       string
	PolicyID   string
	SkillID    string
	EvidenceID string
	Count      int
}

type codeIntelIncomingRelatedState struct {
	Enrichment *codeIntelEnrichment
	Seen       map[string]bool
	Path       string
	MaxEdges   int
}

func codeIntelEnrichmentOutput(event Event, toolOutput string) *HookSpecificOutput {
	context := codeIntelEnrichmentContext(event, toolOutput)
	if context == "" {
		return nil
	}

	return &HookSpecificOutput{
		HookEventName:     event.HookEventName,
		AdditionalContext: context,
	}
}

func codeIntelEnrichmentContext(event Event, toolOutput string) string {
	if event.HookEventName != eventPostToolUse {
		return ""
	}

	root := gitRootFromPath(event.Cwd)
	if root == "" {
		return ""
	}

	options := loadCodeIntelEnrichmentOptions(root)
	if !options.Enabled {
		return ""
	}

	notice := codeIntelFreshnessNotice(event, root)
	candidate := codeIntelEnrichmentCandidate(event)

	if !candidate && notice == "" {
		return ""
	}

	if !candidate {
		return notice
	}

	enrichment := buildCodeIntelEnrichment(event, root, toolOutput, options)
	if rendered := renderCodeIntelEnrichment(enrichment); rendered != "" {
		if notice != "" {
			return notice + "\n\n" + rendered
		}

		return rendered
	}

	return notice
}

func loadCodeIntelEnrichmentOptions(root string) codeIntelEnrichmentOptions {
	options := codeIntelEnrichmentOptions{
		Enabled:     true,
		MaxPaths:    defaultCodeIntelEnrichmentMaxPaths,
		MaxSymbols:  defaultCodeIntelEnrichmentMaxSymbols,
		MaxEdges:    defaultCodeIntelEnrichmentMaxEdges,
		MaxFailures: defaultCodeIntelEnrichmentMaxFailures,
	}

	config, err := loadHookRepoConfig(root)
	if err != nil {
		return options
	}

	settings := configdata.MapValue(
		configdata.GetPath(config, "proxy.code_intel_enrichment", map[string]any{}),
	)
	if settings == nil {
		return options
	}

	if enabled, ok := settings["enabled"].(bool); ok {
		options.Enabled = enabled
	}

	options.MaxPaths = positiveConfigInt(settings, "max_paths", options.MaxPaths)
	options.MaxSymbols = positiveConfigInt(settings, "max_symbols", options.MaxSymbols)
	options.MaxEdges = positiveConfigInt(settings, "max_edges", options.MaxEdges)
	options.MaxFailures = positiveConfigInt(
		settings,
		"max_failures",
		options.MaxFailures,
	)

	return options
}

func codeIntelEnrichmentCandidate(event Event) bool {
	switch event.ToolName {
	case "Read", "Grep", "Glob", "Search":
		return true
	case toolBash:
		return bashCodeIntelEnrichmentCandidate(event.Command())
	default:
		return false
	}
}

func bashCodeIntelEnrichmentCandidate(command string) bool {
	if _, ok := fileReadInvocation(command); ok {
		return true
	}

	if _, ok := directoryListingInvocation(command); ok {
		return true
	}

	commands, err := shellparse.Commands(command)
	if err != nil || len(commands) != 1 {
		return false
	}

	return slices.Contains([]string{"grep", "rg", "fd", "find"}, commands[0].Name)
}

func buildCodeIntelEnrichment(
	event Event,
	root string,
	toolOutput string,
	options codeIntelEnrichmentOptions,
) codeIntelEnrichment {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		defaultCodeIntelEnrichmentTimeout,
	)
	defer cancel()

	store, err := codeintel.OpenReadOnly(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		return codeIntelEnrichment{
			Status:  codeIntelStatusMissingIndex,
			Refresh: codeIntelEnrichmentRefreshCommand,
			Reason:  "code-intel index is not available",
		}
	}
	defer store.Close()

	enrichment := codeIntelEnrichment{
		Status:  codeIntelIndexStatus(ctx, store),
		Refresh: codeIntelEnrichmentRefreshCommand,
	}

	paths := enrichmentTargetPaths(event, root, toolOutput, options.MaxPaths)
	for _, path := range paths {
		addCodeIntelPathHints(ctx, store, path, options, &enrichment)
	}

	if len(enrichment.Paths) == 0 && len(paths) == 0 {
		enrichment.Reason = "no repo-relative code paths were detected"
	}

	enrichment.NextMCPCalls = codeIntelNextMCPCalls(paths, options)

	return enrichment
}

func addCodeIntelPathHints(
	ctx context.Context,
	store *codeintel.Store,
	path string,
	options codeIntelEnrichmentOptions,
	enrichment *codeIntelEnrichment,
) {
	addCodeIntelPathSummary(ctx, store, path, enrichment)
	addCodeIntelSymbolHints(ctx, store, path, options, enrichment)
	addCodeIntelRelatedHints(ctx, store, path, options, enrichment)
	addCodeIntelEvidenceHints(ctx, store, path, options, enrichment)
}

func addCodeIntelPathSummary(
	ctx context.Context,
	store *codeintel.Store,
	path string,
	enrichment *codeIntelEnrichment,
) {
	repoMap, err := store.RepoMap(ctx, codeintel.CompactCodeContextQuery{
		Path:  path,
		Root:  "",
		Limit: 1,
	})
	if err == nil && len(repoMap) > 0 {
		entry := repoMap[0]
		enrichment.Paths = append(enrichment.Paths, codeIntelEnrichmentPath{
			Path:     entry.Path,
			Language: entry.Language,
			Lines:    entry.LineCount,
			Symbols:  entry.Symbols,
			Risk:     codeIntelPathRisk(entry),
		})
	}
}

func addCodeIntelSymbolHints(
	ctx context.Context,
	store *codeintel.Store,
	path string,
	options codeIntelEnrichmentOptions,
	enrichment *codeIntelEnrichment,
) {
	chunks, err := store.CodeChunks(ctx, codeintel.CodeChunkQuery{
		Path:  path,
		Limit: options.MaxSymbols,
	})
	if err == nil {
		for _, chunk := range chunks {
			if len(enrichment.Symbols) >= options.MaxSymbols {
				break
			}

			symbol := firstNonEmpty(chunk.SymbolPath, chunk.SymbolName, chunk.NodeKind)
			if symbol == "" {
				continue
			}

			enrichment.Symbols = append(enrichment.Symbols, codeIntelEnrichmentSymbol{
				Path:      chunk.Path,
				Symbol:    symbol,
				Line:      chunk.StartLine,
				Signature: codeIntelSignature(chunk.RawText),
			})
		}
	}
}

func addCodeIntelRelatedHints(
	ctx context.Context,
	store *codeintel.Store,
	path string,
	options codeIntelEnrichmentOptions,
	enrichment *codeIntelEnrichment,
) {
	addCodeIntelIncomingRelatedHints(ctx, store, path, options, enrichment)

	if len(enrichment.Related) >= options.MaxEdges {
		return
	}

	edges, err := store.CodeEdges(ctx, codeintel.CodeEdgeQuery{
		Path:  path,
		Limit: options.MaxEdges - len(enrichment.Related),
	})
	if err == nil {
		for _, edge := range edges {
			if len(enrichment.Related) >= options.MaxEdges {
				break
			}

			enrichment.Related = append(enrichment.Related, codeIntelEnrichmentRelated{
				Path:       firstNonEmpty(edge.TargetPath, edge.Path),
				Kind:       edge.Kind,
				Target:     firstNonEmpty(edge.TargetSymbolPath, edge.TargetName),
				EvidenceID: edge.ID,
			})
		}
	}
}

func addCodeIntelIncomingRelatedHints(
	ctx context.Context,
	store *codeintel.Store,
	path string,
	options codeIntelEnrichmentOptions,
	enrichment *codeIntelEnrichment,
) {
	state := codeIntelIncomingRelatedState{
		Enrichment: enrichment,
		Seen:       map[string]bool{},
		Path:       path,
		MaxEdges:   options.MaxEdges,
	}
	targetCandidates := codeIntelTargetCandidates(path)

	for _, candidate := range targetCandidates {
		if state.Full() {
			return
		}

		addQueriedCodeIntelIncomingEdges(ctx, store, &state, codeintel.CodeEdgeQuery{
			TargetPath: candidate,
		})
	}

	for _, candidate := range codeIntelTargetSymbolCandidates(ctx, store, path) {
		if state.Full() {
			return
		}

		addQueriedCodeIntelIncomingEdges(ctx, store, &state, codeintel.CodeEdgeQuery{
			TargetName: candidate,
		})
	}
}

func addQueriedCodeIntelIncomingEdges(
	ctx context.Context,
	store *codeintel.Store,
	state *codeIntelIncomingRelatedState,
	query codeintel.CodeEdgeQuery,
) {
	query.Limit = state.Remaining()

	edges, err := store.CodeEdges(ctx, query)
	if err != nil {
		return
	}

	for _, edge := range edges {
		if state.Full() {
			return
		}

		appendCodeIntelIncomingRelated(edge, state)
	}
}

func appendCodeIntelIncomingRelated(
	edge codeintel.CodeEdge,
	state *codeIntelIncomingRelatedState,
) {
	if edge.Path == state.Path {
		return
	}

	key := codeIntelEdgeKey(edge)
	if state.Seen[key] {
		return
	}

	state.Seen[key] = true
	state.Enrichment.Related = append(
		state.Enrichment.Related,
		codeIntelEnrichmentRelated{
			Path:       edge.Path,
			Kind:       incomingCodeIntelEdgeKind(edge.Kind),
			Target:     firstNonEmpty(edge.TargetSymbolPath, edge.TargetName),
			EvidenceID: edge.ID,
		},
	)
}

func codeIntelEdgeKey(edge codeintel.CodeEdge) string {
	if edge.ID != "" {
		return edge.ID
	}

	return edge.Path + "\x00" + edge.Kind + "\x00" + edge.TargetName
}

func (state codeIntelIncomingRelatedState) Full() bool {
	return len(state.Enrichment.Related) >= state.MaxEdges
}

func (state codeIntelIncomingRelatedState) Remaining() int {
	return state.MaxEdges - len(state.Enrichment.Related)
}

func codeIntelTargetCandidates(path string) []string {
	normalized := strings.Trim(filepath.ToSlash(path), "/")
	if normalized == "" {
		return nil
	}

	withoutExt := strings.TrimSuffix(normalized, filepath.Ext(normalized))

	candidates := []string{normalized}
	if withoutExt != "" && withoutExt != normalized {
		candidates = append(candidates, withoutExt)
	}

	modulePath := strings.ReplaceAll(withoutExt, "/", ".")
	if modulePath != "" && modulePath != withoutExt {
		candidates = append(candidates, modulePath)
	}

	if parentModule := parentModuleCandidate(withoutExt); parentModule != "" {
		candidates = append(candidates, parentModule)
	}

	if base := pathBaseWithoutExt(normalized); base != "" {
		candidates = append(candidates, base)
	}

	return slices.Compact(candidates)
}

func parentModuleCandidate(path string) string {
	parent := filepath.Dir(path)
	if parent == "." || parent == "" {
		return ""
	}

	return strings.ReplaceAll(parent, "/", ".")
}

func pathBaseWithoutExt(path string) string {
	base := filepath.Base(path)

	return strings.TrimSuffix(base, filepath.Ext(base))
}

func codeIntelTargetSymbolCandidates(
	ctx context.Context,
	store *codeintel.Store,
	path string,
) []string {
	chunks, err := store.CodeChunks(ctx, codeintel.CodeChunkQuery{
		Path:  path,
		Limit: defaultCodeIntelEnrichmentMaxSymbols,
	})
	if err != nil {
		return nil
	}

	candidates := []string{}
	for _, chunk := range chunks {
		candidates = appendNonEmptyCandidate(candidates, chunk.SymbolName)
		candidates = appendNonEmptyCandidate(candidates, chunk.SymbolPath)

		if _, suffix, ok := strings.Cut(chunk.SymbolPath, "."); ok {
			candidates = appendNonEmptyCandidate(candidates, suffix)
		}
	}

	slices.Sort(candidates)

	return slices.Compact(candidates)
}

func appendNonEmptyCandidate(candidates []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return candidates
	}

	return append(candidates, candidate)
}

func incomingCodeIntelEdgeKind(kind string) string {
	switch kind {
	case "imports":
		return "imported_by"
	case "calls":
		return "called_by"
	default:
		if kind == "" {
			return "referenced_by"
		}

		return kind + "_by"
	}
}

func addCodeIntelEvidenceHints(
	ctx context.Context,
	store *codeintel.Store,
	path string,
	options codeIntelEnrichmentOptions,
	enrichment *codeIntelEnrichment,
) {
	failures, err := store.RepeatedFailures(ctx, codeintel.RepeatedFailureQuery{
		Path:  path,
		Limit: options.MaxFailures,
	})
	if err == nil {
		for _, failure := range failures {
			if len(enrichment.Evidence) >= options.MaxFailures {
				break
			}

			enrichment.Evidence = append(
				enrichment.Evidence,
				codeIntelEnrichmentEvidence{
					Path:       failure.Path,
					PolicyID:   failure.PolicyID,
					SkillID:    failure.SkillID,
					EvidenceID: failure.LastTraceID,
					Count:      failure.TraceCount,
				},
			)
		}
	}
}

func enrichmentTargetPaths(
	event Event,
	root string,
	toolOutput string,
	limit int,
) []string {
	paths := []string{}

	for _, path := range event.Files() {
		paths = appendRepoRelativePath(paths, root, event.Cwd, path)
	}

	if event.ToolName == toolBash {
		paths = appendBashTargetPath(paths, event, root)
	}

	for _, path := range outputCandidatePaths(toolOutput) {
		paths = appendRepoRelativePath(paths, root, event.Cwd, path)
		if len(paths) >= defaultCodeIntelEnrichmentMaxOutputPaths {
			break
		}
	}

	return boundedUniquePaths(paths, limit)
}

func appendBashTargetPath(paths []string, event Event, root string) []string {
	if invocation, ok := fileReadInvocation(event.Command()); ok {
		return appendRepoRelativePath(paths, root, event.Cwd, invocation.Path)
	}

	if invocation, ok := directoryListingInvocation(event.Command()); ok {
		return appendRepoRelativePath(paths, root, event.Cwd, invocation.Path)
	}

	return paths
}

func appendRepoRelativePath(paths []string, root, cwd, path string) []string {
	relativePath, ok := repoRelativeListingPath(root, cwd, path)
	if !ok || relativePath == "." {
		return paths
	}

	if !isRepoFile(root, relativePath) {
		return paths
	}

	return append(paths, relativePath)
}

func outputCandidatePaths(output string) []string {
	paths := []string{}

	for line := range strings.Lines(output) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		candidate := fields[0]
		candidate = strings.Trim(candidate, `"'(),`)

		if before, _, ok := strings.Cut(candidate, ":"); ok {
			candidate = before
		}

		if strings.Contains(candidate, "/") || strings.Contains(candidate, ".") {
			paths = append(paths, candidate)
		}
	}

	return paths
}

func boundedUniquePaths(paths []string, limit int) []string {
	if limit <= 0 {
		limit = defaultCodeIntelEnrichmentMaxPaths
	}

	seen := map[string]bool{}
	result := []string{}

	for _, path := range paths {
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == "" || seen[path] {
			continue
		}

		seen[path] = true
		result = append(result, path)

		if len(result) >= limit {
			break
		}
	}

	return result
}

func codeIntelIndexStatus(
	ctx context.Context,
	store *codeintel.Store,
) string {
	stats, err := store.CodeFileIndexStats(ctx)
	if err != nil || stats.ActiveFiles == 0 {
		return codeIntelStatusMissingIndex
	}

	return codeIntelStatusReady
}

func codeIntelFreshnessStatus(
	ctx context.Context,
	store *codeintel.Store,
	root string,
) string {
	stats, err := store.CodeFileIndexStats(ctx)
	if err != nil || stats.ActiveFiles == 0 {
		return codeIntelStatusMissingIndex
	}

	if codeIntelIndexOlderThanHead(ctx, root, stats.LatestIndexedAtUTC) {
		return codeIntelStatusStale
	}

	return codeIntelStatusReady
}

func codeIntelIndexOlderThanHead(
	ctx context.Context,
	root string,
	indexedAt string,
) bool {
	indexedTime, err := parseCodeIntelTime(indexedAt)
	if err != nil {
		return false
	}

	headTime, err := gitHeadCommitTime(ctx, root)
	if err != nil {
		return false
	}

	return indexedTime.Before(headTime)
}

func gitHeadCommitTime(ctx context.Context, root string) (time.Time, error) {
	command := realgit.Command(ctx, false, "-C", root, "log", "-1", "--format=%cI")
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("read git head time: %w", err)
	}

	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(string(output)))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse git head time: %w", err)
	}

	return parsed, nil
}

func parseCodeIntelTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errEmptyCodeIntelTimestamp
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed, nil
	}

	parsed, err = time.Parse(codeIntelTimestampSQLiteLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse code-intel timestamp: %w", err)
	}

	return parsed, nil
}

func codeIntelFreshnessNotice(event Event, root string) string {
	if event.HookEventName != eventPostToolUse ||
		event.ToolName != toolBash ||
		event.ReturnCode() != 0 ||
		!codeIntelFreshnessCommand(event.Command()) {
		return ""
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		defaultCodeIntelEnrichmentTimeout,
	)
	defer cancel()

	store, err := codeintel.OpenReadOnly(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		if !markCodeIntelFreshnessNoticeEmitted(ctx, root) {
			return ""
		}

		return renderCodeIntelFreshnessNotice(codeIntelStatusMissingIndex, root)
	}
	defer store.Close()

	status := codeIntelFreshnessStatus(ctx, store, root)
	if status == codeIntelStatusReady ||
		!markCodeIntelFreshnessNoticeEmitted(ctx, root) {
		return ""
	}

	return renderCodeIntelFreshnessNotice(status, root)
}

func codeIntelFreshnessCommand(command string) bool {
	lower := strings.ToLower(command)

	return strings.Contains(lower, "git commit") ||
		strings.Contains(lower, "git merge") ||
		strings.Contains(lower, "git checkout") ||
		strings.Contains(lower, "git switch") ||
		strings.Contains(lower, "coding-ethos-code-intel")
}

func markCodeIntelFreshnessNoticeEmitted(ctx context.Context, root string) bool {
	head := currentHookGitCommit(ctx, root)
	if head == "" {
		head = codeIntelFreshnessUnknownHead
	}

	stateDir := filepath.Join(root, ".coding-ethos", "state", "freshness-notices")

	err := os.MkdirAll(stateDir, codeIntelFreshnessStateDirMode)
	if err != nil {
		return true
	}

	statePath := filepath.Join(stateDir, head+".notice")

	handle, err := os.OpenFile(
		statePath,
		os.O_RDWR|os.O_CREATE|os.O_EXCL,
		codeIntelFreshnessStateFileMode,
	)
	if err != nil {
		return false
	}
	defer handle.Close()

	_, err = handle.WriteString(time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return true
	}

	return true
}

func currentHookGitCommit(ctx context.Context, root string) string {
	command := realgit.Command(ctx, false, "-C", root, "rev-parse", "--verify", "HEAD")
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

func renderCodeIntelFreshnessNotice(status, root string) string {
	normalizer := hookOutputNormalizer(root)

	return strings.Join([]string{
		"code_intel_freshness:",
		"status: " + toonCell(status),
		"refresh: " + toonCell(codeIntelEnrichmentRefreshCommand),
		"summary: " + toonCell(
			"Code intelligence is not current for this checkout.",
		),
		"cwd: " + toonCell(normalizer.compact(root)),
	}, "\n")
}

func renderCodeIntelEnrichment(enrichment codeIntelEnrichment) string {
	if enrichment.Status == "" {
		return ""
	}

	lines := []string{
		"code_intel_enrichment:",
		"status: " + toonCell(enrichment.Status),
		"refresh: " + toonCell(enrichment.Refresh),
	}
	if enrichment.Reason != "" {
		lines = append(lines, "reason: "+toonCell(enrichment.Reason))
	}

	lines = appendEnrichmentPaths(lines, enrichment.Paths)
	lines = appendEnrichmentSymbols(lines, enrichment.Symbols)
	lines = appendEnrichmentRelated(lines, enrichment.Related)
	lines = appendEnrichmentEvidence(lines, enrichment.Evidence)
	lines = appendEnrichmentNextCalls(lines, enrichment.NextMCPCalls)

	return strings.Join(lines, "\n")
}

func appendEnrichmentPaths(
	lines []string,
	paths []codeIntelEnrichmentPath,
) []string {
	if len(paths) == 0 {
		return lines
	}

	lines = append(
		lines,
		fmt.Sprintf("paths[%d]{path,language,lines,symbols,risk}:", len(paths)),
	)
	for _, path := range paths {
		lines = append(lines, "  "+strings.Join([]string{
			toonCell(path.Path),
			toonCell(path.Language),
			strconv.Itoa(path.Lines),
			strconv.Itoa(path.Symbols),
			toonCell(path.Risk),
		}, ","))
	}

	return lines
}

func appendEnrichmentSymbols(
	lines []string,
	symbols []codeIntelEnrichmentSymbol,
) []string {
	if len(symbols) == 0 {
		return lines
	}

	lines = append(
		lines,
		fmt.Sprintf("symbols[%d]{path,symbol,line,signature}:", len(symbols)),
	)
	for _, symbol := range symbols {
		lines = append(lines, "  "+strings.Join([]string{
			toonCell(symbol.Path),
			toonCell(symbol.Symbol),
			strconv.Itoa(symbol.Line),
			toonCell(symbol.Signature),
		}, ","))
	}

	return lines
}

func appendEnrichmentRelated(
	lines []string,
	related []codeIntelEnrichmentRelated,
) []string {
	if len(related) == 0 {
		return lines
	}

	lines = append(
		lines,
		fmt.Sprintf("related[%d]{path,kind,target,evidence_id}:", len(related)),
	)
	for _, item := range related {
		lines = append(lines, "  "+strings.Join([]string{
			toonCell(item.Path),
			toonCell(item.Kind),
			toonCell(item.Target),
			toonCell(item.EvidenceID),
		}, ","))
	}

	return lines
}

func appendEnrichmentEvidence(
	lines []string,
	evidence []codeIntelEnrichmentEvidence,
) []string {
	if len(evidence) == 0 {
		return lines
	}

	lines = append(
		lines,
		fmt.Sprintf(
			"evidence[%d]{path,policy_id,skill_id,evidence_id,count}:",
			len(evidence),
		),
	)
	for _, item := range evidence {
		lines = append(lines, "  "+strings.Join([]string{
			toonCell(item.Path),
			toonCell(item.PolicyID),
			toonCell(item.SkillID),
			toonCell(item.EvidenceID),
			strconv.Itoa(item.Count),
		}, ","))
	}

	return lines
}

func appendEnrichmentNextCalls(lines, calls []string) []string {
	if len(calls) == 0 {
		return lines
	}

	lines = append(lines, fmt.Sprintf("next_mcp_calls[%d]{call}:", len(calls)))
	for _, call := range calls {
		lines = append(lines, "  "+toonCell(call))
	}

	return lines
}

func codeIntelNextMCPCalls(
	paths []string,
	options codeIntelEnrichmentOptions,
) []string {
	calls := make([]string, 0, 1+len(paths))
	calls = append(
		calls,
		fmt.Sprintf(
			`code_intel_repo_map {"limit":%d,"symbols_per_file":%d}`,
			options.MaxPaths,
			min(options.MaxSymbols, defaultStartupRepoMapSymbolsPerFile),
		),
	)

	for _, path := range paths {
		calls = append(
			calls,
			fmt.Sprintf(`code_intel_context_card {"path":%q}`, path),
		)
	}

	return calls
}

func codeIntelPathRisk(entry codeintel.RepoMapEntry) string {
	switch {
	case entry.StaleReason != "":
		return "stale:" + entry.StaleReason
	case entry.LineCount >= codeIntelLargeFileLineThreshold:
		return "large_file"
	case entry.Symbols >= codeIntelSymbolHotspotThreshold:
		return "symbol_hotspot"
	default:
		return "normal"
	}
}

func codeIntelSignature(rawText string) string {
	for line := range strings.Lines(rawText) {
		line = strings.TrimSpace(line)
		if line != "" {
			return truncateCodeIntelHint(line)
		}
	}

	return ""
}

func truncateCodeIntelHint(value string) string {
	const maxRunes = 88

	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}

	return string(runes[:maxRunes]) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
