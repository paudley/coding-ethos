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

## Status Terms

- `Runtime covered`: deterministic policy/evaluator behavior exists and is
  fixture-backed through the Go agent hook runtime.
- `Installed`: the repo-owned hook installation flow wires the behavior into
  the relevant Git or Claude hook surface.
- `Bridged`: installed Git hook execution includes compiled-policy preflight,
  then delegates to the current bundled hook group runner for checks not yet
  represented in compiled policy.
- `Cutover ready`: the behavior is runtime covered, installed, documented, and
  verified by an end-to-end compiled-policy path.

## Parent Git Hooks

| Hook | Imported Behavior | Replacement | Status |
| --- | --- | --- | --- |
| `pre-commit` | Locate bundle and run `run-go-hook.sh git-hook pre-commit`. | `coding-ethos-git-hook` compiled-policy runtime plus bundled hook groups. | Cutover ready |
| `pre-push` | Locate bundle and run `run-go-hook.sh git-hook pre-push`. | `coding-ethos-git-hook` compiled-policy runtime plus bundled hook groups. | Cutover ready |
| `commit-msg` | Locate bundle and run `run-go-hook.sh git-hook commit-msg`. | `coding-ethos-git-hook` compiled-policy runtime plus commit-message group. | Cutover ready |
| `post-commit` | Delegate to `git lfs post-commit`. | Installed Git LFS delegation shim. | Cutover ready |
| `post-merge` | Delegate to `git lfs post-merge`. | Installed Git LFS delegation shim. | Cutover ready |
| `post-checkout` | Delegate to `git lfs post-checkout`. | Installed Git LFS delegation shim. | Cutover ready |

## Claude Hooks

| Event | Tool | Imported Behavior | Replacement | Status |
| --- | --- | --- | --- | --- |
| `PreToolUse` | `Bash` | Block hook bypass, destructive git, protected-branch checkout, dangerous shell commands, background git, `gh --admin`, admin-only staged files, protected-branch writes, and protected paths. | Policy dispatch plus Go evaluators. | Installed |
| `PreToolUse` | `Write` / `Edit` / `MultiEdit` | Block protected-branch writes, protected paths, bare `except:`, broad `except Exception: pass`, and unexplained `# type: ignore`. | Policy dispatch plus Go evaluators. | Installed |
| `PostToolUse` | `Bash` | Feed git hook output back to the agent for summarization. | `hookSpecificOutput.additionalContext` from `coding-ethos-hook`. | Installed |
| `PreCompact` | any | Generate continuation prompt and notes from the transcript. | Deterministic transcript tail capture in `.git/coding-ethos-hooks/continuation/`. No hook-path AI call. | Installed |
| `SessionStart` | `compact` | Inject generated continuation prompt into session context. | Deterministic continuation context replay from `.git/coding-ethos-hooks/continuation/`. | Installed |

## Current Coverage

Runtime covered in Go agent-hook code:

- `git.hook_bypass`
- `git.destructive_command`
- `git.merge_strategy_shortcut`
- `git.force_push_protected_branch`
- `git.checkout_protected_branch`
- `git.destructive_worktree`
- `git.change_dir_flag`
- `git.stash_blocked`
- `git.commit_attribution`
- `git.staged_admin_files`
- `git.commit_head_advanced`
- `shell.dangerous_command`
- `shell.background_git`
- `shell.github_admin`
- `filesystem.protected_path`
- `filesystem.protected_branch_write`
- `python.conditional_imports`
- `python.optional_returns`
- `python.catch_and_silence`
- `python.structured_logging`
- `python.direct_imports`
- `python.bare_except`
- `python.unexplained_type_ignore`
- `pytest.gate`
- `generated_config.freshness`
- Post-tool hook output feedback for git hook commands
- Pre-compact deterministic continuation capture
- Compact session-start deterministic continuation replay

Imported fixtures live under `go/internal/hooks/testdata/legacy/`.
`go/internal/hooks/testdata/legacy_hook_inventory.json` is the machine-readable
inventory used to keep the migration status explicit.

## Remaining Integration Gaps

- Agent-facing advice can now include deterministic ETHOS reminders keyed to
  violated principles. Future work should move the reminder corpus into
  compiled policy data, add quiet-frequency controls, and include reminders
  such as "keep the todo list current" when work spans multiple planned steps.
- Agent-hook behavior has a repo-owned settings renderer, explicit sync, and
  doctor path through `coding-ethos-agent-hooks` and
  `run-go-hook.sh agent-hooks`. Cutover still requires intentionally choosing
  which repo or Claude settings file to update.
- Git hooks now enter `coding-ethos-git-hook`, a compiled-policy-owned Go
  runtime. It performs policy preflight and then runs the bundled hook groups as
  executable checks. Future work should continue migrating individual checks
  into compiled policy evaluators, but parent hook replacement no longer depends
  on legacy parent shims.
- Protected paths, protected branches, staged admin files, and shell/git policy
  enablement are compiled from `config.yaml`/repo override data. Remaining
  config work is to broaden that pattern to every evaluator as it moves into
  compiled policy.
- External tool-backed policies for pytest gating and generated-config
  freshness now dispatch through `coding-ethos-lint` smoke/full scopes. Future
  work should expand this pattern into typed tool metadata. The shared
  diagnostic model and first parser registry now normalize Ruff, Pyright, mypy,
  Pylint, golangci-lint, and fallback `file:line:column` output for compiled
  external evaluator results. Compiled `policy.evidence_maps` enrich known
  diagnostic codes with ETHOS policy IDs, principles, confidence, meaning, and
  repair advice while preserving unmapped diagnostics. The bundled Python
  type-check hook now uses the same shared parser and enrichment package instead
  of maintaining a private duplicate parser stack.
- Python static-tool defaults now come from a shared typed Go tool catalog.
  This is the first step toward moving shellcheck, yamllint, golangci-lint,
  actionlint, hadolint, and other hook tools into data-driven runtime metadata.
- Future compiled-policy checkers should include a repo ignore checker plus a
  license-header and copyright checker for first-party source and project
  files.
- Claude is the first concrete provider surface. The agent-hook settings
  renderer now consumes provider-neutral hook specs before rendering Claude
  settings. Remaining provider work is to add concrete Codex/Gemini adapters
  where their products expose lifecycle hooks, using the same runtime event
  inventory instead of duplicating policy wiring.

## Fixture Contract

Fixtures are representative imported payloads, not legacy adapters:

- `pretooluse_git_no_verify.json`
- `pretooluse_protected_path_write.json`
- `pretooluse_gh_admin.json`
- `pretooluse_bare_except_write.json`
- `pretooluse_type_ignore_edit.json`
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
