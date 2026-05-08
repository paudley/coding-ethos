// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policycli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"blackcat.ca/coding-ethos/go/internal/agentskills"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/geminiprompts"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

const (
	commandArgIndex   = 1
	commandArgsOffset = 2
	dirMode           = 0o755
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
		fmt.Fprintf(os.Stderr, "%s\n", err)

		return 1
	}

	return 0
}

type policyCommandHandler func([]string) error

func policyCommandHandlers() map[string]policyCommandHandler {
	return map[string]policyCommandHandler{
		"compile":              compile,
		"dump-example":         dumpExample,
		"write-example":        writeExample,
		"validate":             validate,
		"validate-metadata":    validateMetadata,
		"explain":              explain,
		"config-trace":         configTrace,
		"sync-tool-configs":    syncToolConfigs,
		"check-tool-configs":   checkToolConfigs,
		"sync-gemini-prompts":  syncGeminiPrompts,
		"check-gemini-prompts": checkGeminiPrompts,
		"sync-agent-skills":    syncAgentSkills,
		"check-agent-skills":   checkAgentSkills,
	}
}

func syncToolConfigs(args []string) error {
	options, err := parseToolConfigFlags("sync-tool-configs", args)
	if err != nil {
		return err
	}

	written, err := toolconfigs.Sync(
		options.ethosRoot,
		options.repo,
		options.repoConfig,
	)
	if err != nil {
		return fmt.Errorf("sync generated tool configs: %w", err)
	}

	for _, path := range written {
		fmt.Fprintln(os.Stdout, path)
	}

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
		fmt.Fprintln(os.Stdout, path)
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
}

func parseToolConfigFlags(command string, args []string) (toolConfigOptions, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	ethosRoot := flags.String("ethos-root", ".", "Path to coding-ethos checkout")
	repo := flags.String("repo", "", "Repository root where configs are generated")
	repoConfig := flags.String("repo-config", "", "Optional repo override config")

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
	}, nil
}

func syncGeminiPrompts(args []string) error {
	options, err := parseGeminiPromptFlags("sync-gemini-prompts", args)
	if err != nil {
		return err
	}

	written, err := geminiprompts.Sync(options)
	if err != nil {
		return fmt.Errorf("sync Gemini prompt pack: %w", err)
	}

	for _, path := range written {
		fmt.Fprintln(os.Stdout, path)
	}

	return nil
}

