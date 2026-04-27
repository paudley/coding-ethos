# Agent Hook Management Plan

`coding-ethos` already provides shared repository ethos guidance and
pre-commit policy. Agent hooks should become the next enforcement layer: fast,
deterministic checks around individual agent actions.

The goal is to let a consumer repository install a small, reviewed hook runner
from `coding-ethos`, configure repo-specific policy in `repo_config.yaml`, and
receive consistent protection across Claude Code projects without copying
personal hook scripts by hand.

## Emerging Strategy

The cohesive architecture is one policy model with several enforcement
surfaces.

```text
coding_ethos.yml
repo_ethos.yml
config.yaml
repo_config.yaml
        |
        v
compiled coding-ethos policy bundle
        |
        +--> rendered agent guidance
        +--> unified linter
	+--> Go git hooks
        +--> agent hooks
        +--> git wrapper
        +--> generated prompt packs
```

In this model:

- `coding_ethos.yml` and `repo_ethos.yml` explain the engineering contract.
- `config.yaml` and `repo_config.yaml` define executable policy.
- The unified linter runs selected checks and reports policy-aware results.
- Pre-commit remains the hard commit-time gate.
- Agent hooks are the interactive per-action layer.
- The git wrapper becomes the controlled git execution boundary.

The hook system should not duplicate every linter or pre-commit rule. Instead,
hooks should steer agents toward the unified linter, decide when it is required,
and interpret its output in terms of ethos and repo policy.

## Defense-In-Depth Model

Agent policy should be treated as a cyber-defense problem. No single layer is
trusted to catch every violation.

For every rule, `coding-ethos` should ask:

- Can we persuade the agent not to attempt the action?
- Can we intercept the tool call before it runs?
- Can we mediate the operation through a policy-aware wrapper?
- Can we detect the resulting bad state?
- Can we enforce the rule before commit or push?
- Can we verify the claimed outcome?
- Can we record evidence for future actions and compaction?
- Can we notify the agent or user when the runtime supports notifications?

The layers are:

```text
1. Persuade
   Agent guidance, ethos docs, prompt packs, and UserPromptSubmit advice.

2. Intercept
   PreToolUse hooks block, ask, or advise before a tool runs.

3. Mediate
   Binary wrappers and PATH shims route risky tools through policy-aware
   executables.

4. Detect
   Unified lint identifies violations in worktree, staged, command, and runtime
   state.

5. Enforce
   Pre-commit and pre-push gates prevent bad state from entering history or
   leaving the repo.

6. Verify
   PostToolUse hooks and state checks verify that claimed outcomes actually
   happened.

7. Record
   Hook, linter, and wrapper state preserves evidence for later decisions.

8. Notify
   Agent notification hooks surface important policy events when the runtime
   supports notification channels.
```

Example for `git.hook_bypass`:

```text
Persuade:
  ETHOS, AGENTS, CLAUDE, and prompt packs say bypasses are forbidden.

Intercept:
  coding-ethos-hook blocks Bash tool calls containing bypass intent.

Mediate:
  PATH shims and rewritten git calls route through coding-ethos-git.

Detect:
  coding-ethos-lint detects bypass intent from argv and reports a block
  decision.

Enforce:
  Git hooks and pre-commit remain mandatory quality gates.

Verify:
  PostToolUse checks confirm whether a commit actually landed.

Record:
  agent-state records the bypass attempt and policy ID.

Notify:
  Notification-capable agents can surface repeated bypass attempts, failed
  gates, or verified state mismatches without waiting for the next tool call.
```

This overlap is intentional. Hooks are the interaction layer. Wrappers are the
execution boundary. Linters are the detection layer. Pre-commit is the history
gate. Post-action checks are the evidence layer.

The compiler should eventually attach defense-layer metadata to every policy so
tooling can report which layers currently protect a rule, which layers notify,
and which layers are still missing.

## Unified Linter Layer

`coding-ethos` should provide a unified linter that runs the relevant subset of
pre-commit checks, enforces machine-checkable ethos rules, and emits one
consistent result format.

The unified linter should be the policy execution API for agents.

Example commands:

```bash
coding-ethos lint --changed
coding-ethos lint --staged
coding-ethos lint --files path/to/file.py
coding-ethos lint --smoke
coding-ethos lint --full
coding-ethos lint --json --changed
```

The linter should load the same merged policy bundle as hooks, pre-commit, and
the git wrapper. It should not invent another configuration format.

### Linter Responsibilities

The unified linter should coordinate:

- Fast file-scoped checks from the pre-commit bundle.
- Repo-configured pytest smoke gates.
- Generated config freshness checks.
- Machine-checkable ethos policies.
- Git and staging policy checks when running `--staged`.
- Optional AI review checks when configured.
- Consistent result formatting for agents and humans.

It should provide both human-readable and JSON output.

JSON output should include:

- check ID
- policy source
- severity
- result status
- affected files
- concise message
- suggested command or fix
- related ethos principle IDs
- whether the result is blocking for commit or only advisory

### Linter Modes

The linter should support progressively broader scopes.

#### `--files`

Run checks relevant to explicit files.

Use for immediate feedback after `Write`, `Edit`, or `MultiEdit`.

#### `--changed`

Run checks relevant to changed worktree files.

Use for background feedback while the agent is working.

#### `--staged`

Run checks relevant to the current index.

Use before commit attempts.

#### `--smoke`

Run the repo-configured smoke gate.

Use before commit when tests changed or when the user asks for quick
verification.

#### `--full`

Run the canonical repo validation path.

Use before push, PR handoff, or explicit readiness claims.

### Ethos-Aware Lint Results

The unified linter should translate low-level findings into `coding-ethos`
terms.

Example:

```text
FAIL python.conditional_imports
Principle: no-conditional-imports
File: src/app/plugins.py
Reason: Required dependencies should fail immediately; ImportError fallback
creates a soft dependency path.
Fix: Remove the conditional import or add a repo-configured exemption.
```

Example JSON:

```json
{
  "check_id": "python.conditional_imports",
  "status": "fail",
  "severity": "block",
  "principle_ids": ["no-conditional-imports"],
  "files": ["src/app/plugins.py"],
  "message": "Required dependencies should fail immediately; ImportError fallback creates a soft dependency path.",
  "suggestion": "Remove the conditional import or configure an explicit exemption."
}
```

