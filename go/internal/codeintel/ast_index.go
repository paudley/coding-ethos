// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	bytesPerKiB                   = 1024
	lineCountBufferSizeKiB        = 32
	maxIndexedSourceBytes         = 1 * bytesPerKiB * bytesPerKiB
	maxIndexedSourceLines         = 5000
	maxIndexedSourceChunksPerFile = 2000
)

var errCodeIntelPathNotDirectory = errors.New("path is not a directory")

type ASTIndexer struct {
	store *Store
}

type IndexOptions struct {
	ExcludePatterns []string
}

func NewASTIndexer(store *Store) ASTIndexer {
	return ASTIndexer{store: store}
}

func LoadIndexOptions(root string) (IndexOptions, error) {
	config, err := loadRepoConfig(root)
	if err != nil {
		return IndexOptions{}, err
	}

	patterns := configdata.StringList(
		configdata.GetPath(config, "code_intel.exclude_paths", []any{}),
	)

	return validateIndexOptions(IndexOptions{ExcludePatterns: patterns})
}

func loadRepoConfig(root string) (configdata.Map, error) {
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve code-intel config root: %w", err)
	}

	for _, name := range repoConfigCandidates() {
		path := filepath.Join(resolvedRoot, name)

		config, err := configdata.LoadYAMLMap(path)
		if err == nil {
			return config, nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("load code-intel repo config %s: %w", path, err)
		}
	}

	return configdata.Map{}, nil
}

func repoConfigCandidates() []string {
	return []string{
		"repo_config.yaml",
		"repo_config.yml",
		"code-ethos.repo.yaml",
		"code-ethos.repo.yml",
		"coding-ethos.repo.yaml",
		"coding-ethos.repo.yml",
	}
}

func (indexer ASTIndexer) IndexPaths(
	ctx context.Context,
	root string,
	paths []string,
) (CodeIndexSummary, error) {
	options, err := LoadIndexOptions(root)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	return indexer.IndexPathsWithOptions(ctx, root, paths, options)
}

//nolint:gocyclo,cyclop,funlen // Coordinates the repository traversal gate.
func (indexer ASTIndexer) IndexPathsWithOptions(
	ctx context.Context,
	root string,
	paths []string,
	options IndexOptions,
) (CodeIndexSummary, error) {
	options, err := validateIndexOptions(options)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}

	if len(paths) == 0 {
		paths = []string{"."}
	}

	summary := CodeIndexSummary{}
	ignoreMatcher := newGitIgnoreMatcher(ctx, root)

	existingFiles, err := indexer.store.CodeFilesByPath(ctx)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	for _, inputPath := range paths {
		path := inputPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			return CodeIndexSummary{}, fmt.Errorf(
				"stat index path %q: %w",
				inputPath,
				statErr,
			)
		}

		if info.IsDir() {
			err = indexer.indexDir(
				ctx,
				root,
				path,
				ignoreMatcher,
				existingFiles,
				options,
				&summary,
			)
			if err != nil {
				return CodeIndexSummary{}, err
			}

			continue
		}

		inlineErr0 := indexer.indexFile(
			ctx,
			root,
			path,
			info,
			ignoreMatcher,
			existingFiles,
			options,
			&summary,
		)
		if inlineErr0 != nil {
			return CodeIndexSummary{}, inlineErr0
		}
	}

	deleted, err := indexer.store.MarkMissingCodeFilesDeleted(ctx, root, paths)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	summary.Deleted = deleted

	ignored, err := indexer.store.MarkIgnoredCodeFilesDeleted(
		ctx,
		func(path string) bool {
			absolutePath := filepath.Join(root, filepath.FromSlash(path))

			return pathHasSkippedDir(path) ||
				excludedByConfig(path, options.ExcludePatterns) ||
				ignoreMatcher.ignoredFile(ctx, absolutePath)
		},
	)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	summary.Deleted = append(summary.Deleted, ignored...)

	return summary, nil
}

