// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	gitSignedCommitsPolicyID = "git.signed_commits_required"
	gitSignedPushPolicyID    = "git.signed_push_required"
	gitCommitOperation       = "commit"
	gitPushOperation         = "push"
	goodGitSignatureStatus   = "G"
	gitTrueValue             = "true"
)

func gitSigningPreDecisions(options Options, operation string) []policy.Decision {
	if operation == "" || !gitSigningEnforcementEnabled(options.Cwd) {
		return nil
	}

	decisions := gitSigningConfigDecisions(options)

	if operation == gitCommitOperation {
		decisions = append(decisions, gitCommitSignatureDecisions(options)...)
	}

	if operation == gitPushOperation {
		decisions = append(decisions, gitPushSignatureDecisions(options)...)
		decisions = append(decisions, gitUnsignedOutgoingCommitDecisions(options)...)
	}

	return decisions
}

func gitCommitSignatureDecisions(options Options) []policy.Decision {
	argv := ParseArgv(options.Argv).Argv
	for _, arg := range argv {
		if arg == "--no-gpg-sign" || arg == "--gpg-sign=false" {
			return []policy.Decision{gitSigningDecision(
				gitSignedCommitsPolicyID,
				"Git commit signing cannot be disabled.",
				"Remove --no-gpg-sign and keep commit.gpgsign=true.",
			)}
		}
	}

	return nil
}

func gitSigningPostDecisions(options Options, operation string) []policy.Decision {
	if operation == "" || !gitSigningEnforcementEnabled(options.Cwd) {
		return nil
	}

	decisions := gitSigningConfigDecisions(options)

	if operation == gitCommitOperation {
		decisions = append(decisions, gitHeadSignatureDecisions(options)...)
	}

	return decisions
}

func gitSigningConfigDecisions(options Options) []policy.Decision {
	if options.Cwd == "" || !gitInsideWorkTree(options.Cwd) {
		return nil
	}

	decisions := []policy.Decision{}

	if gitConfigOverrideDisablesSigning(options.Argv, "commit.gpgsign") ||
		gitConfigSetsSigningFalse(options.Argv, "commit.gpgsign") ||
		gitConfigBool(options.Cwd, "commit.gpgsign") != gitTrueValue ||
		strings.TrimSpace(gitConfigValue(options.Cwd, "user.signingkey")) == "" {
		decisions = append(decisions, gitSigningDecision(
			gitSignedCommitsPolicyID,
			"Git commit signing is required for every git action.",
			"Set commit.gpgsign=true and user.signingkey to the approved "+
				"signing key before running git.",
		))
	}

	if gitConfigOverrideDisablesSigning(options.Argv, "push.gpgsign") ||
		gitConfigSetsSigningFalse(options.Argv, "push.gpgsign") ||
		gitConfigBool(options.Cwd, "push.gpgsign") != gitTrueValue {
		decisions = append(decisions, gitSigningDecision(
			gitSignedPushPolicyID,
			"Git push signing is required for every git action.",
			"Set push.gpgSign=true before running git; do not use --no-signed.",
		))
	}

	return decisions
}

func gitPushSignatureDecisions(options Options) []policy.Decision {
	argv := ParseArgv(options.Argv).Argv
	for _, arg := range argv {
		if arg == "--no-signed" || arg == "--signed=false" {
			return []policy.Decision{gitSigningDecision(
				gitSignedPushPolicyID,
				"Git push signing cannot be disabled.",
				"Remove --no-signed and keep push.gpgSign=true.",
			)}
		}
	}

	return nil
}

func gitUnsignedOutgoingCommitDecisions(options Options) []policy.Decision {
	statuses := outgoingCommitSignatureStatuses(options.Cwd)
	for _, status := range statuses {
		if status != goodGitSignatureStatus {
			return []policy.Decision{gitSigningDecision(
				gitSignedCommitsPolicyID,
				"Push contains unsigned or unverifiable commits.",
				"Recreate the outgoing commits with valid signatures before pushing.",
			)}
		}
	}

	return nil
}

func gitHeadSignatureDecisions(options Options) []policy.Decision {
	status := strings.TrimSpace(gitOutput(options.Cwd, "log", "-1", "--pretty=%G?"))
	if status != goodGitSignatureStatus {
		return []policy.Decision{gitSigningDecision(
			gitSignedCommitsPolicyID,
			"Latest commit is unsigned or unverifiable.",
			"Recreate the commit with a valid signature before continuing.",
		)}
	}

	return nil
}

func forceSignedGitArgs(argv []string, cwd string) []string {
	if !gitSigningEnforcementEnabled(cwd) {
		return argv
	}

	operationIndex := gitOperationIndex(argv)
	if operationIndex < 0 {
		return argv
	}

	switch argv[operationIndex] {
	case gitCommitOperation:
		if gitCommitSigningArgPresent(argv[operationIndex+1:]) {
			return argv
		}

		return insertGitArg(argv, operationIndex+1, "-S")
	case gitPushOperation:
		if gitPushSigningArgPresent(argv[operationIndex+1:]) {
			return argv
		}

		return insertGitArg(argv, operationIndex+1, "--signed")
	default:
		return argv
	}
}

