// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package githookcli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/hookrunnercli"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	blockedExitCode      = 2
	adminApprovedEnv     = "CODE_ETHOS_ADMIN_APPROVED"
	gitHookRunner        = "coding-ethos-run"
	gitHookWrapper       = "coding-ethos-git"
	policyGitCommand     = "policy-git"
	hookCommitMsg        = "commit-msg"
	hookPrepareMsg       = "prepare-commit-msg"
	hookPreCommit        = "pre-commit"
	hookPrePush          = "pre-push"
	hookMessageFileIndex = 1
	hookSourceIndex      = 2
	hookCommitIndex      = 3
)

var (
	errBundleRequired = apperror.StaticError("--bundle is required")
	errHookRequired   = apperror.StaticError("git hook name is required")
	errRunnerRequired = apperror.StaticError("--runner is required")
	errInvalidBundle  = apperror.StaticError("invalid policy bundle")
	errDirectGitHook  = apperror.StaticError(
		"direct git execution reached coding-ethos hooks",
	)
)

type gitHookConfig struct {
	BundlePath string
	Cwd        string
	RunnerPath string
	HookArgs   []string
}

type gitHookAuthorizationVerifier struct {
	CurrentPID              func() int
	ProcessAncestryContains func(int, int) (bool, error)
	ProcessCommandLine      func(int) ([]string, error)
}

func runWithArgs(args []string) int {
	return runWithArgsAndVerifier(args, defaultGitHookAuthorizationVerifier())
}

func runWithArgsAndVerifier(
	args []string,
	verifier gitHookAuthorizationVerifier,
) int {
	config, err := parseGitHookConfig(args)
	if err != nil {
		printErr(err)

		return 1
	}

	bundle, err := validatedBundle(config.BundlePath)
	if err != nil {
		printErr(err)

		return 1
	}

	hookName := config.HookArgs[0]
	if gitHookRequiresWrapper(hookName) &&
		agentGitHookExecution() &&
		!gitHookWrapperAuthorized(verifier) {
		printDirectGitHookBlock(config, agentGitHookProvider(), hookName)

		return blockedExitCode
	}

	switch hookName {
	case hookCommitMsg:
		return runCommitMsgHook(bundle, config.Cwd, config.HookArgs)
	case hookPrepareMsg:
		return runPrepareCommitMsgHook(bundle, config.Cwd, config.HookArgs)
	case hookPreCommit, hookPrePush:
		code, blocked := runStagedHook(bundle, config.Cwd, hookName)
		if blocked {
			return code
		}
	}

	return runHookGroupRunner(config.Cwd, config.HookArgs)
}

func defaultGitHookAuthorizationVerifier() gitHookAuthorizationVerifier {
	return gitHookAuthorizationVerifier{
		CurrentPID:              os.Getpid,
		ProcessAncestryContains: gitwrap.ProcessAncestryContains,
		ProcessCommandLine:      gitwrap.ProcessCommandLine,
	}
}

func gitHookRequiresWrapper(hookName string) bool {
	switch hookName {
	case hookCommitMsg, hookPrepareMsg, hookPreCommit, hookPrePush:
		return true
	default:
		return false
	}
}

func agentGitHookExecution() bool {
	for _, name := range agentGitHookEnvMarkers() {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}

	return false
}

func agentGitHookEnvMarkers() []string {
	return []string{
		"CODEX_THREAD_ID",
		"CODEX_SESSION_ID",
		"CODEX_CI",
		"CODEX_MANAGED_BY_NPM",
		"CLAUDECODE",
		"CLAUDE_CODE_ENTRYPOINT",
		"CLAUDE_SESSION_ID",
		"CODEX_MANAGED_PACKAGE_ROOT",
		"GEMINI_CLI",
		"GEMINI_SESSION_ID",
	}
}

func agentGitHookProvider() string {
	switch {
	case codexGitHookExecution():
		return "codex"
	case claudeGitHookExecution():
		return "claude"
	case geminiGitHookExecution():
		return "gemini"
	default:
		return "agent"
	}
}

func codexGitHookExecution() bool {
	return gitHookEnvPresent(
		"CODEX_THREAD_ID",
		"CODEX_SESSION_ID",
		"CODEX_CI",
		"CODEX_MANAGED_BY_NPM",
		"CODEX_MANAGED_PACKAGE_ROOT",
	)
}

