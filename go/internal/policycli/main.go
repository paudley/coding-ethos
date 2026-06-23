// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policycli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"blackcat.ca/coding-ethos/go/internal/agentskills"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/geminiprompts"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/syncstate"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

const (
	commandArgIndex    = 1
	commandArgsOffset  = 2
	configSectionProxy = "proxy"
	dirMode            = 0o755
)

var (
	errCompileOutDirRequired      = apperror.StaticError("compile requires --out-dir")
	errWriteExampleOutDirRequired = apperror.StaticError(
		"write-example requires --out-dir",
	)
	errValidateBundleRequired   = apperror.StaticError("validate requires --bundle")
	errValidateMetadataRequired = apperror.StaticError(
		"validate-metadata requires --metadata",
	)
	errExplainBundleRequired   = apperror.StaticError("explain requires --bundle")
	errExplainPolicyIDRequired = apperror.StaticError(
		"explain requires exactly one policy ID",
	)
	errToolConfigRepoRequired = apperror.StaticError(
		"tool config command requires --repo",
	)
	errGeminiPromptRepoRequired = apperror.StaticError(
		"gemini prompt command requires --repo",
	)
	errAgentSkillRepoRequired = apperror.StaticError(
		"agent skill command requires --repo",
	)
	errInvalidBundle = apperror.StaticError("invalid policy bundle")
)

type configTraceReport struct {
	Config             string   `json:"config"`
	RepoConfig         string   `json:"repo_config,omitempty"`
	Status             string   `json:"status"`
	ConfigSections     []string `json:"config_sections"`
	RepoConfigSections []string `json:"repo_config_sections,omitempty"`
	DispatchScopes     int      `json:"dispatch_scopes"`
	EvidenceMaps       int      `json:"evidence_maps"`
	Policies           int      `json:"policies"`
}

func runCLI(args []string) int {
	if len(args) == 0 {
		usage()

		return commandArgsOffset
	}

	handler, ok := policyCommandHandlers()[args[0]]
	if !ok {
		usage()

		return commandArgsOffset
	}

	err := handler(args[1:])
	if err != nil {
		writePolicyError(err)

		return 1
	}

	return 0
}

type policyCommandHandler func([]string) error

func policyCommandHandlers() map[string]policyCommandHandler {
	return map[string]policyCommandHandler{
		"compile":                   compile,
		"dump-example":              dumpExample,
		"write-example":             writeExample,
		"validate":                  validate,
		"validate-metadata":         validateMetadata,
		"explain":                   explain,
		"config-trace":              configTrace,
		"sync-tool-configs":         syncToolConfigs,
		"check-tool-configs":        checkToolConfigs,
		"sync-gemini-prompts":       syncGeminiPrompts,
		"check-gemini-prompts":      checkGeminiPrompts,
		"sync-agent-skills":         syncAgentSkills,
		"check-agent-skills":        checkAgentSkills,
		"install-state-doctor":      installStateDoctor,
		"install-state-repair-plan": installStateRepairPlan,
	}
}

func syncToolConfigs(args []string) error {
	options, err := parseToolConfigFlags("sync-tool-configs", args)
	if err != nil {
		return err
	}

	artifacts, err := toolconfigs.StateArtifacts(
		options.ethosRoot,
		options.repo,
		options.repoConfig,
	)
	if err != nil {
		return fmt.Errorf("plan generated tool configs: %w", err)
	}

	if options.dryRun {
		return writeSyncStateReport(
			syncstate.Plan(options.repo, "sync-tool-configs", artifacts),
			options.format,
		)
	}

	written, err := toolconfigs.Sync(
		options.ethosRoot,
		options.repo,
		options.repoConfig,
	)
	if err != nil {
		return fmt.Errorf("sync generated tool configs: %w", err)
	}

	err = upsertSyncState(syncStateOptions{
		EthosRoot:       options.ethosRoot,
		RepoRoot:        options.repo,
		RepoConfig:      options.repoConfig,
		RequestedAction: "sync-tool-configs",
		Provider:        "tool-configs",
		Artifacts:       artifacts,
		SourcePaths:     toolConfigSourcePaths(options),
	})
	if err != nil {
		return err
	}

	for _, path := range written {
		writePolicyOutput(path)
	}

	writePolicyOutput(syncstate.FilePath(options.repo))

	return nil
}