This lets hooks provide concise advice without scraping raw linter output.

### Hook Enforcement of Linter Use

Agent hooks can use the unified linter in several ways.

#### Required Before Commit

Before `git commit`, hooks can require that the relevant linter scope has run
successfully since the last staged-file change.

Example:

```text
[coding-ethos:lint-required] Staged Python files changed after the last
successful `coding-ethos lint --staged`. Run it before committing.
```

This should be configurable. Some repos may require only `--staged`; stricter
repos may require `--smoke` or `--full`.

#### Required Before Push

Before `git push`, hooks can require a broader validation pass.

Example:

```text
[coding-ethos:lint-required] Push requires a successful
`coding-ethos lint --full` for the current HEAD.
```

The git wrapper can enforce the same rule for unmanaged git invocations.

#### Advisory After Edit

After edits, hooks can suggest the cheapest useful linter command.

Examples:

- Python source changed: suggest `coding-ethos lint --files <paths>`.
- Tests changed: suggest `coding-ethos lint --smoke`.
- Config changed: suggest `coding-ethos lint --changed`.

#### Background Runs

Hooks can start background lint runs when configured.

Useful cases:

- After a batch of edits.
- After generated config changes.
- After a failed pre-commit run.
- Before compaction or session end.

Background lint must be carefully bounded:

- It should run only fast scopes by default.
- It should write output to hook-owned state.
- It should not modify source files.
- It should not hide current tool output.
- It should report results back through `PostToolUse`, `PostToolBatch`,
  `Stop`, or `SessionEnd`.

#### Post-Action Annotation

When background lint completes, hooks can feed concise advice to the agent:

```text
[coding-ethos:lint-background] `coding-ethos lint --changed` found 2 blocking
issues: python.conditional_imports and python.docstrings. Run
`coding-ethos lint --changed` for details before committing.
```

This gives agents useful feedback without turning every edit into a blocking
operation.

### Linter State

The linter should write verification state into a hook-owned directory such as:

```text
.git/coding-ethos-hooks/lint-state/
```

Useful state:

- last successful linter command
- files covered by the last successful run
- staged tree hash covered by the last successful `--staged` run
- commit SHA covered by the last successful `--full` run
- last failing check IDs
- background job status
- generated config freshness hash

Hooks and the git wrapper can consult this state before commit or push.

### Relationship to Pre-Commit

The unified linter should not weaken pre-commit. It should make pre-commit
policy easier to run earlier and easier for agents to understand.

Pre-commit remains authoritative at the commit boundary. The unified linter is
the agent-friendly interface for:

- targeted checks during editing
- staged checks before commit
- smoke checks before risky operations
- full checks before push or handoff
- policy-aware explanation of failures

If the unified linter and pre-commit ever disagree, that is a bug in
`coding-ethos`, not a reason to bypass either gate.

### Hook-to-Linter Flow

Recommended flow:

1. `Write`, `Edit`, or `MultiEdit` completes.
2. `PostToolUse` records changed files and suggests or starts
   `coding-ethos lint --files`.
3. `PostToolBatch` optionally starts `coding-ethos lint --changed` in the
   background.
4. `PreToolUse` for `git commit` checks whether staged content is covered by a
   passing `coding-ethos lint --staged`.
5. `PreToolUse` for `git push` checks whether current `HEAD` is covered by a
   passing `coding-ethos lint --full`.
6. The git wrapper enforces the same commit/push requirements when git is
   invoked outside the hook path.
7. `PostToolUse`, `Stop`, or `SessionEnd` surfaces concise lint results and
   next actions.

### Guardrails for Background Lint

- Do not launch unbounded background jobs.
- Do not run expensive full validation automatically unless explicitly
  configured.
- Do not mutate files from background lint.
- Do not report stale lint results as current.
- Key linter state by file hash, staged tree hash, or commit SHA.
- Cancel or supersede background jobs when newer edits invalidate them.
- Preserve raw linter output in a log, but show agents concise summaries.

## Current Local Hook Landscape

The current lbox environment has three hook sources worth consolidating.

### Global Claude Hooks

`~/.claude/settings.json` currently configures:

- `PreToolUse` for `Bash`, `Write`, and `Edit`:
  - `~/.claude/hooks/pretooluse-guards.py`
  - `~/.claude/hooks/protect-usr-bin-got.py`
- `PostToolUse` for `Bash`:
  - `~/.claude/hooks/precommit-visibility.py`
- `PreCompact`:
  - `~/.claude/hooks/generate-continuation-prompt.py`
- `SessionStart` for compact resumes:
  - `~/.claude/hooks/inject-continuation-prompt.py`

The global `pretooluse-guards.py` already enforces several strong safety
policies:

- Blocks destructive git commands such as `git reset --hard`,
  `git clean -fd`, and `git checkout --theirs/--ours`.
- Blocks hook bypasses such as `SKIP=`, `--no-verify`, `git commit -n`, and
  `GIT_VERIFY=false`.
- Blocks force pushes to protected branches.
- Blocks dangerous shell patterns such as `rm -rf`, `curl | sh`, `wget | sh`,
  and `chmod 777`.
- Blocks backgrounded or timeout-wrapped `git commit` and `git push`.
- Blocks edits on protected branches except explicitly allowed paths.
- Blocks protected `/usr/bin/got` references.
- Blocks commits containing admin-only staged files.
- Blocks known bad edit patterns such as bare `except:`,
  `except Exception: pass`, and unexplained `# type: ignore`.

The global `precommit-visibility.py` is useful but currently has one important
failure mode: it can report "The commit succeeded" based on tool output or
return code even when `HEAD` did not move. `coding-ethos` should replace this
with explicit pre/post `HEAD` verification for `git commit`.

### lbox Project Hooks

The lbox repo currently has project-local Claude hooks under `.claude/hooks`:

- `worktree-guard.py` protects the main checkout and linked worktrees from
  accidental cross-editing.
- `staged-file-guard.py` blocks staging or committing risky hook and
  configuration files unless explicitly allowed.
