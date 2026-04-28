# Hook Replacement Plan

This document tracks the import-only migration from parent repo Git hooks and
repo-local agent hooks into the `coding-ethos` hook runtime. The old hooks are
source material only. Cutover should install `coding-ethos` shims and then
remove old parent and Claude hook entries outside this repo.

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
  the relevant Git or agent hook surface.
- `Settings verified`: `doctor` confirms native provider config files point at
  the expected hook command and feature flags are enabled.
- `Runtime probed`: `verify` runs provider-shaped JSON through the configured
  hook command. This proves the runtime path, not that the real provider binary
  has executed a tool.
- `Bridged`: installed Git hook execution includes compiled-policy preflight,
  then delegates to the current bundled hook group runner for checks not yet
  represented in compiled policy.
- `Cutover ready`: the behavior is runtime covered, installed, documented,
  settings verified, and runtime probed through the compiled-policy path. It
  does not imply real CLI end-to-end activation unless explicitly stated.

## Parent Git Hooks

| Hook | Imported Behavior | Replacement | Status |
| --- | --- | --- | --- |
| `pre-commit` | Locate bundle and run `run-go-hook.sh git-hook pre-commit`. | `coding-ethos-git-hook` compiled-policy runtime plus bundled hook groups. | Cutover ready |
| `pre-push` | Locate bundle and run `run-go-hook.sh git-hook pre-push`. | `coding-ethos-git-hook` compiled-policy runtime plus bundled hook groups. | Cutover ready |
| `commit-msg` | Locate bundle and run `run-go-hook.sh git-hook commit-msg`. | `coding-ethos-git-hook` compiled-policy runtime plus commit-message group. | Cutover ready |
| `post-commit` | Delegate to `git lfs post-commit`. | Installed Git LFS delegation shim. | Cutover ready |
| `post-merge` | Delegate to `git lfs post-merge`. | Installed Git LFS delegation shim. | Cutover ready |
| `post-checkout` | Delegate to `git lfs post-checkout`. | Installed Git LFS delegation shim. | Cutover ready |

## Agent Hooks

| Event | Tool | Imported Behavior | Replacement | Status |
| --- | --- | --- | --- | --- |
| `PreToolUse` / `BeforeTool` | shell command | Block hook bypass, destructive git, protected-branch checkout, dangerous shell commands, background git, `gh --admin`, admin-only staged files, protected-branch writes, and protected paths. Claude can receive transparent `updatedInput` git-wrapper rewrites; Codex and Gemini block raw git because their native hook contracts do not expose safe input rewriting. | Provider-neutral policy dispatch plus provider-specific output adapters. | Cutover ready for Claude/Codex/Gemini supported pre-tool surfaces |
| `PreToolUse` / `BeforeTool` | write/edit tools | Block protected-branch writes, protected paths, bare `except:`, broad `except Exception: pass`, and unexplained `# type: ignore`. Claude covers `Write`, `Edit`, and `MultiEdit`; Gemini maps `write_file`; Codex maps provider-neutral write plus native write/edit aliases. | Provider-neutral policy dispatch plus Go evaluators. | Cutover ready for supported write/edit surfaces |
| `PostToolUse` | shell command | Feed git hook output back to the agent for summarization. | `hookSpecificOutput.additionalContext` from `coding-ethos-hook` where the provider exposes post-tool context. Gemini has no direct equivalent and does not fake one. | Claude/Codex provider-limited |
| `PostToolUse` / `PostToolBatch` | write/edit tools | Surface lint/type feedback after code edits. | Deterministic post-edit checkpoint guidance for `Write`, `Edit`, and `MultiEdit`, plus compiled file-scope lint findings for the edited files. No AI or expensive external lint run in the hook path. | Runtime covered for compiled file-scope policies; external lint-state feedback remains planned |
| `UserPromptSubmit` | user prompt | Inject concise ETHOS reminders, todo-list reminders, and repo-specific guardrails before work starts. | Deterministic prompt guidance context. | Runtime covered where provider exposes the event |
| `Stop` / `SessionEnd` | session close | Surface outstanding failing gates, missing follow-up records, and continuation reminders. | Deterministic stop checkpoint guidance. | Runtime covered where provider exposes the event |
| `SubagentStart` / `SubagentStop` | delegated work | Provide scoped ETHOS context and collect subagent completion evidence. | Deterministic subagent start/stop guidance. | Runtime covered where provider exposes the event |
| `PreCompact` | any | Generate continuation prompt and notes from the transcript. | Deterministic transcript tail capture in `.git/coding-ethos-hooks/continuation/`. No hook-path AI call. Capture failures are visible advisory context. | Claude-focused; advisory |
| `SessionStart` | startup / compact | Inject generated continuation prompt into session context. | Deterministic continuation context replay from `.git/coding-ethos-hooks/continuation/`. | Cutover ready where provider supports context |

