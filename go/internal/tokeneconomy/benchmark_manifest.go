// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:cyclop,funlen,gocyclo,govet,lll,mnd,noinlineerr,tagalign,wsl_v5 // Protocol retains review order.
package tokeneconomy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"

	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	// BenchmarkManifestVersion is the accepted controlled-experiment contract.
	BenchmarkManifestVersion   = 1
	maximumBenchmarkTasks      = 20
	maximumBenchmarkReplicates = 2
	maximumBenchmarkRuns       = 120
	maximumStaticContextBytes  = 128 << 10
	benchmarkTaskDiagnostic    = "diagnostic"
	benchmarkTaskReal          = "real"
)

var (
	errInvalidBenchmarkManifest  = errors.New("invalid token-economy benchmark manifest")
	errInvalidFullConfigOverride = errors.New("invalid benchmark full config override")
	benchmarkIDPattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	benchmarkCommitPattern       = regexp.MustCompile(`^[a-f0-9]{40}$`)
	benchmarkConfigKeyPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	hexSHA256Pattern             = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// BenchmarkManifest is the versioned YAML experiment contract.
type BenchmarkManifest struct {
	ExperimentID        string                 `yaml:"experiment_id"              json:"experiment_id"`
	CreatedAtUTC        string                 `yaml:"created_at_utc"             json:"created_at_utc"`
	RandomizationSeed   string                 `yaml:"randomization_seed"         json:"randomization_seed"`
	StaticContext       BenchmarkStaticContext `yaml:"static_context"             json:"static_context"`
	Provider            BenchmarkProvider      `yaml:"provider"                   json:"provider"`
	Tasks               []BenchmarkTask        `yaml:"tasks"                      json:"tasks"`
	FullConfigOverrides []string               `yaml:"full_config_overrides"      json:"full_config_overrides"`
	SchemaVersion       int                    `yaml:"schema_version"             json:"schema_version"`
	Replicates          int                    `yaml:"replicates"                 json:"replicates"`
	BlockCheckpoints    []int                  `yaml:"analysis_block_checkpoints" json:"analysis_block_checkpoints"`
}

// BenchmarkStaticContext is the exact global AGENTS.md content shared by the
// full and static arms.
type BenchmarkStaticContext struct {
	Path   string `yaml:"path"   json:"path"`
	SHA256 string `yaml:"sha256" json:"sha256"`
}

// BenchmarkProvider freezes the executable and model used by every arm.
type BenchmarkProvider struct {
	Kind             Provider `yaml:"kind"              json:"kind"`
	Executable       string   `yaml:"executable"        json:"executable"`
	ExecutableSHA256 string   `yaml:"executable_sha256" json:"executable_sha256"`
	RuntimeVersion   string   `yaml:"runtime_version"   json:"runtime_version"`
	Model            string   `yaml:"model"             json:"model"`
	ReasoningEffort  string   `yaml:"reasoning_effort"  json:"reasoning_effort"`
	AuthFile         string   `yaml:"auth_file"         json:"auth_file"`
}

// BenchmarkTask freezes one source revision, prompt, scope, and validator.
type BenchmarkTask struct {
	TaskID              string     `yaml:"task_id"               json:"task_id"`
	Kind                string     `yaml:"kind"                  json:"kind"`
	RepositoryPath      string     `yaml:"repository_path"       json:"repository_path"`
	Commit              string     `yaml:"commit"                json:"commit"`
	SourceArchiveSHA256 string     `yaml:"source_archive_sha256" json:"source_archive_sha256"`
	PromptPath          string     `yaml:"prompt_path"           json:"prompt_path"`
	PromptSHA256        string     `yaml:"prompt_sha256"         json:"prompt_sha256"`
	ValidatorSHA256     string     `yaml:"validator_sha256"      json:"validator_sha256"`
	AgentTimeout        string     `yaml:"agent_timeout"         json:"agent_timeout"`
	ValidationTimeout   string     `yaml:"validation_timeout"    json:"validation_timeout"`
	AllowedPaths        []string   `yaml:"allowed_paths"         json:"allowed_paths"`
	Validators          [][]string `yaml:"validators"            json:"validators"`
}

// PreparedBenchmark is a validated manifest plus computed protocol identity.
type PreparedBenchmark struct {
	Manifest       BenchmarkManifest `json:"manifest"`
	ManifestPath   string            `json:"manifest_path"`
	ManifestSHA256 string            `json:"manifest_sha256"`
	ProtocolSHA256 string            `json:"protocol_sha256"`
	TotalRuns      int               `json:"total_runs"`
}

// LoadBenchmarkManifest decodes and validates a benchmark without launching an
// agent or changing a repository.
func LoadBenchmarkManifest(
	ctx context.Context,
	path string,
) (PreparedBenchmark, error) {
	canonical, payload, err := readBenchmarkManifest(path)
	if err != nil {
		return PreparedBenchmark{}, err
	}

	manifest, err := decodeBenchmarkManifest(payload, "manifest")
	if err != nil {
		return PreparedBenchmark{}, err
	}

	if err = validateBenchmarkManifest(ctx, manifest); err != nil {
		return PreparedBenchmark{}, err
	}

	manifestDigest := sha256.Sum256(payload)
	protocolPayload, err := json.Marshal(manifest)
	if err != nil {
		return PreparedBenchmark{}, fmt.Errorf("encode benchmark protocol: %w", err)
	}
	protocolDigest := sha256.Sum256(protocolPayload)

	return PreparedBenchmark{
		Manifest:       manifest,
		ManifestPath:   canonical,
		ManifestSHA256: hex.EncodeToString(manifestDigest[:]),
		ProtocolSHA256: hex.EncodeToString(protocolDigest[:]),
		TotalRuns:      len(manifest.Tasks) * manifest.Replicates * 3,
	}, nil
}

// FreezeBenchmarkManifest computes every external identity in a draft and
// creates a validated immutable manifest without launching an agent.
func FreezeBenchmarkManifest(
	ctx context.Context,
	draftPath string,
	outputPath string,
) (PreparedBenchmark, error) {
	_, payload, err := readBenchmarkManifest(draftPath)
	if err != nil {
		return PreparedBenchmark{}, err
	}

	manifest, err := decodeBenchmarkManifest(payload, "draft")
	if err != nil {
		return PreparedBenchmark{}, err
	}

	if !filepath.IsAbs(outputPath) {
		return PreparedBenchmark{}, fmt.Errorf(
			"%w: frozen manifest output path must be absolute",
			errInvalidBenchmarkManifest,
		)
	}
	outputPath = filepath.Clean(outputPath)
	if err = fingerprintBenchmarkManifest(ctx, &manifest); err != nil {
		return PreparedBenchmark{}, err
	}
	if err = validateBenchmarkManifest(ctx, manifest); err != nil {
		return PreparedBenchmark{}, err
	}

	frozen, err := yaml.Marshal(manifest)
	if err != nil {
		return PreparedBenchmark{}, fmt.Errorf("encode frozen benchmark manifest: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(outputPath), storeDirMode); err != nil {
		return PreparedBenchmark{}, fmt.Errorf("create frozen manifest directory: %w", err)
	}
	if err = writeExclusiveArtifact(outputPath, frozen); err != nil {
		return PreparedBenchmark{}, err
	}

	prepared, err := LoadBenchmarkManifest(ctx, outputPath)
	if err != nil {
		_ = os.Remove(outputPath)

		return PreparedBenchmark{}, fmt.Errorf("verify frozen benchmark manifest: %w", err)
	}

	return prepared, nil
}

func decodeBenchmarkManifest(payload []byte, label string) (BenchmarkManifest, error) {
	var manifest BenchmarkManifest
	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	decoder.KnownFields(true)
	err := decoder.Decode(&manifest)
	if err != nil {
		return BenchmarkManifest{}, fmt.Errorf("decode benchmark %s: %w", label, err)
	}

	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		if err == nil {
			return BenchmarkManifest{}, fmt.Errorf(
				"%w: benchmark %s contains multiple YAML documents",
				errInvalidBenchmarkManifest,
				label,
			)
		}

		return BenchmarkManifest{}, fmt.Errorf("decode benchmark %s trailer: %w", label, err)
	}

	return manifest, nil
}

func fingerprintBenchmarkManifest(
	ctx context.Context,
	manifest *BenchmarkManifest,
) error {
	if !filepath.IsAbs(manifest.Provider.Executable) {
		return fmt.Errorf(
			"%w: provider executable path must be absolute",
			errInvalidBenchmarkManifest,
		)
	}

	providerDigest, err := ledgerFileSHA256(manifest.Provider.Executable)
	if err != nil {
		return fmt.Errorf("hash provider executable: %w", err)
	}
	manifest.Provider.ExecutableSHA256 = providerDigest

	version, err := safeexec.CommandContext(ctx, manifest.Provider.Executable, "--version").
		Output()
	if err != nil {
		return fmt.Errorf("query provider runtime version: %w", err)
	}
	manifest.Provider.RuntimeVersion = strings.TrimSpace(string(version))

	staticDigest, err := ledgerFileSHA256(manifest.StaticContext.Path)
	if err != nil {
		return fmt.Errorf("hash static context: %w", err)
	}
	manifest.StaticContext.SHA256 = staticDigest

	for index := range manifest.Tasks {
		task := &manifest.Tasks[index]
		task.SourceArchiveSHA256, err = benchmarkSourceArchiveSHA256(
			ctx,
			task.RepositoryPath,
			task.Commit,
		)
		if err != nil {
			return fmt.Errorf("task %q: %w", task.TaskID, err)
		}
		task.PromptSHA256, err = ledgerFileSHA256(task.PromptPath)
		if err != nil {
			return fmt.Errorf("task %q prompt: %w", task.TaskID, err)
		}
		task.ValidatorSHA256, err = benchmarkValidatorSHA256(task.Validators)
		if err != nil {
			return fmt.Errorf("task %q: %w", task.TaskID, err)
		}
	}

	return nil
}

func readBenchmarkManifest(path string) (string, []byte, error) {
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		return "", nil, fmt.Errorf(
			"%w: manifest path must be absolute",
			errInvalidBenchmarkManifest,
		)
	}

	canonical := filepath.Clean(path)
	payload, err := os.ReadFile(canonical)
	if err != nil {
		return "", nil, fmt.Errorf("read benchmark manifest: %w", err)
	}

	return canonical, payload, nil
}