func checkGeminiPrompts(args []string) error {
	options, err := parseGeminiPromptFlags("check-gemini-prompts", args)
	if err != nil {
		return err
	}

	mismatched, err := geminiprompts.Check(options)
	if err != nil {
		return fmt.Errorf("check Gemini prompt pack: %w", err)
	}

	for _, path := range mismatched {
		fmt.Fprintln(os.Stdout, path)
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

	written, err := agentskills.Sync(options)
	if err != nil {
		return fmt.Errorf("sync agent skills: %w", err)
	}

	for _, path := range written {
		fmt.Fprintln(os.Stdout, path)
	}

	return nil
}

func checkAgentSkills(args []string) error {
	options, err := parseAgentSkillFlags("check-agent-skills", args)
	if err != nil {
		return err
	}

	mismatched, err := agentskills.Check(options)
	if err != nil {
		return fmt.Errorf("check agent skills: %w", err)
	}

	for _, path := range mismatched {
		fmt.Fprintln(os.Stdout, path)
	}

	if len(mismatched) > 0 {
		return apperror.StaticError("generated agent skill surfaces out of sync")
	}

	return nil
}

func parseAgentSkillFlags(command string, args []string) (agentskills.Options, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	ethosRoot := flags.String("ethos-root", ".", "Path to coding-ethos checkout")
	repo := flags.String("repo", "", "Repository root where skills are generated")
	primary := flags.String("primary", "", "Path to coding_ethos.yml")
	repoEthos := flags.String("repo-ethos", "", "Optional repo ethos overlay")

	err := flags.Parse(args)
	if err != nil {
		return agentskills.Options{}, fmt.Errorf("parse %s flags: %w", command, err)
	}

	if strings.TrimSpace(*repo) == "" {
		return agentskills.Options{}, errAgentSkillRepoRequired
	}

	return agentskills.Options{
		EthosRoot: *ethosRoot,
		RepoRoot:  *repo,
		Primary:   *primary,
		RepoEthos: *repoEthos,
	}, nil
}

func parseGeminiPromptFlags(
	command string,
	args []string,
) (geminiprompts.Options, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	ethosRoot := flags.String("ethos-root", ".", "Path to coding-ethos checkout")
	repo := flags.String("repo", "", "Repository root where prompt pack is generated")
	primary := flags.String("primary", "", "Path to coding_ethos.yml")
	repoEthos := flags.String("repo-ethos", "", "Optional repo ethos overlay")
	repoConfig := flags.String("repo-config", "", "Optional repo override config")

	err := flags.Parse(args)
	if err != nil {
		return geminiprompts.Options{}, fmt.Errorf("parse %s flags: %w", command, err)
	}

	if strings.TrimSpace(*repo) == "" {
		return geminiprompts.Options{}, errGeminiPromptRepoRequired
	}

	return geminiprompts.Options{
		EthosRoot:  *ethosRoot,
		RepoRoot:   *repo,
		Primary:    *primary,
		RepoEthos:  *repoEthos,
		RepoConfig: *repoConfig,
	}, nil
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
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(report)
	if err != nil {
		return fmt.Errorf("encode policy validation report: %w", err)
	}

	return nil
}

func writeConfigTraceText(report configTraceReport) {
	fmt.Fprintf(os.Stdout, "status: %s\n", report.Status)
	fmt.Fprintf(os.Stdout, "config: %s\n", report.Config)
	fmt.Fprintf(
		os.Stdout,
		"config_sections: %s\n",
		strings.Join(report.ConfigSections, ", "),
	)

	if report.RepoConfig != "" {
		fmt.Fprintf(os.Stdout, "repo_config: %s\n", report.RepoConfig)
		fmt.Fprintf(
			os.Stdout,
			"repo_config_sections: %s\n",
			strings.Join(report.RepoConfigSections, ", "),
		)
	}

	fmt.Fprintf(os.Stdout, "policies: %d\n", report.Policies)
	fmt.Fprintf(os.Stdout, "evidence_maps: %d\n", report.EvidenceMaps)
	fmt.Fprintf(os.Stdout, "dispatch_scopes: %d\n", report.DispatchScopes)
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
		"filesystem",
		"gemini",
		"generated_config",
		"git",
		"go",
		"hooks",
		"policy",
		"project",
		"python",
		"repo",
		"security",
		"shell",
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

	err = policy.EncodeBundle(os.Stdout, policy.ExampleBundle())
	if err != nil {
		return fmt.Errorf("encode example bundle: %w", err)
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

	fmt.Fprintln(os.Stdout, "policy bundle valid")

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

	err = policy.ExplainPolicy(os.Stdout, bundle, flags.Arg(0))
	if err != nil {
		return fmt.Errorf("explain policy: %w", err)
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

	fmt.Fprintf(os.Stdout, "wrote policy artifacts to %s\n", outDir)

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
	fmt.Fprintf(os.Stderr, `usage:
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
      [--repo-config repo_config.yaml]
  coding-ethos-policy check-tool-configs --repo REPO [--ethos-root .]
      [--repo-config repo_config.yaml]
  coding-ethos-policy sync-gemini-prompts --repo REPO [--ethos-root .]
      [--primary coding_ethos.yml] [--repo-ethos repo_ethos.yml]
      [--repo-config repo_config.yaml]
  coding-ethos-policy check-gemini-prompts --repo REPO [--ethos-root .]
      [--primary coding_ethos.yml] [--repo-ethos repo_ethos.yml]
      [--repo-config repo_config.yaml]
  coding-ethos-policy sync-agent-skills --repo REPO [--ethos-root .]
      [--primary coding_ethos.yml] [--repo-ethos repo_ethos.yml]
  coding-ethos-policy check-agent-skills --repo REPO [--ethos-root .]
      [--primary coding_ethos.yml] [--repo-ethos repo_ethos.yml]
`)
}
