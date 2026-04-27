# Hook Replacement Plan

This document tracks the import-only migration from parent repo Git hooks and
`~/.claude/hooks` into the `coding-ethos` hook runtime. The old hooks are source
material only. Cutover should install `coding-ethos` shims and then remove old
parent and Claude hook entries outside this repo.

## Goals

- One `coding-ethos` runtime owns Git hook and agent hook enforcement.
- Success output is quiet; failure output is structured and useful.
- Agent-facing output supports human, JSON, and TOON paths.
- Old hooks are not executed as adapters after cutover.
- Imported behavior is represented as policy, deterministic evaluators,
  fixtures, and tests.

## Parent Git Hooks

| Hook | Imported Behavior | Replacement | Status |
| --- | --- | --- | --- |
| `pre-commit` | Locate bundle and run `run-go-hook.sh git-hook pre-commit`. | Current coding-ethos Git shim. | Covered |
| `pre-push` | Locate bundle and run `run-go-hook.sh git-hook pre-push`. | Current coding-ethos Git shim. | Covered |
| `commit-msg` | Locate bundle and run `run-go-hook.sh git-hook commit-msg`. | Current coding-ethos Git shim. | Covered |
| `post-commit` | Delegate to `git lfs post-commit`. | Preserve as external Git LFS delegation at cutover. | External |
| `post-merge` | Delegate to `git lfs post-merge`. | Preserve as external Git LFS delegation at cutover. | External |
| `post-checkout` | Delegate to `git lfs post-checkout`. | Preserve as external Git LFS delegation at cutover. | External |

## Claude Hooks

| Event | Tool | Imported Behavior | Replacement | Status |
| --- | --- | --- | --- | --- |
| `PreToolUse` | `Bash` | Block hook bypass, destructive git, protected-branch checkout, dangerous shell commands, background git, admin-only staged files, and protected paths. | Policy dispatch plus Go evaluators. | Partial |
| `PreToolUse` | `Write` / `Edit` | Block protected branch writes, protected paths, bare `except:`, broad `except Exception: pass`, and unexplained `# type: ignore`. | Policy dispatch plus Go evaluators. | Partial |
| `PostToolUse` | `Bash` | Feed git hook output back to the agent for summarization. | `hookSpecificOutput.additionalContext` from `coding-ethos-hook`. | Covered |
| `PreCompact` | any | Generate continuation prompt and notes from the transcript. | Planned deterministic transcript capture and replay. No hook-path AI call. | Planned |
| `SessionStart` | `compact` | Inject continuation prompt into the next compacted session. | Planned coding-ethos continuation context store. | Planned |

## Current Coverage

Covered in Go agent/runtime code:

- `git.hook_bypass`
- `git.destructive_command`
- `git.merge_strategy_shortcut`
- `git.force_push_protected_branch`
- `git.checkout_protected_branch`
- `git.destructive_worktree`
- `git.change_dir_flag`
- `git.stash_blocked`
- `git.staged_admin_files`
- `git.commit_head_advanced`
- `shell.dangerous_command`
- `shell.background_git`
- `filesystem.protected_path`
- `python.conditional_imports`
- `python.optional_returns`
- `python.catch_and_silence`
- `python.structured_logging`
- `python.direct_imports`
- Post-tool hook output feedback for git hook commands

Imported fixtures live under `go/internal/hooks/testdata/legacy/`.
`go/internal/hooks/testdata/legacy_hook_inventory.json` is the machine-readable
inventory used to keep the migration status explicit.

## Remaining Runtime Gaps

- Protected branch write detection needs a deterministic worktree/branch
  evaluator for Bash, Write, Edit, and MultiEdit.
- `gh --admin` command blocking should become a named shell policy.
- AI co-author commit-message blocking currently exists in commit-message hooks;
  the agent hook path should route to the same policy.
- Write/Edit content checks should distinguish bare `except:` and unexplained
  `# type: ignore` as explicit policy evidence rather than only generic Python
  catch/suppression checks.
- PreCompact should be redesigned as deterministic context capture. The legacy
  script made a direct external model call and contained local credentials; that
  behavior should not be cloned.
- SessionStart continuation injection needs a repo-owned continuation store and
  a fixture-backed output contract.

## Fixture Contract

Fixtures are representative imported payloads, not legacy adapters:

- `pretooluse_git_no_verify.json`
- `pretooluse_protected_path_write.json`
- `posttooluse_precommit_failure.json`
- `precompact_continuation_request.json`
- `sessionstart_compact.json`

Supported fixtures must be executable through the Go hook runner tests. Planned
fixtures document target contracts and should move to executable tests as the
runtime grows.

## Cutover Rule

When a behavior moves to `coding-ethos`, remove the legacy implementation from
the migration checklist. Do not add compatibility adapters around old hook
scripts. The old parent and Claude hook entries should remain untouched until
cutover, then be removed outside this repo.