func checkToolConfigs(args []string) error {
	options, err := parseToolConfigFlags("check-tool-configs", args)
	if err != nil {
		return fmt.Errorf("check generated tool configs: %w", err)
	}

	mismatched, err := toolconfigs.Check(
		options.ethosRoot,
		options.repo,
		options.repoConfig,
	)
	if err != nil {
		return fmt.Errorf("check generated tool configs: %w", err)
	}

	for _, path := range mismatched {
		writePolicyOutput(path)
	}

	if len(mismatched) > 0 {
		return apperror.StaticError("generated tool configs out of sync")
	}

	return nil
}

type toolConfigOptions struct {
	ethosRoot  string
	repo       string
	repoConfig string
	dryRun     bool
	format     string
}

func parseToolConfigFlags(command string, args []string) (toolConfigOptions, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	ethosRoot := flags.String("ethos-root", ".", "Path to coding-ethos checkout")
	repo := flags.String("repo", "", "Repository root where configs are generated")
	repoConfig := flags.String("repo-config", "", "Optional repo override config")
	dryRun := flags.Bool("dry-run", false, "Report planned writes without mutating files")
	format := flags.String(
		"format",
		feedback.FormatTOON,
		"Output format for dry-run reports",
	)

	err := flags.Parse(args)
	if err != nil {
		return toolConfigOptions{}, fmt.Errorf("parse %s flags: %w", command, err)
	}

	if strings.TrimSpace(*repo) == "" {
		return toolConfigOptions{}, errToolConfigRepoRequired
	}

	return toolConfigOptions{
		ethosRoot:  *ethosRoot,
		repo:       *repo,
		repoConfig: *repoConfig,
		dryRun:     *dryRun,
		format:     *format,
	}, nil
}

func syncGeminiPrompts(args []string) error {
	options, err := parseGeminiPromptFlags("sync-gemini-prompts", args)
	if err != nil {
		return err
	}

	artifacts, err := geminiprompts.StateArtifacts(options.Options)
	if err != nil {
		return fmt.Errorf("plan Gemini prompt pack: %w", err)
	}

	if options.dryRun {
		return writeSyncStateReport(
			syncstate.Plan(options.Options.RepoRoot, "sync-gemini-prompts", artifacts),
			options.format,
		)
	}

	written, err := geminiprompts.Sync(options.Options)
	if err != nil {
		return fmt.Errorf("sync Gemini prompt pack: %w", err)
	}

	err = upsertSyncState(syncStateOptions{
		EthosRoot:       options.Options.EthosRoot,
		RepoRoot:        options.Options.RepoRoot,
		RepoConfig:      options.Options.RepoConfig,
		RequestedAction: "sync-gemini-prompts",
		Provider:        "gemini-prompts",
		Artifacts:       artifacts,
		SourcePaths:     geminiSourcePaths(options.Options),
	})
	if err != nil {
		return err
	}

	for _, path := range written {
		writePolicyOutput(path)
	}

	writePolicyOutput(syncstate.FilePath(options.Options.RepoRoot))

	return nil
}

func checkGeminiPrompts(args []string) error {
	options, err := parseGeminiPromptFlags("check-gemini-prompts", args)
	if err != nil {
		return err
	}

	mismatched, err := geminiprompts.Check(options.Options)
	if err != nil {
		return fmt.Errorf("check Gemini prompt pack: %w", err)
	}

	for _, path := range mismatched {
		writePolicyOutput(path)
	}

	if len(mismatched) > 0 {
		return apperror.StaticError("generated Gemini prompt pack out of sync")
	}

	return nil
}

func syncAgentSkills(args []string) error {
	options, err := parseAgentSkillFlags("sync-agent-skills", args)
	if err != nil {
		return err
	}

	artifacts, err := agentskills.StateArtifacts(options.Options)
	if err != nil {
		return fmt.Errorf("plan agent skills: %w", err)
	}

	if options.dryRun {
		return writeSyncStateReport(
			syncstate.Plan(options.Options.RepoRoot, "sync-agent-skills", artifacts),
			options.format,
		)
	}

	written, err := agentskills.Sync(options.Options)
	if err != nil {
		return fmt.Errorf("sync agent skills: %w", err)
	}

	err = upsertSyncState(syncStateOptions{
		EthosRoot:       options.Options.EthosRoot,
		RepoRoot:        options.Options.RepoRoot,
		RequestedAction: "sync-agent-skills",
		Provider:        "agent-skills",
		Artifacts:       artifacts,
		SourcePaths:     agentSkillSourcePaths(options.Options),
	})
	if err != nil {
		return err
	}

	for _, path := range written {
		writePolicyOutput(path)
	}

	writePolicyOutput(syncstate.FilePath(options.Options.RepoRoot))

	return nil
}