func validateBenchmarkManifest(ctx context.Context, manifest BenchmarkManifest) error {
	if manifest.SchemaVersion != BenchmarkManifestVersion {
		return fmt.Errorf(
			"%w: schema_version is %d, expected %d",
			errInvalidBenchmarkManifest,
			manifest.SchemaVersion,
			BenchmarkManifestVersion,
		)
	}
	if !benchmarkIDPattern.MatchString(manifest.ExperimentID) {
		return fmt.Errorf("%w: invalid experiment_id", errInvalidBenchmarkManifest)
	}
	if strings.TrimSpace(manifest.RandomizationSeed) == "" {
		return fmt.Errorf("%w: randomization_seed is required", errInvalidBenchmarkManifest)
	}
	if _, err := time.Parse(time.RFC3339Nano, manifest.CreatedAtUTC); err != nil {
		return fmt.Errorf(
			"%w: created_at_utc must be RFC3339: %w",
			errInvalidBenchmarkManifest,
			err,
		)
	}
	if manifest.Replicates < 1 || manifest.Replicates > maximumBenchmarkReplicates {
		return fmt.Errorf(
			"%w: replicates must be between 1 and %d",
			errInvalidBenchmarkManifest,
			maximumBenchmarkReplicates,
		)
	}
	if len(manifest.Tasks) == 0 || len(manifest.Tasks) > maximumBenchmarkTasks {
		return fmt.Errorf(
			"%w: tasks must contain between 1 and %d entries",
			errInvalidBenchmarkManifest,
			maximumBenchmarkTasks,
		)
	}
	if len(manifest.Tasks)*manifest.Replicates*3 > maximumBenchmarkRuns {
		return fmt.Errorf(
			"%w: run count exceeds %d",
			errInvalidBenchmarkManifest,
			maximumBenchmarkRuns,
		)
	}
	if err := validateAnalysisBlockCheckpoints(
		manifest.BlockCheckpoints,
		len(manifest.Tasks)*manifest.Replicates,
	); err != nil {
		return err
	}

	if err := validateBenchmarkProvider(ctx, manifest.Provider); err != nil {
		return err
	}
	if err := validateStaticContext(manifest.StaticContext); err != nil {
		return err
	}
	if err := validateFullConfigOverrides(manifest.FullConfigOverrides); err != nil {
		return err
	}

	taskIDs := map[string]struct{}{}
	taskKinds := map[string]struct{}{}
	for _, task := range manifest.Tasks {
		if _, found := taskIDs[task.TaskID]; found {
			return fmt.Errorf(
				"%w: duplicate task_id %q",
				errInvalidBenchmarkManifest,
				task.TaskID,
			)
		}
		taskIDs[task.TaskID] = struct{}{}
		taskKinds[task.Kind] = struct{}{}

		if err := validateBenchmarkTask(ctx, task); err != nil {
			return err
		}
	}
	if _, found := taskKinds[benchmarkTaskReal]; !found {
		return fmt.Errorf(
			"%w: mixed corpus requires at least one real task",
			errInvalidBenchmarkManifest,
		)
	}
	if _, found := taskKinds[benchmarkTaskDiagnostic]; !found {
		return fmt.Errorf(
			"%w: mixed corpus requires at least one diagnostic task",
			errInvalidBenchmarkManifest,
		)
	}

	return nil
}