## Provider Capability Matrix

| Capability | Claude | Codex | Gemini CLI | Remaining Work |
| --- | --- | --- | --- | --- |
| Native settings generation | Full | Full (`.codex/config.toml`) | Full (`.gemini/settings.json`) | Promote manual real-provider smoke into an optional local command once provider CLI auth/trust can be made non-interactive. |
| Pre-tool shell blocking | Full | Full for provider-neutral and native shell aliases | Full for `BeforeTool` / `run_shell_command` | Keep adding observed tool aliases as fixtures. |
| Pre-tool git rewrite | Full via `updatedInput` | Unsupported by provider; block raw git | Unsupported by provider; block raw git | None unless providers add input rewrite. |
| Pre-tool write/edit blocking | Full for `Write`, `Edit`, `MultiEdit` | Full for native edit aliases | Full for `write_file`, `replace`, and `MultiEdit` | Add any newly observed native names. |
| Post-tool shell advice | Full | Full when Codex accepts additional context | Full through `AfterTool` where Gemini provides tool output | Keep provider-native, no legacy adapters. |
| Post-edit lint advice | Checkpoint plus compiled file-scope findings | Checkpoint plus compiled file-scope findings | Checkpoint plus compiled file-scope findings through `AfterTool` | Add external tool lint-state summaries later. |
| Prompt/session guidance | Runtime covered | Runtime covered | Runtime covered for mapped events | Add provider aliases if real CLIs use different names. |
| Continuation capture | Advisory and fail-visible | Advisory where payload includes transcript | Advisory where payload includes transcript | Keep failures visible without blocking normal work. |
| Verification | Settings plus synthetic runtime probes plus manual CLI activation smoke | Settings plus synthetic runtime probes plus manual CLI activation smoke | Settings plus synthetic runtime probes plus manual CLI activation smoke | Keep automated smoke deterministic; provider CLIs remain optional local verification. |

## Completion Checklist

- [x] Replace parent Git hook shims with repo-owned `coding-ethos` Git hook
  runtime and Git LFS delegation shims.
- [x] Generate all supported agent settings together; no single-provider
  install path.
- [x] Enforce git wrapper use across Claude, Codex, and Gemini pre-tool shell
  surfaces.
- [x] Block raw git bypasses including absolute git, nested shell git, Python
  subprocess git, PATH edits, and fake wrapper basenames.
- [x] Keep AI review calls out of the agent-hook path.
- [x] Add lifecycle hooks for `UserPromptSubmit`, `Stop`, `SessionEnd`, and
  subagent events where providers expose them.
- [x] Add deterministic post-edit lint/type feedback and concise advice.
- [x] Make continuation capture failures visible according to an explicit
  advisory-or-required policy.
- [x] Add real provider activation checks, or label verification as settings
  plus runtime-probe readiness everywhere.
- [x] Reconcile README, HOOKS, PRE-COMMIT, and this plan so cutover status is
  provider- and event-specific.

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
- `shell.forbidden_strings`
- `shell.best_practices`
- `syntax.file_syntax`
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
- Post-edit deterministic verification guidance plus compiled file-scope
  findings for `Write`, `Edit`, and `MultiEdit`
