// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

type commitHeadState struct {
	Argv    []string `json:"argv"`
	Command string   `json:"command,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Head    string   `json:"head"`
}

func EvaluateGitCommitHeadAdvanced(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
	if !isGitSubcommand(context.Argv, "commit") {
		return nil, nil
	}
	switch context.Scope {
	case "PreToolUse":
		return recordCommitHead(policyDef, context)
	case "PostToolUse":
		return verifyCommitHead(policyDef, context)
	default:
		return nil, nil
	}
}

func recordCommitHead(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
	head, err := currentHead(context.Cwd)
	if err != nil && !errors.Is(err, errNoHead) {
		return nil, err
	}
	state := commitHeadState{
		Argv:    append([]string(nil), context.Argv...),
		Command: context.Command,
		Cwd:     context.Cwd,
		Head:    head,
	}
	path, err := commitHeadStatePath(context.Cwd)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		return nil, err
	}
	decision := policy.NewDecision("record", policyDef)
	decision.Severity = "record"
	decision.Evidence = map[string]any{
		"phase":    "pre",
		"pre_head": head,
	}
	return []policy.Decision{decision}, nil
}

func verifyCommitHead(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
	state, ok, err := readCommitHeadState(context.Cwd)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	head, err := currentHead(context.Cwd)
	if err != nil && !errors.Is(err, errNoHead) {
		return nil, err
	}
	if state.Head != head {
		decision := policy.NewDecision("record", policyDef)
		decision.Severity = "record"
		decision.Evidence = map[string]any{
			"phase":     "post",
			"pre_head":  state.Head,
			"post_head": head,
			"advanced":  true,
		}
		return []policy.Decision{decision}, nil
	}
	decision := policy.NewDecision("block", policyDef)
	decision.Severity = "block"
	decision.Message = "git commit did not advance HEAD; do not report commit success."
	decision.Evidence = map[string]any{
		"phase":     "post",
		"pre_head":  state.Head,
		"post_head": head,
		"advanced":  false,
	}
	return []policy.Decision{decision}, nil
}

func readCommitHeadState(cwd string) (commitHeadState, bool, error) {
	path, err := commitHeadStatePath(cwd)
	if err != nil {
		return commitHeadState{}, false, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return commitHeadState{}, false, nil
		}
		return commitHeadState{}, false, err
	}
	var state commitHeadState
	if err := json.Unmarshal(payload, &state); err != nil {
		return commitHeadState{}, false, err
	}
	return state, true, nil
}

func commitHeadStatePath(cwd string) (string, error) {
	gitDir, err := gitDir(cwd)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(gitDir) && cwd != "" {
		gitDir = filepath.Join(cwd, gitDir)
	}
	return filepath.Join(gitDir, "coding-ethos", "commit-head.json"), nil
}

func gitDir(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	if cwd != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve git dir: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

var errNoHead = errors.New("git repository has no HEAD commit")

func currentHead(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", "HEAD")
	if cwd != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.Output()
	if err != nil {
		return "", errNoHead
	}
	return strings.TrimSpace(string(output)), nil
}