- `git-commit-post-verify.py` verifies whether a `git commit` actually landed
  by comparing the pre-commit and post-commit `HEAD` values.

These are repo-specific policies and should become configurable
`coding-ethos` policies rather than copied scripts.

### OpenWolf Hooks

The lbox `.wolf/hooks` system provides context and memory support:

- Session start ledger and continuation context.
- Repeated read/write tracking.
- File anatomy updates.
- Buglog and cerebrum reminders.
- Stop-time reminders for missing follow-up records.

These are useful but should be treated as advisory context hooks, not core
blocking safety policies.

## Claude Hook Capabilities

Claude Code hooks receive structured JSON on stdin and can run at lifecycle
points such as:

- `PreToolUse`
- `PostToolUse`
- `PermissionRequest`
- `PermissionDenied`
- `PostToolBatch`
- `UserPromptSubmit`
- `SessionStart`
- `SessionEnd`
- `PreCompact`
- `PostCompact`
- `SubagentStart`
- `SubagentStop`
- `TaskCreated`
- `TaskCompleted`
- `ConfigChange`
- `FileChanged`
- `CwdChanged`

`PreToolUse` can enforce policy before actions such as `Bash`, `Read`, `Write`,
`Edit`, `MultiEdit`, `WebFetch`, `WebSearch`, `Agent`, and MCP tool calls.

Important behavior:

- Exit code `2` blocks a tool call and sends stderr back to Claude.
- Exit code `0` allows the tool call.
- Other non-zero exit codes are generally treated as errors or warnings, not
  policy blocks.
- Advanced JSON output can express hook-specific decisions such as allow,
  deny, ask, defer, or modified tool input.
- Hooks execute with the user's permissions, so generated hooks must be small,
  reviewed, deterministic, and testable.

## Expressing `coding_ethos.yml` Through Hooks

`coding_ethos.yml` is already structured enough to drive hook behavior. Each
principle has a stable `id`, `order`, `title`, `directive`, `summary`,
`quick_ref`, `tags`, `related`, per-agent `agent_hints`, and detailed sections.
Hooks should treat this as policy data, not just rendered prose.

Agent hooks should not reduce ethos enforcement to denial. Most ethos guidance
is better expressed as timely context, targeted reminders, permission prompts,
or post-action reflection.

### Hook Interaction Modes

Each ethos principle should be expressible through one or more interaction
modes.

#### `block`

Use only when a requested action clearly violates a hard rule and allowing the
tool call would create damage or bypass a required gate.

Examples:

- Block `--no-verify` under `linting-as-code-quality-enforcement`.
- Block `SKIP=` under `one-path-for-critical-operations`.
- Block edits to protected external data under `security-by-design`.
- Block destructive git under `no-rationalized-shortcuts`.

#### `ask`

Use when an action may be legitimate but needs explicit human confirmation or a
more precise plan.

Examples:

- Ask before modifying generated agent hook configuration.
- Ask before editing repo administration files.
- Ask before changing public interfaces without nearby tests.
- Ask before touching external stateful data roots.

This maps well to Claude hook JSON decisions that request permission rather
than hard-denying the tool call.

#### `advise`

Use when the agent needs a principle at the point of action, but the action is
not obviously unsafe.

Examples:

- Before editing architecture or interfaces, surface the `solid-is-law` and
  `protocol-first-design` quick refs.
- Before adding optional fallbacks, surface `fail-fast-fail-hard-overview`,
  `no-conditional-imports`, and `no-if-available-capability-checks`.
- Before editing validation code, surface `validation-at-the-gate` and
  `no-conditional-validation`.
- Before writing public APIs, surface `documentation-as-contract`.

Advice should be short, specific, and attached to the tool result as additional
context. It should not obscure the actual command or edit output.

#### `annotate`

Use after an action to connect what happened to relevant ethos principles.

Examples:

- After a failed test run, remind the agent that `testing-as-specification`
  requires addressing the failure rather than calling it acceptable.
- After a hook modifies files, remind the agent to inspect and account for the
  generated changes.
- After a commit attempt fails, annotate the failure with the specific blocked
  policy and the next corrective command.

#### `record`

Use for lightweight audit trails and learning signals.

Examples:

- Record which principle blocked a command.
- Record repeated advisory warnings that the agent ignored.
- Record files modified under a principle-sensitive area.
- Record post-commit `HEAD` verification results.

These records should be hook-owned metadata, not edits to project source files.

#### `route`

Use to guide the agent toward the right detailed reference.

Examples:

- A `Write` to `interfaces/` can route the agent to the
  `protocol-first-design` detail doc.
- A `Write` to tests can route the agent to `testing-as-specification`.
- A `Bash` command involving `git push` can route the agent to
  `linting-as-code-quality-enforcement`, `one-path-for-critical-operations`,
  and `feedback-as-a-first-class-citizen`.

Routing is a good fit for advisory hooks because it keeps prompts small while
still making the right detailed document discoverable.

### Principle-to-Hook Mapping

`coding-ethos` should build an index from the merged primary ethos and
repo-specific overlay:

```yaml
agent_hooks:
  ethos:
    enabled: true
    default_mode: advise
    principles:
      linting-as-code-quality-enforcement:
        events:
          PreToolUse:
            Bash:
              mode: block
              patterns:
                - "--no-verify"
                - "SKIP="
                - "git commit -n"
      protocol-first-design:
        events:
          PreToolUse:
            Write:
              mode: advise
              path_patterns:
                - "**/interfaces/**"
                - "**/protocols/**"
            Edit:
              mode: advise
              path_patterns:
                - "**/interfaces/**"
                - "**/protocols/**"
      testing-as-specification:
        events:
          PostToolUse:
            Bash:
              mode: annotate
              command_patterns:
                - "pytest"
                - "make check"
```

The policy engine should compile this into a fast runtime table:

- event name
- tool matcher
- path matcher
- command matcher
- principle ID
- interaction mode
- message template
- severity

The default message should use the principle's `directive`, `quick_ref`, and
agent-specific hint for the active provider.

### Generated Advice Shape

Advice should be concise and directly tied to the attempted action.

Example for an edit to an interface file:

```text
[coding-ethos:protocol-first-design] Before changing interfaces, verify the
Protocol exists and update callers/tests against the interface contract.
Detail: .agents/ethos/protocol-first-design.md
```