func validateAnalysisBlockCheckpoints(checkpoints []int, blockCount int) error {
	if len(checkpoints) == 0 {
		return fmt.Errorf(
			"%w: analysis_block_checkpoints is required",
			errInvalidBenchmarkManifest,
		)
	}

	previous := 0
	for _, checkpoint := range checkpoints {
		if checkpoint <= previous || checkpoint > blockCount {
			return fmt.Errorf(
				"%w: analysis block checkpoints must increase and not exceed %d",
				errInvalidBenchmarkManifest,
				blockCount,
			)
		}
		previous = checkpoint
	}
	if checkpoints[len(checkpoints)-1] != blockCount {
		return fmt.Errorf(
			"%w: final analysis block checkpoint must equal block count %d",
			errInvalidBenchmarkManifest,
			blockCount,
		)
	}

	return nil
}

func validateBenchmarkProvider(ctx context.Context, provider BenchmarkProvider) error {
	if provider.Kind != ProviderCodex {
		return fmt.Errorf(
			"%w: benchmark runner currently requires provider codex",
			errInvalidBenchmarkManifest,
		)
	}
	if strings.TrimSpace(provider.Model) == "" ||
		strings.TrimSpace(provider.ReasoningEffort) == "" {
		return fmt.Errorf(
			"%w: provider model and reasoning_effort are required",
			errInvalidBenchmarkManifest,
		)
	}
	if err := validateExpectedFileHash(
		provider.Executable,
		provider.ExecutableSHA256,
		"provider executable",
	); err != nil {
		return err
	}
	if !filepath.IsAbs(provider.AuthFile) {
		return fmt.Errorf(
			"%w: provider auth_file must be absolute",
			errInvalidBenchmarkManifest,
		)
	}

	info, err := os.Stat(provider.AuthFile)
	if err != nil {
		return fmt.Errorf("stat provider auth_file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf(
			"%w: provider auth_file must be a private regular file",
			errInvalidBenchmarkManifest,
		)
	}

	command := safeexec.CommandContext(ctx, provider.Executable, "--version")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("query provider runtime version: %w", err)
	}
	actualVersion := strings.TrimSpace(string(output))
	if actualVersion != strings.TrimSpace(provider.RuntimeVersion) {
		return fmt.Errorf(
			"%w: provider runtime version is %q, expected %q",
			errInvalidBenchmarkManifest,
			actualVersion,
			provider.RuntimeVersion,
		)
	}

	return nil
}

