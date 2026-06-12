// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

const frontMatterMinimumLineCount = 2

type DecisionImportSummary struct {
	Skipped           []string         `json:"skipped,omitempty"`
	Imported          []DecisionRecord `json:"imported,omitempty"`
	PathsScanned      int              `json:"paths_scanned"`
	FilesScanned      int              `json:"files_scanned"`
	DecisionsImported int              `json:"decisions_imported"`
}

type decisionFrontMatter struct {
	Title               string                   `yaml:"title"`
	Status              string                   `yaml:"status"`
	Rationale           string                   `yaml:"rationale"`
	Alternatives        string                   `yaml:"alternatives"`
	Author              string                   `yaml:"author"`
	RecordedAtUTC       string                   `yaml:"recorded_at_utc"`
	UpdatedAtUTC        string                   `yaml:"updated_at_utc"`
	Tags                any                      `yaml:"tags"`
	AffectedPaths       []string                 `yaml:"affected_paths"`
	AffectedFiles       []string                 `yaml:"affected_files"`
	AffectedModules     []string                 `yaml:"affected_modules"`
	AffectedSymbols     []decisionFrontMatterRef `yaml:"affected_symbols"`
	CodingEthosDecision bool                     `yaml:"coding_ethos_decision"`
}

type decisionFrontMatterRef struct {
	Path       string `yaml:"path"`
	SymbolPath string `yaml:"symbol_path"`
	Symbol     string `yaml:"symbol"`
}

func DefaultDecisionImportPaths() []string {
	return []string{
		"adr",
		"docs/adr",
		"docs/decisions",
		".coding-ethos/decisions",
	}
}

func (store *Store) ImportDecisionRecords(
	ctx context.Context,
	root string,
	paths []string,
) (DecisionImportSummary, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}

	if len(paths) == 0 {
		paths = DefaultDecisionImportPaths()
	}

	options, err := LoadIndexOptions(root)
	if err != nil {
		return DecisionImportSummary{}, err
	}

	importer := decisionImporter{
		store:  store,
		root:   root,
		gate:   newGitIgnoreMatcher(ctx, root),
		config: options,
	}

	retained := map[string]struct{}{}
	scopes := make([]decisionImportPruneScope, 0, len(paths))
	summary := DecisionImportSummary{PathsScanned: len(paths)}

	for _, inputPath := range paths {
		if scope, ok := decisionImportScope(root, inputPath); ok {
			scopes = append(scopes, scope)
		}

		err = importer.importPath(ctx, inputPath, retained, &summary)
		if err != nil {
			return DecisionImportSummary{}, err
		}
	}

	err = importer.store.pruneSourceDecisionRecords(
		ctx,
		DecisionSourceDocument,
		retained,
		scopes,
	)
	if err != nil {
		return DecisionImportSummary{}, err
	}

	return summary, nil
}

type decisionImporter struct {
	store  *Store
	root   string
	gate   gitIgnoreMatcher
	config IndexOptions
}

func (importer decisionImporter) importPath(
	ctx context.Context,
	inputPath string,
	retained map[string]struct{},
	summary *DecisionImportSummary,
) error {
	path := strings.TrimSpace(inputPath)
	if path == "" {
		return nil
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(importer.root, path)
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("stat decision import path %q: %w", inputPath, err)
	}

	if info.IsDir() {
		err = filepath.WalkDir(
			path,
			func(currentPath string, entry fs.DirEntry, walkErr error) error {
				return importer.importWalkEntry(
					ctx,
					path,
					currentPath,
					entry,
					walkErr,
					retained,
					summary,
				)
			},
		)
		if err != nil {
			return fmt.Errorf("walk decision import path %q: %w", inputPath, err)
		}

		return nil
	}

	return importer.importFile(ctx, path, info, retained, summary)
}

