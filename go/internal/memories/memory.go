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
	"blackcat.ca/coding-ethos/go/internal/feedback"
)

const (
	// CentralDir is the provider-agnostic memory directory under the selected
	// state root.
	CentralDir = ".coding-ethos/memories"
	// PrimaryFile is the Markdown memory surface providers should read and write.
	PrimaryFile = CentralDir + "/MEMORY.md"

	defaultIndexName = "index.yaml"
	defaultLockName  = ".lock"
	fileMode         = 0o600
	dirMode          = 0o700
)

var errUnknownMemoryConfigPath = errors.New("unknown memories TOML config path")

// DeniedGuidance is the exact remediation for providers that cannot rewrite.
func DeniedGuidance() string {
	return feedback.MustRender(feedback.Message{
		Scalars: []feedback.Scalar{
			feedback.S("status", "denied"),
			feedback.S("reason", "provider memory writes are centralized"),
			feedback.S("allowed_path", PrimaryFile),
		},
		Tables: []feedback.Table{feedback.T(
			"repair",
			[]string{"path"},
			[][]string{{PrimaryFile}, {CentralDir + "/*.yaml"}},
		)},
	}, feedback.FormatTOON)
}

// DeniedReason is a single-line policy reason for compact block output.
func DeniedReason() string {
	return "Action denied: write memories to " + PrimaryFile +
		" and " + CentralDir + "/*.yaml"
}

// Settings controls the root-scoped memory system.
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

	return ensureWithSettings(root, settings)
}

func ensureWithSettings(root string, settings Settings) error {
	if !settings.Enabled {
		return nil
	}

	primary := filepath.Join(rootOrDot(root), filepath.FromSlash(settings.PrimaryFile))

	err := os.MkdirAll(filepath.Dir(primary), dirMode)
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

	return verifyWithSettings(root, settings)
}

// VerifyForRoots checks stateRoot using memory settings loaded from
// settingsRoot. Keeping policy input separate from durable state lets callers
// use private state without copying repository configuration into it.
func VerifyForRoots(settingsRoot, stateRoot string) error {
	settings, err := LoadSettings(settingsRoot)
	if err != nil {
		return err
	}

	return verifyWithSettings(stateRoot, settings)
}

func verifyWithSettings(root string, settings Settings) error {
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
	settings, err := LoadSettings(root)
	if err != nil {
		return Classification{Path: rawPath}
	}

	return ClassifyWithSettings(root, rawPath, provider, settings)
}

// MayManagePath reports whether a path is a provider memory candidate before
// loading repo memory settings.
func MayManagePath(root, rawPath string) bool {
	cleanPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(rawPath)))
	if cleanPath == "." || cleanPath == "" {
		return false
	}

	normalized := normalizedMemoryPath(root, cleanPath)

	return providerForPath(normalized) != "" && memoryPath(normalized)
}

// ClassifyWithSettings identifies provider-specific memory paths with known settings.
func ClassifyWithSettings(root, rawPath, _ string, settings Settings) Classification {
	cleanPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(rawPath)))
	if cleanPath == "." || cleanPath == "" {
		return Classification{Path: rawPath}
	}

	normalized := normalizedMemoryPath(root, cleanPath)
	if centralMemoryPath(settings, normalized) {
		return Classification{Path: rawPath, Kind: "central"}
	}

	if protectedProviderStatePath(normalized) {
		return Classification{Path: rawPath, Protected: true, Kind: "protected"}
	}

	detectedProvider := providerForPath(normalized)
	if detectedProvider == "" || !memoryPath(normalized) {
		return Classification{Path: rawPath}
	}

	return Classification{
		Provider:      detectedProvider,
		Path:          rawPath,
		CanonicalPath: canonicalPathForInput(root, rawPath, settings),
		Kind:          "memory",
		Managed:       true,
	}
}

// ImportExisting imports known provider memory files without deleting sources.
func ImportExisting(root string) (ImportReport, error) {
	return ImportExistingForRoots(root, root)
}

