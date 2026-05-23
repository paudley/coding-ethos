// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package memories

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

const (
	// CentralDir is the repo-local provider-agnostic memory directory.
	CentralDir = ".coding-ethos/memories"
	// PrimaryFile is the Markdown memory surface providers should read and write.
	PrimaryFile = CentralDir + "/MEMORY.md"
	// DeniedGuidance is the exact remediation for providers that cannot rewrite.
	DeniedGuidance = "Action denied: write memories to " +
		".coding-ethos/memories/MEMORY.md and .coding-ethos/memories/*.yaml"

	defaultIndexName = "index.yaml"
	fileMode         = 0o600
	dirMode          = 0o700
)

var errUnknownMemoryConfigPath = errors.New("unknown memories TOML config path")

// Settings controls the repo-local memory system.
type Settings struct {
	CentralDir     string `json:"central_dir"`
	PrimaryFile    string `json:"primary_file"`
	ImportExisting bool   `json:"import_existing"`
	Enabled        bool   `json:"enabled"`
}

// Classification describes whether a provider path is managed memory.
type Classification struct {
	Provider      string `json:"provider,omitempty"`
	Path          string `json:"path"`
	CanonicalPath string `json:"canonical_path,omitempty"`
	Kind          string `json:"kind"`
	Managed       bool   `json:"managed"`
	Protected     bool   `json:"protected"`
}

// ImportRecord records one source file imported into central memory.
type ImportRecord struct {
	ID            string `json:"id"              yaml:"id"`
	Provider      string `json:"provider"        yaml:"provider"`
	SourcePath    string `json:"source_path"     yaml:"source_path"`
	ImportedAtUTC string `json:"imported_at_utc" yaml:"imported_at_utc"`
	SHA256        string `json:"sha256"          yaml:"sha256"`
}

// ImportReport summarizes an idempotent import pass.
type ImportReport struct {
	Root    string         `json:"root"`
	Records []ImportRecord `json:"records"`
	Changed bool           `json:"changed"`
}

type memoryIndex struct {
	Records []ImportRecord `yaml:"records"`
}

// DefaultSettings returns conservative central-memory defaults.
func DefaultSettings() Settings {
	return Settings{
		Enabled:        true,
		CentralDir:     CentralDir,
		PrimaryFile:    PrimaryFile,
		ImportExisting: true,
	}
}

// LoadSettings reads [memories] from config.toml and repo_config.toml.
func LoadSettings(root string) (Settings, error) {
	settings := DefaultSettings()

	cleanRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return Settings{}, fmt.Errorf("resolve memory settings root: %w", err)
	}

	for _, name := range []string{"config.toml", "repo_config.toml"} {
		config, loadErr := configdata.LoadTOMLMap(filepath.Join(cleanRoot, name))
		if loadErr != nil {
			if errors.Is(loadErr, os.ErrNotExist) {
				continue
			}

			return Settings{}, fmt.Errorf("load memory settings %s: %w", name, loadErr)
		}

		err = applySettingsMap(&settings, config, name)
		if err != nil {
			return Settings{}, err
		}
	}

	return settings, nil
}

func applySettingsMap(settings *Settings, config configdata.Map, file string) error {
	values := configdata.MapValue(config["memories"])
	if values == nil {
		values = configdata.MapValue(config["memories"])
	}

	if values == nil {
		return nil
	}

	for key, value := range values {
		switch key {
		case "enabled":
			settings.Enabled = boolValue(value)
		case "central_dir":
			settings.CentralDir = cleanConfigPath(value, settings.CentralDir)
		case "primary_file":
			settings.PrimaryFile = cleanConfigPath(value, settings.PrimaryFile)
		case "import_existing":
			settings.ImportExisting = boolValue(value)
		default:
			return fmt.Errorf(
				"%w: memories.%s in %s",
				errUnknownMemoryConfigPath,
				key,
				file,
			)
		}
	}

	return nil
}

func boolValue(value any) bool {
	typed, ok := value.(bool)
	if ok {
		return typed
	}

	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true")
}

func cleanConfigPath(value any, fallback string) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return fallback
	}

	return filepath.ToSlash(filepath.Clean(text))
}

// Ensure creates the central memory skeleton when enabled.
func Ensure(root string) error {
	settings, err := LoadSettings(root)
	if err != nil {
		return err
	}

	if !settings.Enabled {
		return nil
	}

	primary := filepath.Join(rootOrDot(root), filepath.FromSlash(settings.PrimaryFile))

	err = os.MkdirAll(filepath.Dir(primary), dirMode)
	if err != nil {
		return fmt.Errorf("create memory directory: %w", err)
	}

	_, err = os.Stat(primary)
	if err == nil {
		return ensureIndex(root, settings, nil)
	}

	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat memory primary file: %w", err)
	}

	content := "# Memory\n\n"

	err = os.WriteFile(primary, []byte(content), fileMode)
	if err != nil {
		return fmt.Errorf("write memory primary file: %w", err)
	}

	return ensureIndex(root, settings, nil)
}