func validateStaticContext(context BenchmarkStaticContext) error {
	if err := validateExpectedFileHash(
		context.Path,
		context.SHA256,
		"static context",
	); err != nil {
		return err
	}

	info, err := os.Stat(context.Path)
	if err != nil {
		return fmt.Errorf("stat static context: %w", err)
	}
	if info.Size() > maximumStaticContextBytes {
		return fmt.Errorf(
			"%w: static context exceeds %d bytes",
			errInvalidBenchmarkManifest,
			maximumStaticContextBytes,
		)
	}

	return nil
}

func validateFullConfigOverrides(overrides []string) error {
	if len(overrides) == 0 {
		return fmt.Errorf(
			"%w: full_config_overrides is required",
			errInvalidBenchmarkManifest,
		)
	}

	controlledPaths := [][]string{
		{"approval_policy"},
		{"model"},
		{"model_reasoning_effort"},
		{"project_doc_max_bytes"},
		{"sandbox_mode"},
		{"sandbox_workspace_write", "network_access"},
	}
	seenPaths := map[string]struct{}{}
	for index, override := range overrides {
		config := map[string]any{}
		if strings.TrimSpace(override) == "" ||
			toml.Unmarshal([]byte(override), &config) != nil || len(config) == 0 {
			return fmt.Errorf(
				"%w: full config override %d is invalid TOML",
				errInvalidBenchmarkManifest,
				index+1,
			)
		}

		paths, leafPaths := benchmarkConfigPaths(config, nil)
		if err := validateControlledFullConfigPaths(paths, controlledPaths); err != nil {
			return err
		}
		if _, err := canonicalFullConfigOverride(override); err != nil {
			return fmt.Errorf(
				"%w: full config override %d: %w",
				errInvalidBenchmarkManifest,
				index+1,
				err,
			)
		}

		for _, path := range leafPaths {
			identity := strings.Join(path, "\x00")
			if _, found := seenPaths[identity]; found {
				return fmt.Errorf(
					"%w: duplicate full config override %q",
					errInvalidBenchmarkManifest,
					strings.Join(path, "."),
				)
			}
			seenPaths[identity] = struct{}{}
		}
	}

	return nil
}