Example for a blocked bypass:

```text
[coding-ethos:linting-as-code-quality-enforcement] Blocked `--no-verify`.
This repo treats hooks as mandatory quality gates. Fix the hook failure instead
of bypassing it.
```

Example for a post-test annotation:

```text
[coding-ethos:testing-as-specification] Pytest failed. Treat the failure as an
executable specification mismatch: fix code, tests, or both before claiming the
task is complete.
```

### Event-Level Ethos Applications

#### `UserPromptSubmit`

This event can add context before the agent starts acting.

Useful behaviors:

- Detect destructive requests and surface `no-rationalized-shortcuts`.
- Detect architecture requests and surface `solid-is-law`.
- Detect security-sensitive requests and surface `security-by-design`.
- Detect requests involving tests and surface `testing-as-specification`.
- Detect git or PR requests and surface `feedback-as-a-first-class-citizen`.

This should normally be advisory. Blocking user prompts would be too heavy
except for explicit policy violations in a managed environment.

#### `PreToolUse`

This is the main enforcement point.

Useful behaviors:

- Block unsafe shell commands.
- Ask before high-risk repo administration edits.
- Advise before principle-sensitive code edits.
- Route the agent to relevant detailed docs.
- Add repo-specific reminders based on path ownership.

#### `PostToolUse`

This is the best place for evidence-based follow-up.

Useful behaviors:

- Verify git state after commit/push commands.
- Annotate failed tests with `testing-as-specification`.
- Annotate failed type/lint checks with
  `static-analysis-is-the-first-line-of-defense`.
- Remind the agent to inspect hook-generated modifications.
- Record which principles were implicated.

#### `PermissionRequest`

Permission decisions can use ethos context to distinguish legitimate escalation
from shortcut-seeking.

Examples:

- Approve read-only inspection of protected metadata.
- Ask for clarification before mutating protected data.
- Deny escalation whose only purpose is bypassing hooks or quality gates.

#### `SubagentStart` and `SubagentStop`

Subagent hooks can express `sub-agent-delegation-and-context-isolation`.

Useful behaviors:

- Require a scoped task description for subagents.
- Advise against delegating the immediate critical-path blocker.
- Record subagent scope and final result.
- Warn if a subagent returns without evidence or changed-file summary.

#### `SessionStart`, `PreCompact`, and `PostCompact`

Session hooks can express durable ethos context without loading the full ethos
every turn.

Useful behaviors:

- Inject a short ethos index.
- Link to `.agents/ethos/README.md`.
- Restore recent policy decisions after compaction.
- Preserve unresolved hook warnings across context transitions.

### Configurable Severity

Every principle-derived hook should support these severities:

- `off`: do not evaluate this hook.
- `record`: log only.
- `advise`: add context to the agent.
- `ask`: request explicit permission or clarification.
- `block`: deny the tool call.

The same principle may use different severities by event. For example,
`security-by-design` might advise on ordinary edits, ask on credential-adjacent
changes, and block known secret writes.

### Repo-Specific Refinement

`repo_ethos.yml` and `repo_config.yaml` should be able to refine the shared
ethos without forking it.

Examples:

- Add repo-specific path matchers for a principle.
- Promote a shared advisory principle to blocking in a stricter repo.
- Demote a noisy shared check to `record` while it is being tuned.
- Add repo-specific message text.
- Connect principles to canonical repo commands.

This keeps `coding_ethos.yml` as the shared contract while allowing each repo to
express local topology and risk.

### Prompt Pack Reuse

`coding-ethos` already generates a Gemini prompt pack from the merged ethos and
repo policy. Agent hooks should reuse the same concept at smaller scope:

- Select only principles relevant to the attempted action.
- Prefer quick refs over full prose.
- Link to generated detail docs for depth.
- Include repo-specific overrides from the merged bundle.
- Cache the compiled prompt fragments under a hook-owned cache directory.

This avoids loading the entire ethos into every hook response while still
making the hook advice grounded in the canonical source data.

### Guardrails for Advice

Ethos-driven advice must stay useful:

- Do not emit generic reminders on every action.
- Do not repeat the same advice indefinitely in one session.
- Do not block when the principle only implies a design preference.
- Do not bury command output under long policy prose.
- Include the principle ID so the agent can cite and resolve the issue.
- Prefer one or two relevant principles over broad ethos dumps.
- Treat repeated ignored advice as a signal that may justify escalation from
  `advise` to `ask` in future sessions, but not automatically in the current
  action.

## Expressing Pre-Commit Through Hooks

The pre-commit system is the hard quality gate. Agent hooks should not replace
it or fork its policy. They should make the same policy visible earlier in the
agent workflow, while there is still time to choose a better action.

`coding-ethos` already separates policy from execution:

- `config.yaml` defines the shared enforcement model.
- `repo_config.yaml` refines that model for a consumer repo.
- `pre-commit/` is the execution bundle.
- Generated tool configs and Gemini prompt packs are derived artifacts.

Agent hooks should use that same merged policy model. The hook layer should be
an interactive front end to pre-commit policy, not a second source of truth.

### Pre-Commit Interaction Modes

Pre-commit-derived hooks should support the same interaction modes as
ethos-derived hooks.

#### `block`

Use when the agent is trying to bypass or disable the gate.

Examples:

- Block `--no-verify`.
- Block `SKIP=`.
- Block edits that disable generated lint, type, or hook configuration without
  explicit approval.
- Block removing required hook shims from `.git/hooks/`.

#### `ask`

Use when the action changes the quality gate itself.

Examples:

- Ask before editing hook configuration or `pre-commit/`.
- Ask before editing `pyproject.toml`, `mypy.ini`, `ruff.toml`,
  `pyrightconfig.json`, or generated tool config.
- Ask before modifying `repo_config.yaml` sections that relax checks.
- Ask before changing smoke-test marker configuration.

The prompt should name the affected gate and ask for confirmation that the
change is intentional.

#### `advise`

Use before edits likely to trigger a known pre-commit policy.

Examples:

- On Python edits, advise about direct imports, conditional imports, optional
  returns, catch-and-silence, structured logging, docstrings, and type checking
  when the file content or path suggests risk.