func (importer decisionImporter) importWalkEntry(
	ctx context.Context,
	rootPath string,
	currentPath string,
	entry fs.DirEntry,
	walkErr error,
	retained map[string]struct{},
	summary *DecisionImportSummary,
) error {
	if walkErr != nil {
		return fmt.Errorf("walk decision import path %q: %w", currentPath, walkErr)
	}

	if entry.IsDir() {
		if currentPath != rootPath && importer.skipsDir(ctx, currentPath) {
			return filepath.SkipDir
		}

		return nil
	}

	fileInfo, err := entry.Info()
	if err != nil {
		return fmt.Errorf("stat decision import file %q: %w", currentPath, err)
	}

	return importer.importFile(ctx, currentPath, fileInfo, retained, summary)
}

func (importer decisionImporter) skipsDir(ctx context.Context, path string) bool {
	relative, ok := decisionRelativePath(importer.root, path)
	if !ok {
		return true
	}

	return importer.skipsRelativeDir(ctx, path, relative)
}

func (importer decisionImporter) skipsRelativeDir(
	ctx context.Context,
	path string,
	relative string,
) bool {
	if isCodingEthosDecisionPath(relative) {
		return false
	}

	return pathHasSkippedDir(relative) ||
		importer.excluded(relative) ||
		importer.gate.ignoredDir(ctx, path)
}

func (importer decisionImporter) importFile(
	ctx context.Context,
	path string,
	info os.FileInfo,
	retained map[string]struct{},
	summary *DecisionImportSummary,
) error {
	relative, ok := decisionRelativePath(importer.root, path)
	if !ok {
		return nil
	}

	if !importer.importsDecisionFile(ctx, path, relative, info) {
		summary.Skipped = append(summary.Skipped, relative)

		return nil
	}

	retained[relative] = struct{}{}

	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read decision file %s: %w", relative, err)
	}

	return importer.importDecisionPayload(ctx, relative, payload, summary)
}

func (importer decisionImporter) importsDecisionFile(
	ctx context.Context,
	path string,
	relative string,
	info os.FileInfo,
) bool {
	if !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".md") {
		return false
	}

	if isCodingEthosDecisionPath(relative) {
		return true
	}

	return !pathHasSkippedDir(relative) &&
		!importer.excluded(relative) &&
		!importer.gate.ignoredFile(ctx, path)
}

func (importer decisionImporter) importDecisionPayload(
	ctx context.Context,
	relative string,
	payload []byte,
	summary *DecisionImportSummary,
) error {
	summary.FilesScanned++

	decision, found, err := ParseDecisionDocument(relative, payload)
	if err != nil {
		return err
	}

	if !found {
		err = importer.store.ReplaceSourceDecisions(
			ctx,
			DecisionSourceDocument,
			relative,
			nil,
		)
		if err != nil {
			return err
		}

		return nil
	}

	return importer.importDecisionRecord(ctx, relative, decision, summary)
}

func (importer decisionImporter) importDecisionRecord(
	ctx context.Context,
	relative string,
	decision DecisionRecord,
	summary *DecisionImportSummary,
) error {
	err := importer.store.ReplaceSourceDecisions(
		ctx,
		DecisionSourceDocument,
		relative,
		[]DecisionRecord{decision},
	)
	if err != nil {
		return err
	}

	summary.DecisionsImported++
	summary.Imported = append(summary.Imported, decision)

	return nil
}

func (importer decisionImporter) excluded(relative string) bool {
	return excludedByConfig(relative, importer.config.ExcludePatterns)
}

func isCodingEthosDecisionPath(relative string) bool {
	return relative == ".coding-ethos/decisions" ||
		strings.HasPrefix(relative, ".coding-ethos/decisions/")
}

