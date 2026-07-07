// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolchaincli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	cutoverBlockedStatus = "blocked"
	cutoverFailStatus    = "FAIL"
)

func cutoverVerify(args []string) error {
	options, err := parseCutoverVerifyFlags(args)
	if err != nil {
		return err
	}

	state, err := runCutoverSurfaceChecks(options)
	if err != nil {
		return err
	}

	writeCutoverVerifyReport(options, state)

	return cutoverVerifyResult(state)
}

type cutoverVerifyOptions struct {
	action     string
	root       string
	runner     string
	hooksDir   string
	realGit    string
	bundleRoot string
}

type cutoverVerifyState struct {
	agentErr         error
	repoIgnoreErr    error
	runtimeErr       error
	surfaces         map[string]string
	agentOutput      string
	repoIgnoreOutput string
	runtimeOutput    string
	status           string
	fixItems         []string
}

func parseCutoverVerifyFlags(args []string) (cutoverVerifyOptions, error) {
	flags := flag.NewFlagSet("cutover-verify", flag.ExitOnError)
	action := flags.String("action", "verify", "Cutover action")
	root := flags.String("root", "", "Repository root")
	runner := flags.String("runner", "", "runner path")
	hooksDir := flags.String("hooks-dir", "", "Git hooks directory")
	flags.String("source-dir", "", "Deprecated; hook files are generated from --runner")
	realGit := flags.String("real-git", "", "Real git executable")

	bundleRoot := flags.String("bundle-root", "", "Policy bundle root")

	inlineErr0 := flags.Parse(args)
	if inlineErr0 != nil {
		return cutoverVerifyOptions{}, fmt.Errorf(
			"parse cutover-verify flags: %w",
			inlineErr0,
		)
	}

	options := cutoverVerifyOptions{
		action:     *action,
		root:       *root,
		runner:     *runner,
		hooksDir:   *hooksDir,
		realGit:    *realGit,
		bundleRoot: *bundleRoot,
	}

	err := validateCutoverVerifyOptions(options)
	if err != nil {
		return cutoverVerifyOptions{}, err
	}

	return options, nil
}

func validateCutoverVerifyOptions(options cutoverVerifyOptions) error {
	for name, value := range cutoverVerifyRequiredOptions(options) {
		if strings.TrimSpace(value) != "" {
			continue
		}

		return apperror.Wrapf(
			apperror.StaticError("cutover-verify requires --%s"),
			"cutover-verify requires --%s",
			name,
		)
	}

	return nil
}

func cutoverVerifyRequiredOptions(options cutoverVerifyOptions) map[string]string {
	return map[string]string{
		"root":        options.root,
		"runner":      options.runner,
		"hooks-dir":   options.hooksDir,
		"real-git":    options.realGit,
		"bundle-root": options.bundleRoot,
	}
}

func runCutoverSurfaceChecks(
	options cutoverVerifyOptions,
) (cutoverVerifyState, error) {
	state := cutoverVerifyState{
		status: cutoverReadyStatus,
		surfaces: map[string]string{
			"git-hooks":      "PASS",
			"agent-hooks":    "PASS",
			"repo-ignores":   "PASS",
			"policy-runtime": "PASS",
		},
		fixItems: make([]string, 0),
	}

	err := checkCutoverGitHooks(options, &state)
	if err != nil {
		return cutoverVerifyState{}, err
	}

	checkCutoverAgentHooks(options, &state)

	err = checkCutoverRepoIgnores(options, &state)
	if err != nil {
		return cutoverVerifyState{}, err
	}

	checkCutoverRuntime(options, &state)

	return state, nil
}

const cutoverReadyStatus = "ready"

func checkCutoverGitHooks(
	options cutoverVerifyOptions,
	state *cutoverVerifyState,
) error {
	gitItems, err := gitHookShimFixItems(options.hooksDir, options.runner)
	if err != nil {
		return err
	}

	if len(gitItems) == 0 {
		return nil
	}

	blockCutoverSurface(state, "git-hooks")
	state.fixItems = append(state.fixItems, gitItems...)

	return nil
}