// IndexDirectoryChildren refreshes only direct child source files under dir.
func (indexer ASTIndexer) IndexDirectoryChildren(
	ctx context.Context,
	root string,
	dir string,
) (CodeIndexSummary, error) {
	root, path, err := indexDirectoryTarget(root, dir)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	ignoreMatcher := newGitIgnoreMatcher(ctx, root)

	options, err := LoadIndexOptions(root)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	existingFiles, err := indexer.store.CodeFilesByPath(ctx)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	summary := CodeIndexSummary{}

	deleted, err := indexer.markIgnoredDirectoryChildrenDeleted(
		ctx,
		root,
		path,
		ignoreMatcher,
		options,
	)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	summary.Deleted = deleted

	entries, err := os.ReadDir(path)
	if err != nil {
		return CodeIndexSummary{}, fmt.Errorf("read index directory %q: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		childPath := filepath.Join(path, entry.Name())

		fileInfo, statErr := entry.Info()
		if statErr != nil {
			return CodeIndexSummary{}, fmt.Errorf("stat indexed file %q: %w", childPath, statErr)
		}

		err = indexer.indexFile(
			ctx,
			root,
			childPath,
			fileInfo,
			ignoreMatcher,
			existingFiles,
			options,
			&summary,
		)
		if err != nil {
			return CodeIndexSummary{}, err
		}
	}

	return summary, nil
}

func (indexer ASTIndexer) markIgnoredDirectoryChildrenDeleted(
	ctx context.Context,
	root string,
	dir string,
	ignoreMatcher gitIgnoreMatcher,
	options IndexOptions,
) ([]string, error) {
	relativeDir, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, fmt.Errorf("relativize indexed directory %q: %w", dir, err)
	}

	relativeDir = filepath.ToSlash(relativeDir)

	return indexer.store.MarkIgnoredCodeFilesDeleted(
		ctx,
		func(path string) bool {
			if !directChildCodeFilePath(relativeDir, path) {
				return false
			}

			absolutePath := filepath.Join(root, filepath.FromSlash(path))

			return pathHasSkippedDir(path) ||
				excludedByConfig(path, options.ExcludePatterns) ||
				ignoreMatcher.ignoredFile(ctx, absolutePath)
		},
	)
}

func directChildCodeFilePath(dir, path string) bool {
	dir = filepath.ToSlash(filepath.Clean(dir))
	path = filepath.ToSlash(filepath.Clean(path))

	if dir == "." {
		return path != "." && !strings.Contains(path, "/")
	}

	relativePath, found := strings.CutPrefix(path, strings.TrimSuffix(dir, "/")+"/")
	if !found || relativePath == "" {
		return false
	}

	return !strings.Contains(relativePath, "/")
}

func indexDirectoryTarget(root, dir string) (string, string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}

	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	path := dir
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("stat index directory %q: %w", dir, err)
	}

	if !info.IsDir() {
		return "", "", fmt.Errorf(
			"index directory %q: %w",
			dir,
			errCodeIntelPathNotDirectory,
		)
	}

	return root, path, nil
}

func (indexer ASTIndexer) indexDir(
	ctx context.Context,
	root string,
	dir string,
	ignoreMatcher gitIgnoreMatcher,
	existingFiles map[string]CodeFile,
	options IndexOptions,
	summary *CodeIndexSummary,
) error {
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk code-intel AST source path %s: %w", path, err)
		}

		if entry.IsDir() {
			relativePath, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return fmt.Errorf("relativize indexed directory %q: %w", path, relErr)
			}

			relativePath = filepath.ToSlash(relativePath)
			if relativePath != "." &&
				(shouldSkipDir(entry.Name()) ||
					pathHasSkippedDir(relativePath) ||
					ignoreMatcher.ignoredDir(ctx, path) ||
					excludedByConfig(relativePath, options.ExcludePatterns)) {
				return filepath.SkipDir
			}

			return nil
		}

		info, statErr := entry.Info()
		if statErr != nil {
			return fmt.Errorf("stat indexed file %q: %w", path, statErr)
		}

		return indexer.indexFile(
			ctx,
			root,
			path,
			info,
			ignoreMatcher,
			existingFiles,
			options,
			summary,
		)
	})
	if err != nil {
		return fmt.Errorf("walk code-intel AST directory %s: %w", dir, err)
	}

	return nil
}