// ImportExistingForRoots imports provider memory discovered under sourceRoot
// into the central memory store under stateRoot. Memory settings remain
// repository-owned and are therefore loaded from sourceRoot.
func ImportExistingForRoots(sourceRoot, stateRoot string) (ImportReport, error) {
	settings, err := LoadSettings(sourceRoot)
	if err != nil {
		return ImportReport{}, err
	}

	if !settings.Enabled || !settings.ImportExisting {
		return ImportReport{Root: rootOrDot(stateRoot)}, nil
	}

	report := ImportReport{Root: rootOrDot(stateRoot)}

	err = withMemoryLock(stateRoot, settings, func() error {
		return importExistingLocked(sourceRoot, stateRoot, settings, &report)
	})
	if err != nil {
		return ImportReport{}, err
	}

	return report, nil
}

func importExistingLocked(
	sourceRoot string,
	stateRoot string,
	settings Settings,
	report *ImportReport,
) error {
	err := ensureWithSettings(stateRoot, settings)
	if err != nil {
		return err
	}

	sources, err := importSourcePaths(sourceRoot)
	if err != nil {
		return err
	}

	primary := filepath.Join(
		rootOrDot(stateRoot),
		filepath.FromSlash(settings.PrimaryFile),
	)

	existing, err := os.ReadFile(filepath.Clean(primary))
	if err != nil {
		return fmt.Errorf("read central memory %s: %w", primary, err)
	}

	existingText := string(existing)
	records := make([]ImportRecord, 0, len(sources))
	blocks := strings.Builder{}

	for _, source := range sources {
		record, block, importErr := importOne(sourceRoot, source, existingText)
		if importErr != nil {
			return importErr
		}

		if record.ID != "" {
			records = append(records, record)

			blocks.WriteString(block)
		}
	}

	if blocks.Len() > 0 {
		err = atomicWriteFile(primary, append(existing, []byte(blocks.String())...), fileMode)
		if err != nil {
			return fmt.Errorf("append central memory %s: %w", primary, err)
		}
	}

	err = ensureIndex(stateRoot, settings, records)
	if err != nil {
		return err
	}

	*report = ImportReport{
		Root:    rootOrDot(stateRoot),
		Records: records,
		Changed: len(records) > 0,
	}

	return nil
}