func validateControlledFullConfigPaths(paths, controlledPaths [][]string) error {
	for _, path := range paths {
		for _, controlledPath := range controlledPaths {
			if !benchmarkConfigPathsRelated(path, controlledPath) {
				continue
			}

			return fmt.Errorf(
				"%w: full config override changes controlled setting %q",
				errInvalidBenchmarkManifest,
				strings.Join(controlledPath, "."),
			)
		}
	}

	return nil
}

func canonicalFullConfigOverride(override string) (string, error) {
	config := map[string]any{}
	if strings.TrimSpace(override) == "" ||
		toml.Unmarshal([]byte(override), &config) != nil || len(config) == 0 {
		return "", fmt.Errorf("%w: invalid TOML", errInvalidFullConfigOverride)
	}

	_, leafPaths := benchmarkConfigPaths(config, nil)
	if len(leafPaths) != 1 {
		return "", fmt.Errorf(
			"%w: must define exactly one setting",
			errInvalidFullConfigOverride,
		)
	}
	path := leafPaths[0]
	for _, segment := range path {
		if !benchmarkConfigKeyPattern.MatchString(segment) {
			return "", fmt.Errorf(
				"%w: uses a key that cannot be forwarded safely",
				errInvalidFullConfigOverride,
			)
		}
	}

	value, found := benchmarkConfigValue(config, path)
	if !found {
		return "", fmt.Errorf(
			"%w: does not resolve to its parsed setting",
			errInvalidFullConfigOverride,
		)
	}
	encoded, err := toml.Marshal(map[string]any{"value": value})
	if err != nil {
		return "", fmt.Errorf("cannot encode its parsed value: %w", err)
	}
	line := strings.TrimSuffix(string(encoded), "\n")
	const valuePrefix = "value = "
	if !strings.HasPrefix(line, valuePrefix) || strings.Contains(line, "\n") {
		return "", fmt.Errorf(
			"%w: uses a value that cannot be forwarded safely",
			errInvalidFullConfigOverride,
		)
	}

	return strings.Join(path, ".") + "=" + strings.TrimPrefix(line, valuePrefix), nil
}

func benchmarkConfigValue(config map[string]any, path []string) (any, bool) {
	current := config
	for index, segment := range path {
		value, found := current[segment]
		if !found {
			return nil, false
		}
		if index == len(path)-1 {
			return value, true
		}
		current, found = value.(map[string]any)
		if !found {
			return nil, false
		}
	}

	return nil, false
}

