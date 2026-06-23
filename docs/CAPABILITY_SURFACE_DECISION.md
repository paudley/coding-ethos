<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Capability Surface Decision Guide

`coding-ethos` grows through several public surfaces: CEL policy, generated
skills, MCP tools, local CLI commands, SARIF/code-intel/output evidence, hook
runtime behavior, and provider adapters. New capabilities must choose one
primary surface and document why that surface owns the behavior before adding
another entry point.

## Decision Tree

1. **Can the behavior make a deterministic allow, deny, or diagnostic decision
   from normalized facts?**
   Put it in CEL, policy, or hook enforcement. Extend Go fact collection first
   when the needed fact does not exist, then express the configurable decision
   in CEL and emit SARIF through the existing policy path.

2. **Is the behavior rich workflow guidance for an agent or maintainer?**
   Put it in a generated skill. Skills should explain what to inspect, what to
   avoid, and what validation proves the work. They must point back to policy,
   docs, MCP tools, or runnable commands when those own the facts.

3. **Does an agent need repeated, structured, interactive access to current
   repo evidence?**
   Put it in MCP. MCP tools should expose compiled policy, retained traces,
   SARIF, code-intel records, or generated skill metadata. They must not create
   a second policy interpretation path.

4. **Is the action a one-shot deterministic local operation for a human,
   script, or hook?**
   Put it in a CLI command. CLI commands should use validated repo-local
   configuration and report machine-readable output when other surfaces will
   consume the result.

5. **Is the main value durable evidence, replay, audit, or trend analysis?**
   Put it in SARIF, code-intel storage, or an outputsurface contract. Evidence
   should be repository-relative, stable across line movement when possible,
   and linked to policy IDs, skill IDs, trace IDs, or AST identity.

6. **Is the behavior provider-specific?**
   Put the provider-specific part in the provider registry or adapter contract.
   Keep provider behavior declarative: name, executable or managed command,
   fixed arguments, validation, timeout, network need, limits, and output
   format.

7. **Does more than one answer seem correct?**
   Choose the surface that owns the first durable fact. Add bridging only after
   the owning surface exists. For example, a new policy should land in
   CEL/policy/hook first, then MCP can explain it and SARIF can retain it.

## Surface Responsibilities

| Surface | Owns | Must Reference |
| --- | --- | --- |
| CEL/policy/hook | Deterministic enforcement and diagnostics | ETHOS principle, policy ID, normalized facts, SARIF output |
| Generated skills | Workflow guidance and remediation playbooks | Principle IDs, policy IDs, validation commands, related MCP tools |
| MCP | Structured interactive access for agents | Compiled bundle, retained traces, SARIF, code-intel, skill metadata |
| CLI | One-shot local actions and operator workflows | Documented command, validated config, output contract |
| SARIF/code-intel/outputsurface | Durable evidence, replay, trends, and audit | Stable IDs, repository-relative paths, trace or AST provenance |
| Provider registry | Provider-specific execution and limits | Adapter contract, capability declaration, validation requirements |

## Public Surface Grounding

Every PR that adds a public surface must identify:

- chosen primary surface;
- why that surface is the owner;
- policy, skill, SARIF, code-intel, outputsurface, provider, or docs references
  that ground the behavior;
- validation that would fail if the public surface exists without its grounding.

Minimum grounding by category:

- New MCP tools must appear in `docs/MCP_SERVER.md` and include
  `coding_ethos` tool metadata.
- New `code-intel` CLI commands must appear in `docs/CODE_INTEL.md`.
- Code-intel measurement features must document their storage table, schema,
  and windowing contract in `docs/CODE_INTEL.md`.
- New policy IDs must live with their ETHOS principle or bundle configuration
  metadata and emit stable SARIF rule IDs when they diagnose user work.
- New provider-specific behavior must be declared in the provider adapter or
  registry instead of branching through call sites.
- New output contracts must identify whether SARIF, code-intel storage, or
  outputsurface owns the retained evidence.

## Current Lightweight Checks

The test suite includes public-surface contract checks for MCP tools and
`code-intel` CLI commands. They compare registered tool/command names against
the corresponding documentation so new entries fail fast when they are added
without documented grounding.