func checkAgentSkills(args []string) error {
	options, err := parseAgentSkillFlags("check-agent-skills", args)
	if err != nil {
		return err
	}

	mismatched, err := agentskills.Check(options.Options)
	if err != nil {
		return fmt.Errorf("check agent skills: %w", err)
	}

	for _, path := range mismatched {
		writePolicyOutput(path)
	}

	if len(mismatched) > 0 {
		return apperror.StaticError("generated agent skill surfaces out of sync")
	}

	return nil
}

type agentSkillCLIOptions struct {
	Options agentskills.Options
	format  string
	dryRun  bool
}

func parseAgentSkillFlags(command string, args []string) (agentSkillCLIOptions, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	ethosRoot := flags.String("ethos-root", ".", "Path to coding-ethos checkout")
	repo := flags.String("repo", "", "Repository root where skills are generated")
	primary := flags.String("primary", "", "Path to coding_ethos.yml")
	repoEthos := flags.String("repo-ethos", "", "Optional repo ethos overlay")
	dryRun := flags.Bool("dry-run", false, "Report planned writes without mutating files")
	format := flags.String(
		"format",
		feedback.FormatTOON,
		"Output format for dry-run reports",
	)

	err := flags.Parse(args)
	if err != nil {
		return agentSkillCLIOptions{}, fmt.Errorf("parse %s flags: %w", command, err)
	}

	if strings.TrimSpace(*repo) == "" {
		return agentSkillCLIOptions{}, errAgentSkillRepoRequired
	}

	return agentSkillCLIOptions{
		Options: agentskills.Options{
			EthosRoot: *ethosRoot,
			RepoRoot:  *repo,
			Primary:   *primary,
			RepoEthos: *repoEthos,
		},
		dryRun: *dryRun,
		format: *format,
	}, nil
}

type geminiPromptCLIOptions struct {
	Options geminiprompts.Options
	format  string
	dryRun  bool
}

func parseGeminiPromptFlags(
	command string,
	args []string,
) (geminiPromptCLIOptions, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	ethosRoot := flags.String("ethos-root", ".", "Path to coding-ethos checkout")
	repo := flags.String("repo", "", "Repository root where prompt pack is generated")
	primary := flags.String("primary", "", "Path to coding_ethos.yml")
	repoEthos := flags.String("repo-ethos", "", "Optional repo ethos overlay")
	repoConfig := flags.String("repo-config", "", "Optional repo override config")
	dryRun := flags.Bool("dry-run", false, "Report planned writes without mutating files")
	format := flags.String(
		"format",
		feedback.FormatTOON,
		"Output format for dry-run reports",
	)

	err := flags.Parse(args)
	if err != nil {
		return geminiPromptCLIOptions{}, fmt.Errorf("parse %s flags: %w", command, err)
	}

	if strings.TrimSpace(*repo) == "" {
		return geminiPromptCLIOptions{}, errGeminiPromptRepoRequired
	}

	return geminiPromptCLIOptions{
		Options: geminiprompts.Options{
			EthosRoot:  *ethosRoot,
			RepoRoot:   *repo,
			Primary:    *primary,
			RepoEthos:  *repoEthos,
			RepoConfig: *repoConfig,
		},
		dryRun: *dryRun,
		format: *format,
	}, nil
}

func installStateDoctor(args []string) error {
	options, err := parseInstallStateFlags("install-state-doctor", args)
	if err != nil {
		return err
	}

	report, err := syncstate.Doctor(options.repo)
	if err != nil {
		return fmt.Errorf("doctor install state: %w", err)
	}

	return writeSyncStateReport(report, options.format)
}

func installStateRepairPlan(args []string) error {
	options, err := parseInstallStateFlags("install-state-repair-plan", args)
	if err != nil {
		return err
	}

	report, err := syncstate.RepairPlan(options.repo)
	if err != nil {
		return fmt.Errorf("plan install state repair: %w", err)
	}

	return writeSyncStateReport(report, options.format)
}

type installStateOptions struct {
	repo   string
	format string
}