//nolint:gocognit,gocyclo,cyclop,funlen // Ordered gates stay together.
func (indexer ASTIndexer) indexFile(
	ctx context.Context,
	root string,
	path string,
	info os.FileInfo,
	ignoreMatcher gitIgnoreMatcher,
	existingFiles map[string]CodeFile,
	options IndexOptions,
	summary *CodeIndexSummary,
) error {
	language, ok := astfacts.LanguageForPath(path)
	if !ok {
		return nil
	}

	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("relativize indexed file %q: %w", path, err)
	}

	relativePath = filepath.ToSlash(relativePath)
	if ignoreMatcher.ignoredFile(ctx, path) ||
		pathHasSkippedDir(relativePath) ||
		excludedByConfig(relativePath, options.ExcludePatterns) {
		return nil
	}

	existing, found := existingFiles[relativePath]

	parserName, parserVersion := astfacts.ParserMetadataForLanguage(language)
	sourceModTime := formatSourceModTime(info.ModTime())

	if found &&
		inactiveCodeFileFresh(existing, parserName, parserVersion, sourceModTime, info) {
		summary.Skipped = append(summary.Skipped, relativePath)

		return nil
	}

	if found && codeFileFresh(existing, parserName, parserVersion, sourceModTime, info) {
		summary.Skipped = append(summary.Skipped, relativePath)

		return nil
	}

	if info.Size() > maxIndexedSourceBytes {
		inactiveFile := inactiveCodeFile(
			relativePath,
			language,
			"too_large",
			stableID(
				"too-large-code-file",
				relativePath,
				sourceModTime,
				strconv.FormatInt(info.Size(), 10),
			),
			parserName,
			parserVersion,
			sourceModTime,
			info,
			0,
		)

		err = indexer.store.ReplaceCodeFileIndex(ctx, inactiveFile, nil, nil)
		if err != nil {
			return err
		}

		existingFiles[relativePath] = inactiveFile

		summary.Skipped = append(summary.Skipped, relativePath)

		return nil
	}

	lineCount, tooManyLines, err := countSourceLinesUpTo(
		ctx,
		path,
		maxIndexedSourceLines,
	)
	if err != nil {
		return err
	}

	if tooManyLines {
		inactiveFile := inactiveCodeFile(
			relativePath,
			language,
			"too_many_lines",
			stableID(
				"too-many-lines-code-file",
				relativePath,
				sourceModTime,
				strconv.FormatInt(info.Size(), 10),
			),
			parserName,
			parserVersion,
			sourceModTime,
			info,
			lineCount,
		)

		err = indexer.store.ReplaceCodeFileIndex(ctx, inactiveFile, nil, nil)
		if err != nil {
			return err
		}

		existingFiles[relativePath] = inactiveFile

		summary.Skipped = append(summary.Skipped, relativePath)

		return nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read indexed file %q: %w", path, err)
	}

	hash := astfacts.ContentHash(contents)

	if found &&
		existing.ContentHash == hash &&
		existing.ParserName == parserName &&
		existing.ParserVersion == parserVersion &&
		existing.StaleReason == "" {
		summary.Skipped = append(summary.Skipped, relativePath)

		return nil
	}

	parsed, _, err := astfacts.Analyze(relativePath, contents)
	if err != nil {
		return fmt.Errorf("analyze AST facts for %s: %w", relativePath, err)
	}

	chunks := codeChunksFromSymbols(parsed.Symbols)

	chunks = attachParentChunks(chunks)
	if len(chunks) > maxIndexedSourceChunksPerFile {
		inactiveFile := inactiveCodeFile(
			relativePath,
			language,
			"too_many_chunks",
			parsed.ContentHash,
			parserName,
			parserVersion,
			sourceModTime,
			info,
			parsed.LineCount,
		)

		err = indexer.store.ReplaceCodeFileIndex(ctx, inactiveFile, nil, nil)
		if err != nil {
			return err
		}

		existingFiles[relativePath] = inactiveFile

		summary.Skipped = append(summary.Skipped, relativePath)

		return nil
	}

	edges := codeEdgesFromParsedFile(relativePath, parsed, chunks)

	file := CodeFile{
		Path:             relativePath,
		Language:         language,
		ContentHash:      parsed.ContentHash,
		ParserName:       parserName,
		ParserVersion:    parserVersion,
		SourceModTimeUTC: sourceModTime,
		SizeBytes:        len(contents),
		LineCount:        parsed.LineCount,
		IndexedAtUTC:     time.Now().UTC().Format(time.RFC3339),
	}

	inlineErr1 := indexer.store.ReplaceCodeFileIndex(ctx, file, chunks, edges)
	if inlineErr1 != nil {
		return inlineErr1
	}

	existingFiles[relativePath] = file

	summary.FilesIndexed++
	summary.ChunksIndexed += len(chunks)

	return nil
}