func blockCutoverSurface(state *cutoverVerifyState, surface string) {
	state.status = cutoverBlockedStatus
	state.surfaces[surface] = cutoverFailStatus
}

func checkCutoverAgentHooks(
	options cutoverVerifyOptions,
	state *cutoverVerifyState,
) {
	state.agentOutput, state.agentErr = runCutoverCommand(
		[]string{options.runner, "agent-hooks", "verify", "--root", options.root},
		cutoverHookEnv(),
	)
	if state.agentErr == nil {
		return
	}

	blockCutoverSurface(state, "agent-hooks")
	state.fixItems = append(
		state.fixItems,
		agentHookFixItemLines(state.agentOutput)...,
	)
}

func checkCutoverRepoIgnores(
	options cutoverVerifyOptions,
	state *cutoverVerifyState,
) error {
	state.repoIgnoreOutput, state.repoIgnoreErr = runCutoverCommand(
		[]string{
			options.runner,
			"policy-lint",
			"--scope",
			"cutover",
			"--cwd",
			options.root,
			"--json",
		},
		cutoverHookEnv(),
	)
	if state.repoIgnoreErr == nil {
		return nil
	}

	blockCutoverSurface(state, "repo-ignores")

	items, err := repoIgnoreFixItemLines(options.realGit, options.root)
	if err != nil {
		return err
	}

	state.fixItems = append(state.fixItems, items...)

	return nil
}

func checkCutoverRuntime(
	options cutoverVerifyOptions,
	state *cutoverVerifyState,
) {
	state.runtimeOutput, state.runtimeErr = runCutoverCommand(
		[]string{options.runner, "git-hook", "validate"},
		map[string]string{
			"CODE_ETHOS_HOOK_LOGGING_ACTIVE": "1",
			"CODE_ETHOS_PRECOMMIT_ROOT":      options.bundleRoot,
		},
	)
	if state.runtimeErr == nil {
		return
	}

	blockCutoverSurface(state, "policy-runtime")
	state.fixItems = append(
		state.fixItems,
		runtimeFixItemLines(state.runtimeOutput)...,
	)
}

func cutoverHookEnv() map[string]string {
	return map[string]string{"CODE_ETHOS_HOOK_LOGGING_ACTIVE": "1"}
}

func writeCutoverVerifyReport(
	options cutoverVerifyOptions,
	state cutoverVerifyState,
) {
	report := cutoverStatusReport{
		Action:   options.action,
		Status:   state.status,
		Repo:     options.root,
		Surfaces: state.surfaces,
		FixItems: state.fixItems,
	}
	for _, line := range cutoverReportLines(report) {
		writeToolchainText(os.Stdout, line)
	}
}

func cutoverVerifyResult(state cutoverVerifyState) error {
	if state.status == cutoverReadyStatus {
		return nil
	}

	writeFailedCutoverOutputs(state)

	return apperror.StaticError("cutover verification blocked")
}

func writeFailedCutoverOutputs(state cutoverVerifyState) {
	if state.agentErr != nil {
		writeToolchainText(os.Stderr, "agent hook verify output:\n"+state.agentOutput)
	}

	if state.runtimeErr != nil {
		writeToolchainText(os.Stderr, "policy runtime verify output:\n"+state.runtimeOutput)
	}

	if state.repoIgnoreErr != nil {
		writeToolchainText(os.Stderr, "repo ignore verify output:\n"+state.repoIgnoreOutput)
	}
}

func runCutoverCommand(args []string, env map[string]string) (string, error) {
	if len(args) == 0 {
		return "", apperror.StaticError("cutover command args are required")
	}

	command := safeexec.Command(args[0], args[1:]...)

	// Verification children are legitimate nested coding-ethos invocations;
	// without scrubbing the exec-guard stack they block themselves as
	// recursive and every cutover verify fails.
	command.Env = make([]string, 0, len(os.Environ())+len(env))
	stackPrefix := execguard.EnvStack + "="

	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, stackPrefix) {
			command.Env = append(command.Env, item)
		}
	}

	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}

	output, err := command.CombinedOutput()

	return string(output), err
}