func parseInstallStateFlags(
	command string,
	args []string,
) (installStateOptions, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	repo := flags.String("repo", "", "Repository root containing sync state")
	format := flags.String("format", feedback.FormatTOON, "Output format")

	err := flags.Parse(args)
	if err != nil {
		return installStateOptions{}, fmt.Errorf("parse %s flags: %w", command, err)
	}

	if strings.TrimSpace(*repo) == "" {
		return installStateOptions{}, errToolConfigRepoRequired
	}

	return installStateOptions{repo: *repo, format: *format}, nil
}

type syncStateOptions struct {
	EthosRoot       string
	RepoRoot        string
	RepoConfig      string
	RequestedAction string
	Provider        string
	Artifacts       []syncstate.Artifact
	SourcePaths     []string
}

func upsertSyncState(options syncStateOptions) error {
	_, err := syncstate.Upsert(syncstate.UpsertOptions{
		RepoRoot:        options.RepoRoot,
		EthosRoot:       options.EthosRoot,
		RequestedAction: options.RequestedAction,
		SourcePaths:     options.SourcePaths,
		ProviderTargets: []syncstate.ProviderTarget{
			{Provider: options.Provider, Root: options.RepoRoot},
		},
		Artifacts: options.Artifacts,
	})
	if err != nil {
		return fmt.Errorf("write install sync state: %w", err)
	}

	return nil
}

func writeSyncStateReport(report syncstate.Report, format string) error {
	err := feedback.Write(os.Stdout, report, format)
	if err != nil {
		return fmt.Errorf("write install sync state report: %w", err)
	}

	return nil
}

func toolConfigSourcePaths(options toolConfigOptions) []string {
	return compactExistingOrCandidatePaths([]string{
		filepath.Join(options.ethosRoot, "config.yaml"),
		options.repoConfig,
		filepath.Join(options.repo, "repo_config.yaml"),
		filepath.Join(options.repo, "repo_config.yml"),
		filepath.Join(options.repo, "coding-ethos.repo.yaml"),
		filepath.Join(options.repo, "coding-ethos.repo.yml"),
	})
}

func geminiSourcePaths(options geminiprompts.Options) []string {
	return compactExistingOrCandidatePaths([]string{
		filepath.Join(options.EthosRoot, "config.yaml"),
		defaultPath(options.Primary, filepath.Join(options.EthosRoot, "coding_ethos.yml")),
		defaultPath(options.RepoEthos, filepath.Join(options.RepoRoot, "repo_ethos.yml")),
		options.RepoConfig,
		filepath.Join(options.RepoRoot, "repo_config.yaml"),
		filepath.Join(options.RepoRoot, "repo_config.yml"),
	})
}

func agentSkillSourcePaths(options agentskills.Options) []string {
	return compactExistingOrCandidatePaths([]string{
		defaultPath(options.Primary, filepath.Join(options.EthosRoot, "coding_ethos.yml")),
		defaultPath(options.RepoEthos, filepath.Join(options.RepoRoot, "repo_ethos.yml")),
	})
}

func defaultPath(path, fallback string) string {
	if strings.TrimSpace(path) != "" {
		return path
	}

	return fallback
}

func compactExistingOrCandidatePaths(paths []string) []string {
	compacted := make([]string, 0, len(paths))
	seen := map[string]bool{}

	for _, path := range paths {
		cleaned := strings.TrimSpace(path)
		if cleaned == "" {
			continue
		}

		cleaned = filepath.Clean(cleaned)
		if seen[cleaned] {
			continue
		}

		seen[cleaned] = true
		compacted = append(compacted, cleaned)
	}

	return compacted
}

func validateMetadata(args []string) error {
	flags := flag.NewFlagSet("validate-metadata", flag.ExitOnError)
	metadataPath := flags.String("metadata", "", "Path to policy-metadata.json")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse validate-metadata flags: %w", err)
	}

	if *metadataPath == "" {
		return errValidateMetadataRequired
	}

	file, err := os.Open(*metadataPath)
	if err != nil {
		return fmt.Errorf("open metadata %s: %w", *metadataPath, err)
	}
	defer file.Close()

	metadata, err := policy.DecodeMetadata(file)
	if err != nil {
		return fmt.Errorf("decode metadata %s: %w", *metadataPath, err)
	}

	inlineErr0 := policy.ValidateMetadataSourceHashes(metadata)
	if inlineErr0 != nil {
		return fmt.Errorf(
			"compiled policy bundle does not match its source hash manifest %s: %w",
			*metadataPath,
			inlineErr0,
		)
	}

	return nil
}