- On test edits, advise about banned skip markers and the configured pytest
  gate.
- On documentation or source module edits, advise about module documentation
  and source docs expectations.
- On SQL-adjacent edits, advise about SQL centralization and security patterns
  when those checks are enabled.

This is the most useful non-blocking mode: it lets the agent adjust the edit
before pre-commit has to reject it.

#### `annotate`

Use after a command runs to explain gate output in policy terms.

Examples:

- If Ruff fails, annotate the output with the configured style source.
- If mypy or pyright fails, remind the agent that static analysis is a
  required gate.
- If pytest fails, connect it to the configured `pytest_gate` command and
  marker policy.
- If a generated config check fails, explain which source file should be
  edited instead of the generated artifact.

Annotations should summarize and route. They should not hide the original hook
output.

#### `record`

Use to capture quality-gate signals across a session.

Examples:

- Record which pre-commit checks failed most recently.
- Record that hook-generated files changed after a command.
- Record repeated attempts to bypass gates.
- Record successful full-gate runs before commit or push.

This state can support better `Stop`, `SessionEnd`, and `PreCompact` reminders.

#### `prepare`

Use before an expensive or likely failing gate to suggest a cheaper local
check.

Examples:

- Before commit, suggest running the configured smoke pytest gate if Python
  tests changed.
- Before push, suggest the repo's full validation entrypoint.
- Before running a broad test suite, suggest the configured smoke command when
  the user asked for a quick check.
- Before running generated config checks, suggest `sync-tool-configs` if the
  source policy changed.

Preparation should be advisory unless the repo marks the gate as mandatory for
that action.

### Pre-Commit-to-Hook Mapping

The hook runner should compile the merged enforcement config into action-level
signals.

Example:

```yaml
agent_hooks:
  pre_commit:
    enabled: true
    default_mode: advise
    checks:
      hook_bypass:
        mode: block
      generated_tool_configs:
        mode: ask
        source_files:
          - config.yaml
          - repo_config.yaml
        generated_files:
          - ruff.toml
          - mypy.ini
          - pyrightconfig.json
          - .yamllint.yml
      pytest_gate:
        mode: prepare
        before:
          - git commit
          - git push
      direct_imports:
        mode: advise
        tools:
          - Write
          - Edit
          - MultiEdit
      conditional_imports:
        mode: advise
        tools:
          - Write
          - Edit
          - MultiEdit
```

The compiled table should include:

- check ID
- enabled state
- configured severity
- relevant files or path patterns
- relevant tools
- relevant shell commands
- source config path
- generated artifact paths
- suggested corrective command

### Event-Level Pre-Commit Applications

#### `UserPromptSubmit`

Use this to detect work that likely needs quality-gate context.

Useful behaviors:

- If the prompt mentions commit or push, add a short reminder that hooks are
  mandatory and bypasses are blocked.
- If the prompt asks to relax lint/type/test policy, ask for explicit intent and
  point to the relevant source config.
- If the prompt asks for a quick verification pass, suggest the configured
  smoke gate.

#### `PreToolUse`

Use this to catch bypasses and provide pre-edit advice.

Useful behaviors:

- Block gate bypass commands.
- Ask before changing hook or tool configuration.
- Advise when an edit is likely to trip a configured check.
- Route generated-file edits back to `config.yaml` or `repo_config.yaml`.
- Suggest targeted pre-commit checks after high-risk edits.

#### `PostToolUse`

Use this to interpret tool outcomes.

Useful behaviors:

- Parse pre-commit, pytest, Ruff, mypy, and pyright output into a
  short action summary.
- Preserve original output and avoid false success summaries.
- Detect when generated configs changed and advise inspecting them.
- Verify that `git commit` moved `HEAD` before reporting success.
- Record the last known failing gate for `Stop` or `SessionEnd`.

#### `PostToolBatch`

Use this after batches of edits to suggest the next cheapest quality gate.

Examples:

- After Python source edits, suggest static analysis.
- After test edits, suggest the smoke pytest command.
- After config edits, suggest generated config sync/check commands.

#### `Stop` or `SessionEnd`

Use this to catch unfinished quality work.

Useful behaviors:

- Warn if relevant files changed and no quality gate ran.
- Warn if the last quality gate failed.
- Warn if generated artifacts are stale.
- Summarize the commands that would close the loop.

### Advice Examples

Pre-edit advice for a Python file:

```text
[coding-ethos:pre-commit:conditional_imports] This repo checks conditional
imports. Required dependencies should fail fast; avoid ImportError fallbacks
unless repo_config.yaml explicitly exempts this path.
```

Generated config routing:

```text
[coding-ethos:pre-commit:generated_tool_configs] `ruff.toml` is generated.
Edit `config.yaml` or the consumer repo's `repo_config.yaml`, then run
`coding-ethos sync-tool-configs`.
```

Post-test annotation:

```text
[coding-ethos:pre-commit:pytest_gate] The configured pytest gate failed.
Address the failing tests before committing; do not bypass with SKIP or
--no-verify.
```

Preparation before commit:

```text
[coding-ethos:pre-commit:prepare] Python tests changed. The configured quick
gate is `uv run --frozen pytest ... -m smoke`; running it before commit should
catch the cheapest failures first.
```

### Relationship to Pre-Commit Execution

Agent hooks should not run the full pre-commit suite automatically on every
edit. That would be slow and noisy. Instead:

- Run cheap static checks only when explicitly configured.
- Prefer advice and routing before edits.
- Prefer summaries and corrective commands after failures.
- Leave full gate execution to commit, push, or explicit user request.
- Never claim readiness unless the relevant configured gate actually ran and
  passed.

### Pre-Commit Hook State

The hook layer should keep minimal state in a hook-owned directory such as
`.git/coding-ethos-hooks/agent-state/`.

Useful state:

- last pre-commit command
- last failing check ID
- last successful full validation timestamp
- pre-commit `HEAD` before commit attempts
- generated artifact freshness hash
- repeated bypass attempts

This state should support better advice. It should not become another project
artifact that users must maintain by hand.

### Guardrails for Pre-Commit Advice

