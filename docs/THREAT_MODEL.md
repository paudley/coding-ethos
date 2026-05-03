<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Threat Model

`coding-ethos` protects repository workflows used by humans and AI coding
agents. The project assumes agents can make mistakes, overfit to local context,
or attempt unsafe tool use when prompts, hooks, and policy drift apart.

## Protected Assets

- Source code and generated policy artifacts.
- Git history, staged changes, and review evidence.
- Hook and agent-hook enforcement points.
- Generated tool configs and managed toolchain metadata.
- Secrets, credentials, local machine details, and private paths.
- Agent memory, plan, and settings directories.
- SARIF, lint, hook, and sandbox traces used for audit and remediation.

## Actors

- Human contributors.
- AI coding agents such as Codex, Claude Code, and Gemini CLI.
- MCP clients connected to the local server.
- Managed lint and analysis tools.
- CI runners and release jobs.
- Malicious or compromised local processes outside the intended workflow.

## Trust Boundaries

| Boundary | Enforced by |
| --- | --- |
| User prompt to agent tool call | agent hook policy checks |
| Shell command text to execution | shell parser, CEL, and compiled policy |
| File edit request to filesystem write | file policy, protected path rules, and CEL |
| Raw linter to managed lint result | toolcatalog, generated config checks, capture normalization |
| Repo config to enforcement bundle | config validation and policy compilation |
| Local findings to CI evidence | SARIF generation and upload workflows |
| Managed tool execution to host OS | sandbox mode, capabilities, timeout, cgroup, and seccomp metadata |

## Primary Risks

### Hook Or Git Bypass

Agents may try alternate Git paths, hook bypass flags, shell wrappers, or
subprocess indirection. The safe path is the coding-ethos Git wrapper plus
compiled hook policy. Bypass attempts are treated as enforcement failures, not
as normal workflow errors.

### Protected Enforcement Point Writes

Agent memory and plan files should be writable, but hook definitions, managed
settings, policy bundles, generated tool configs, and enforcement bootstrap
paths require stronger protection. Policies should prefer inverse allow models:
agent workspaces are writable except for explicit enforcement points.

### Malformed Shell Text

Shell text is parsed before policy evaluation. Malformed commands are denied
instead of treated as compatibility cases because partial parsing creates
policy gaps.

### Tool Capability Drift

Linters and other managed tools can gain network, Git, environment, or writable
path behavior over time. Runtime capability metadata makes those requirements
visible to CEL, MCP, traces, SARIF, and reviews.

### MCP Misuse

The MCP server is advisory context and managed execution, not a bypass around
Git hooks or agent hooks. MCP clients receive policy, lint, SARIF, and skill
context from the same compiled bundle used by enforcement paths.

### Sandbox Overclaiming

Sandboxing is defense in depth. Required sandbox mode fails closed when the
backend is unavailable; advisory mode records degraded evidence. Documentation
and SARIF evidence must distinguish requested controls from controls actually
enforced.

## Out Of Scope

- Preventing a fully compromised user account from modifying files outside the
  repository.
- Replacing OS access controls, endpoint detection, or CI runner isolation.
- Treating LD_PRELOAD-style interception as a security boundary.
- Guaranteeing semantic correctness of every third-party linter finding.

## Defense In Depth

- ETHOS principles define the intended behavior.
- Generated docs, skills, and axioms steer humans and agents.
- CEL and Go evaluators enforce policy in hooks and lint paths.
- MCP exposes the same policy context to agents.
- SARIF carries policy findings into CI, editors, and trend analysis.
- Managed tool capture normalizes diagnostics and records evidence.
- Runtime sandboxing limits managed tool behavior where available.
- Red-team tests cover bypass, parser, MCP, SARIF, sandbox, and protected-path
  behavior.