func configTrace(args []string) error {
	options, err := parseConfigTraceFlags(args)
	if err != nil {
		return err
	}

	report, err := buildConfigTraceReport(options)
	if err != nil {
		return err
	}

	if options.jsonOutput {
		return writeConfigTraceJSON(report)
	}

	writeConfigTraceText(report)

	return nil
}

type configTraceOptions struct {
	primary    string
	config     string
	repoEthos  string
	repoConfig string
	jsonOutput bool
}

func parseConfigTraceFlags(args []string) (configTraceOptions, error) {
	flags := flag.NewFlagSet("config-trace", flag.ExitOnError)
	primary := flags.String("primary", "coding_ethos.yml", "Path to coding_ethos.yml")
	config := flags.String("config", "config.yaml", "Path to config.yaml")
	repoEthos := flags.String("repo-ethos", "", "Optional path to repo_ethos.yml")
	repoConfig := flags.String("repo-config", "", "Optional path to repo_config.yaml")
	jsonOutput := flags.Bool("json", false, "Emit JSON output")

	err := flags.Parse(args)
	if err != nil {
		return configTraceOptions{}, fmt.Errorf("parse config-trace flags: %w", err)
	}

	return configTraceOptions{
		primary:    *primary,
		config:     *config,
		repoEthos:  *repoEthos,
		repoConfig: *repoConfig,
		jsonOutput: *jsonOutput,
	}, nil
}

func buildConfigTraceReport(options configTraceOptions) (configTraceReport, error) {
	configShape, configSections, err := validatedConfigSections(options.config, nil)
	if err != nil {
		return configTraceReport{}, err
	}

	repoConfigSections, err := configTraceRepoConfigSections(
		options.repoConfig,
		configShape,
	)
	if err != nil {
		return configTraceReport{}, err
	}

	bundle, err := compileValidConfigTraceBundle(options)
	if err != nil {
		return configTraceReport{}, err
	}

	return configTraceReport{
		Config:             options.config,
		RepoConfig:         options.repoConfig,
		ConfigSections:     configSections,
		RepoConfigSections: repoConfigSections,
		Status:             "valid",
		Policies:           len(bundle.Policies),
		EvidenceMaps:       len(bundle.EvidenceMaps),
		DispatchScopes:     dispatchScopeCount(bundle),
	}, nil
}

func configTraceRepoConfigSections(
	repoConfig string,
	configShape map[string]any,
) ([]string, error) {
	if strings.TrimSpace(repoConfig) == "" {
		return []string{}, nil
	}

	return validateRepoConfigSections(repoConfig, configShape)
}

func compileValidConfigTraceBundle(options configTraceOptions) (policy.Bundle, error) {
	bundle, _, err := policy.Compile(policy.CompileOptions{
		Primary:    options.primary,
		RepoEthos:  options.repoEthos,
		Config:     options.config,
		RepoConfig: options.repoConfig,
	})
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("compile policy bundle: %w", err)
	}

	err = bundle.Validate()
	if err != nil {
		return policy.Bundle{}, fmt.Errorf(
			"%w:\n%s",
			errInvalidBundle,
			policy.FormatValidationError(err),
		)
	}

	return bundle, nil
}

func writeConfigTraceJSON(report configTraceReport) error {
	err := feedback.WriteJSON(os.Stdout, report)
	if err != nil {
		return fmt.Errorf("encode policy validation report: %w", err)
	}

	return nil
}

func writeConfigTraceText(report configTraceReport) {
	scalars := []feedback.Scalar{
		feedback.S("status", report.Status),
		feedback.S("config", report.Config),
		feedback.S("config_sections", strings.Join(report.ConfigSections, ", ")),
	}

	if report.RepoConfig != "" {
		scalars = append(
			scalars,
			feedback.S("repo_config", report.RepoConfig),
			feedback.S("repo_config_sections", strings.Join(report.RepoConfigSections, ", ")),
		)
	}

	scalars = append(
		scalars,
		feedback.S("policies", strconv.Itoa(report.Policies)),
		feedback.S("evidence_maps", strconv.Itoa(report.EvidenceMaps)),
		feedback.S("dispatch_scopes", strconv.Itoa(report.DispatchScopes)),
	)

	feedback.Emit(os.Stdout, feedback.Message{Scalars: scalars}, feedback.FormatTOON)
}