// Verify checks that enabled central memory surfaces exist.
func Verify(root string) error {
	settings, err := LoadSettings(root)
	if err != nil {
		return err
	}

	if !settings.Enabled {
		return nil
	}

	for _, path := range []string{
		filepath.Join(rootOrDot(root), filepath.FromSlash(settings.CentralDir)),
		filepath.Join(rootOrDot(root), filepath.FromSlash(settings.PrimaryFile)),
		filepath.Join(
			rootOrDot(root),
			filepath.FromSlash(settings.CentralDir),
			defaultIndexName,
		),
	} {
		_, statErr := os.Stat(path)
		if statErr != nil {
			return fmt.Errorf("verify memory surface %s: %w", path, statErr)
		}
	}

	return nil
}

// Classify identifies provider-specific memory paths and their central target.
func Classify(root, rawPath, provider string) Classification {
	cleanPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(rawPath)))
	if cleanPath == "." || cleanPath == "" {
		return Classification{Path: rawPath}
	}

	normalized := normalizedMemoryPath(root, cleanPath)
	if centralMemoryPath(normalized) {
		return Classification{Path: rawPath, Kind: "central"}
	}

	if protectedProviderStatePath(normalized) {
		return Classification{Path: rawPath, Protected: true, Kind: "protected"}
	}

	detectedProvider := providerForPath(normalized)
	if detectedProvider == "" && provider != "" {
		detectedProvider = provider
	}

	if detectedProvider == "" || !memoryPath(normalized) {
		return Classification{Path: rawPath}
	}

	return Classification{
		Provider:      detectedProvider,
		Path:          rawPath,
		CanonicalPath: canonicalPathForInput(root, rawPath),
		Kind:          "memory",
		Managed:       true,
	}
}

// ImportExisting imports known provider memory files without deleting sources.
func ImportExisting(root string) (ImportReport, error) {
	settings, err := LoadSettings(root)
	if err != nil {
		return ImportReport{}, err
	}

	if !settings.Enabled || !settings.ImportExisting {
		return ImportReport{Root: rootOrDot(root)}, nil
	}

	err = Ensure(root)
	if err != nil {
		return ImportReport{}, err
	}

	sources := importSourcePaths(root)
	records := make([]ImportRecord, 0, len(sources))

	for _, source := range sources {
		record, importErr := importOne(root, settings, source)
		if importErr != nil {
			return ImportReport{}, importErr
		}

		if record.ID != "" {
			records = append(records, record)
		}
	}

	err = ensureIndex(root, settings, records)
	if err != nil {
		return ImportReport{}, err
	}

	return ImportReport{
		Root:    rootOrDot(root),
		Records: records,
		Changed: len(records) > 0,
	}, nil
}

func importOne(root string, settings Settings, source string) (ImportRecord, error) {
	data, err := os.ReadFile(filepath.Clean(source))
	if err != nil {
		return ImportRecord{}, fmt.Errorf("read provider memory %s: %w", source, err)
	}

	if strings.TrimSpace(string(data)) == "" {
		return ImportRecord{}, nil
	}

	record := ImportRecord{
		ID:            importID(source),
		Provider:      providerForPath(normalizedMemoryPath(root, source)),
		SourcePath:    source,
		ImportedAtUTC: time.Now().UTC().Format(time.RFC3339),
		SHA256:        sha256Text(data),
	}

	primary := filepath.Join(rootOrDot(root), filepath.FromSlash(settings.PrimaryFile))

	existing, err := os.ReadFile(filepath.Clean(primary))
	if err != nil {
		return ImportRecord{}, fmt.Errorf("read central memory %s: %w", primary, err)
	}

	if strings.Contains(string(existing), "<!-- coding-ethos-memory:"+record.ID+" -->") {
		return ImportRecord{}, nil
	}

	block := "\n<!-- coding-ethos-memory:" + record.ID + " -->\n" +
		"## Imported from " + source + "\n\n" +
		strings.TrimSpace(string(data)) + "\n" +
		"<!-- /coding-ethos-memory:" + record.ID + " -->\n"
	// #nosec G703 -- primary is constrained to the configured repo memory file.
	err = os.WriteFile(primary, append(existing, []byte(block)...), fileMode)
	if err != nil {
		return ImportRecord{}, fmt.Errorf("append central memory %s: %w", primary, err)
	}

	return record, nil
}

