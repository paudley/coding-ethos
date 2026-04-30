# Skills Advice And Steering Plan

This note captures current agent-skill behavior and proposes how coding-ethos
should use skills as an advice and steering layer. It is current as of
2026-04-30.

## Goal

Use skills to make ETHOS advice more actionable before and after agent actions:

- before tool calls: short reminders and links to the right workflow
- after tool calls: concrete repair guidance tied to lint, policy, and hook
  evidence
- during repeated workflows: reusable, provider-native instructions that agents
  can load on demand

Skills must not replace deterministic hooks. Hooks enforce policy. Skills teach
the agent how to satisfy policy without bloating always-on context.

## Current State Of The Ecosystem

### Agent Skills Standard

The portable baseline is the Agent Skills format: a directory with a required
`SKILL.md`, YAML frontmatter, and optional `scripts/`, `references/`, and
`assets/` directories. The standard relies on progressive disclosure: agents
load skill metadata first, load the full `SKILL.md` only when the task matches,
and load supporting files only when needed.

Important rules for our use:

- `name` and `description` are the trigger surface, so descriptions must say
  both what the skill does and when to use it.
- `SKILL.md` should stay small; detailed policy examples belong in referenced
  files.
- Scripts are appropriate for deterministic helpers, but they need clear errors
  and must not become bypass paths around hooks.
- Skills are version-controlled policy artifacts, not local personal notes.

