// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

var (
	errNoCompiledPrinciples = apperror.StaticError("no principles found")
	errNoCompiledPolicies   = apperror.StaticError("no enabled policies found")
)

const (
	defaultBundleBaseParts = 2
	maxReminderFrequency   = 100
)

type CompileOptions struct {
	GeneratedAt string
	BundleID    string
	Primary     string
	RepoEthos   string
	Config      string
	RepoConfig  string
}

type compileInputPayloads struct {
	Primary           map[string]any
	Config            map[string]any
	SourceHashes      map[string]string
	RepoConfig        map[string]any
	ExpressionSources []expressionPolicySource
}

func Compile(options CompileOptions) (Bundle, Metadata, error) {
	options = normalizedCompileOptions(options)

	inputs, err := compileInputs(options)
	if err != nil {
		return Bundle{}, Metadata{}, err
	}

	principles := compilePrinciples(inputs.Primary)
	if len(principles) == 0 {
		return Bundle{}, Metadata{}, fmt.Errorf(
			"compile principles: %w in %s",
			errNoCompiledPrinciples,
			options.Primary,
		)
	}

	policies, err := compilePolicies(
		inputs.Config,
		inputs.RepoConfig,
		inputs.ExpressionSources,
		principles,
		sourceRoot(options.Config),
	)
	if err != nil {
		return Bundle{}, Metadata{}, err
	}

	if len(policies) == 0 {
		return Bundle{}, Metadata{}, fmt.Errorf(
			"compile policies: %w in %s",
			errNoCompiledPolicies,
			options.Config,
		)
	}

	bundleID := options.BundleID
	if bundleID == "" {
		bundleID = defaultBundleID(options.Primary, options.Config, inputs.SourceHashes)
	}

	bundle := Bundle{
		Version:     1,
		BundleID:    bundleID,
		GeneratedAt: options.GeneratedAt,
		Sources: Sources{
			Ethos: SourcePair{
				Primary: options.Primary,
				Repo:    options.RepoEthos,
			},
			Enforcement: SourcePair{
				Primary: options.Config,
				Repo:    options.RepoConfig,
			},
		},
		Advice:     compileAdvice(inputs.Primary, inputs.Config, principles),
		Principles: principles,
		Policies:   policies,
		Skills:     compileSkills(inputs.Primary, principles, options.Primary),
		Dispatch:   compileDispatch(policies),
		EvidenceMaps: compileEvidenceMaps(
			inputs.Config,
			principles,
		),
	}

	err = bundle.Validate()
	if err != nil {
		return Bundle{}, Metadata{}, err
	}

	metadata, err := BuildMetadata(bundle, inputs.SourceHashes)
	if err != nil {
		return Bundle{}, Metadata{}, err
	}

	return bundle, metadata, nil
}

func normalizedCompileOptions(options CompileOptions) CompileOptions {
	if options.Primary == "" {
		options.Primary = "coding_ethos.yml"
	}

	if options.Config == "" {
		options.Config = "config.yaml"
	}

	if options.GeneratedAt == "" {
		options.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}

	return options
}

func sourceRoot(path string) string {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Dir(path)
	}

	return filepath.Dir(absolutePath)
}

func sourceFileName(path, fallback string) string {
	if path == "" {
		return fallback
	}

	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) {
		return fallback
	}

	return name
}

func compileInputs(options CompileOptions) (compileInputPayloads, error) {
	primaryPayload, primaryHash, err := loadYAMLFile(options.Primary)
	if err != nil {
		return compileInputPayloads{}, err
	}

	configPayload, configHash, err := loadYAMLFile(options.Config)
	if err != nil {
		return compileInputPayloads{}, err
	}

	sourceHashes := map[string]string{
		options.Primary: primaryHash,
		options.Config:  configHash,
	}
	expressionSources := []expressionPolicySource{}

	source, found, err := expressionPolicySourceFromConfig(
		configPayload,
		sourceFileName(options.Config, "config.yaml"),
	)
	if err != nil {
		return compileInputPayloads{}, err
	}

	if found {
		expressionSources = append(expressionSources, source)
	}

	primaryPayload, err = mergeOptionalYAML(
		primaryPayload,
		options.RepoEthos,
		sourceHashes,
	)
	if err != nil {
		return compileInputPayloads{}, err
	}

	expressionSources = append(
		expressionSources,
		expressionPolicySourcesFromPrinciples(
			primaryPayload,
			sourceFileName(options.Primary, "coding_ethos.yml"),
		)...,
	)

	var repoConfigPayload map[string]any

	if options.RepoConfig != "" && fileExists(options.RepoConfig) {
		repoConfigPayload, err = mergeRepoConfigInput(
			options,
			sourceHashes,
			&expressionSources,
		)
		if err != nil {
			return compileInputPayloads{}, err
		}

		configPayload = mergeMaps(configPayload, repoConfigPayload)
	}

	return compileInputPayloads{
		Primary:           primaryPayload,
		Config:            configPayload,
		RepoConfig:        repoConfigPayload,
		ExpressionSources: expressionSources,
		SourceHashes:      sourceHashes,
	}, nil
}