func codeFileFresh(
	existing CodeFile,
	parserName string,
	parserVersion string,
	sourceModTime string,
	info os.FileInfo,
) bool {
	return existing.SourceModTimeUTC != "" &&
		existing.SourceModTimeUTC == sourceModTime &&
		existing.SizeBytes == int(info.Size()) &&
		existing.ParserName == parserName &&
		existing.ParserVersion == parserVersion &&
		existing.StaleReason == ""
}

func inactiveCodeFileFresh(
	existing CodeFile,
	parserName string,
	parserVersion string,
	sourceModTime string,
	info os.FileInfo,
) bool {
	return (existing.StaleReason == "too_large" ||
		existing.StaleReason == "too_many_lines" ||
		existing.StaleReason == "too_many_chunks") &&
		existing.SourceModTimeUTC == sourceModTime &&
		existing.SizeBytes == int(info.Size()) &&
		existing.ParserName == parserName &&
		existing.ParserVersion == parserVersion
}

func formatSourceModTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func countSourceLinesUpTo(
	ctx context.Context,
	path string,
	maxLines int,
) (int, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, false, fmt.Errorf("open indexed file for line count %q: %w", path, err)
	}
	defer file.Close()

	lineCount := 0

	buffer := make([]byte, lineCountBufferSizeKiB*bytesPerKiB)

	for {
		err = ctx.Err()
		if err != nil {
			return 0, false, fmt.Errorf("count indexed file lines %q: %w", path, err)
		}

		read, readErr := file.Read(buffer)
		if read > 0 {
			lineCount += bytes.Count(buffer[:read], []byte{'\n'})
			if lineCount > maxLines {
				return lineCount, true, nil
			}
		}

		if readErr == nil {
			continue
		}

		if errors.Is(readErr, io.EOF) {
			return lineCount, false, nil
		}

		return 0, false, fmt.Errorf("count indexed file lines %q: %w", path, readErr)
	}
}

func inactiveCodeFile(
	relativePath string,
	language string,
	reason string,
	contentHash string,
	parserName string,
	parserVersion string,
	sourceModTime string,
	info os.FileInfo,
	lineCount int,
) CodeFile {
	indexedAt := time.Now().UTC().Format(time.RFC3339)

	return CodeFile{
		Path:             relativePath,
		Language:         language,
		ContentHash:      contentHash,
		ParserName:       parserName,
		ParserVersion:    parserVersion,
		SourceModTimeUTC: sourceModTime,
		SizeBytes:        int(info.Size()),
		LineCount:        lineCount,
		IndexedAtUTC:     indexedAt,
		DeletedAtUTC:     indexedAt,
		StaleReason:      reason,
	}
}

type gitIgnoreMatcher struct {
	allowedPath map[string]bool
	allowedDir  map[string]bool
	root        string
	active      bool
}

func newGitIgnoreMatcher(ctx context.Context, root string) gitIgnoreMatcher {
	allowedPaths, allowedDirs := gitTrackedAndUnignoredPaths(ctx, root)

	return gitIgnoreMatcher{
		root:        root,
		active:      gitWorkTreeAvailable(ctx, root),
		allowedPath: allowedPaths,
		allowedDir:  allowedDirs,
	}
}