func importOne(root, source, existing string) (ImportRecord, string, error) {
	data, err := os.ReadFile(filepath.Clean(source))
	if err != nil {
		return ImportRecord{}, "", fmt.Errorf("read provider memory %s: %w", source, err)
	}

	if strings.TrimSpace(string(data)) == "" {
		return ImportRecord{}, "", nil
	}

	sourceID := normalizedMemoryPath(root, source)
	record := ImportRecord{
		ID:            importID(sourceID),
		Provider:      providerForPath(sourceID),
		SourcePath:    sourceID,
		ImportedAtUTC: time.Now().UTC().Format(time.RFC3339),
		SHA256:        sha256Text(data),
	}

	if strings.Contains(existing, "<!-- coding-ethos-memory:"+record.ID+" -->") {
		return ImportRecord{}, "", nil
	}

	block := "\n<!-- coding-ethos-memory:" + record.ID + " -->\n" +
		"## Imported from " + sourceID + "\n\n" +
		strings.TrimSpace(string(data)) + "\n" +
		"<!-- /coding-ethos-memory:" + record.ID + " -->\n"

	return record, block, nil
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

	err = atomicWriteFile(path, data, fileMode)
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

func importSourcePaths(root string) ([]string, error) {
	candidates := []string{}

	for _, base := range []string{
		filepath.Join(rootOrDot(root), ".claude"),
		filepath.Join(rootOrDot(root), ".codex"),
		filepath.Join(rootOrDot(root), ".gemini"),
	} {
		files, err := walkMemoryFiles(base)
		if err != nil {
			return nil, err
		}

		candidates = append(candidates, files...)
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		for _, base := range homeMemoryRoots(home, root) {
			files, walkErr := walkMemoryFiles(base)
			if walkErr != nil {
				return nil, walkErr
			}

			candidates = append(candidates, files...)
		}
	}

	slices.Sort(candidates)

	return slices.Compact(candidates), nil
}

func walkMemoryFiles(base string) ([]string, error) {
	info, err := os.Stat(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("stat memory import root %s: %w", base, err)
	}

	if !info.IsDir() {
		return nil, nil
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
		return nil, fmt.Errorf("walk memory import root %s: %w", base, err)
	}

	return files, nil
}

func homeMemoryRoots(home, root string) []string {
	rootKey := claudeProjectKey(root)
	if rootKey == "" {
		return nil
	}

	return []string{
		filepath.Join(home, ".claude", "projects", rootKey, "memory"),
		filepath.Join(home, "claude", rootKey, "memories"),
		filepath.Join(home, "claude", rootKey, "memory"),
	}
}

func claudeProjectKey(root string) string {
	absolute, err := filepath.Abs(rootOrDot(root))
	if err != nil {
		return ""
	}

	key := filepath.ToSlash(filepath.Clean(absolute))
	key = strings.TrimSpace(key)

	if key == "" || key == "." {
		return ""
	}

	replacer := strings.NewReplacer("/", "-", "_", "-", ".", "-")

	return replacer.Replace(key)
}

func normalizedMemoryPath(root, path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
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

func centralMemoryPath(settings Settings, path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))
	centralDir := filepath.ToSlash(filepath.Clean(settings.CentralDir))
	primaryFile := filepath.ToSlash(filepath.Clean(settings.PrimaryFile))

	return normalized == primaryFile ||
		normalized == centralDir ||
		strings.HasPrefix(normalized, centralDir+"/")
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
	base := filepath.Base(lower)

	return base == "memory.md" ||
		base == "memory" ||
		strings.Contains(lower, "/memory/") ||
		strings.Contains(lower, "/memories/")
}

func canonicalPathForInput(root, rawPath string, settings Settings) string {
	if filepath.IsAbs(rawPath) {
		return filepath.Join(rootOrDot(root), filepath.FromSlash(settings.PrimaryFile))
	}

	return settings.PrimaryFile
}

func withMemoryLock(root string, settings Settings, operation func() error) error {
	lockPath := filepath.Join(
		rootOrDot(root),
		filepath.FromSlash(settings.CentralDir),
		defaultLockName,
	)

	err := os.MkdirAll(filepath.Dir(lockPath), dirMode)
	if err != nil {
		return fmt.Errorf("create memory lock directory: %w", err)
	}

	for attempt := range 25 {
		file, lockErr := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
		if lockErr == nil {
			_ = file.Close()

			defer func() {
				_ = os.Remove(lockPath)
			}()

			return operation()
		}

		if !errors.Is(lockErr, os.ErrExist) {
			return fmt.Errorf("create memory lock %s: %w", lockPath, lockErr)
		}

		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}

	return fmt.Errorf("acquire memory lock %s: %w", lockPath, os.ErrExist)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, dirMode)
	if err != nil {
		return fmt.Errorf("create atomic write directory: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary memory file: %w", err)
	}

	tempPath := temp.Name()

	defer func() {
		_ = os.Remove(tempPath)
	}()

	_, err = temp.Write(data)
	if err != nil {
		_ = temp.Close()

		return fmt.Errorf("write temporary memory file: %w", err)
	}

	err = temp.Chmod(mode)
	if err != nil {
		_ = temp.Close()

		return fmt.Errorf("chmod temporary memory file: %w", err)
	}

	err = temp.Close()
	if err != nil {
		return fmt.Errorf("close temporary memory file: %w", err)
	}

	err = os.Rename(tempPath, path)
	if err != nil {
		return fmt.Errorf("rename temporary memory file: %w", err)
	}

	return nil
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
