// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentskills"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/geminiprompts"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

func TestEvaluatePytestGatePassesWhenCommandPasses(t *testing.T) {
	t.Parallel()

	decisions, err := evaluators.EvaluatePytestGate(
		externalPolicy("pytest.gate"),
		evaluators.Context{
			EvaluatorOptions: map[string]any{
				"command": []string{"go", "env", "GOVERSION"},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate pytest gate: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("decisions = %#v, want none", decisions)
	}
}

func TestEvaluateGeneratedConfigFreshnessPassesWhenGeneratedConfigsMatch(t *testing.T) {
	t.Parallel()

	ethosRoot, repoRoot := syncedGeneratedConfigRepo(t)

	decisions, err := evaluators.EvaluateGeneratedConfigFreshness(
		externalPolicy("generated_config.freshness"),
		evaluators.Context{
			Cwd: repoRoot,
			EvaluatorOptions: map[string]any{
				"ethos_root": ethosRoot,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate generated config freshness: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("decisions = %#v, want none", decisions)
	}
}

func TestEvaluateGeneratedConfigFreshnessEmitsFileDiagnostics(t *testing.T) {
	t.Parallel()

	ethosRoot, repoRoot := syncedGeneratedConfigRepo(t)
	corruptGeneratedConfig(t, repoRoot)

	decisions := evaluateGeneratedConfigFreshness(t, ethosRoot, repoRoot)
	assertGeneratedConfigDiagnostics(t, decisions)
	assertGeneratedConfigSARIF(t, decisions)
}

func TestEvaluateGeneratedGeminiPromptsFreshnessPassesWhenPromptPackMatches(
	t *testing.T,
) {
	t.Parallel()

	ethosRoot, repoRoot := syncedGeneratedGeminiPromptsRepo(t)

	decisions, err := evaluators.EvaluateGeneratedGeminiPromptsFreshness(
		externalPolicy("generated_gemini_prompts.freshness"),
		generatedGeminiPromptsContext(ethosRoot, repoRoot),
	)
	if err != nil {
		t.Fatalf("evaluate generated Gemini prompt freshness: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("decisions = %#v, want none", decisions)
	}
}

func TestEvaluateGeneratedGeminiPromptsFreshnessEmitsFileDiagnostics(
	t *testing.T,
) {
	t.Parallel()

	ethosRoot, repoRoot := syncedGeneratedGeminiPromptsRepo(t)
	corruptGeneratedGeminiPromptPack(t, repoRoot)

	decisions := evaluateGeneratedGeminiPromptsFreshness(t, ethosRoot, repoRoot)
	assertGeneratedGeminiPromptsDiagnostics(t, decisions)
	assertGeneratedGeminiPromptsSARIF(t, decisions)
}

func TestEvaluateGeneratedAgentSkillsFreshnessPassesWhenSkillSurfacesMatch(
	t *testing.T,
) {
	t.Parallel()

	ethosRoot, repoRoot := syncedGeneratedAgentSkillsRepo(t)

	decisions, err := evaluators.EvaluateGeneratedAgentSkillsFreshness(
		externalPolicy("generated_agent_skills.freshness"),
		generatedAgentSkillsContext(ethosRoot, repoRoot),
	)
	if err != nil {
		t.Fatalf("evaluate generated agent skill freshness: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("decisions = %#v, want none", decisions)
	}
}

func TestEvaluateGeneratedAgentSkillsFreshnessEmitsFileDiagnostics(
	t *testing.T,
) {
	t.Parallel()

	ethosRoot, repoRoot := syncedGeneratedAgentSkillsRepo(t)
	corruptGeneratedAgentSkill(t, repoRoot)

	decisions := evaluateGeneratedAgentSkillsFreshness(t, ethosRoot, repoRoot)
	assertGeneratedAgentSkillsDiagnostics(t, decisions)
	assertGeneratedAgentSkillsSARIF(t, decisions)
}

func evaluateGeneratedConfigFreshness(
	t *testing.T,
	ethosRoot string,
	repoRoot string,
) []policy.Decision {
	t.Helper()

	decisions, err := evaluators.EvaluateGeneratedConfigFreshness(
		externalPolicy("generated_config.freshness"),
		evaluators.Context{
			Cwd: repoRoot,
			EvaluatorOptions: map[string]any{
				"ethos_root": ethosRoot,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate generated config freshness: %v", err)
	}

	return decisions
}

func evaluateGeneratedGeminiPromptsFreshness(
	t *testing.T,
	ethosRoot string,
	repoRoot string,
) []policy.Decision {
	t.Helper()

	decisions, err := evaluators.EvaluateGeneratedGeminiPromptsFreshness(
		externalPolicy("generated_gemini_prompts.freshness"),
		generatedGeminiPromptsContext(ethosRoot, repoRoot),
	)
	if err != nil {
		t.Fatalf("evaluate generated Gemini prompt freshness: %v", err)
	}

	return decisions
}

func evaluateGeneratedAgentSkillsFreshness(
	t *testing.T,
	ethosRoot string,
	repoRoot string,
) []policy.Decision {
	t.Helper()

	decisions, err := evaluators.EvaluateGeneratedAgentSkillsFreshness(
		externalPolicy("generated_agent_skills.freshness"),
		generatedAgentSkillsContext(ethosRoot, repoRoot),
	)
	if err != nil {
		t.Fatalf("evaluate generated agent skill freshness: %v", err)
	}

	return decisions
}

func assertGeneratedConfigDiagnostics(
	t *testing.T,
	decisions []policy.Decision,
) {
	t.Helper()

	if len(decisions) != 1 {
		t.Fatalf("decision count = %d, want 1", len(decisions))
	}

	diagnostics := decisions[0].Diagnostics
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostic count = %d, want 2: %#v", len(diagnostics), diagnostics)
	}

	if diagnostics[0].Tool != "generated-config" ||
		diagnostics[0].File != "ruff.toml" ||
		diagnostics[0].Code != "generated-config-drift" ||
		diagnostics[0].PolicyID != "generated_config.freshness" {
		t.Fatalf("unexpected generated config diagnostic: %#v", diagnostics[0])
	}

	if diagnostics[1].File != ".code-ethos/tool-config-hashes.json" {
		t.Fatalf("unexpected manifest diagnostic: %#v", diagnostics[1])
	}
}

func assertGeneratedGeminiPromptsDiagnostics(
	t *testing.T,
	decisions []policy.Decision,
) {
	t.Helper()

	if len(decisions) != 1 {
		t.Fatalf("decision count = %d, want 1", len(decisions))
	}

	diagnostics := decisions[0].Diagnostics
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %#v", len(diagnostics), diagnostics)
	}

	if diagnostics[0].Tool != "generated-gemini-prompts" ||
		diagnostics[0].File != geminiprompts.PromptPackPath ||
		diagnostics[0].Code != "generated-gemini-prompt-pack-drift" ||
		diagnostics[0].PolicyID != "generated_gemini_prompts.freshness" {
		t.Fatalf("unexpected generated Gemini prompt diagnostic: %#v", diagnostics[0])
	}
}

func assertGeneratedAgentSkillsDiagnostics(
	t *testing.T,
	decisions []policy.Decision,
) {
	t.Helper()

	if len(decisions) != 1 {
		t.Fatalf("decision count = %d, want 1", len(decisions))
	}

	diagnostics := decisions[0].Diagnostics
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %#v", len(diagnostics), diagnostics)
	}

	if diagnostics[0].Tool != "generated-agent-skills" ||
		diagnostics[0].File != ".codex/skills/agent-operating-discipline/SKILL.md" ||
		diagnostics[0].Code != "generated-agent-skill-drift" ||
		diagnostics[0].PolicyID != "generated_agent_skills.freshness" {
		t.Fatalf("unexpected generated agent skill diagnostic: %#v", diagnostics[0])
	}
}

func assertGeneratedConfigSARIF(t *testing.T, decisions []policy.Decision) {
	t.Helper()

	result := lint.Result{
		Scope:     lint.ScopeSmoke,
		Status:    "blocked",
		Decisions: decisions,
	}

	output, err := hookoutput.FormatLintResult(result, hookoutput.FormatSARIF)
	if err != nil {
		t.Fatalf("format generated config SARIF: %v", err)
	}

	for _, want := range []string{
		`"ruleId": "generated_config.freshness"`,
		`"uri": "ruff.toml"`,
		`"uri": ".code-ethos/tool-config-hashes.json"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated config SARIF missing %q:\n%s", want, output)
		}
	}
}

func assertGeneratedGeminiPromptsSARIF(t *testing.T, decisions []policy.Decision) {
	t.Helper()

	result := lint.Result{
		Scope:     lint.ScopeSmoke,
		Status:    "blocked",
		Decisions: decisions,
	}

	output, err := hookoutput.FormatLintResult(result, hookoutput.FormatSARIF)
	if err != nil {
		t.Fatalf("format generated Gemini prompt SARIF: %v", err)
	}

	for _, want := range []string{
		`"ruleId": "generated_gemini_prompts.freshness"`,
		`"uri": ".code-ethos/gemini/prompt-pack.json"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated Gemini prompt SARIF missing %q:\n%s", want, output)
		}
	}
}

func assertGeneratedAgentSkillsSARIF(t *testing.T, decisions []policy.Decision) {
	t.Helper()

	result := lint.Result{
		Scope:     lint.ScopeSmoke,
		Status:    "blocked",
		Decisions: decisions,
	}

	output, err := hookoutput.FormatLintResult(result, hookoutput.FormatSARIF)
	if err != nil {
		t.Fatalf("format generated agent skill SARIF: %v", err)
	}

	for _, want := range []string{
		`"ruleId": "generated_agent_skills.freshness"`,
		`"uri": ".codex/skills/agent-operating-discipline/SKILL.md"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated agent skill SARIF missing %q:\n%s", want, output)
		}
	}
}

func syncedGeneratedConfigRepo(t *testing.T) (string, string) {
	t.Helper()

	ethosRoot := repoRoot(t)
	repoRoot := t.TempDir()

	_, err := toolconfigs.Sync(ethosRoot, repoRoot, "")
	if err != nil {
		t.Fatalf("sync generated config: %v", err)
	}

	return ethosRoot, repoRoot
}

func syncedGeneratedGeminiPromptsRepo(t *testing.T) (string, string) {
	t.Helper()

	ethosRoot := repoRoot(t)
	repoRoot := t.TempDir()

	_, err := geminiprompts.Sync(generatedGeminiPromptsOptions(
		ethosRoot,
		repoRoot,
	))
	if err != nil {
		t.Fatalf("sync generated Gemini prompt pack: %v", err)
	}

	return ethosRoot, repoRoot
}

func syncedGeneratedAgentSkillsRepo(t *testing.T) (string, string) {
	t.Helper()

	ethosRoot := repoRoot(t)
	repoRoot := t.TempDir()

	_, err := agentskills.Sync(generatedAgentSkillsOptions(ethosRoot, repoRoot))
	if err != nil {
		t.Fatalf("sync generated agent skills: %v", err)
	}

	return ethosRoot, repoRoot
}

func generatedGeminiPromptsContext(
	ethosRoot string,
	repoRoot string,
) evaluators.Context {
	return evaluators.Context{
		Cwd: repoRoot,
		EvaluatorOptions: map[string]any{
			"ethos_root": ethosRoot,
			"primary":    filepath.Join(ethosRoot, "coding_ethos.yml"),
			"repo_ethos": filepath.Join(ethosRoot, "repo_ethos.yml"),
		},
	}
}

func generatedAgentSkillsContext(
	ethosRoot string,
	repoRoot string,
) evaluators.Context {
	return evaluators.Context{
		Cwd: repoRoot,
		EvaluatorOptions: map[string]any{
			"ethos_root": ethosRoot,
			"primary":    filepath.Join(ethosRoot, "coding_ethos.yml"),
			"repo_ethos": filepath.Join(ethosRoot, "repo_ethos.yml"),
		},
	}
}

func generatedGeminiPromptsOptions(
	ethosRoot string,
	repoRoot string,
) geminiprompts.Options {
	return geminiprompts.Options{
		EthosRoot: ethosRoot,
		RepoRoot:  repoRoot,
		Primary:   filepath.Join(ethosRoot, "coding_ethos.yml"),
		RepoEthos: filepath.Join(ethosRoot, "repo_ethos.yml"),
	}
}

func generatedAgentSkillsOptions(
	ethosRoot string,
	repoRoot string,
) agentskills.Options {
	return agentskills.Options{
		EthosRoot: ethosRoot,
		RepoRoot:  repoRoot,
		Primary:   filepath.Join(ethosRoot, "coding_ethos.yml"),
		RepoEthos: filepath.Join(ethosRoot, "repo_ethos.yml"),
	}
}

func corruptGeneratedConfig(t *testing.T, repoRoot string) {
	t.Helper()

	staleConfig := filepath.Join(repoRoot, "ruff.toml")

	err := os.WriteFile(staleConfig, []byte("# stale\n"), 0o600)
	if err != nil {
		t.Fatalf("corrupt generated config: %v", err)
	}

	staleManifest := filepath.Join(
		repoRoot,
		filepath.FromSlash(toolconfigs.HashManifestPath),
	)

	err = os.WriteFile(staleManifest, []byte("{}\n"), 0o600)
	if err != nil {
		t.Fatalf("corrupt generated manifest: %v", err)
	}
}

func corruptGeneratedGeminiPromptPack(t *testing.T, repoRoot string) {
	t.Helper()

	path := filepath.Join(repoRoot, filepath.FromSlash(geminiprompts.PromptPackPath))

	err := os.WriteFile(path, []byte("{\"stale\": true}\n"), 0o600)
	if err != nil {
		t.Fatalf("corrupt generated Gemini prompt pack: %v", err)
	}
}

func corruptGeneratedAgentSkill(t *testing.T, repoRoot string) {
	t.Helper()

	path := filepath.Join(
		repoRoot,
		".codex",
		"skills",
		"agent-operating-discipline",
		"SKILL.md",
	)

	err := os.WriteFile(path, []byte("stale\n"), 0o600)
	if err != nil {
		t.Fatalf("corrupt generated agent skill: %v", err)
	}
}

func TestEvaluateExternalCommandAttachesParsedDiagnostics(t *testing.T) {
	t.Parallel()

	decisions, err := evaluators.EvaluateExternalCommand(
		externalPolicy("python.lint"),
		evaluators.Context{
			EvaluatorOptions: map[string]any{
				"command": []string{
					"sh",
					"-c",
					printRuffDiagnosticCommand + "; exit 1",
				},
				"parser": "ruff",
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate external command: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count = %d, want 1", len(decisions))
	}

	diagnostics := decisions[0].Diagnostics
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %#v", len(diagnostics), diagnostics)
	}

	if diagnostics[0].Tool != "ruff" ||
		diagnostics[0].File != "pkg/app.py" ||
		diagnostics[0].Code != "F401" {
		t.Fatalf("unexpected diagnostic: %#v", diagnostics[0])
	}
}

func externalPolicy(policyID string) policy.Policy {
	return policy.Policy{
		ID:              policyID,
		DefaultSeverity: blockDecision,
		Message:         "external command failed",
		SupportedModes:  []string{blockDecision, "record"},
		Evaluators:      []policy.Evaluator{{Kind: "external", Name: policyID}},
	}
}

const printRuffDiagnosticCommand = `printf '%s\n' ` +
	`'[{"filename":"pkg/app.py","code":"F401","message":"unused import",` +
	`"location":{"row":4,"column":8}}]'`