- Do not duplicate full lint or type output.
- Do not run expensive gates implicitly on every edit.
- Do not suggest bypasses.
- Do not invent commands; use the configured `test_command`, Make targets, or
  generated tool config commands.
- Do not treat advisory mode as proof of readiness.
- Do not produce success banners from return codes alone.
- Prefer "what to run next" over generic quality reminders.

## Git Wrapper Integration

The `git-wrapper/` prototype is a Go command intended to intercept git
operations, apply workflow safety checks, and then exec the real git binary.
It overlaps heavily with the existing Bash-oriented agent hooks, which makes it
a good candidate for the shared git enforcement boundary.

### Current Wrapper Findings

The prototype currently:

- Executes `/usr/bin/got` as the real git binary.
- Uses `GIT_WRAPPER_DEPTH` and parent-process inspection to avoid blocking
  internal git subprocesses.
- Blocks destructive git commands such as `reset --hard`, `clean -fd`,
  `checkout --theirs`, `checkout --ours`, and broad `restore --`.
- Blocks destructive worktree operations such as `worktree remove --force`,
  `worktree prune`, and `worktree move --force`.
- Blocks `merge -X theirs` and `merge -X ours`.
- Blocks force pushes to `main` and `master`.
- Blocks switching to `main` and `master`.
- Blocks hook bypasses through flags and environment variables.
- Blocks AI `Co-Authored-By` commit trailers.
- Blocks commits containing admin-only staged files.
- Blocks direct commits to protected branches except docs-only commits.
- Blocks `git -C` to force explicit working-directory context.
- Blocks `git push` from paths containing `lbox`.

The policy intent is useful, but several details should be generalized before
this becomes part of `coding-ethos`:

- The real git path must be configurable and discovered, not hardcoded to
  `/usr/bin/got`.
- Repo-specific rules such as "no push from lbox" must live in
  `repo_config.yaml`, not in the wrapper binary.
- Protected branches, admin-only files, docs-only exceptions, and worktree
  policy must be config-driven.
- The wrapper should avoid regex-only command reasoning for high-risk git
  operations; it already receives argv and should classify subcommands from
  structured arguments.
- The wrapper should emit structured decisions that agent hooks can reuse.
- The checked-in compiled binary should not be the source of truth; builds
  should come from source through the normal toolchain.

### Wrapper Role

The wrapper should become the canonical git execution path for managed agent
sessions.

Agent hooks are good at observing and deciding around tool calls. The wrapper
is better at being the actual executable boundary for git. The two should work
together:

- Hooks detect intent and provide advice, routing, permission prompts, and
  post-action verification.
- The wrapper enforces git policy even when a command reaches the shell.
- Both use the same compiled policy model from `config.yaml`,
  `repo_config.yaml`, `coding_ethos.yml`, and `repo_ethos.yml`.

This avoids duplicating git rules across Python hooks, Go wrappers, shell
snippets, and pre-commit checks.

### Mapping Raw `git` Calls to the Wrapper

There are several ways hooks can route ordinary `git` calls to the wrapper.

#### PATH Shim

`coding-ethos agent-hooks sync` can install a repo-local shim:

```text
.git/coding-ethos-hooks/bin/git
```

The shim would exec:

```bash
coding-ethos git-wrapper "$@"
```

The generated agent hook configuration can then ensure the agent's Bash
environment prepends that bin directory to `PATH` for managed sessions.

This is the most transparent option when supported by the agent runtime.

#### Tool Input Rewrite

For simple commands, a `PreToolUse` hook can rewrite:

```bash
git status
```

to:

```bash
coding-ethos git-wrapper status
```

This should only be used for clearly parseable single git commands. Complex
shell pipelines, command substitutions, and chained commands should not be
rewritten silently.

#### Advisory Routing

When automatic rewrite is unsafe, the hook can advise:

```text
[coding-ethos:git-wrapper] Use `coding-ethos git-wrapper ...` for managed git
operations so repo git policy and post-action verification are applied.
```

This is appropriate for exploratory commands or complex shell expressions.

#### Hard Block for Bypasses

If a raw git command attempts a known bypass or destructive action, hooks
should block immediately rather than merely route to the wrapper.

Examples:

- `git commit --no-verify`
- `SKIP=... git commit`
- `git reset --hard`
- `git clean -fd`
- `git push --force origin main`

### Shared Policy Engine

The wrapper should not own independent policy definitions. It should consume
the same policy artifacts as the hook runner.

Proposed layering:

```text
config.yaml + repo_config.yaml + coding_ethos.yml + repo_ethos.yml
        |
        v
compiled coding-ethos policy bundle
        |
        +--> agent hook runner
        +--> git wrapper
        +--> pre-commit hook runner
        +--> generated prompt packs
```

The compiled bundle should include:

- protected branch names
- protected paths
- admin-only staged files
- allowed commit exceptions
- worktree topology rules
- bypass patterns
- push policy
- wrapper real-git path or discovery strategy
- ethos principle IDs behind each decision
- message templates for human-facing output

### Wrapper Decisions

The wrapper should return machine-readable decisions when requested.

Example:

```bash
coding-ethos git-wrapper --decision-json commit -m "message"
```

Example output on block:

```json
{
  "decision": "block",
  "policy_id": "git_safety.destructive_command",
  "principle_id": "no-rationalized-shortcuts",
  "message": "Blocked git reset --hard. Preserve work and resolve the current state explicitly.",
  "command": ["git", "reset", "--hard"]
}
```

Agent hooks can use this in `PreToolUse` without reimplementing the wrapper's
git logic.

### Post-Action Verification

The wrapper can make git post-action checks more reliable.

For `git commit`, it can:

- record `HEAD` before execing git
- run git
- compare `HEAD` afterward
- emit a verified result

However, a pure `syscall.Exec` wrapper cannot inspect state after git exits,
because the process is replaced. For commands that require post-action
verification, the wrapper should use `exec.Command` and relay stdin/stdout/stderr
instead of `syscall.Exec`.

Suggested split:

- Use `syscall.Exec` for simple pass-through read-only commands.
- Use managed subprocess execution for commit, push, merge, rebase, checkout,
  switch, reset, clean, and worktree commands.
- Emit post-action JSON when running under an agent hook.

This lets hooks avoid false success messages and lets the wrapper become the
authoritative source for git outcome evidence.