func validatedConfigSections(
	path string,
	reference map[string]any,
) (map[string]any, []string, error) {
	decoded, err := readConfigMap(path)
	if err != nil {
		return nil, nil, err
	}

	inlineErr2 := validateConfigPathKeys(path, decoded, reference, nil)
	if inlineErr2 != nil {
		return nil, nil, inlineErr2
	}

	sections := sortedMapKeys(decoded)

	return decoded, sections, nil
}

func validateRepoConfigSections(
	path string,
	configShape map[string]any,
) ([]string, error) {
	reference := cloneAnyMap(configShape)
	addRepoConfigOnlyShape(reference)

	_, sections, err := validatedConfigSections(path, reference)

	return sections, err
}

func readConfigMap(path string) (map[string]any, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	decoded := map[string]any{}

	inlineErr3 := yaml.Unmarshal(payload, &decoded)
	if inlineErr3 != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, inlineErr3)
	}

	return decoded, nil
}

func validateConfigPathKeys(
	file string,
	values map[string]any,
	reference map[string]any,
	path []string,
) error {
	for key, value := range values {
		nextPath := append(append([]string(nil), path...), key)
		if len(path) == 0 {
			if !knownConfigSection(key) {
				return apperror.Wrapf(
					apperror.StaticError("unknown top-level config section %q in %s"),
					"unknown top-level config section %q in %s",
					key,
					file,
				)
			}
		} else if reference != nil {
			if _, ok := reference[key]; !ok {
				return apperror.Wrapf(
					apperror.StaticError("unknown config path %q in %s"),
					"unknown config path %q in %s",
					strings.Join(nextPath, "."),
					file,
				)
			}
		}

		err := validateConfigChildKeys(
			file,
			value,
			referenceValue(reference, key),
			nextPath,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateConfigChildKeys(
	file string,
	value any,
	reference any,
	path []string,
) error {
	valueMap, isMap := value.(map[string]any)
	if isMap {
		referenceMap, ok := reference.(map[string]any)
		if !ok {
			referenceMap = nil
		}

		return validateConfigPathKeys(file, valueMap, referenceMap, path)
	}

	items, isSlice := value.([]any)
	if !isSlice {
		return nil
	}

	referenceItems, ok := reference.([]any)
	if !ok {
		referenceItems = nil
	}

	referenceItem := firstMapItem(referenceItems)
	if referenceItem == nil {
		return nil
	}

	for index, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		itemPath := append(append([]string(nil), path...), fmt.Sprintf("[%d]", index))

		err := validateConfigPathKeys(file, itemMap, referenceItem, itemPath)
		if err != nil {
			return err
		}
	}

	return nil
}

func firstMapItem(items []any) map[string]any {
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if ok {
			return itemMap
		}
	}

	return nil
}

func referenceValue(reference map[string]any, key string) any {
	if reference == nil {
		return nil
	}

	return reference[key]
}

func sortedMapKeys(values map[string]any) []string {
	sections := make([]string, 0, len(values))
	for section := range values {
		sections = append(sections, section)
	}

	sort.Strings(sections)

	return sections
}

func addRepoConfigOnlyShape(reference map[string]any) {
	repo, ok := reference["repo"].(map[string]any)
	if !ok {
		repo = map[string]any{}
		reference["repo"] = repo
	}

	repo["license"] = map[string]any{
		"copyright":       "",
		"license_file":    "",
		"scan_lines":      0,
		"spdx_identifier": "",
		"text":            "",
		"url":             "",
	}
	repo["kind"] = ""

	reference["code_intel"] = map[string]any{
		"exclude_paths": []any{},
	}
	reference["profiles"] = []any{}

	reference[configSectionProxy] = map[string]any{
		"code_intel_enrichment": map[string]any{
			"enabled":      true,
			"max_edges":    0,
			"max_failures": 0,
			"max_paths":    0,
			"max_symbols":  0,
		},
		"output_compression": map[string]any{
			"head_lines":      0,
			"head_tokens":     0,
			"max_diagnostics": 0,
			"max_lines":       0,
			"max_tokens":      0,
			"tail_lines":      0,
			"tail_tokens":     0,
		},
		"interception": map[string]any{
			"mode":                "",
			"ca_approval":         "",
			"allow_hosts":         []any{},
			"max_normalize_bytes": 0,
			"on_error":            "",
		},
	}
}

func cloneAnyMap(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneAny(value)
	}

	return cloned
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneAny(item))
		}

		return cloned
	default:
		return typed
	}
}