func claudeGitHookExecution() bool {
	return gitHookEnvPresent(
		"CLAUDECODE",
		"CLAUDE_CODE_ENTRYPOINT",
		"CLAUDE_SESSION_ID",
	)
}

func geminiGitHookExecution() bool {
	return gitHookEnvPresent("GEMINI_CLI", "GEMINI_SESSION_ID")
}

func gitHookEnvPresent(names ...string) bool {
	for _, name := range names {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}

	return false
}

func gitHookWrapperAuthorized(verifier gitHookAuthorizationVerifier) bool {
	if os.Getenv(gitwrap.WrapperAuthorizedEnv) != "1" {
		return false
	}

	wrapperPID, err := strconv.Atoi(os.Getenv(gitwrap.WrapperPIDEnv))
	if err != nil || wrapperPID <= 0 {
		return false
	}

	contained, err := verifier.ProcessAncestryContains(
		verifier.CurrentPID(),
		wrapperPID,
	)
	if err != nil || !contained {
		return false
	}

	commandLine, err := verifier.ProcessCommandLine(wrapperPID)
	if err != nil {
		return false
	}

	return trustedGitWrapperProcess(commandLine)
}

func trustedGitWrapperProcess(commandLine []string) bool {
	if len(commandLine) == 0 {
		return false
	}

	switch filepath.Base(commandLine[0]) {
	case gitHookWrapper:
		return true
	case gitHookRunner:
		return commandLineContains(commandLine, policyGitCommand)
	default:
		return false
	}
}

func commandLineContains(commandLine []string, value string) bool {
	return slices.Contains(commandLine, value)
}