Sources: [Agent Skills overview](https://agentskills.io/),
[Agent Skills specification](https://agentskills.io/specification).

### Claude Code

Claude Code has first-class skills. Project skills live under
`.claude/skills/<skill-name>/SKILL.md`; personal skills live under
`~/.claude/skills/<skill-name>/SKILL.md`; plugin skills are also supported.
The skill directory name becomes a slash-command, and the frontmatter
description helps Claude decide when to load the skill automatically.

Claude Code also has mature adjacent steering surfaces:

- `CLAUDE.md` and related memory files for always-on project context
- custom slash commands under `.claude/commands/`
- subagents under `.claude/agents/`
- provider-native hooks for tool gating, additional context, rewrites, and
  lifecycle events

Best fit for coding-ethos:

- Use `CLAUDE.md` as the small repo map and policy entrypoint.
- Use `.claude/skills/` for specific ETHOS remediation workflows.
- Use hooks to inject short skill hints when the action or failure matches a
  policy.
- Use subagents for scoped independent review or remediation work, not for
  basic policy advice.

Sources: [Claude Code skills](https://code.claude.com/docs/en/skills),
[Claude Code hooks](https://code.claude.com/docs/en/hooks),
[Claude Code memory](https://code.claude.com/docs/en/memory),
[Claude Code subagents](https://docs.anthropic.com/en/docs/claude-code/sub-agents).

### Codex

Codex supports the `SKILL.md` pattern through Codex skills. The current local
runtime exposes skills as folders with a required `SKILL.md`, required `name`
and `description` frontmatter, and optional `scripts/`, `references/`,
`assets/`, and `agents/openai.yaml` UI metadata. The current Codex skill
guidance emphasizes concise instructions, progressive disclosure, and putting
deterministic repeated work in scripts.

OpenAI's public material positions skills as portable workflow packages that
can be exported to tools supporting the Agent Skills format, including Codex.
The `openai/skills` catalog is the concrete public distribution point; system
skills are preinstalled in recent Codex, while curated and experimental skills
can be installed with the `$skill-installer`.

Best fit for coding-ethos:

- Keep `AGENTS.md` as the repo orientation layer.
- Publish ETHOS skills in the portable `SKILL.md` shape so Codex can install or
  consume them without provider-specific rewrites.
- Add `agents/openai.yaml` only when we want Codex UI polish; it should be
  generated from the skill body, not manually maintained separately.
- Use Codex hooks for enforcement and small additional-context hints; use
  skills for the longer workflow that explains how to comply.

Sources: [OpenAI skills resource](https://academy.openai.com/public/resources/skills),
[OpenAI skills catalog](https://github.com/openai/skills),
[OpenAI Codex docs](https://platform.openai.com/docs/codex),
local Codex system skills in `$CODEX_HOME/skills/.system/`.

### Gemini CLI

Gemini CLI has first-class extension packaging. Extensions can bundle context,
custom commands, MCP servers, hooks, policies, subagents, and agent skills.
The extension reference documents a `skills/` directory where
`skills/<skill-name>/SKILL.md` exposes a skill. Gemini CLI also uses
`GEMINI.md` for hierarchical context, `.gemini/commands/` for custom commands,
and `.gemini/settings.json` hooks for lifecycle and tool interception.

Best fit for coding-ethos:

- Generate a Gemini extension for ETHOS skills rather than only writing loose
  repo files.
- Keep `GEMINI.md` short and hierarchical, matching our current generated doc
  strategy.
- Use Gemini hooks for deterministic deny/advice output.
- Use extension policy only for Gemini-native safety UX; coding-ethos remains
  the cross-agent enforcement source of truth.

Sources: [Gemini CLI overview](https://docs.cloud.google.com/gemini/docs/codeassist/gemini-cli),
[Gemini CLI extension reference](https://github.com/google-gemini/gemini-cli/blob/main/docs/extensions/reference.md),
[Gemini CLI extension guide](https://github.com/google-gemini/gemini-cli/blob/main/docs/extensions/writing-extensions.md),
[Gemini CLI hooks guide](https://github.com/google-gemini/gemini-cli/blob/main/docs/hooks/writing-hooks.md),
[Gemini CLI custom commands](https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/custom-commands.md).

## Best Practices To Adopt

1. Treat skills as progressive disclosure.
   Always-on files should contain navigation and trigger language. Detailed
   remediation belongs in skill bodies or one-level reference files.

2. Keep hooks deterministic.
   Hook-time policy must remain compiled, local, and auditable. Skills may
   explain or guide; they must not be the only mechanism that blocks unsafe
   behavior.

3. Make skill triggers explicit.
   Descriptions should include the actual task and failure words agents see:
   `conditional import`, `TYPE_CHECKING`, `cyclic import`, `ruff local import`,
   `mypy import cycle`, `git wrapper`, `lint suppression`, and similar terms.

4. Map policy evidence to skills.
   Each ETHOS-backed lint or hook finding should be able to name the principle,
   the practical repair advice, and the skill that contains the longer playbook.

5. Prefer generated provider adapters.
   Author one canonical skill source, then render provider-specific placements:
   Claude project skills, Codex-compatible skills, and Gemini extension skills.

6. Keep scripts narrow.
   Skill scripts should be deterministic helpers for inspection, summarization,
   or config rendering. They must not invoke alternate linters, raw git, or
   host tools outside the managed coding-ethos path.

7. Log skill nudges.
   When a hook suggests or injects a skill hint, write that decision to
   `.coding-ethos/hook-runs/` so later analysis can tell whether the advice
   reduced repeated failures.

8. Avoid context litter.
   Hook messages should contain one compact skill hint, not a pasted skill body.
   The agent can load the skill when it needs the full workflow.

## Proposed coding-ethos Architecture

Add a canonical skill source layer:

```text
coding_ethos.yml / repo_ethos.yml
        |
        v
skill source records
        |
        +--> portable Agent Skills tree
        +--> .claude/skills/<name>/SKILL.md
        +--> .codex/skills/<name>/SKILL.md or installable skill bundle
        +--> .gemini/extensions/coding-ethos/skills/<name>/SKILL.md
```

The source record should be ETHOS-native, not provider-native:

```yaml
skills:
  conditional-imports:
    principle_ids:
      - no-conditional-imports
      - protocol-first-design
    trigger_terms:
      - conditional import
      - TYPE_CHECKING
      - ruff local import
      - cyclic import
    short_hint: "Conditional imports are banned. Use protocols or module boundaries instead."
    body: ...
    references:
      - python-protocols.md
```

Hook and lint output should then carry:

- `principle_id`
- `policy_id`
- `skill_id`
- short hint
- one provider-safe instruction for loading or invoking the skill

Example concise hook advice:

```toon
advice[1]{principle_id,skill_id,message,next}:
  no-conditional-imports,conditional-imports,Conditional imports are banned; use protocols or a cleaner module boundary.,Load the conditional-imports skill for the remediation playbook.
```

## Initial Skill Set

Start with skills that map directly to existing ETHOS policies and repeated
agent failure modes:

| Skill | Purpose | Primary Triggers |
| --- | --- | --- |
| `conditional-imports` | Replace conditional imports with required imports, protocols, or refactored boundaries. | Ruff local-import diagnostics, `TYPE_CHECKING`, import-cycle errors |
| `lint-remediation` | Fix lint structurally without weakening config or adding suppressions. | Ruff, mypy, pyright, pylint, captured lint failures |
| `safe-git-workflow` | Use the coding-ethos git wrapper and admin-approved flow correctly. | raw git, absolute git, git subprocess bypass blocks |
| `hook-output-quality` | Diagnose bad hook/linter output and add golden tests. | empty findings, raw JSON, absolute paths, noisy output |
| `long-running-work` | Preflight docs, define success, spot-check early, keep todos current. | long runs, sweeps, background jobs |
| `review-feedback` | Pull, triage, address, and verify PR feedback. | PR feedback, review comments, CI failures |
| `managed-toolchain` | Use coding-ethos managed linters/configs instead of host or consumer repo tools. | config drift, missing binary, host linter capture |

## Integration Plan

1. Add a canonical skill schema to `coding_ethos.yml` or a generated companion
   file owned by the ethos renderer.
2. Render provider-specific skill directories from that source.
3. Extend policy evidence maps so findings can name `skill_id`.
4. Add hook advice rendering for `skill_id` with provider-specific invocation
   wording. Initial Go runtime support now renders concise skill advice for
   normalized lint output, captured linters, type-check output, and post-edit
   context.
5. Add trace fields for emitted skill hints. Normalized lint traces now persist
   `skill_hints` alongside findings.
6. Add `agent-hooks verify` probes that confirm Claude, Codex, and Gemini
   generated skill surfaces exist.
7. Add golden-output tests for skill-hint rendering in human, JSON, and TOON
   output.
8. Analyze `.coding-ethos/lint-runs/` and `.coding-ethos/hook-runs/` to choose
   the next skill mappings by frequency and severity.

## Open Design Questions

- Should portable skills be checked in under a neutral path such as
  `.agents/skills/` and then copied into provider-native locations, or should
  provider directories be the only checked-in artifacts?
- Should coding-ethos install skills into user-level locations, or only generate
  repo-local skills to avoid surprising global behavior?
- Should hooks only suggest skills, or should some failures inject a short
  skill excerpt as additional context?
- How do we keep generated Gemini extensions from becoming a second policy
  source of truth?
- Which skill hints are useful enough to emit during successful tool calls
  without creating context noise?

## Recommended Direction

Use ETHOS as the source of truth, generate portable skills from ETHOS policy
records, and have hooks point to those skills when deterministic evidence says
the agent needs a specific workflow.

The first implementation should be deliberately narrow:

1. Implement `conditional-imports`.
2. Map Ruff local-import diagnostics, direct conditional-import checker
   findings, and import cycle diagnostics from mypy/pyright/pylint to that skill
   where possible.
3. Render the skill for Claude, Codex, and Gemini.
4. Add one hook-output golden test proving the finding includes policy,
   rationale, and skill guidance without dumping the full skill body.

That gives us a concrete end-to-end slice before expanding to broader lint and
workflow skills.