func knownConfigSection(section string) bool {
	switch section {
	case "agent_advice",
		"bundle",
		"code_intel",
		"filesystem",
		"gemini",
		"generated_config",
		"git",
		"go",
		"hooks",
		"policy",
		"project",
		configSectionProxy,
		"profiles",
		"python",
		"repo",
		"sandbox",
		"security",
		"shell",
		"similarity",
		"style",
		"syntax",
		"tooling",
		"version":
		return true
	default:
		return false
	}
}

func dispatchScopeCount(bundle policy.Bundle) int {
	return len(bundle.Dispatch.Git) +
		len(bundle.Dispatch.Linter) +
		len(bundle.Dispatch.Hooks)
}

func compile(args []string) error {
	flags := flag.NewFlagSet("compile", flag.ExitOnError)
	outDir := flags.String("out-dir", "", "Directory to write policy artifacts into")
	primary := flags.String("primary", "coding_ethos.yml", "Path to coding_ethos.yml")
	repoEthos := flags.String("repo-ethos", "", "Optional path to repo_ethos.yml")
	config := flags.String("config", "config.yaml", "Path to config.yaml")
	repoConfig := flags.String("repo-config", "", "Optional path to repo_config.yaml")
	bundleID := flags.String("bundle-id", "", "Optional bundle ID override")
	generatedAt := flags.String(
		"generated-at",
		"",
		"Optional generated_at timestamp override",
	)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse compile flags: %w", err)
	}

	if *outDir == "" {
		return errCompileOutDirRequired
	}

	bundle, metadata, err := policy.Compile(policy.CompileOptions{
		Primary:     *primary,
		RepoEthos:   *repoEthos,
		Config:      *config,
		RepoConfig:  *repoConfig,
		BundleID:    *bundleID,
		GeneratedAt: *generatedAt,
	})
	if err != nil {
		return fmt.Errorf("compile policy bundle: %w", err)
	}

	return writeArtifacts(*outDir, bundle, metadata)
}

func dumpExample(args []string) error {
	flags := flag.NewFlagSet("dump-example", flag.ExitOnError)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse dump-example flags: %w", err)
	}

	var buffer bytes.Buffer

	err = policy.EncodeBundle(&buffer, policy.ExampleBundle())
	if err != nil {
		return fmt.Errorf("encode example bundle: %w", err)
	}

	err = feedback.WriteRendered(
		os.Stdout,
		strings.TrimSuffix(buffer.String(), "\n"),
		feedback.FormatJSON,
	)
	if err != nil {
		return fmt.Errorf("write example bundle: %w", err)
	}

	return nil
}

func writeExample(args []string) error {
	flags := flag.NewFlagSet("write-example", flag.ExitOnError)
	outDir := flags.String("out-dir", "", "Directory to write policy artifacts into")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse write-example flags: %w", err)
	}

	if *outDir == "" {
		return errWriteExampleOutDirRequired
	}

	bundle := policy.ExampleBundle()

	metadata, err := policy.BuildMetadata(bundle, nil)
	if err != nil {
		return fmt.Errorf("build example metadata: %w", err)
	}

	return writeArtifacts(*outDir, bundle, metadata)
}

func validate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse validate flags: %w", err)
	}

	if *bundlePath == "" {
		return errValidateBundleRequired
	}

	file, err := os.Open(*bundlePath)
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return fmt.Errorf("decode bundle: %w", err)
	}

	err = bundle.Validate()
	if err != nil {
		return fmt.Errorf(
			"%w:\n%s",
			errInvalidBundle,
			policy.FormatValidationError(err),
		)
	}

	writePolicyOutput("policy bundle valid")

	return nil
}

func explain(args []string) error {
	flags := flag.NewFlagSet("explain", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse explain flags: %w", err)
	}

	if *bundlePath == "" {
		return errExplainBundleRequired
	}

	if flags.NArg() != 1 {
		return errExplainPolicyIDRequired
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		return err
	}

	err = bundle.Validate()
	if err != nil {
		return fmt.Errorf(
			"%w:\n%s",
			errInvalidBundle,
			policy.FormatValidationError(err),
		)
	}

	var buffer bytes.Buffer

	err = policy.ExplainPolicy(&buffer, bundle, flags.Arg(0))
	if err != nil {
		return fmt.Errorf("explain policy: %w", err)
	}

	err = feedback.WriteRendered(
		os.Stdout,
		strings.TrimSuffix(buffer.String(), "\n"),
		feedback.FormatTOON,
	)
	if err != nil {
		return fmt.Errorf("write policy explanation: %w", err)
	}

	return nil
}