### Wrapper Interaction Modes

The wrapper should support more than denial.

#### `block`

Hard-deny destructive commands, bypasses, protected branch pushes, and
configured repo violations.

#### `ask`

Request explicit confirmation for legitimate but risky git actions.

Examples:

- `git rebase`
- `git worktree remove`
- `git branch -D`
- pushing from a repo with unusual protected-branch rules

In a non-interactive shell, `ask` should fail closed with a clear message. Under
an agent hook, `ask` can map to a permission request.

#### `advise`

Provide safer alternatives for suspicious but allowed commands.

Examples:

- For `git checkout main`, advise `git fetch origin main` plus inspection via
  `git show` or `git diff` instead of branch switching.
- For broad `git add .`, advise reviewing `git status --short` first when many
  files are dirty.
- For committing generated artifacts, advise confirming the source config
  changed.

#### `annotate`

After git output, add concise workflow context.

Examples:

- After a failed commit, explain whether hooks failed or no commit landed.
- After push rejection, suggest fetch/rebase/merge policy depending on config.
- After a successful commit, report the new commit SHA only after verifying
  `HEAD` changed.

#### `record`

Write hook-owned state:

- last git command
- last git decision
- pre/post `HEAD`
- staged admin-file detection
- bypass attempts
- push attempts

This state can feed `Stop`, `SessionEnd`, and future advice.

### Agent Hook Flow

Recommended flow for `Bash` tool calls:

1. `PreToolUse` detects whether the command invokes git.
2. If the command is a simple git invocation, ask the wrapper for a dry-run
   decision.
3. If the decision is `block`, block the tool call with the wrapper message.
4. If the decision is `ask`, request permission or clarification.
5. If the decision is `advise`, attach concise context.
6. If configured, rewrite the command to use the wrapper or rely on the PATH
   shim.
7. `PostToolUse` asks the wrapper or git state verifier to annotate the result.
8. Hook state records the verified outcome.

Ordering matters because git policy has multiple enforcement points.

The intended managed-flow order is:

1. Hook receives the raw tool call.
2. Hook classifies the command and blocks obvious hard violations immediately.
3. Hook rewrites simple git invocations to `coding-ethos-git` or relies on a
   repo-local PATH shim that maps `git` to the wrapper.
4. `coding-ethos-git` loads the same policy bundle and re-evaluates git policy.
5. Allowed commands pass through to the real git executable.
6. Post-action hooks verify state and record outcomes.

This means the hook and wrapper intentionally overlap. The hook is the
per-action routing and user-interaction layer. The wrapper is the executable
boundary that still protects the repo if a git command reaches the shell.

Hooks should not silently rewrite complex shell commands. For complex commands,
the hook should either block a known violation or advise the agent to use
`coding-ethos-git` explicitly. Only simple single git invocations should be
rewritten automatically.

### CLI Shape

The wrapper should eventually move behind the main `coding-ethos` CLI:

```bash
coding-ethos git -- status
coding-ethos git -- commit -m "message"
coding-ethos git-policy check --json -- commit -m "message"
coding-ethos git-policy install-shim --repo .
```

Compatibility entrypoints can still exist:

```bash
coding-ethos-git
git-wrapper
```

The important point is that all entrypoints load the same policy bundle.

### Rollout for Git Wrapper Support

1. Move wrapper policy from hardcoded constants into config-backed structures.
2. Replace command-string regex checks with argv-based git command
   classification where practical.
3. Add dry-run decision output for hooks.
4. Add post-action managed execution for commit and push verification.
5. Add repo-local PATH shim installation through `agent-hooks sync`.
6. Add hook rewrite support for simple raw `git` calls.
7. Add tests for wrapper decisions and hook-wrapper integration.
8. Enable wrapper routing in advisory mode first.
9. Promote bypass and destructive-command handling to blocking mode.

### Guardrails for Wrapper Integration

- Do not hide raw git output.
- Do not silently rewrite complex shell commands.
- Do not hardcode repo names or paths in the wrapper.
- Do not make personal global git aliases the only installation path.
- Do not rely on the wrapper alone for post-commit success messaging; verify
  git state.
- Do not duplicate git policy in both hook code and wrapper code.
- Keep wrapper decisions traceable to policy IDs and ethos principle IDs.

## Enforceable Policy Areas

### Bash

`coding-ethos` can block or warn on:

- Destructive filesystem commands.
- Unsafe shell install patterns such as `curl | sh`.
- Hook bypasses such as `SKIP=`, `--no-verify`, and `git commit -n`.
- Dangerous git commands on protected branches.
- Force pushes to protected branches.
- Backgrounded or timeout-wrapped commit and push commands.
- Inline environment variables when the repo ethos forbids them.
- Commands targeting protected paths.
- Commands that mutate the main checkout from a linked worktree.

### Write, Edit, and MultiEdit

`coding-ethos` can block or warn on:

- Writes outside allowed repo roots.
- Writes to protected generated or administrative files.
- Writes to external data roots such as `/opt/foundation`.
- Edits on protected branches.
- Known dangerous code patterns.
- Secrets or credentials in new file content.
- Repo-specific forbidden path patterns.

### Read

`coding-ethos` can provide advisory context for:

- Repeated reads of the same file.
- Reads of large files where a narrower query would be better.
- Reads of known sensitive paths.
- Project anatomy or ownership hints.

Read hooks should usually be advisory. Blocking reads can make agents blind and
should be reserved for explicit secrets or sensitive external data.

### PostToolUse

`coding-ethos` can verify outcomes after completed actions:

- Confirm that `git commit` actually moved `HEAD`.
- Summarize failed pre-commit output without claiming success.
- Detect formatter or hook-generated file changes.
- Surface test failure context.
- Record structured hook audit logs.

### Session and Compaction

`coding-ethos` can manage:

- Session startup context.
- Continuation prompts before compaction.
- Repository issue or phase reminders.
- Dirty-worktree reminders at session end.

These hooks should usually be advisory.

### Subagents and Tasks

`coding-ethos` can enforce:

- Scoped subagent responsibilities.
- Required context handoff at subagent start.
- Result summaries at subagent stop.
- No commit or push from unapproved agent roles.
- Task completion evidence requirements.

