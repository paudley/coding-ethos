// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	hookScopePostToolUse = "PostToolUse"
	recordDecision       = "record"
	stateDirMode         = 0o755
	stateFileMode        = 0o600
)

type commitHeadState struct {
	Command string   `json:"command,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Head    string   `json:"head"`
	Argv    []string `json:"argv"`
}

func EvaluateGitCommitHeadAdvanced(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if !isGitSubcommand(context.Argv, "commit") {
		return nil, nil
	}

	switch context.Scope {
	case "PreToolUse":
		return recordCommitHead(policyDef, context)
	case hookScopePostToolUse:
		return verifyCommitHead(policyDef, context)
	default:
		return nil, nil
	}
}

func recordCommitHead(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
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

	err = os.MkdirAll(filepath.Dir(path), stateDirMode)
	if err != nil {
		return nil, fmt.Errorf("create commit head state dir: %w", err)
	}

	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode commit head state: %w", err)
	}

	err = os.WriteFile(path, append(payload, '\n'), stateFileMode)
	if err != nil {
		return nil, fmt.Errorf("write commit head state: %w", err)
	}

	decision := policy.NewDecision(recordDecision, policyDef)
	decision.Severity = recordDecision
	decision.Evidence = map[string]any{
		"phase":    "pre",
		"pre_head": head,
	}

	return []policy.Decision{decision}, nil
}

func verifyCommitHead(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	path, err := commitHeadStatePath(context.Cwd)
	if err != nil {
		return nil, err
	}

	state, ok, err := readCommitHeadStatePath(path)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, nil
	}

	defer os.Remove(path)

	if !context.HasToolResponse {
		return commitHeadRecordDecision(
			policyDef,
			state.Head,
			"",
			"missing tool response",
			0,
		), nil
	}

	if !context.HasReturnCode {
		return commitHeadRecordDecision(
			policyDef,
			state.Head,
			"",
			"missing tool return code",
			0,
		), nil
	}

	if context.ReturnCode != 0 {
		return commitHeadRecordDecision(
			policyDef,
			state.Head,
			"",
			"commit command failed",
			context.ReturnCode,
		), nil
	}

	head, err := currentHead(context.Cwd)
	if err != nil && !errors.Is(err, errNoHead) {
		return nil, err
	}

	if state.Head != head {
		return commitHeadAdvancedDecision(policyDef, state.Head, head), nil
	}

	return commitHeadBlockedDecision(policyDef, state.Head, head), nil
}

func commitHeadRecordDecision(
	policyDef policy.Policy,
	preHead string,
	postHead string,
	skipped string,
	returnCode int,
) []policy.Decision {
	decision := policy.NewDecision(recordDecision, policyDef)
	decision.Severity = recordDecision
	decision.Evidence = commitHeadEvidence(preHead, postHead, false)

	decision.Evidence["skipped"] = skipped
	if returnCode != 0 {
		decision.Evidence["return_code"] = returnCode
	}

	return []policy.Decision{decision}
}

func commitHeadAdvancedDecision(
	policyDef policy.Policy,
	preHead string,
	postHead string,
) []policy.Decision {
	decision := policy.NewDecision(recordDecision, policyDef)
	decision.Severity = recordDecision
	decision.Evidence = commitHeadEvidence(preHead, postHead, true)

	return []policy.Decision{decision}
}

func commitHeadBlockedDecision(
	policyDef policy.Policy,
	preHead string,
	postHead string,
) []policy.Decision {
	decision := policy.NewDecision("block", policyDef)
	decision.Severity = "block"
	decision.Evidence = commitHeadEvidence(preHead, postHead, false)
	decision.Evidence["skipped"] = "commit head did not advance"

	return []policy.Decision{decision}
}

func commitHeadEvidence(preHead, postHead string, advanced bool) map[string]any {
	evidence := map[string]any{
		"phase":    "post",
		"pre_head": preHead,
		"advanced": advanced,
	}
	if postHead != "" {
		evidence["post_head"] = postHead
	}

	return evidence
}

func ReadCommitHeadState(cwd string) (bool, error) {
	path, err := commitHeadStatePath(cwd)
	if err != nil {
		return false, err
	}

	_, ok, err := readCommitHeadStatePath(path)

	return ok, err
}

func readCommitHeadStatePath(path string) (commitHeadState, bool, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return commitHeadState{}, false, nil
		}

		return commitHeadState{}, false, fmt.Errorf("read commit head state: %w", err)
	}

	var state commitHeadState

	err = json.Unmarshal(payload, &state)
	if err != nil {
		return commitHeadState{}, false, fmt.Errorf("decode commit head state: %w", err)
	}

	return state, true, nil
}

func commitHeadStatePath(cwd string) (string, error) {
	root, err := gitWorktreeRoot(cwd)
	if err != nil {
		return "", err
	}

	return filepath.Join(root, ".coding-ethos", "state", "commit-head.json"), nil
}

func gitWorktreeRoot(cwd string) (string, error) {
	cmd := gitCommand(cwd, "rev-parse", "--show-toplevel")

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve git worktree root: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

var errNoHead = apperror.StaticError("git repository has no HEAD commit")

func currentHead(cwd string) (string, error) {
	cmd := gitCommand(cwd, "rev-parse", "--verify", "HEAD")

	output, err := cmd.Output()
	if err != nil {
		return "", errNoHead
	}

	return strings.TrimSpace(string(output)), nil
}