func gitTrackedAndUnignoredPaths(
	ctx context.Context,
	root string,
) (map[string]bool, map[string]bool) {
	command := realgit.Command(
		ctx,
		false,
		"-C",
		root,
		"ls-files",
		"-co",
		"--exclude-standard",
		"-z",
	)
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()
	if err != nil {
		return nil, nil
	}

	paths := map[string]bool{}
	dirs := map[string]bool{"": true}

	for rawPath := range bytes.SplitSeq(output, []byte{0}) {
		if len(rawPath) == 0 {
			continue
		}

		path := filepath.ToSlash(string(rawPath))
		paths[path] = true

		dir := filepath.ToSlash(filepath.Dir(path))
		for dir != "." && dir != "" {
			dirs[dir] = true
			dir = filepath.ToSlash(filepath.Dir(dir))
		}
	}

	return paths, dirs
}

func gitWorkTreeAvailable(ctx context.Context, root string) bool {
	command := realgit.Command(
		ctx,
		false,
		"-C",
		root,
		"rev-parse",
		"--is-inside-work-tree",
	)
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()

	return err == nil && strings.TrimSpace(string(output)) == "true"
}

func (matcher gitIgnoreMatcher) ignoredFile(ctx context.Context, path string) bool {
	if !matcher.active {
		return false
	}

	relativePath, err := filepath.Rel(matcher.root, path)
	if err != nil {
		return false
	}

	relativePath = filepath.ToSlash(relativePath)
	if matcher.allowedPath != nil {
		return !matcher.allowedPath[relativePath]
	}

	return matcher.gitCheckIgnored(ctx, relativePath)
}

func (matcher gitIgnoreMatcher) ignoredDir(ctx context.Context, path string) bool {
	if !matcher.active {
		return false
	}

	relativePath, err := filepath.Rel(matcher.root, path)
	if err != nil {
		return false
	}

	relativePath = filepath.ToSlash(filepath.Clean(relativePath))
	if relativePath == "." {
		return false
	}

	if matcher.allowedDir != nil && !matcher.allowedDir[relativePath] {
		return true
	}

	return matcher.gitCheckIgnored(ctx, relativePath)
}

func (matcher gitIgnoreMatcher) gitCheckIgnored(
	ctx context.Context,
	relativePath string,
) bool {
	command := realgit.Command(
		ctx,
		false,
		"-C",
		matcher.root,
		"check-ignore",
		"--quiet",
		"--no-index",
		relativePath,
	)
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	err := command.Run()
	if err == nil {
		return true
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false
	}

	return false
}

func codeChunksFromSymbols(symbols []astfacts.Symbol) []CodeChunk {
	chunks := make([]CodeChunk, 0, len(symbols))
	for _, symbol := range symbols {
		chunks = append(chunks, codeChunkFromSymbol(symbol))
	}

	return chunks
}

func codeChunkFromSymbol(symbol astfacts.Symbol) CodeChunk {
	search := strings.Join(compactStrings([]string{
		symbol.Path,
		symbol.Language,
		symbol.NodeKind,
		symbol.SymbolKind,
		symbol.SymbolName,
		symbol.SymbolPath,
		symbol.RawText,
	}), "\n")

	parentSymbolPath := parentSymbolPath(symbol.SymbolPath)

	return CodeChunk{
		ID: stableID(
			"code-chunk",
			symbol.Path,
			symbol.Language,
			symbol.NodeKind,
			symbol.SymbolPath,
			symbol.ContentHash,
		),
		Path:             symbol.Path,
		Language:         symbol.Language,
		NodeKind:         symbol.NodeKind,
		SymbolKind:       symbol.SymbolKind,
		SymbolName:       symbol.SymbolName,
		SymbolPath:       symbol.SymbolPath,
		ParentSymbolPath: parentSymbolPath,
		StartByte:        symbol.StartByte,
		EndByte:          symbol.EndByte,
		StartLine:        symbol.StartLine,
		EndLine:          symbol.EndLine,
		ContentHash:      symbol.ContentHash,
		NormalizedHash:   symbol.NormalizedHash,
		MinHashSig:       symbol.MinHashSig,
		SearchText:       search,
		RawText:          symbol.RawText,
	}
}