func benchmarkConfigPaths(
	config map[string]any,
	prefix []string,
) ([][]string, [][]string) {
	paths := [][]string{}
	leaves := [][]string{}
	for key, value := range config {
		path := append(slices.Clone(prefix), key)
		paths = append(paths, path)

		nested, isTable := value.(map[string]any)
		if !isTable || len(nested) == 0 {
			leaves = append(leaves, path)

			continue
		}

		nestedPaths, nestedLeaves := benchmarkConfigPaths(nested, path)
		paths = append(paths, nestedPaths...)
		leaves = append(leaves, nestedLeaves...)
	}

	return paths, leaves
}

func benchmarkConfigPathsRelated(left, right []string) bool {
	for index := range min(len(left), len(right)) {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}

func validateBenchmarkTask(ctx context.Context, task BenchmarkTask) error {
	if !benchmarkIDPattern.MatchString(task.TaskID) {
		return fmt.Errorf("%w: invalid task_id %q", errInvalidBenchmarkManifest, task.TaskID)
	}
	if task.Kind != benchmarkTaskReal && task.Kind != benchmarkTaskDiagnostic {
		return fmt.Errorf(
			"%w: task %q kind must be real or diagnostic",
			errInvalidBenchmarkManifest,
			task.TaskID,
		)
	}
	if !filepath.IsAbs(task.RepositoryPath) || !filepath.IsAbs(task.PromptPath) {
		return fmt.Errorf(
			"%w: task %q paths must be absolute",
			errInvalidBenchmarkManifest,
			task.TaskID,
		)
	}
	if !benchmarkCommitPattern.MatchString(task.Commit) {
		return fmt.Errorf(
			"%w: task %q commit must be a full SHA-1",
			errInvalidBenchmarkManifest,
			task.TaskID,
		)
	}
	if err := validateExpectedFileHash(
		task.PromptPath,
		task.PromptSHA256,
		"task prompt",
	); err != nil {
		return fmt.Errorf("task %q: %w", task.TaskID, err)
	}
	if !hexSHA256Pattern.MatchString(task.SourceArchiveSHA256) ||
		!hexSHA256Pattern.MatchString(task.ValidatorSHA256) {
		return fmt.Errorf(
			"%w: task %q expected hashes are invalid",
			errInvalidBenchmarkManifest,
			task.TaskID,
		)
	}
	if err := validateTaskTimeout(
		task.AgentTimeout,
		4*time.Hour,
		"agent_timeout",
	); err != nil {
		return fmt.Errorf("task %q: %w", task.TaskID, err)
	}
	if err := validateTaskTimeout(
		task.ValidationTimeout,
		time.Hour,
		"validation_timeout",
	); err != nil {
		return fmt.Errorf("task %q: %w", task.TaskID, err)
	}
	if err := validateAllowedPaths(task.AllowedPaths); err != nil {
		return fmt.Errorf("task %q: %w", task.TaskID, err)
	}
	if len(task.Validators) == 0 {
		return fmt.Errorf(
			"%w: task %q validators are required",
			errInvalidBenchmarkManifest,
			task.TaskID,
		)
	}

	actualSource, err := benchmarkSourceArchiveSHA256(
		ctx,
		task.RepositoryPath,
		task.Commit,
	)
	if err != nil {
		return fmt.Errorf("task %q: %w", task.TaskID, err)
	}
	if actualSource != task.SourceArchiveSHA256 {
		return fmt.Errorf(
			"%w: task %q source archive hash is %s, expected %s",
			errInvalidBenchmarkManifest,
			task.TaskID,
			actualSource,
			task.SourceArchiveSHA256,
		)
	}

	actualValidator, err := benchmarkValidatorSHA256(task.Validators)
	if err != nil {
		return fmt.Errorf("task %q: %w", task.TaskID, err)
	}
	if actualValidator != task.ValidatorSHA256 {
		return fmt.Errorf(
			"%w: task %q validator hash is %s, expected %s",
			errInvalidBenchmarkManifest,
			task.TaskID,
			actualValidator,
			task.ValidatorSHA256,
		)
	}

	return nil
}

func validateTaskTimeout(
	value string,
	maximum time.Duration,
	field string,
) error {
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%w: %s is invalid: %w", errInvalidBenchmarkManifest, field, err)
	}
	if duration <= 0 || duration > maximum {
		return fmt.Errorf(
			"%w: %s must be positive and no greater than %s",
			errInvalidBenchmarkManifest,
			field,
			maximum,
		)
	}

	return nil
}