func mergeRepoConfigInput(
	options CompileOptions,
	sourceHashes map[string]string,
	expressionSources *[]expressionPolicySource,
) (map[string]any, error) {
	repoConfigPayload, repoConfigHash, err := loadYAMLFile(options.RepoConfig)
	if err != nil {
		return nil, err
	}

	sourceHashes[options.RepoConfig] = repoConfigHash

	source, found, err := expressionPolicySourceFromConfig(
		repoConfigPayload,
		sourceFileName(options.RepoConfig, "repo_config.yaml"),
	)
	if err != nil {
		return nil, err
	}

	if found {
		*expressionSources = append(*expressionSources, source)
	}

	return repoConfigPayload, nil
}

func mergeOptionalYAML(
	base map[string]any,
	path string,
	sourceHashes map[string]string,
) (map[string]any, error) {
	if path == "" || !fileExists(path) {
		return base, nil
	}

	overlay, hash, err := loadYAMLFile(path)
	if err != nil {
		return nil, err
	}

	sourceHashes[path] = hash

	return mergeMaps(base, overlay), nil
}

func loadYAMLFile(path string) (map[string]any, string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read YAML %s: %w", path, err)
	}

	var decoded map[string]any

	err = yaml.Unmarshal(payload, &decoded)
	if err != nil {
		return nil, "", fmt.Errorf("parse YAML %s: %w", path, err)
	}

	sum := sha256.Sum256(payload)

	return decoded, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func compilePrinciples(payload map[string]any) map[string]Principle {
	principles := map[string]Principle{}

	rawPrinciples, found := payload["principles"].([]any)
	if !found {
		return principles
	}

	for _, raw := range rawPrinciples {
		item, found := raw.(map[string]any)
		if !found {
			continue
		}

		principleID := stringValue(item["id"])
		if principleID == "" {
			continue
		}

		principles[principleID] = Principle{
			ID:         principleID,
			Order:      intValue(item["order"]),
			Title:      stringValue(item["title"]),
			Summary:    stringValue(item["summary"]),
			Directive:  stringValue(item["directive"]),
			QuickRef:   stringSlice(item["quick_ref"]),
			Tags:       stringSlice(item["tags"]),
			Related:    stringSlice(item["related"]),
			AgentHints: stringMap(item["agent_hints"]),
			DetailPath: filepath.ToSlash(
				filepath.Join(".agents", "ethos", principleID+".md"),
			),
		}
	}

	return principles
}

func compileSkills(
	payload map[string]any,
	principles map[string]Principle,
	sourceFile string,
) map[string]Skill {
	rawSkills, found := payload["skills"].([]any)
	if !found {
		return nil
	}

	skills := map[string]Skill{}

	for _, raw := range rawSkills {
		item, found := raw.(map[string]any)
		if !found {
			continue
		}

		skillID := stringValue(item["id"])
		if skillID == "" {
			continue
		}

		skill := Skill{
			ID:          skillID,
			Title:       stringValue(item["title"]),
			Description: stringValue(item["description"]),
			ShortHint:   stringValue(item["short_hint"]),
			Focus:       stringValue(item["focus"]),
			PrincipleIDs: principleRefs(
				principles,
				stringSlice(item["principle_ids"])...),
			TriggerTerms:     stringSlice(item["trigger_terms"]),
			RemediationSteps: stringSlice(item["remediation_steps"]),
			Source: SourceRef{
				File: sourceFile,
				Path: "skills." + skillID,
			},
		}
		if skill.Title == "" || skill.Description == "" ||
			len(skill.PrincipleIDs) == 0 {
			continue
		}

		skills[skillID] = skill
	}

	if len(skills) == 0 {
		return nil
	}

	return skills
}

func compileAdvice(
	ethos map[string]any,
	config map[string]any,
	principles map[string]Principle,
) Advice {
	return Advice{
		Reminders: compileReminderConfig(ethos, config, principles),
	}
}

func compileReminderConfig(
	ethos map[string]any,
	config map[string]any,
	principles map[string]Principle,
) ReminderConfig {
	reminders := deriveReminderConfigFromEthos(ethos, principles)
	if reminders.AmbientFrequencyPercent == 0 {
		reminders.AmbientFrequencyPercent = defaultReminderAmbientFrequencyPercent
	}

	if len(reminders.Items) == 0 {
		reminders = defaultReminderConfig()
	}

	configuredPercent := intAt(
		config,
		[]string{"agent_advice", "reminders", "ambient_frequency_percent"},
		0,
	)
	if configuredPercent > 0 {
		reminders.AmbientFrequencyPercent = clampPercent(configuredPercent)
	}

	configuredFrequency := intAt(
		config,
		[]string{"agent_advice", "reminders", "quiet_frequency"},
		0,
	)
	if configuredFrequency > 0 {
		reminders.QuietFrequency = configuredFrequency
		if configuredPercent == 0 {
			reminders.AmbientFrequencyPercent = frequencyToPercent(configuredFrequency)
		}
	}

	return reminders
}

