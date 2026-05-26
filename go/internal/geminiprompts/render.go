// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package geminiprompts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

const PromptPackPath = ".coding-ethos/gemini/prompt-pack.json"

const (
	promptPackDirMode  = 0o700
	promptPackFileMode = 0o600
)

type Options struct {
	EthosRoot  string
	RepoRoot   string
	Primary    string
	RepoEthos  string
	RepoConfig string
}

func Render(options Options) (string, error) {
	options = normalizeOptions(options)

	principles, repo, err := loadInputs(options.Primary, options.RepoEthos)
	if err != nil {
		return "", err
	}

	config, err := toolconfigs.LoadMergedConfig(
		options.EthosRoot,
		options.RepoRoot,
		options.RepoConfig,
	)
	if err != nil {
		return "", fmt.Errorf("load merged tool config: %w", err)
	}

	context := buildContext(principles, repo, config, options.RepoRoot)

	prompts, err := renderPrompts(options.EthosRoot, context)
	if err != nil {
		return "", err
	}

	payload := map[string]any{
		"version": 1,
		"sources": map[string]string{
			"primary":    relativePath(options.Primary, options.EthosRoot),
			"repo_ethos": relativePath(options.RepoEthos, options.RepoRoot),
			"repo_config": relativePath(
				resolvedRepoConfig(options.RepoRoot, options.RepoConfig),
				options.RepoRoot,
			),
		},
		"project": map[string]string{
			"name":          context.ProjectName,
			"context":       context.ProjectContext,
			"repo_overview": context.RepoOverview,
		},
		"grounding": map[string]any{
			"principles":        context.Principles,
			"repo_commands":     context.RepoCommands,
			"repo_paths":        context.RepoPaths,
			"repo_notes":        context.RepoNotes,
			"gemini_notes":      context.GeminiNotes,
			"enforcement_notes": context.EnforcementNotes,
		},
		"checks":  checkSpecPayloads(checkSpecs()),
		"prompts": prompts,
	}

	var buffer bytes.Buffer

	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err = encoder.Encode(payload)
	if err != nil {
		return "", fmt.Errorf("render prompt pack json: %w", err)
	}

	return buffer.String(), nil
}

func Sync(options Options) ([]string, error) {
	rendered, err := Render(options)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(options.RepoRoot, filepath.FromSlash(PromptPackPath))

	err = os.MkdirAll(filepath.Dir(path), promptPackDirMode)
	if err != nil {
		return nil, fmt.Errorf("create prompt pack dir: %w", err)
	}

	err = os.WriteFile(path, []byte(rendered), promptPackFileMode)
	if err != nil {
		return nil, fmt.Errorf("write prompt pack %s: %w", path, err)
	}

	return []string{path}, nil
}

func Check(options Options) ([]string, error) {
	rendered, err := Render(options)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(options.RepoRoot, filepath.FromSlash(PromptPackPath))

	current, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		return []string{path}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read prompt pack %s: %w", path, err)
	}

	if string(current) != rendered {
		return []string{path}, nil
	}

	return nil, nil
}

func normalizeOptions(options Options) Options {
	if strings.TrimSpace(options.EthosRoot) == "" {
		options.EthosRoot = "."
	}

	if strings.TrimSpace(options.Primary) == "" {
		options.Primary = filepath.Join(options.EthosRoot, "coding_ethos.yml")
	}

	if strings.TrimSpace(options.RepoEthos) == "" {
		options.RepoEthos = filepath.Join(options.RepoRoot, "repo_ethos.yml")
	}

	return options
}

func relativePath(path, root string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}

	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(filepath.Clean(path))
	}

	return filepath.ToSlash(rel)
}

func resolvedRepoConfig(repoRoot, repoConfig string) string {
	if strings.TrimSpace(repoConfig) != "" {
		return repoConfig
	}

	for _, name := range []string{"repo_config.yaml", "repo_config.yml"} {
		candidate := filepath.Join(repoRoot, name)

		_, err := os.Stat(filepath.Clean(candidate))
		if err == nil {
			return candidate
		}
	}

	return filepath.Join(repoRoot, "repo_config.yaml")
}

func buildContext(
	principles []principle,
	repo repoData,
	config map[string]any,
	repoRoot string,
) promptContext {
	payloads := make([]principlePayload, 0, len(principles))
	for _, item := range principles {
		payloads = append(payloads, principlePayload{
			ID:        item.ID,
			Order:     item.Order,
			Title:     item.Title,
			Directive: item.Directive,
			Summary:   item.Summary,
			QuickRef:  item.QuickRef,
			AgentHint: strings.TrimSpace(item.AgentHints["gemini"]),
		})
	}

	projectName := firstNonEmpty(
		repo.Name,
		filepath.Base(repoRoot),
		stringInConfig(config, "project", "name"),
	)
	projectContext := firstNonEmpty(
		repo.Overview,
		stringInConfig(config, "project", "review_context"),
		"shared repository automation and engineering tooling",
	)

	return promptContext{
		ProjectName:      firstNonEmpty(projectName, "repository"),
		ProjectContext:   projectContext,
		RepoOverview:     repo.Overview,
		RepoCommands:     repo.Commands,
		RepoPaths:        repo.Paths,
		RepoNotes:        repo.Notes,
		GeminiNotes:      repo.GeminiNotes,
		Principles:       payloads,
		EnforcementNotes: enforcementNotes(config),
	}
}