func readBundle(path string) (policy.Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("decode bundle: %w", err)
	}

	return bundle, nil
}

func writeArtifacts(
	outDir string,
	bundle policy.Bundle,
	metadata policy.Metadata,
) error {
	err := os.MkdirAll(outDir, dirMode)
	if err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	err = writeBundleFile(filepath.Join(outDir, "policy-bundle.json"), bundle)
	if err != nil {
		return fmt.Errorf("write bundle file: %w", err)
	}

	err = writeMetadataFile(filepath.Join(outDir, "policy-metadata.json"), metadata)
	if err != nil {
		return fmt.Errorf("write metadata file: %w", err)
	}

	err = writeSummaryFile(filepath.Join(outDir, "policy-summary.md"), bundle)
	if err != nil {
		return fmt.Errorf("write summary file: %w", err)
	}

	writePolicyOutput("wrote policy artifacts to " + outDir)

	return nil
}

func writeBundleFile(path string, bundle policy.Bundle) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create bundle file: %w", err)
	}
	defer file.Close()

	err = policy.EncodeBundle(file, bundle)
	if err != nil {
		return fmt.Errorf("encode bundle: %w", err)
	}

	return nil
}

func writeMetadataFile(path string, metadata policy.Metadata) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create metadata file: %w", err)
	}
	defer file.Close()

	err = policy.EncodeMetadata(file, metadata)
	if err != nil {
		return fmt.Errorf("encode metadata: %w", err)
	}

	return nil
}

func writeSummaryFile(path string, bundle policy.Bundle) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create summary file: %w", err)
	}
	defer file.Close()

	err = policy.WriteSummary(file, bundle)
	if err != nil {
		return fmt.Errorf("write summary: %w", err)
	}

	return nil
}

func usage() {
	writePolicyOutputTo(os.Stderr, `usage:
  coding-ethos-policy compile --out-dir .git/coding-ethos-hooks/policy
      [--primary coding_ethos.yml] [--config config.yaml]
  coding-ethos-policy dump-example
  coding-ethos-policy write-example --out-dir .git/coding-ethos-hooks/policy
  coding-ethos-policy validate --bundle policy-bundle.json
  coding-ethos-policy validate-metadata --metadata policy-metadata.json
  coding-ethos-policy explain --bundle policy-bundle.json POLICY_ID
  coding-ethos-policy config-trace [--primary coding_ethos.yml] [--config config.yaml]
      [--repo-config repo_config.yaml] [--json]
  coding-ethos-policy sync-tool-configs --repo REPO [--ethos-root .]
      [--repo-config repo_config.yaml] [--dry-run] [--format json|toon]
  coding-ethos-policy check-tool-configs --repo REPO [--ethos-root .]
      [--repo-config repo_config.yaml]
  coding-ethos-policy sync-gemini-prompts --repo REPO [--ethos-root .]
      [--primary coding_ethos.yml] [--repo-ethos repo_ethos.yml]
      [--repo-config repo_config.yaml] [--dry-run] [--format json|toon]
  coding-ethos-policy check-gemini-prompts --repo REPO [--ethos-root .]
      [--primary coding_ethos.yml] [--repo-ethos repo_ethos.yml]
      [--repo-config repo_config.yaml]
  coding-ethos-policy sync-agent-skills --repo REPO [--ethos-root .]
      [--primary coding_ethos.yml] [--repo-ethos repo_ethos.yml]
      [--dry-run] [--format json|toon]
  coding-ethos-policy check-agent-skills --repo REPO [--ethos-root .]
      [--primary coding_ethos.yml] [--repo-ethos repo_ethos.yml]
  coding-ethos-policy install-state-doctor --repo REPO [--format json|toon]
  coding-ethos-policy install-state-repair-plan --repo REPO [--format json|toon]
`)
}

func writePolicyError(err error) {
	feedback.Emit(
		os.Stderr,
		feedback.Error{Message: err.Error()},
		feedback.FormatTOON,
	)
}

func writePolicyOutput(text string) {
	writePolicyOutputTo(os.Stdout, text)
}

func writePolicyOutputTo(writer *os.File, text string) {
	feedback.Emit(
		writer,
		feedback.Text{Text: strings.TrimSuffix(text, "\n")},
		feedback.FormatTOON,
	)
}
