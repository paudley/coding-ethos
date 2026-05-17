// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolconfigs

import "fmt"

const gitLabSARIFConfigTemplate = `workflow:
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'

stages:
  - policy
%s%s

coding_ethos_sarif:
  stage: policy
  interruptible: true
  timeout: %dm
  variables:
    CODING_ETHOS_PATH: %s
    CODING_ETHOS_REPO_ROOT: %s
    CODING_ETHOS_GATE_COMMAND: %s
    CODING_ETHOS_SARIF_PATH: %s
    CODING_ETHOS_SANDBOX_MODE: %s
    CODING_ETHOS_FILES: ""
  script:
    - make -C "$CODING_ETHOS_PATH" build
    - |
      if [ -n "$CODING_ETHOS_GATE_COMMAND" ]; then
        ethos_path="$(cd "$CODING_ETHOS_PATH" && pwd)"
        export PATH="$ethos_path/bin:$PATH"
        cd "$CODING_ETHOS_REPO_ROOT"
        bash -c "$CODING_ETHOS_GATE_COMMAND"
        cd "$CI_PROJECT_DIR"
      fi
    - |
      "$CODING_ETHOS_PATH/bin/coding-ethos-run" ci-sarif --provider gitlab
  artifacts:
    when: always
    expire_in: %s
    paths:
      - "$CODING_ETHOS_SARIF_PATH"
      - .coding-ethos/lint-runs/
      - .coding-ethos/hook-runs/
%s%s`

type gitLabSARIFSettings struct {
	CodingEthosPath  string
	RepoRoot         string
	GateCommand      string
	TestCommand      string
	BuildCommand     string
	PackageCheck     string
	SARIFPath        string
	SandboxMode      string
	ArtifactExpireIn string
	DistArtifactPath string
	TestStage        string
	BuildStage       string
	TimeoutMinutes   int
}

func renderGitLabSARIFConfig(config configMap) (string, error) {
	settings, err := gitLabSARIFSettingsFromConfig(config)
	if err != nil {
		return "", err
	}

	return spdxHeader + fmt.Sprintf(
		gitLabSARIFConfigTemplate,
		settings.TestStage,
		settings.BuildStage,
		settings.TimeoutMinutes,
		settings.CodingEthosPath,
		settings.RepoRoot,
		settings.GateCommand,
		settings.SARIFPath,
		settings.SandboxMode,
		settings.ArtifactExpireIn,
		renderGitLabTestJob(
			settings.TestCommand,
			settings.TimeoutMinutes,
			settings.ArtifactExpireIn,
		),
		renderGitLabBuildJob(
			settings.BuildCommand,
			settings.PackageCheck,
			settings.TimeoutMinutes,
			settings.ArtifactExpireIn,
			settings.DistArtifactPath,
		),
	), nil
}

func gitLabSARIFSettingsFromConfig(config configMap) (gitLabSARIFSettings, error) {
	sandboxMode, err := configuredChoice(
		config,
		"generated_config.ci.gitlab.sandbox_mode",
		"required",
		sandboxModes(),
	)
	if err != nil {
		return gitLabSARIFSettings{}, err
	}

	settings := gitLabSARIFSettings{
		CodingEthosPath: configuredString(
			config,
			"generated_config.ci.gitlab.coding_ethos_path",
			".",
		),
		RepoRoot:     gitLabConfiguredString(config, "repo_root", "."),
		GateCommand:  gitLabConfiguredString(config, "gate_command", "make check"),
		TestCommand:  gitLabConfiguredString(config, "test_command", ""),
		BuildCommand: gitLabConfiguredString(config, "build_command", ""),
		PackageCheck: gitLabConfiguredString(config, "package_check_command", ""),
		SARIFPath: gitLabConfiguredString(
			config,
			"sarif_path",
			"coding-ethos.sarif",
		),
		SandboxMode:    sandboxMode,
		TimeoutMinutes: gitLabConfiguredInt(config, "timeout_minutes"),
		ArtifactExpireIn: configuredString(
			config,
			"generated_config.ci.gitlab.artifact_expire_in",
			"7 days",
		),
		DistArtifactPath: configuredString(
			config,
			"generated_config.ci.gitlab.dist_artifact_path",
			"dist/",
		),
	}
	settings.TestStage = gitLabStage(settings.TestCommand, "test")
	settings.BuildStage = gitLabStage(settings.BuildCommand, "build")

	return settings, nil
}

func gitLabConfiguredString(config configMap, key, fallback string) string {
	return configuredString(config, "generated_config.ci.gitlab."+key, fallback)
}

func gitLabConfiguredInt(config configMap, key string) int {
	return configuredInt(
		config,
		"generated_config.ci.gitlab."+key,
		defaultCITimeoutMinutes,
	)
}

func gitLabStage(command, stage string) string {
	if command == "" {
		return ""
	}

	return "  - " + stage + "\n"
}

func renderGitLabTestJob(
	testCommand string,
	timeoutMinutes int,
	artifactExpireIn string,
) string {
	if testCommand == "" {
		return ""
	}

	return fmt.Sprintf(`
coding_ethos_test:
  stage: test
  interruptible: true
  timeout: %dm
  script:
    - %s
  artifacts:
    when: always
    expire_in: %s
    paths:
      - .coding-ethos/
`, timeoutMinutes, testCommand, artifactExpireIn)
}

func renderGitLabBuildJob(
	buildCommand string,
	packageCheckCommand string,
	timeoutMinutes int,
	artifactExpireIn string,
	distArtifactPath string,
) string {
	if buildCommand == "" {
		return ""
	}

	packageCheckStep := ""
	if packageCheckCommand != "" {
		packageCheckStep = fmt.Sprintf("    - %s\n", packageCheckCommand)
	}

	return fmt.Sprintf(`
coding_ethos_build:
  stage: build
  interruptible: true
  timeout: %dm
  script:
    - %s
%s  artifacts:
    when: always
    expire_in: %s
    paths:
      - %s
`, timeoutMinutes, buildCommand, packageCheckStep, artifactExpireIn, distArtifactPath)
}