func attachParentChunks(chunks []CodeChunk) []CodeChunk {
	bySymbolPath := map[string]CodeChunk{}

	for _, chunk := range chunks {
		if chunk.SymbolPath != "" {
			bySymbolPath[chunk.SymbolPath] = chunk
		}
	}

	for index := range chunks {
		if parent, ok := bySymbolPath[chunks[index].ParentSymbolPath]; ok {
			chunks[index].ParentChunkID = parent.ID
		}
	}

	return chunks
}

func parentSymbolPath(symbolPath string) string {
	parts := strings.Split(symbolPath, ".")
	if len(parts) <= 1 {
		return ""
	}

	return strings.Join(parts[:len(parts)-1], ".")
}

func codeEdgesFromParsedFile(
	path string,
	parsed astfacts.File,
	chunks []CodeChunk,
) []CodeEdge {
	edges := []CodeEdge{}

	for _, chunk := range chunks {
		if chunk.ParentChunkID != "" {
			edges = append(edges, CodeEdge{
				ID: stableID(
					"code-edge",
					"contains",
					path,
					chunk.ParentChunkID,
					chunk.ID,
				),
				Kind:             "contains",
				Path:             path,
				SourceChunkID:    chunk.ParentChunkID,
				TargetPath:       path,
				TargetChunkID:    chunk.ID,
				TargetSymbolPath: chunk.SymbolPath,
				TargetName:       chunk.SymbolName,
			})
		}
	}

	edges = append(edges, importEdges(path, parsed.Imports)...)
	edges = append(edges, referenceEdges(path, parsed.Symbols, chunks)...)
	edges = append(edges, callEdges(path, parsed.Symbols, chunks)...)
	edges = append(edges, inheritanceEdges(path, parsed.Symbols, chunks)...)
	edges = append(edges, testEdges(path, parsed.Symbols, chunks)...)
	edges = append(edges, documentationEdges(path, parsed.Symbols, chunks)...)

	return dedupeCodeEdges(edges)
}

func importEdges(path string, imports []astfacts.Import) []CodeEdge {
	edges := make([]CodeEdge, 0, len(imports))
	for _, imported := range imports {
		if imported.Target == "" {
			continue
		}

		edges = append(edges, CodeEdge{
			ID: stableID(
				"code-edge",
				"imports",
				path,
				imported.Target,
				imported.RawText,
			),
			Kind:       "imports",
			Path:       path,
			TargetPath: imported.Target,
			TargetName: imported.Target,
			RawText:    imported.RawText,
		})
	}

	return edges
}

func callEdges(
	path string,
	symbols []astfacts.Symbol,
	chunks []CodeChunk,
) []CodeEdge {
	chunksBySymbolPath := map[string]CodeChunk{}
	targetsByName := map[string][]CodeChunk{}

	for _, chunk := range chunks {
		chunksBySymbolPath[chunk.SymbolPath] = chunk
		if chunk.SymbolName != "" {
			targetsByName[chunk.SymbolName] = append(
				targetsByName[chunk.SymbolName],
				chunk,
			)
		}
	}

	edges := []CodeEdge{}

	for _, symbol := range symbols {
		source, ok := chunksBySymbolPath[symbol.SymbolPath]
		if !ok {
			continue
		}

		for _, name := range symbol.CallNames {
			// Intra-file calls
			for _, target := range targetsByName[name] {
				if source.ID == target.ID {
					continue
				}

				edges = append(edges, CodeEdge{
					ID: stableID(
						"code-edge",
						"calls",
						path,
						source.ID,
						target.ID,
					),
					Kind:             "calls",
					Path:             path,
					SourceChunkID:    source.ID,
					TargetPath:       path,
					TargetChunkID:    target.ID,
					TargetSymbolPath: target.SymbolPath,
					TargetName:       target.SymbolName,
				})
			}

			// Cross-file call intent (symbolic)
			// If no local target, we still record the call name
			if len(targetsByName[name]) == 0 {
				edges = append(edges, CodeEdge{
					ID: stableID(
						"code-edge",
						"calls-symbolic",
						path,
						source.ID,
						name,
					),
					Kind:          "calls",
					Path:          path,
					SourceChunkID: source.ID,
					TargetName:    name,
				})
			}
		}
	}

	return edges
}

