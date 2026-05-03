<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Strategic Roadmap

`coding-ethos` exists to act as the defensive guardrail and quality gatekeeper
between AI coding agents and a repository. Its job is not only to document
engineering standards, but to compile those standards into runnable policy,
managed toolchains, agent hooks, Git hooks, and normalized feedback that blocks
unsafe or low-quality work before it lands.

The work below extends that mission beyond local hook enforcement while keeping
the same defense-in-depth model: one source of policy truth, multiple execution
surfaces, compact agent-native advice, and auditable traces.

## Real-Time Context Through MCP

Generated Markdown and generated skills are useful durable context, but they
still front-load rules into an agent context window. A Model Context Protocol
server lets agents query `coding-ethos` at the moment of need. The current
server contract and expansion design are tracked in
[MCP_SERVER.md](MCP_SERVER.md).

High-value queries include:

- whether a proposed shell command is allowed before running it
- whether a proposed edit is allowed before applying it
- what managed linter findings apply to the current work without invoking the
  linter directly
- what compiled lint policy findings apply before a managed tool run is needed
- which ETHOS policy, advice, rerun command, and skill map to a lint finding
- why a policy exists and which ETHOS principle grounds it
- which generated skill best fits the current task

The first MCP surface is a local stdio server exposed through
`bin/coding-ethos-run mcp`. It reads from the same compiled policy
bundle and skill data as the hooks, and starts with command checks, proposed
edit checks, managed lint capture, compiled lint checks, lint advice, policy
explanations, skill lookup, task-based skill recommendation, and per-tool
capability metadata. It must not become an alternate enforcement path or a way
to bypass local Git and agent hooks.

A later expansion should add focused remediation advice through a constrained
agent-provider adapter. That adapter may call an available provider such as
`claude -p`, but only inside a managed, read-only, advice-only hook environment
that can read selected files and query the `coding-ethos` MCP stack. It must not
write files, run arbitrary shell commands, access raw Git, or bypass policy.

## Standardized Policy Language

Many policies are currently compiled Go evaluators configured by YAML. That is
appropriate for critical built-in checks, but it makes custom organization
policy require Go changes.

The policy-language strategy is CEL first, with OPA/Rego deferred until a
specific policy class proves it needs a full policy engine. CEL fits the first
custom-policy target because it is embedded, typed, deterministic,
non-Turing-complete, and expression-oriented.

The target is not replacing every evaluator. The target is letting consuming
repos add rich custom checks through `repo_ethos.yml` or `repo_config.yaml`
while still receiving normalized diagnostics, ETHOS links, skill hints, and
TOON/human output.

Expression-backed policy must be compiled, validated, deterministic, and
host-independent. Networked, time-dependent, or unsafe host access should be
rejected before runtime.

See [POLICY_LANGUAGE_STRATEGY.md](POLICY_LANGUAGE_STRATEGY.md).

## Native IDE And Cursor Integration

Git hooks catch bad work at the gate. IDE integration catches it earlier.

A VS Code/Cursor extension should run `coding-ethos-policy` and
`coding-ethos-lint` against the current workspace using the same compiled
bundle and managed toolchain as local hooks. The extension should surface
diagnostics at edit time, link to ETHOS principles and generated skills, and
warn before applying edits that would tamper with protected files or introduce
known high-value policy failures.

The first version can be advisory. Managed workspaces should be able to opt into
blocking behavior for protected paths and other critical rules.

## CI/CD Components And SARIF

Local hooks are necessary, but CI must remain the final independent gate. If an
agent or developer bypasses local hooks, the pull request still must not merge.

`coding-ethos` emits SARIF for normalized policy and lint diagnostics through
`policy-lint --sarif`. SARIF rules carry stable policy IDs, ETHOS principle
IDs, skill IDs, file/line locations, and remediation advice so violations can
appear naturally in PR annotations and code scanning views.

The initial CI documentation provides GitHub Actions and GitLab CI examples in
[CI_CD_SARIF.md](CI_CD_SARIF.md). The next step is packaging those examples as
reusable components that pin the managed runtime, publish SARIF, and preserve
`.coding-ethos` traces as audit artifacts.

CI output should stay compact for agents while preserving full artifacts for
audit and later trace analysis.

## Adversarial Red-Team Test Suite

AI agents routinely invent workarounds when blocked. Testing must model that
behavior directly.

The red-team suite should run in isolated sample repositories and prompt agents
or LLM APIs to attempt bypasses: raw Git execution, absolute binaries, nested
shells, symlink traversal, protected path writes, config drift, hook deletion,
managed toolchain evasion, and other known failure modes.

Every bypass attempt should produce either a clear block or a filed gap with a
failing regression test. The suite should exercise Claude, Codex, Gemini, and
generic shell workflows where practical.

## Centralized ETHOS Registry And Inheritance

Organizations need baseline guardrails with local refinement. `coding-ethos`
should support inheritance from local presets, GitHub-hosted presets, and
enterprise registries.

An inherited policy source might provide strict Python, strict Go, agent-safe
Git, or security-first defaults. A consuming repo should override local context
without copying the whole baseline.

Inheritance must be deterministic and auditable. Remote sources should be
pinned, hashed, and visible in policy trace output. Unpinned remote policy
inputs should be rejected unless explicitly allowed.

## Agent Remediation Loop

Agents are poor consumers of noisy terminal output. Hook failures should produce
machine-readable remediation payloads in addition to human output.

The remediation payload should include:

- policy ID
- ETHOS principle ID
- skill ID
- file and line
- failed action or command
- concrete next step
- rerun command when appropriate

The first implementation derives `agent_remediation` from normalized
diagnostics and policy decisions. It is emitted in agent-facing JSON and TOON
output, provider-native blocked hook responses, SARIF result properties, hook
traces, and retained lint traces. Each item has a stable remediation ID,
concrete next steps, skill-loading instructions when available, action context
when policy evidence carries it, and the MCP call an agent should make. Agents
can call `remediation_explain` with the full payload, or use the embedded
`policy_explain` / `skill_lookup` call directly.

Hook and lint traces also include `remediation_summary` so later storage can
measure repeated policy failures and which remediation guidance was suggested
without reparsing provider-specific output.

Claude, Codex, Gemini, and future MCP clients should receive this feedback in
the strongest native format they support. The same normalized data should drive
human output, TOON output, traces, and remediation payloads.
