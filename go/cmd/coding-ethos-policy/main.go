// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "compile":
		err = compile(os.Args[2:])
	case "dump-example":
		err = dumpExample(os.Args[2:])
	case "write-example":
		err = writeExample(os.Args[2:])
	case "validate":
		err = validate(os.Args[2:])
	case "explain":
		err = explain(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func compile(args []string) error {
	flags := flag.NewFlagSet("compile", flag.ExitOnError)
	outDir := flags.String("out-dir", "", "Directory to write policy artifacts into")
	primary := flags.String("primary", "coding_ethos.yml", "Path to coding_ethos.yml")
	repoEthos := flags.String("repo-ethos", "", "Optional path to repo_ethos.yml")
	config := flags.String("config", "config.yaml", "Path to config.yaml")
	repoConfig := flags.String("repo-config", "", "Optional path to repo_config.yaml")
	bundleID := flags.String("bundle-id", "", "Optional bundle ID override")
	generatedAt := flags.String("generated-at", "", "Optional generated_at timestamp override")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *outDir == "" {
		return fmt.Errorf("compile requires --out-dir")
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
		return err
	}
	return writeArtifacts(*outDir, bundle, metadata)
}

func dumpExample(args []string) error {
	flags := flag.NewFlagSet("dump-example", flag.ExitOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	return policy.EncodeBundle(os.Stdout, policy.ExampleBundle())
}

func writeExample(args []string) error {
	flags := flag.NewFlagSet("write-example", flag.ExitOnError)
	outDir := flags.String("out-dir", "", "Directory to write policy artifacts into")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *outDir == "" {
		return fmt.Errorf("write-example requires --out-dir")
	}

	bundle := policy.ExampleBundle()
	metadata, err := policy.BuildMetadata(bundle, nil)
	if err != nil {
		return err
	}
	return writeArtifacts(*outDir, bundle, metadata)
}

func validate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *bundlePath == "" {
		return fmt.Errorf("validate requires --bundle")
	}

	file, err := os.Open(*bundlePath)
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return err
	}
	if err := bundle.Validate(); err != nil {
		return fmt.Errorf("invalid policy bundle:\n%s", policy.FormatValidationError(err))
	}
	fmt.Fprintln(os.Stdout, "policy bundle valid")
	return nil
}

func explain(args []string) error {
	flags := flag.NewFlagSet("explain", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *bundlePath == "" {
		return fmt.Errorf("explain requires --bundle")
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("explain requires exactly one policy ID")
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		return err
	}
	if err := bundle.Validate(); err != nil {
		return fmt.Errorf("invalid policy bundle:\n%s", policy.FormatValidationError(err))
	}
	return policy.ExplainPolicy(os.Stdout, bundle, flags.Arg(0))
}

func readBundle(path string) (policy.Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()
	return policy.DecodeBundle(file)
}

func writeArtifacts(outDir string, bundle policy.Bundle, metadata policy.Metadata) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := writeBundleFile(filepath.Join(outDir, "policy-bundle.json"), bundle); err != nil {
		return err
	}
	if err := writeMetadataFile(filepath.Join(outDir, "policy-metadata.json"), metadata); err != nil {
		return err
	}
	if err := writeSummaryFile(filepath.Join(outDir, "policy-summary.md"), bundle); err != nil {
		return err
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
	return policy.EncodeBundle(file, bundle)
}

func writeMetadataFile(path string, metadata policy.Metadata) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create metadata file: %w", err)
	}
	defer file.Close()
	return policy.EncodeMetadata(file, metadata)
}

func writeSummaryFile(path string, bundle policy.Bundle) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create summary file: %w", err)
	}
	defer file.Close()
	return policy.WriteSummary(file, bundle)
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  coding-ethos-policy compile --out-dir .git/coding-ethos-hooks/policy [--primary coding_ethos.yml] [--config config.yaml]
  coding-ethos-policy dump-example
  coding-ethos-policy write-example --out-dir .git/coding-ethos-hooks/policy
  coding-ethos-policy validate --bundle policy-bundle.json
  coding-ethos-policy explain --bundle policy-bundle.json POLICY_ID
`)
}