func inheritanceEdges(
	path string,
	symbols []astfacts.Symbol,
	chunks []CodeChunk,
) []CodeEdge {
	chunksBySymbolPath := map[string]CodeChunk{}
	targetsByName := map[string][]CodeChunk{}

	for _, chunk := range chunks {
		chunksBySymbolPath[chunk.SymbolPath] = chunk
		if chunk.SymbolName != "" {
			targetsByName[chunk.SymbolName] = append(
				targetsByName[chunk.SymbolName],
				chunk,
			)
		}
	}

	edges := []CodeEdge{}

	for _, symbol := range symbols {
		source, ok := chunksBySymbolPath[symbol.SymbolPath]
		if !ok {
			continue
		}

		for _, name := range symbol.BaseNames {
			// Intra-file inheritance
			for _, target := range targetsByName[name] {
				edges = append(edges, CodeEdge{
					ID: stableID(
						"code-edge",
						"inherits",
						path,
						source.ID,
						target.ID,
					),
					Kind:             "inherits",
					Path:             path,
					SourceChunkID:    source.ID,
					TargetPath:       path,
					TargetChunkID:    target.ID,
					TargetSymbolPath: target.SymbolPath,
					TargetName:       target.SymbolName,
				})
			}

			// Cross-file inheritance intent
			if len(targetsByName[name]) == 0 {
				edges = append(edges, CodeEdge{
					ID: stableID(
						"code-edge",
						"inherits-symbolic",
						path,
						source.ID,
						name,
					),
					Kind:          "inherits",
					Path:          path,
					SourceChunkID: source.ID,
					TargetName:    name,
				})
			}
		}
	}

	return edges
}

func testEdges(
	path string,
	symbols []astfacts.Symbol,
	chunks []CodeChunk,
) []CodeEdge {
	isTestFile := strings.HasSuffix(path, "_test.go") ||
		strings.HasSuffix(path, "_test.py") ||
		strings.HasPrefix(filepath.Base(path), "test_")

	if !isTestFile {
		return nil
	}

	chunksBySymbolPath := map[string]CodeChunk{}
	for _, chunk := range chunks {
		chunksBySymbolPath[chunk.SymbolPath] = chunk
	}

	edges := []CodeEdge{}

	for _, symbol := range symbols {
		if !strings.HasPrefix(symbol.SymbolName, "Test") {
			continue
		}

		source, ok := chunksBySymbolPath[symbol.SymbolPath]
		if !ok {
			continue
		}

		// Heuristic: TestFoo verifies Foo
		targetName := strings.TrimPrefix(symbol.SymbolName, "Test")
		if targetName == "" {
			continue
		}

		// Also record the verifying intent
		edges = append(edges, CodeEdge{
			ID: stableID(
				"code-edge",
				"verifies",
				path,
				source.ID,
				targetName,
			),
			Kind:          "verifies",
			Path:          path,
			SourceChunkID: source.ID,
			TargetName:    targetName,
		})
	}

	return edges
}

func documentationEdges(
	path string,
	symbols []astfacts.Symbol,
	chunks []CodeChunk,
) []CodeEdge {
	if !strings.HasSuffix(path, ".md") {
		return nil
	}

	chunksBySymbolPath := map[string]CodeChunk{}
	for _, chunk := range chunks {
		chunksBySymbolPath[chunk.SymbolPath] = chunk
	}

	edges := []CodeEdge{}

	for _, symbol := range symbols {
		if symbol.SymbolKind != "heading" {
			continue
		}

		source, ok := chunksBySymbolPath[symbol.SymbolPath]
		if !ok {
			continue
		}

		// Heuristic: Link headings to mentioned identifiers in the heading text
		// (Assuming the heading text itself might name a symbol)
		edges = append(edges, CodeEdge{
			ID: stableID(
				"code-edge",
				"documents",
				path,
				source.ID,
				symbol.SymbolName,
			),
			Kind:          "documents",
			Path:          path,
			SourceChunkID: source.ID,
			TargetName:    symbol.SymbolName,
		})
	}

	return edges
}