func ensureIndex(root string, settings Settings, records []ImportRecord) error {
	path := filepath.Join(
		rootOrDot(root),
		filepath.FromSlash(settings.CentralDir),
		defaultIndexName,
	)

	existing, err := readIndexRecords(path)
	if err != nil {
		return err
	}

	payload := memoryIndex{Records: mergeImportRecords(existing, records)}

	data, err := yaml.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode memory index: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(path), dirMode)
	if err != nil {
		return fmt.Errorf("create memory index directory: %w", err)
	}

	err = os.WriteFile(path, data, fileMode)
	if err != nil {
		return fmt.Errorf("write memory index %s: %w", path, err)
	}

	return nil
}

func readIndexRecords(path string) ([]ImportRecord, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read memory index %s: %w", path, err)
	}

	var decoded memoryIndex

	err = yaml.Unmarshal(data, &decoded)
	if err != nil {
		return nil, fmt.Errorf("decode memory index %s: %w", path, err)
	}

	return decoded.Records, nil
}

func mergeImportRecords(existing, imported []ImportRecord) []ImportRecord {
	merged := make([]ImportRecord, 0, len(existing)+len(imported))
	seen := map[string]int{}

	for _, record := range existing {
		if strings.TrimSpace(record.ID) == "" {
			continue
		}

		seen[record.ID] = len(merged)
		merged = append(merged, record)
	}

	for _, record := range imported {
		if strings.TrimSpace(record.ID) == "" {
			continue
		}

		index, found := seen[record.ID]
		if found {
			merged[index] = record

			continue
		}

		seen[record.ID] = len(merged)
		merged = append(merged, record)
	}

	return merged
}

func importSourcePaths(root string) []string {
	candidates := []string{}
	for _, base := range []string{
		filepath.Join(rootOrDot(root), ".claude"),
		filepath.Join(rootOrDot(root), ".codex"),
		filepath.Join(rootOrDot(root), ".gemini"),
	} {
		candidates = append(candidates, walkMemoryFiles(base)...)
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		for _, base := range []string{
			filepath.Join(home, ".claude"),
			filepath.Join(home, "claude"),
		} {
			candidates = append(candidates, walkMemoryFiles(base)...)
		}
	}

	slices.Sort(candidates)

	return slices.Compact(candidates)
}

func walkMemoryFiles(base string) []string {
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return nil
	}

	files := []string{}

	err = filepath.WalkDir(
		base,
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.IsDir() {
				return nil
			}

			if memoryPath(filepath.ToSlash(path)) {
				files = append(files, path)
			}

			return nil
		},
	)
	if err != nil {
		return nil
	}

	return files
}

func normalizedMemoryPath(root, path string) string {
	expanded, found := strings.CutPrefix(path, "~/")
	if found {
		expanded = "~/" + expanded
	} else {
		expanded = path
	}

	clean := filepath.ToSlash(filepath.Clean(expanded))
	rootSlash := filepath.ToSlash(filepath.Clean(rootOrDot(root)))

	repoPath, found := strings.CutPrefix(clean, rootSlash+"/")
	if found {
		return repoPath
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		homeSlash := filepath.ToSlash(filepath.Clean(home))

		homePath, found := strings.CutPrefix(clean, homeSlash+"/")
		if found {
			return "~/" + homePath
		}
	}

	return clean
}

func protectedProviderStatePath(path string) bool {
	switch path {
	case ".claude/settings.json",
		".claude/settings.local.json",
		".mcp.json",
		".codex/config.toml",
		".codex/hooks.json",
		".gemini/settings.json":
		return true
	default:
		return false
	}
}

func centralMemoryPath(path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))

	return normalized == PrimaryFile ||
		normalized == CentralDir ||
		strings.HasPrefix(normalized, CentralDir+"/")
}

func providerForPath(path string) string {
	switch {
	case strings.HasPrefix(path, ".claude/") || strings.HasPrefix(path, "~/.claude/") ||
		strings.HasPrefix(path, "~/claude/"):
		return "claude"
	case strings.HasPrefix(path, ".codex/") || strings.HasPrefix(path, "~/.codex/"):
		return "codex"
	case strings.HasPrefix(path, ".gemini/") || strings.HasPrefix(path, "~/.gemini/"):
		return "gemini"
	default:
		return ""
	}
}

func memoryPath(path string) bool {
	lower := strings.ToLower(path)

	return strings.HasSuffix(lower, "/memory.md") ||
		strings.HasSuffix(lower, "/memory") ||
		strings.Contains(lower, "/memory/") ||
		strings.Contains(lower, "/memories/") ||
		strings.HasSuffix(lower, "/memory.md")
}

func canonicalPathForInput(root, rawPath string) string {
	if filepath.IsAbs(rawPath) {
		return filepath.Join(rootOrDot(root), filepath.FromSlash(PrimaryFile))
	}

	return PrimaryFile
}

func rootOrDot(root string) string {
	if strings.TrimSpace(root) == "" {
		return "."
	}

	return root
}

func importID(path string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(filepath.Clean(path))))

	return hex.EncodeToString(sum[:])[:16]
}

func sha256Text(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