func deriveReminderConfigFromEthos(
	ethos map[string]any,
	principles map[string]Principle,
) ReminderConfig {
	rawPrinciples, found := ethos["principles"].([]any)
	if !found {
		return ReminderConfig{}
	}

	items := []EthosReminder{}

	for _, raw := range rawPrinciples {
		item, found := raw.(map[string]any)
		if !found {
			continue
		}

		principleID := stringValue(item["id"])
		if _, found := principles[principleID]; !found {
			continue
		}

		items = append(
			items,
			ethosAxiomsFromPrincipleItem(item, principles[principleID])...)
	}

	return ReminderConfig{
		AmbientFrequencyPercent: defaultReminderAmbientFrequencyPercent,
		QuietFrequency:          defaultReminderQuietFrequency,
		Items:                   items,
	}
}

func ethosAxiomsFromPrincipleItem(
	item map[string]any,
	principle Principle,
) []EthosReminder {
	reminders := ethosAxiomsFromExplicitItems(item, principle)
	if len(reminders) > 0 {
		return reminders
	}

	quickRef := principle.QuickRef
	if len(quickRef) > 0 {
		reminders := make([]EthosReminder, 0, len(quickRef))
		for _, axiom := range quickRef {
			reminder := EthosReminder{
				PrincipleID: principle.ID,
				Axiom:       strings.TrimSpace(axiom),
				Action:      principleReminderAction(principle),
			}
			if reminder.Axiom != "" {
				reminders = append(reminders, reminder)
			}
		}

		if len(reminders) > 0 {
			return reminders
		}
	}

	axiom := strings.TrimSpace(
		firstNonEmpty(principle.Summary, principle.Directive, principle.Title),
	)
	if axiom == "" {
		return nil
	}

	return []EthosReminder{{
		PrincipleID: principle.ID,
		Axiom:       axiom,
		Action:      principleReminderAction(principle),
	}}
}

func ethosAxiomsFromExplicitItems(
	item map[string]any,
	principle Principle,
) []EthosReminder {
	rawAxioms, found := item["axioms"].([]any)
	if !found {
		return nil
	}

	reminders := make([]EthosReminder, 0, len(rawAxioms))
	for _, raw := range rawAxioms {
		axiom, found := raw.(map[string]any)
		if !found {
			continue
		}

		reminder := EthosReminder{
			PrincipleID: principle.ID,
			Axiom:       stringValue(axiom["axiom"]),
			Action:      stringValue(axiom["action"]),
		}
		if reminder.Axiom == "" {
			continue
		}

		if reminder.Action == "" {
			reminder.Action = principleReminderAction(principle)
		}

		reminders = append(reminders, reminder)
	}

	return reminders
}

func principleReminderAction(principle Principle) string {
	return firstNonEmpty(principle.Directive, principle.Summary, principle.Title)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func defaultReminderConfig() ReminderConfig {
	return ReminderConfig{
		AmbientFrequencyPercent: defaultReminderAmbientFrequencyPercent,
		QuietFrequency:          defaultReminderQuietFrequency,
		Items: []EthosReminder{
			{
				PrincipleID: "evidence-based-engineering-and-decision-quality",
				Axiom:       "Todo lists prevent partial work from masquerading as completion.",
				Action: sentence(
					"Keep the task list current, mark progress as it happens,",
					"and do not report done while planned work remains.",
				),
			},
			{
				PrincipleID: "no-rationalized-shortcuts",
				Axiom:       "Laziness only moves the cost downstream.",
				Action: sentence(
					"Stop, use the documented path,",
					"and do not trade correctness for completion.",
				),
			},
			{
				PrincipleID: "testing-as-specification",
				Axiom:       "A green process is not the same as a correct result.",
				Action: sentence(
					"Define success by user-visible behavior",
					"and inspect representative output.",
				),
			},
			{
				PrincipleID: "static-analysis-is-the-first-line-of-defense",
				Axiom:       "StaticError analysis is a gate, not background noise.",
				Action:      "Treat the finding as a structural signal and fix the cause.",
			},
			{
				PrincipleID: "no-conditional-imports",
				Axiom:       "Conditional imports are banned.",
				Action: sentence(
					"Use module-scope required imports; if that exposes a cycle,",
					"refactor with SOLID boundaries or a Python Protocol",
					"instead of hiding the dependency.",
				),
			},
			{
				PrincipleID: "linting-as-code-quality-enforcement",
				Axiom:       "A linter warning is review feedback in executable form.",
				Action:      "Resolve it structurally instead of weakening the rule.",
			},
			{
				PrincipleID: "forward-motion-only",
				Axiom:       "History is context, not an excuse.",
				Action:      "Fix the current state with evidence and move forward.",
			},
		},
	}
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}

	if value > maxReminderFrequency {
		return maxReminderFrequency
	}

	return value
}

func frequencyToPercent(frequency int) int {
	if frequency <= 0 {
		return defaultReminderAmbientFrequencyPercent
	}

	return maxReminderFrequency / frequency
}