func referenceEdges(
	path string,
	symbols []astfacts.Symbol,
	chunks []CodeChunk,
) []CodeEdge {
	chunksBySymbolPath := map[string]CodeChunk{}
	targetsByName := map[string][]CodeChunk{}

	for _, chunk := range chunks {
		chunksBySymbolPath[chunk.SymbolPath] = chunk
		if chunk.SymbolName != "" {
			targetsByName[chunk.SymbolName] = append(
				targetsByName[chunk.SymbolName],
				chunk,
			)
		}
	}

	edges := []CodeEdge{}

	for _, symbol := range symbols {
		source, ok := chunksBySymbolPath[symbol.SymbolPath]
		if !ok {
			continue
		}

		for _, name := range symbol.ReferencedNames {
			for _, target := range targetsByName[name] {
				if source.ID == target.ID {
					continue
				}

				edges = append(edges, CodeEdge{
					ID: stableID(
						"code-edge",
						"references",
						path,
						source.ID,
						target.ID,
					),
					Kind:             "references",
					Path:             path,
					SourceChunkID:    source.ID,
					TargetPath:       path,
					TargetChunkID:    target.ID,
					TargetSymbolPath: target.SymbolPath,
					TargetName:       target.SymbolName,
				})
			}
		}
	}

	return edges
}

func dedupeCodeEdges(edges []CodeEdge) []CodeEdge {
	seen := map[string]bool{}
	deduped := []CodeEdge{}

	for _, edge := range edges {
		if edge.ID == "" || seen[edge.ID] {
			continue
		}

		seen[edge.ID] = true
		deduped = append(deduped, edge)
	}

	slices.SortFunc(deduped, func(left, right CodeEdge) int {
		return strings.Compare(left.ID, right.ID)
	})

	return deduped
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git",
		".coding-ethos",
		".code-ethos",
		".hg",
		".svn",
		".tox",
		".venv",
		".wolf",
		".pytest_cache",
		".mypy_cache",
		".ruff_cache",
		"node_modules",
		"build",
		".cache":
		return true
	default:
		return false
	}
}

func validateIndexOptions(options IndexOptions) (IndexOptions, error) {
	patterns := make([]string, 0, len(options.ExcludePatterns))
	for _, pattern := range options.ExcludePatterns {
		normalizedPattern := normalizeIndexExcludePattern(pattern)
		if normalizedPattern == "" {
			continue
		}

		err := validateIndexExcludePattern(normalizedPattern)
		if err != nil {
			return IndexOptions{}, err
		}

		patterns = append(patterns, normalizedPattern)
	}

	return IndexOptions{ExcludePatterns: patterns}, nil
}

func normalizeIndexExcludePattern(pattern string) string {
	return strings.TrimPrefix(
		strings.TrimSpace(filepath.ToSlash(pattern)),
		"./",
	)
}

func validateIndexExcludePattern(pattern string) error {
	_, err := doublestar.Match(pattern, "a")
	if err != nil {
		return fmt.Errorf("invalid code_intel.exclude_paths pattern %q: %w", pattern, err)
	}

	prefix := directoryExcludePrefix(pattern)
	if prefix == "" {
		return nil
	}

	_, err = doublestar.Match(prefix, "a")
	if err != nil {
		return fmt.Errorf("invalid code_intel.exclude_paths pattern %q: %w", pattern, err)
	}

	return nil
}

func excludedByConfig(path string, patterns []string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))
	if normalized == "." {
		return false
	}

	for _, pattern := range patterns {
		if pathMatchesConfiguredPattern(normalized, pattern) {
			return true
		}
	}

	return false
}

func pathMatchesConfiguredPattern(path, pattern string) bool {
	normalizedPattern := normalizeIndexExcludePattern(pattern)
	if normalizedPattern == "" {
		return false
	}

	matched, err := doublestar.Match(normalizedPattern, path)
	if err == nil && matched {
		return true
	}

	prefix := directoryExcludePrefix(normalizedPattern)
	if prefix == "" {
		return false
	}

	matched, err = doublestar.Match(prefix, path)

	return err == nil && matched
}

func directoryExcludePrefix(pattern string) string {
	if !strings.HasSuffix(pattern, "/**") {
		return ""
	}

	prefix := strings.TrimSuffix(pattern, "/**")

	return strings.TrimSuffix(prefix, "/")
}
