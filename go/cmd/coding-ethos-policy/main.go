// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"go.yaml.in/yaml/v3"
)

const (
	commandArgIndex   = 1
	commandArgsOffset = 2
	dirMode           = 0o755
)

var (
	errCompileOutDirRequired      = errors.New("compile requires --out-dir")
	errWriteExampleOutDirRequired = errors.New("write-example requires --out-dir")
	errValidateBundleRequired     = errors.New("validate requires --bundle")
	errExplainBundleRequired      = errors.New("explain requires --bundle")
	errExplainPolicyIDRequired    = errors.New("explain requires exactly one policy ID")
	errInvalidBundle              = errors.New("invalid policy bundle")
)

type configTraceReport struct {
	ConfigSections     []string `json:"config_sections"`
	RepoConfigSections []string `json:"repo_config_sections,omitempty"`
	Config             string   `json:"config"`
	RepoConfig         string   `json:"repo_config,omitempty"`
	Status             string   `json:"status"`
	DispatchScopes     int      `json:"dispatch_scopes"`
	EvidenceMaps       int      `json:"evidence_maps"`
	Policies           int      `json:"policies"`
}

func main() {
	if len(os.Args) < commandArgsOffset {
		usage()
		os.Exit(commandArgsOffset)
	}

	var err error

	switch os.Args[commandArgIndex] {
	case "compile":
		err = compile(os.Args[commandArgsOffset:])
	case "dump-example":
		err = dumpExample(os.Args[commandArgsOffset:])
	case "write-example":
		err = writeExample(os.Args[commandArgsOffset:])
	case "validate":
		err = validate(os.Args[commandArgsOffset:])
	case "explain":
		err = explain(os.Args[commandArgsOffset:])
	case "config-trace":
		err = configTrace(os.Args[commandArgsOffset:])
	default:
		usage()
		os.Exit(commandArgsOffset)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func configTrace(args []string) error {
	flags := flag.NewFlagSet("config-trace", flag.ExitOnError)
	primary := flags.String("primary", "coding_ethos.yml", "Path to coding_ethos.yml")
	config := flags.String("config", "config.yaml", "Path to config.yaml")
	repoEthos := flags.String("repo-ethos", "", "Optional path to repo_ethos.yml")
	repoConfig := flags.String("repo-config", "", "Optional path to repo_config.yaml")
	jsonOutput := flags.Bool("json", false, "Emit JSON output")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse config-trace flags: %w", err)
	}

	configSections, err := validatedConfigSections(*config)
	if err != nil {
		return err
	}
	repoConfigSections := []string{}
	if strings.TrimSpace(*repoConfig) != "" {
		repoConfigSections, err = validatedConfigSections(*repoConfig)
		if err != nil {
			return err
		}
	}

	bundle, _, err := policy.Compile(policy.CompileOptions{
		Primary:    *primary,
		RepoEthos:  *repoEthos,
		Config:     *config,
		RepoConfig: *repoConfig,
	})
	if err != nil {
		return fmt.Errorf("compile policy bundle: %w", err)
	}
	if err := bundle.Validate(); err != nil {
		return fmt.Errorf(
			"%w:\n%s",
			errInvalidBundle,
			policy.FormatValidationError(err),
		)
	}

	report := configTraceReport{
		Config:             *config,
		RepoConfig:         *repoConfig,
		ConfigSections:     configSections,
		RepoConfigSections: repoConfigSections,
		Status:             "valid",
		Policies:           len(bundle.Policies),
		EvidenceMaps:       len(bundle.EvidenceMaps),
		DispatchScopes:     dispatchScopeCount(bundle),
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	fmt.Fprintf(os.Stdout, "status: %s\n", report.Status)
	fmt.Fprintf(os.Stdout, "config: %s\n", report.Config)
	fmt.Fprintf(os.Stdout, "config_sections: %s\n", strings.Join(report.ConfigSections, ", "))
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

	return nil
}

func validatedConfigSections(path string) ([]string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	decoded := map[string]any{}
	if err := yaml.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	sections := make([]string, 0, len(decoded))
	for section := range decoded {
		if !knownConfigSection(section) {
			return nil, fmt.Errorf(
				"unknown top-level config section %q in %s",
				section,
				path,
			)
		}
		sections = append(sections, section)
	}
	sort.Strings(sections)

	return sections, nil
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
  coding-ethos-policy explain --bundle policy-bundle.json POLICY_ID
  coding-ethos-policy config-trace [--primary coding_ethos.yml] [--config config.yaml]
      [--repo-config repo_config.yaml] [--json]
`)
}
