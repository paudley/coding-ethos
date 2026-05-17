// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolconfigs

import "fmt"

const defaultCITimeoutMinutes = 30

const githubSARIFWorkflowTemplate = `name: Coding Ethos SARIF Gate

%s

permissions:
  contents: read

jobs:
  coding-ethos:
    name: Coding Ethos SARIF Gate
    runs-on: ubuntu-latest
    timeout-minutes: %d
    permissions:
      actions: read
      contents: read
      security-events: write
    env:
      CODING_ETHOS_PATH: %s
      CODING_ETHOS_REPO_ROOT: %s
      CODING_ETHOS_GATE_COMMAND: %s
      CODING_ETHOS_SARIF_PATH: %s
      CODING_ETHOS_SARIF_CATEGORY: %s
      CODING_ETHOS_SANDBOX_MODE: %s
      CODING_ETHOS_FILES: ""
      CODING_ETHOS_GITHUB_BASE_REF: ${{ github.base_ref }}
      CODING_ETHOS_GITHUB_EVENT_NAME: ${{ github.event_name }}
      CODING_ETHOS_GITHUB_EVENT_BEFORE: ${{ github.event.before }}
      CODING_ETHOS_GITHUB_SHA: ${{ github.sha }}
    steps:
      - name: Check out repository
        uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd
        with:
          fetch-depth: 0
          persist-credentials: false
          submodules: recursive

      - name: Set up Go
        uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c
        with:
          go-version-file: ${{ env.CODING_ETHOS_PATH }}/go/go.mod
          cache-dependency-path: ${{ env.CODING_ETHOS_PATH }}/go/go.sum

      - name: Set up Python
        uses: actions/setup-python@a309ff8b426b58ec0e2a45f0f869d46889d02405
        with:
          python-version: "3.13"

      - name: Install uv
        uses: astral-sh/setup-uv@08807647e7069bb48b6ef5acd8ec9567f424441b
        with:
          enable-cache: true

      - name: Install Bubblewrap
        run: |
          sudo apt-get update
          sudo apt-get install --yes bubblewrap

      - name: Build coding-ethos runtime
        env:
          GITHUB_TOKEN: ${{ github.token }}
        run: make -C "$CODING_ETHOS_PATH" build

      - name: Run project gate
        id: project-gate
        continue-on-error: true
        run: |
          ethos_path="$(cd "$CODING_ETHOS_PATH" && pwd)"
          export PATH="$ethos_path/bin:$PATH"
          if [ -n "$CODING_ETHOS_GATE_COMMAND" ]; then
            cd "$CODING_ETHOS_REPO_ROOT"
            bash -c "$CODING_ETHOS_GATE_COMMAND"
          fi

      - name: Emit coding-ethos SARIF
        id: emit-sarif
        if: ${{ always() }}
        continue-on-error: true
        run: '"$CODING_ETHOS_PATH/bin/coding-ethos-run" ci-sarif --provider github'

      - name: Upload coding-ethos SARIF
        if: ${{ always() && hashFiles(env.CODING_ETHOS_SARIF_PATH) != '' }}
        uses: github/codeql-action/upload-sarif@68bde559dea0fdcac2102bfdf6230c5f70eb485e
        with:
          sarif_file: ${{ env.CODING_ETHOS_SARIF_PATH }}
          category: ${{ env.CODING_ETHOS_SARIF_CATEGORY }}

      - name: Upload coding-ethos audit artifacts
        if: ${{ always() }}
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a
        with:
          name: %s
          if-no-files-found: ignore
          path: |
            ${{ env.CODING_ETHOS_SARIF_PATH }}
            ${{ env.CODING_ETHOS_REPO_ROOT }}/.coding-ethos/lint-runs/
            ${{ env.CODING_ETHOS_REPO_ROOT }}/.coding-ethos/hook-runs/

      - name: Fail on coding-ethos violations
        if: >-
          ${{ steps.project-gate.outcome == 'failure' ||
          steps.emit-sarif.outcome == 'failure' }}
        run: exit 1
`

type githubSARIFWorkflowSettings struct {
	CodingEthosPath string
	RepoRoot        string
	GateCommand     string
	SARIFPath       string
	ArtifactName    string
	SARIFCategory   string
	SandboxMode     string
	Triggers        string
	TimeoutMinutes  int
}

func sandboxModes() map[string]struct{} {
	return map[string]struct{}{
		"auto":     {},
		"off":      {},
		"required": {},
	}
}

func renderGitHubSARIFWorkflow(config configMap) (string, error) {
	settings, err := githubSARIFWorkflowSettingsFromConfig(config)
	if err != nil {
		return "", err
	}

	return spdxHeader + fmt.Sprintf(
		githubSARIFWorkflowTemplate,
		settings.Triggers,
		settings.TimeoutMinutes,
		settings.CodingEthosPath,
		settings.RepoRoot,
		settings.GateCommand,
		settings.SARIFPath,
		settings.SARIFCategory,
		settings.SandboxMode,
		settings.ArtifactName,
	), nil
}

func githubSARIFWorkflowSettingsFromConfig(
	config configMap,
) (githubSARIFWorkflowSettings, error) {
	sandboxMode, err := configuredChoice(
		config,
		"generated_config.ci.github_actions.sandbox_mode",
		"required",
		sandboxModes(),
	)
	if err != nil {
		return githubSARIFWorkflowSettings{}, err
	}

	return githubSARIFWorkflowSettings{
		CodingEthosPath: configuredString(
			config,
			"generated_config.ci.github_actions.coding_ethos_path",
			".",
		),
		RepoRoot: configuredString(
			config,
			"generated_config.ci.github_actions.repo_root",
			".",
		),
		GateCommand: configuredString(
			config,
			"generated_config.ci.github_actions.gate_command",
			"make check",
		),
		SARIFPath: configuredString(
			config,
			"generated_config.ci.github_actions.sarif_path",
			"coding-ethos.sarif",
		),
		ArtifactName: configuredString(
			config,
			"generated_config.ci.github_actions.artifact_name",
			"coding-ethos-audit",
		),
		SARIFCategory: configuredString(
			config,
			"generated_config.ci.github_actions.sarif_category",
			"policy",
		),
		SandboxMode: sandboxMode,
		Triggers:    githubWorkflowTriggers(config),
		TimeoutMinutes: configuredInt(
			config,
			"generated_config.ci.github_actions.timeout_minutes",
			defaultCITimeoutMinutes,
		),
	}, nil
}

func githubWorkflowTriggers(config configMap) string {
	if configuredBool(
		config,
		"generated_config.ci.github_actions.standalone_triggers",
		false,
	) {
		return `on:
  workflow_call:
  pull_request:
  push:
    branches:
      - main
  workflow_dispatch:
`
	}

	return "on:\n  workflow_call:\n"
}