func ParseDecisionDocument(
	path string,
	payload []byte,
) (DecisionRecord, bool, error) {
	frontMatter, body, found := splitYAMLFrontMatter(payload)
	if !found {
		return DecisionRecord{}, false, nil
	}

	var decoded decisionFrontMatter

	err := yaml.Unmarshal(frontMatter, &decoded)
	if err != nil {
		return DecisionRecord{}, false, fmt.Errorf(
			"parse decision front matter %s: %w",
			path,
			err,
		)
	}

	tags := configdata.StringList(decoded.Tags)

	if !decoded.CodingEthosDecision &&
		!decisionTagsOptIn(tags) {
		return DecisionRecord{}, false, nil
	}

	title := strings.TrimSpace(decoded.Title)
	if title == "" {
		title = firstMarkdownHeading(body)
	}

	rationale := strings.TrimSpace(decoded.Rationale)
	if rationale == "" {
		rationale = strings.TrimSpace(string(body))
	}

	decision := normalizeDecisionRecord(DecisionRecord{
		Title:           title,
		Status:          decoded.Status,
		Rationale:       rationale,
		Alternatives:    decoded.Alternatives,
		SourceKind:      DecisionSourceDocument,
		SourcePath:      path,
		SourceLine:      1,
		ProvenanceClass: ProvenanceDocDerived,
		Author:          decoded.Author,
		RecordedAtUTC:   decoded.RecordedAtUTC,
		UpdatedAtUTC:    decoded.UpdatedAtUTC,
		Links:           decisionDocumentLinks(decoded),
	})
	if decision.Title == "" || decision.Rationale == "" {
		return DecisionRecord{}, false, errDecisionRecordRequired
	}

	return decision, true, nil
}

func splitYAMLFrontMatter(payload []byte) ([]byte, []byte, bool) {
	if !bytes.HasPrefix(payload, []byte("---\n")) &&
		!bytes.HasPrefix(payload, []byte("---\r\n")) {
		return nil, payload, false
	}

	lines := bytes.SplitAfter(payload, []byte("\n"))
	if len(lines) < frontMatterMinimumLineCount {
		return nil, payload, false
	}

	offset := len(lines[0])
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "---" {
			frontMatter := payload[len(lines[0]):offset]
			body := payload[offset+len(line):]

			return frontMatter, body, true
		}

		offset += len(line)
	}

	return nil, payload, false
}

func decisionTagsOptIn(tags []string) bool {
	return slices.ContainsFunc(tags, func(tag string) bool {
		normalized := strings.ToLower(strings.TrimSpace(tag))

		return normalized == "decision" || normalized == "architecture-decision"
	})
}

func firstMarkdownHeading(payload []byte) string {
	for line := range strings.Lines(string(payload)) {
		trimmed := strings.TrimSpace(line)
		if heading, found := strings.CutPrefix(trimmed, "# "); found {
			return strings.TrimSpace(heading)
		}
	}

	return ""
}

func decisionDocumentLinks(decoded decisionFrontMatter) []DecisionLink {
	paths := append([]string{}, decoded.AffectedPaths...)
	paths = append(paths, decoded.AffectedFiles...)
	paths = append(paths, decoded.AffectedModules...)

	links := make([]DecisionLink, 0, len(paths)+len(decoded.AffectedSymbols))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		links = append(links, DecisionLink{Path: path, Kind: DecisionLinkAffects})
	}

	for _, symbol := range decoded.AffectedSymbols {
		path := strings.TrimSpace(symbol.Path)
		symbolPath := firstNonEmptyDecisionText(symbol.SymbolPath, symbol.Symbol)

		if path == "" || symbolPath == "" {
			continue
		}

		links = append(links, DecisionLink{
			Path:       path,
			SymbolPath: symbolPath,
			Kind:       DecisionLinkAffects,
		})
	}

	return links
}

func decisionRelativePath(root, path string) (string, bool) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}

	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}

	relative, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(pathAbs))
	if err != nil ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(relative) {
		return "", false
	}

	return filepath.ToSlash(relative), true
}

type decisionImportPruneScope struct {
	relative string
	exact    bool
}

func decisionImportScope(root, path string) (decisionImportPruneScope, bool) {
	relative := decisionImportRelativeInput(root, path)
	if relative == "" {
		return decisionImportPruneScope{}, false
	}

	return decisionImportPruneScope{
		relative: relative,
		exact:    strings.EqualFold(filepath.Ext(relative), ".md"),
	}, true
}

func firstNonEmptyDecisionText(values ...string) string {
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text != "" {
			return text
		}
	}

	return ""
}