## Proposed Architecture

Add an agent hook subsystem to `coding-ethos`:

```text
coding-ethos/
  agent-hooks/
    adapters/
      claude/
    policies/
    schemas/
    fixtures/
    docs/
```

Expose CLI commands:

```bash
coding-ethos agent-hooks validate
coding-ethos agent-hooks sync --repo .
coding-ethos agent-hooks doctor
coding-ethos agent-hooks run --event PreToolUse
```

The hook runner should use a provider-neutral internal event model. Claude Code
is the first adapter, but the policy engine should not hard-code Claude-specific
payloads outside the adapter layer.

## Repo Configuration

Consumer repos should configure agent hooks in `repo_config.yaml`.

Example:

```yaml
agent_hooks:
  enabled: true
  provider: claude
  mode: blocking

  policies:
    dangerous_shell: block
    hook_bypass: block
    protected_paths: block
    git_integrity: block
    commit_post_verify: block
    worktree_boundary: block
    staged_admin_files: block
    code_smell_edit_guard: warn
    session_context: advisory
    openwolf_memory: advisory

  protected_paths:
    - /opt/foundation
    - /usr/bin/got

  git:
    protected_branches:
      - main
      - master
    forbid_no_verify: true
    verify_commit_head_advanced: true

  worktrees:
    main_checkout: /home/paudley/Active/lbox
    allow_main_repo_paths:
      - .claude/hooks/
      - .claude/settings
```

The global `coding_ethos.yml` should define default policies. A repo's
`repo_config.yaml` should refine path lists, branch lists, advisory/blocking
mode, and repo-specific exceptions.

## Initial Policy Set

The first implementation should keep the blocking surface small and
high-confidence.

### `dangerous_shell`

Blocks dangerous shell patterns:

- `rm -rf` on broad or protected paths.
- `curl | sh` and `wget | sh`.
- `chmod 777`.
- Known unsafe command substitutions when used in mutating commands.

### `hook_bypass`

Blocks bypassing repository quality gates:

- `SKIP=`
- `--no-verify`
- `git commit -n`
- `GIT_VERIFY=false`

### `git_safety`

Blocks dangerous git behavior:

- `git reset --hard`
- `git clean -fd`
- `git checkout --theirs`
- `git checkout --ours`
- `git restore --source`
- Force push to protected branches.
- Commit or push commands launched in the background.
- Commit or push commands wrapped in `timeout`.

### `protected_paths`

Blocks writes, edits, and mutating shell commands targeting protected paths.

### `worktree_boundary`

Blocks accidental edits to the main checkout from a linked worktree and can
also protect linked worktrees from the main checkout.

### `staged_admin_files`

Blocks commits containing sensitive repo administration files unless explicitly
allowed by repo config.

Examples:

- `.pre-commit-config.yaml`
- `.pre-commit-hooks.yaml`
- `pre-commit/`
- `pyproject.toml`
- `importlinter`
- `.claude/`

### `commit_post_verify`

Records `HEAD` before `git commit` and verifies `HEAD` afterward.

This policy must be the source of truth for commit success messaging. It should
not claim success from tool output alone.

## Installation and Sync

`coding-ethos agent-hooks sync --repo .` should generate or update the
consumer repo's Claude hook configuration.

The generated Claude settings should call stable `coding-ethos` entrypoints
rather than embedding long shell snippets. For example:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash|Write|Edit|MultiEdit",
        "hooks": [
          {
            "type": "command",
            "command": "coding-ethos agent-hooks run --event PreToolUse"
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "coding-ethos agent-hooks run --event PostToolUse"
          }
        ]
      }
    ]
  }
}
```

Personal `.claude/settings.local.json` files can still exist, but repo-enforced
policy should be generated from the repo configuration.

## Testing Requirements

Agent hooks should have fixture-driven tests. Each policy needs input payloads
for allowed and blocked behavior.

Required test categories:

- Claude `PreToolUse` payload parsing.
- Claude `PostToolUse` payload parsing.
- Bash command segmentation.
- Git command policy decisions.
- Write/Edit path policy decisions.
- Commit pre/post `HEAD` state handling.
- Config validation.
- Generated settings output.

Every blocking policy should include the exact message Claude will receive.
Policy failures need actionable output, not vague refusal text.

## Rollout Plan

1. Add `agent_hooks` config schema and validation.
2. Add Claude event adapter and provider-neutral event model.
3. Add `agent-hooks run`, `validate`, `doctor`, and `sync` commands.
4. Port high-confidence global guards:
   - `dangerous_shell`
   - `hook_bypass`
   - `git_safety`
   - `protected_paths`
5. Port lbox repo guards:
   - `worktree_boundary`
   - `staged_admin_files`
   - `commit_post_verify`
6. Generate Claude settings from `repo_config.yaml`.
7. Run lbox in advisory mode for non-critical policies.
8. Promote only stable safety policies to blocking mode.
9. Add documentation for adopting agent hooks in other repos.
10. Migrate OpenWolf-style memory/context hooks as optional advisory policies.

## Design Rules

- Blocking hooks must be fast, deterministic, and dependency-light.
- Hooks should avoid network access.
- Hooks should not mutate repo files during `PreToolUse`; state writes should
  be limited to hook-owned metadata.
- Blocking should be reserved for clear safety or policy violations.
- Advisory hooks should never obscure real tool output.
- Commit success must be verified from git state, not inferred from prose.
- Policies must have stable IDs so repos can configure them precisely.
- Every policy should support `block`, `warn`, and `off` where practical.
- Hook output should include the policy ID, decision, and fix guidance.
- Generated hook configuration should be reproducible and testable.

## First Implementation Scope

The first pull request should include:

- `agent_hooks` config schema.
- Claude `PreToolUse` runner.
- Claude `PostToolUse` runner.
- `dangerous_shell` policy.
- `hook_bypass` policy.
- `git_safety` policy.
- `protected_paths` policy.
- `commit_post_verify` policy.
- `agent-hooks validate`.
- `agent-hooks sync`.
- Documentation with an lbox example configuration.

OpenWolf memory integration, subagent workflow enforcement, and prompt/session
context should follow in later changes once the core blocking safety layer is
stable.