func validateAllowedPaths(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("%w: allowed_paths is required", errInvalidBenchmarkManifest)
	}

	for _, path := range paths {
		cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		if cleaned == "." || filepath.IsAbs(path) || strings.HasPrefix(cleaned, "../") ||
			cleaned == ".git" || strings.HasPrefix(cleaned, ".git/") {
			return fmt.Errorf("%w: unsafe allowed path %q", errInvalidBenchmarkManifest, path)
		}
	}

	return nil
}

func validateExpectedFileHash(path, expected, label string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %s path must be absolute", errInvalidBenchmarkManifest, label)
	}
	if !hexSHA256Pattern.MatchString(expected) {
		return fmt.Errorf("%w: %s SHA-256 is invalid", errInvalidBenchmarkManifest, label)
	}

	actual, err := ledgerFileSHA256(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("hash %s: %w", label, err)
	}
	if actual != expected {
		return fmt.Errorf(
			"%w: %s hash is %s, expected %s",
			errInvalidBenchmarkManifest,
			label,
			actual,
			expected,
		)
	}

	return nil
}

func benchmarkSourceArchiveSHA256(
	ctx context.Context,
	repositoryPath string,
	commit string,
) (string, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("resolve git for benchmark source: %w", err)
	}

	resolved, err := safeexec.CommandContext(
		ctx,
		git,
		"-C",
		repositoryPath,
		"rev-parse",
		commit+"^{commit}",
	).Output()
	if err != nil {
		return "", fmt.Errorf("resolve benchmark source commit: %w", err)
	}
	if strings.TrimSpace(string(resolved)) != commit {
		return "", fmt.Errorf(
			"%w: benchmark commit did not resolve exactly",
			errInvalidBenchmarkManifest,
		)
	}

	command := safeexec.CommandContext(
		ctx,
		git,
		"-C",
		repositoryPath,
		"archive",
		"--format=tar",
		commit,
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("open benchmark source archive: %w", err)
	}
	if err = command.Start(); err != nil {
		return "", fmt.Errorf("start benchmark source archive: %w", err)
	}

	digest := sha256.New()
	_, copyErr := io.Copy(digest, stdout)
	waitErr := command.Wait()
	if err = errors.Join(copyErr, waitErr); err != nil {
		return "", fmt.Errorf("hash benchmark source archive: %w", err)
	}

	return hex.EncodeToString(digest.Sum(nil)), nil
}

func benchmarkValidatorSHA256(validators [][]string) (string, error) {
	type validatorIdentity struct {
		Arguments        []string `json:"arguments"`
		Executable       string   `json:"executable"`
		ExecutableSHA256 string   `json:"executable_sha256"`
	}

	identities := make([]validatorIdentity, 0, len(validators))
	for _, validator := range validators {
		if len(validator) == 0 || strings.TrimSpace(validator[0]) == "" {
			return "", fmt.Errorf("%w: validator command is empty", errInvalidBenchmarkManifest)
		}

		executable, err := exec.LookPath(validator[0])
		if err != nil {
			return "", fmt.Errorf("resolve validator %q: %w", validator[0], err)
		}
		executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			return "", fmt.Errorf("resolve validator symlink %q: %w", validator[0], err)
		}

		digest, err := ledgerFileSHA256(executable)
		if err != nil {
			return "", fmt.Errorf("hash validator %q: %w", validator[0], err)
		}

		identities = append(identities, validatorIdentity{
			Arguments:        slices.Clone(validator[1:]),
			Executable:       executable,
			ExecutableSHA256: digest,
		})
	}

	payload, err := json.Marshal(identities)
	if err != nil {
		return "", fmt.Errorf("encode validator identities: %w", err)
	}
	digest := sha256.Sum256(payload)

	return hex.EncodeToString(digest[:]), nil
}
