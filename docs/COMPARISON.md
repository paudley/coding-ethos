<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Comparison

`coding-ethos` overlaps with several policy, static-analysis, and agent-guidance
tools, but it is not a drop-in replacement for any single one of them. Its role
is to connect guidance and enforcement across human contributors, AI coding
agents, Git hooks, CI, SARIF, MCP, and repo-local policy.

## Short Version

| Tool or pattern | Primary strength | How `coding-ethos` differs |
| --- | --- | --- |
| `pre-commit` | Runs configured hooks before commit | Adds generated ETHOS docs, agent hooks, MCP, CEL policy, managed tool capture, SARIF, and runtime capability evidence. |
| CodeQL | Deep semantic code analysis | Focuses on repo workflow policy, agent behavior, generated configs, and custom guardrails around development actions. |
| Semgrep | Pattern and semantic static analysis | Uses existing linters plus CEL and Go evaluators to enforce repository workflow and agent-safety policies. |
| OPA/Rego | General-purpose policy engine | Keeps repo policy close to engineering principles, agent docs, hooks, MCP tools, and SARIF outputs. |
| Plain agent instructions | Human-readable guidance for agents | Compiles guidance into checked hook behavior, MCP responses, skills, axioms, and CI evidence. |
| GitHub branch protection | Server-side merge gate | Catches bad actions earlier in local hooks and agent tool-use hooks before the PR exists. |

## Compared With Pre-Commit

`pre-commit` is excellent at running hook commands. `coding-ethos` uses Git
hooks too, but the core problem is broader: agents and humans need consistent
guidance, policy decisions, lint remediation, and evidence across local tools,
MCP, SARIF, and CI.

`coding-ethos` adds:

- generated Claude, Codex, Gemini, and portable agent context
- generated skill playbooks and runtime axioms
- compiled Go policy preflight
- CEL policy expressions tied to ETHOS principles
- managed lint capture and normalized diagnostics
- MCP tools for agents to query policy and lint advice
- SARIF output for CI, editor, and trend workflows
- runtime capability metadata for network, Git, sandbox, timeout, memory, CPU,
  seccomp, read paths, and write paths

## Compared With CodeQL

CodeQL is a deep static-analysis engine. `coding-ethos` is a repo-policy and
agent-safety framework. They work well together: CodeQL can find semantic code
risks, while `coding-ethos` can enforce development workflow constraints such
as protected hook paths, agent memory writes, shell command safety, file growth,
tool capability contracts, and generated config drift.

## Compared With Semgrep

Semgrep is good for code-pattern rules. `coding-ethos` can consume findings
from managed static-analysis tools, but it also evaluates proposed commands,
file edits, Git actions, and tool runtime capabilities. CEL policies are
intended for narrow repo-specific predicates where the input is already typed
and policy-owned.

## Compared With OPA

OPA is a general-purpose policy engine. `coding-ethos` uses CEL for custom
policy because the immediate target is typed, local, fast hook enforcement with
small repo-authored expressions. The larger design goal is not just policy
evaluation; it is keeping policy, ETHOS principles, generated docs, remediation
skills, MCP guidance, SARIF, and hooks aligned.

## Compared With Agent Instructions Alone

Plain Markdown instructions are necessary but not sufficient. Agents can miss
instructions, overfit to local context, or treat mismatched guidance and hooks
as tool defects. `coding-ethos` still generates agent instructions, but it also
enforces the same contract in tool-use hooks, Git hooks, CI, SARIF, MCP, and
compiled policy bundles.

## Where It Fits

Use `coding-ethos` when you want one source contract for:

- AI-agent coding guardrails
- policy-as-code for repository workflows
- Git hook and agent-hook enforcement
- managed static analysis with ETHOS-grounded remediation
- local MCP policy and lint services
- SARIF/code-scanning output tied to repo policy
- defense-in-depth around agentic development