- Pre-compact deterministic continuation capture
- Compact session-start deterministic continuation replay
- User-prompt, tool-batch, stop/session-end, and subagent lifecycle guidance

Imported fixtures live under `go/internal/hooks/testdata/legacy/`.
`go/internal/hooks/testdata/legacy_hook_inventory.json` is the machine-readable
inventory used to keep the migration status explicit.

## Remaining Integration Gaps

- Agent-facing advice can now include deterministic ETHOS reminders keyed to
  violated principles. Future work should move the reminder corpus into
  compiled policy data, add quiet-frequency controls, and include reminders
  such as "keep the todo list current" when work spans multiple planned steps.
- Agent-hook behavior has a repo-owned settings renderer, explicit sync,
  doctor, and verify path through `coding-ethos-agent-hooks` and
  `run-go-hook.sh agent-hooks`. Generation is all-provider only: Claude,
  Codex, and Gemini surfaces are rendered together. Doctor verifies the native
  activation files for each provider. Verify runs doctor and then executes
  provider-native smoke payloads through the configured hook command to prove
  the installed config reaches a runnable policy path. Claude uses Claude
  Code's native settings file. Codex uses `[features].codex_hooks = true` plus
  native `[hooks]` entries in `.codex/config.toml`. Gemini uses native
  `.gemini/settings.json` hooks with `hooksConfig.enabled = true`.
- Cutover is a first-class runtime operation: `run-go-hook.sh cutover install`
  installs Git hook shims, syncs all supported agent hook settings, and then
  runs `cutover verify`. `cutover verify` checks installed Git shims,
  provider-native agent hook probes, and the policy runtime validation hook,
  then emits one TOON readiness report.
- Git hooks now enter `coding-ethos-git-hook`, a compiled-policy-owned Go
  runtime. It performs policy preflight and then runs the bundled hook groups as
  executable checks. Syntax validation and shell best-practice checks have
  moved into compiled policy evaluators; future work should continue migrating
  individual checks, but parent hook replacement no longer depends on legacy
  parent shims.
- Protected paths, protected branches, staged admin files, and shell/git policy
  enablement are compiled from `config.yaml`/repo override data. Syntax file
  extensions and shell best-practice prefixes are also compiled from config.
  Remaining config work is to broaden that pattern to every evaluator as it
  moves into compiled policy.
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
- Claude, Codex, and Gemini now share the provider-neutral hook spec and
  runtime event inventory. The hook runtime normalizes Claude native payloads,
  provider-neutral Codex/Gemini payloads, Gemini `BeforeTool` payloads, and
  nested Codex-style `tool_call` payloads into one policy event before
  evaluation, then adapts output to each provider's strongest supported native
  response contract. Claude keeps rewrite-capable `hookSpecificOutput`. Codex
  supports native block and context feedback but not `updatedInput`, so git
  wrapper enforcement blocks raw git instead of transparent rewrite. Gemini
  supports native `deny` / `systemMessage` for tool gates and context on
  supported lifecycle hooks, but does not expose a direct `PostToolUse`
  equivalent; unsupported lifecycle points remain absent rather than being
  faked through legacy adapters.
- Real-provider activation was manually smoke-tested on 2026-04-28 in a
  disposable repo created with `cutover install`. Claude loaded project hooks
  and emitted `SessionStart` / `UserPromptSubmit` hook events before local auth
  failed. Codex `exec --enable codex_hooks` loaded repo-local `.codex/config.toml`
  and created hook-run logs for `SessionStart`, `UserPromptSubmit`,
  `PreToolUse`, `PostToolUse`, and `Stop`. Gemini ran with explicit workspace
  trust and reported successful `SessionStart`, `BeforeAgent`, and `AfterAgent`
  hook execution while writing `.coding-ethos/hook-runs/` logs.

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