func outgoingCommitSignatureStatuses(cwd string) []string {
	upstream := strings.TrimSpace(gitOutput(
		cwd,
		"rev-parse",
		"--abbrev-ref",
		"--symbolic-full-name",
		"@{upstream}",
	))
	if upstream == "" {
		return nonEmptyLines(
			gitOutput(cwd, "log", "--pretty=%G?", "HEAD", "--not", "--remotes"),
		)
	}

	output := gitOutput(cwd, "log", "--pretty=%G?", upstream+"..HEAD")

	return nonEmptyLines(output)
}

func gitSigningEnforcementEnabled(cwd string) bool {
	repoRoot := gitRepoRoot(cwd)
	if repoRoot == "" {
		return true
	}

	config := loadGitRepoConfig(repoRoot)
	value := configdata.GetPath(config, "git.signed_operations.enabled", true)

	enabled, ok := value.(bool)
	if !ok {
		return true
	}

	return enabled
}

func loadGitRepoConfig(repoRoot string) configdata.Map {
	for _, name := range configdata.RepoConfigCandidates() {
		path := filepath.Join(repoRoot, name)

		_, err := os.Stat(path)
		if err != nil {
			continue
		}

		config, err := configdata.LoadYAMLMap(path)
		if err == nil {
			return config
		}
	}

	return configdata.Map{}
}

func gitRepoRoot(cwd string) string {
	root := strings.TrimSpace(gitOutput(cwd, "rev-parse", "--show-toplevel"))
	if root != "" {
		return root
	}

	return cwd
}

func gitInsideWorkTree(cwd string) bool {
	return strings.TrimSpace(
		gitOutput(cwd, "rev-parse", "--is-inside-work-tree"),
	) == "true"
}

func gitOperationIndex(argv []string) int {
	if len(argv) < 2 || argv[0] != gitExecutableName {
		return -1
	}

	for index := 1; index < len(argv); index++ {
		arg := argv[index]
		if arg == "--" {
			return -1
		}

		if arg == "" {
			continue
		}

		if !strings.HasPrefix(arg, "-") {
			return index
		}

		if skipNextGitGlobalArg(arg) && index+1 < len(argv) {
			index++
		}
	}

	return -1
}

func gitCommitSigningArgPresent(args []string) bool {
	for _, arg := range args {
		if arg == "-S" ||
			strings.HasPrefix(arg, "-S") ||
			arg == "--gpg-sign" ||
			strings.HasPrefix(arg, "--gpg-sign=") ||
			arg == "--no-gpg-sign" {
			return true
		}
	}

	return false
}

func gitPushSigningArgPresent(args []string) bool {
	for _, arg := range args {
		if arg == "--signed" ||
			arg == "--signed=true" ||
			arg == "--no-signed" ||
			arg == "--signed=false" {
			return true
		}
	}

	return false
}

func insertGitArg(argv []string, index int, arg string) []string {
	withArg := make([]string, 0, len(argv)+1)
	withArg = append(withArg, argv[:index]...)
	withArg = append(withArg, arg)
	withArg = append(withArg, argv[index:]...)

	return withArg
}

func gitConfigOverrideDisablesSigning(argv []string, key string) bool {
	normalized := ParseArgv(argv).Argv
	for index := 1; index < len(normalized); index++ {
		arg := normalized[index]
		switch {
		case arg == "-c" && index+1 < len(normalized):
			if gitConfigAssignmentDisablesSigning(normalized[index+1], key) {
				return true
			}

			index++
		case strings.HasPrefix(arg, "-c"):
			if gitConfigAssignmentDisablesSigning(strings.TrimPrefix(arg, "-c"), key) {
				return true
			}
		}
	}

	return false
}

func gitConfigSetsSigningFalse(argv []string, key string) bool {
	parsed := ParseArgv(argv)
	if parsed.Operation != "config" {
		return false
	}

	for index, arg := range parsed.Argv {
		if !strings.EqualFold(arg, key) || index+1 >= len(parsed.Argv) {
			continue
		}

		return !gitBoolString(parsed.Argv[index+1])
	}

	return false
}

func gitConfigAssignmentDisablesSigning(assignment, key string) bool {
	name, value, ok := strings.Cut(assignment, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(name), key) {
		return false
	}

	return !gitBoolString(value)
}

func gitConfigBool(cwd, key string) string {
	return strings.ToLower(strings.TrimSpace(
		gitOutput(cwd, "config", "--type=bool", "--get", key),
	))
}

func gitConfigValue(cwd, key string) string {
	return strings.TrimSpace(gitOutput(cwd, "config", "--get", key))
}

func gitOutput(cwd string, args ...string) string {
	command := evaluators.GitCommand(cwd, args...)

	output, err := command.Output()
	if err != nil {
		return ""
	}

	return string(output)
}

func nonEmptyLines(output string) []string {
	lines := []string{}

	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)

		if line != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

func gitBoolString(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "on", "1":
		return true
	default:
		return false
	}
}

func gitSigningDecision(policyID, message, suggestion string) policy.Decision {
	return policy.Decision{
		Decision:   "block",
		PolicyID:   policyID,
		Severity:   "block",
		Message:    message,
		Suggestion: suggestion,
		PrincipleIDs: []string{
			"security-by-design",
			"one-path-for-critical-operations",
		},
		Evidence: map[string]any{
			"commit_gpgsign": true,
			"push_gpgsign":   true,
		},
	}
}