func parseGitHookConfig(args []string) (gitHookConfig, error) {
	flags := flag.NewFlagSet("coding-ethos-git-hook", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	runnerPath := flags.String("runner", "", "Path to the hook group runner")
	cwd := flags.String("cwd", "", "Repository root")

	err := flags.Parse(args)
	if err != nil {
		return gitHookConfig{}, fmt.Errorf("parse git hook args: %w", err)
	}

	if *bundlePath == "" {
		return gitHookConfig{}, errBundleRequired
	}

	if *runnerPath == "" {
		return gitHookConfig{}, errRunnerRequired
	}

	hookArgs := flags.Args()
	if len(hookArgs) == 0 {
		return gitHookConfig{}, errHookRequired
	}

	return gitHookConfig{
		BundlePath: *bundlePath,
		Cwd:        *cwd,
		HookArgs:   hookArgs,
		RunnerPath: *runnerPath,
	}, nil
}

func validatedBundle(bundlePath string) (policy.Bundle, error) {
	bundle, err := readBundle(bundlePath)
	if err != nil {
		return policy.Bundle{}, err
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

func runCommitMsgHook(bundle policy.Bundle, cwd string, hookArgs []string) int {
	if len(hookArgs) < 2 || strings.TrimSpace(hookArgs[1]) == "" {
		printErr(apperror.StaticError("commit-msg hook requires a message file"))

		return 1
	}

	result, err := runHookPolicy(bundle, cwd, lint.ScopeCommit, []string{hookArgs[1]})
	if err != nil {
		printErr(err)

		return 1
	}

	return policyResultExitCode(cwd, result)
}

func runPrepareCommitMsgHook(bundle policy.Bundle, cwd string, hookArgs []string) int {
	if len(hookArgs) <= hookMessageFileIndex ||
		strings.TrimSpace(hookArgs[hookMessageFileIndex]) == "" {
		printErr(apperror.StaticError(
			"prepare-commit-msg hook requires a message file",
		))

		return 1
	}

	source := ""
	if len(hookArgs) > hookSourceIndex {
		source = strings.TrimSpace(hookArgs[hookSourceIndex])
	}

	if source != "commit" {
		return 0
	}

	commit := ""
	if len(hookArgs) > hookCommitIndex {
		commit = strings.TrimSpace(hookArgs[hookCommitIndex])
	}

	decision := historyRewriteDecision(bundle, hookPrepareMsg, source, commit)
	result := lint.Result{
		Scope:     lint.ScopeCommit,
		Status:    "blocked",
		Files:     []string{hookArgs[hookMessageFileIndex]},
		Decisions: []policy.Decision{decision},
	}

	if len(decision.Diagnostics) > 0 {
		result.Diagnostics = append(
			result.Diagnostics,
			decision.Diagnostics...,
		)
	}

	return policyResultExitCode(cwd, result)
}

func historyRewriteDecision(
	bundle policy.Bundle,
	hookName string,
	source string,
	commit string,
) policy.Decision {
	const policyID = "git.history_rewrite_prevention"

	decision := policy.Decision{
		Decision:   "block",
		PolicyID:   policyID,
		Severity:   "block",
		Message:    "Branch history rewriting is forbidden.",
		Suggestion: "Make a new commit that preserves review history.",
	}
	policyDef, policyFound := bundle.Policies[policyID]

	if policyFound {
		decision = policy.NewDecision("block", policyDef)
	}

	diagnostic := diagnostics.Diagnostic{
		Tool:     "git",
		Severity: "block",
		PolicyID: policyID,
		Message:  "History rewrite commit flow reached prepare-commit-msg.",
		Advice: "Create a new commit instead of amending or reusing a " +
			"commit message from existing history.",
		Detail: fmt.Sprintf(
			"hook=%q source=%q commit=%q",
			hookName,
			source,
			commit,
		),
	}

	decision.Diagnostics = append(decision.Diagnostics, diagnostic)
	decision.Evidence = map[string]any{
		"commit": commit,
		"hook":   hookName,
		"source": source,
	}

	return decision
}

func runStagedHook(bundle policy.Bundle, cwd, hookName string) (int, bool) {
	files, err := hookFiles(cwd, hookName)
	if err != nil {
		printErr(err)

		return 1, true
	}

	result, err := runHookPolicy(bundle, cwd, lint.ScopeStaged, files)
	if err != nil {
		printErr(err)

		return 1, true
	}

	code := policyResultExitCode(cwd, result)

	return code, code != 0
}

func runHookPolicy(
	bundle policy.Bundle,
	cwd string,
	scope string,
	files []string,
) (lint.Result, error) {
	result, err := lint.Run(bundle, lint.Options{
		AdminApproved: adminApproved(cwd),
		Scope:         scope,
		Cwd:           cwd,
		Files:         files,
	})
	if err != nil {
		return lint.Result{}, fmt.Errorf("run hook policy: %w", err)
	}

	return result, nil
}

func policyResultExitCode(cwd string, result lint.Result) int {
	lint.EnsureTraceID(&result)
	logLintResult(cwd, result)

	if result.Blocked() {
		encodeLintResult(result)

		return blockedExitCode
	}

	return 0
}

func adminApproved(cwd string) bool {
	return adminApprovedWithVerifier(cwd, gitwrap.VerifyAdminApproved)
}

func adminApprovedWithVerifier(cwd string, verifier func(string) error) bool {
	if os.Getenv(adminApprovedEnv) == "1" {
		return true
	}

	return verifier(cwd) == nil
}

func hookFiles(cwd, hookName string) ([]string, error) {
	if hookName != hookPreCommit {
		return nil, nil
	}

	command := evaluators.GitCommand(
		cwd,
		"diff",
		"--cached",
		"--name-only",
		"--diff-filter=ACMR",
		"--",
	)

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list staged files: %w", err)
	}

	files := []string{}

	for line := range strings.SplitSeq(string(output), "\n") {
		file := strings.TrimSpace(line)
		if file != "" {
			files = append(files, file)
		}
	}

	return files, nil
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

func encodeLintResult(result lint.Result) {
	err := encodeLintResultTo(os.Stderr, result)
	if err != nil {
		writeGitHookText("coding-ethos policy blocked " + result.Scope)
	}
}

func logLintResult(cwd string, result lint.Result) {
	tracePath, inlineErrA := lint.LogResult(cwd, result)
	if inlineErrA != nil {
		writeGitHookText("warning: lint trace not written: " + inlineErrA.Error())

		return
	}

	inlineErrB := hookoutput.WriteLintSARIFSidecar(tracePath, result)
	if inlineErrB != nil {
		writeGitHookText("warning: lint SARIF sidecar not written: " + inlineErrB.Error())
	}

	err := outputsurface.AutoPruneSurface(
		context.Background(),
		cwd,
		"lint_traces",
		false,
	)
	if err != nil {
		writeGitHookText("warning: lint trace auto-prune failed: " + err.Error())
	}
}

func encodeLintResultTo(writer io.Writer, result lint.Result) error {
	if result.Blocked() {
		result = blockedOnlyResult(result)
	}

	err := hookoutput.EncodeLintResult(writer, result, hookoutput.SelectedFormat())
	if err != nil {
		return fmt.Errorf("encode lint result: %w", err)
	}

	return nil
}

func blockedOnlyResult(result lint.Result) lint.Result {
	filtered := lint.Result{
		TraceID:    result.TraceID,
		Scope:      result.Scope,
		Status:     result.Status,
		SkillHints: append([]lint.SkillHint(nil), result.SkillHints...),
	}

	for _, decision := range result.Decisions {
		if decision.Decision != "block" && decision.Severity != "block" {
			continue
		}

		filtered.Decisions = append(filtered.Decisions, decision)
		filtered.Diagnostics = append(filtered.Diagnostics, decision.Diagnostics...)
	}

	for _, finding := range result.Findings {
		if !finding.Blocking {
			continue
		}

		filtered.Findings = append(filtered.Findings, finding)
	}

	return filtered
}

func runHookGroupRunner(cwd string, args []string) int {
	restore := setenvForHookRunner("CODE_ETHOS_CONSUMER_ROOT", cwd)
	defer restore()

	commandArgs := append([]string{"git-hook"}, args...)

	return hookrunnercli.Run(commandArgs)
}

func setenvForHookRunner(name, value string) func() {
	previous, existed := os.LookupEnv(name)
	if strings.TrimSpace(value) != "" {
		_ = os.Setenv(name, value)
	}

	return func() {
		if existed {
			_ = os.Setenv(name, previous)

			return
		}

		_ = os.Unsetenv(name)
	}
}

func printErr(err error) {
	feedback.Emit(
		os.Stderr,
		feedback.Error{Message: err.Error()},
		feedback.FormatTOON,
	)
}

func printDirectGitHookBlock(
	config gitHookConfig,
	provider string,
	hookName string,
) {
	if provider != "codex" {
		printErr(errDirectGitHook)

		return
	}

	feedback.Emit(
		os.Stderr,
		directGitHookBlockMessage(config, hookName),
		feedback.FormatTOON,
	)
}

func directGitHookBlockMessage(
	config gitHookConfig,
	hookName string,
) feedback.Message {
	return feedback.Message{
		Scalars: []feedback.Scalar{
			feedback.S("event", hookName),
			feedback.S("provider", "codex"),
			feedback.S("status", "blocked"),
			feedback.S("policy_id", "git.wrapper_required"),
			feedback.S("summary", errDirectGitHook.Error()),
		},
		Tables: []feedback.Table{
			feedback.T(
				"guidance",
				[]string{"message"},
				[][]string{{
					"Codex must retry the original git command through cerun; " +
						"do not run raw git from the Codex shell.",
				}},
			),
			feedback.T(
				"retry",
				[]string{"command"},
				[][]string{{directGitHookRetryCommand(config, hookName)}},
			),
		},
	}
}

func directGitHookRetryCommand(config gitHookConfig, hookName string) string {
	return cerunForGitHook(config) + " -- " + shellQuote(gitHookRetryGitCommand(hookName))
}

func cerunForGitHook(config gitHookConfig) string {
	runner := strings.TrimSpace(config.RunnerPath)
	if filepath.Base(runner) == gitHookRunner {
		return filepath.Join(filepath.Dir(runner), "cerun")
	}

	cwd := strings.TrimSpace(config.Cwd)
	if cwd != "" {
		return filepath.Join(cwd, "bin", "cerun")
	}

	return "cerun"
}

func gitHookRetryGitCommand(hookName string) string {
	switch hookName {
	case hookPrePush:
		return "git push"
	case hookCommitMsg, hookPrepareMsg, hookPreCommit:
		return "git commit <same arguments>"
	default:
		return "git <same command>"
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeGitHookText(text string) {
	feedback.Emit(os.Stderr, feedback.Text{Text: text}, feedback.FormatTOON)
}
