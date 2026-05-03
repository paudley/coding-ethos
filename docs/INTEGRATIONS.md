<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Integrations

`coding-ethos` is designed to meet developers and agents where they already
work: local Git hooks, AI coding assistants, MCP clients, GitHub Actions, GitLab
CI, SARIF consumers, and managed static-analysis tools.

## Codex

Generated Codex surfaces include:

- `AGENTS.md` repo instructions
- `.codex/skills/*/SKILL.md` remediation playbooks
- generated MCP server configuration when installed into a consumer repo
- tool-use hooks that evaluate the same compiled policy bundle as Git hooks

Recommended workflow:

1. Let generated hooks evaluate proposed shell commands and file edits.
2. Call the MCP server for `lint_check`, `lint_advice`, `policy_explain`, and
   `skill_recommend` before running raw tools.
3. Report changed files, checks run, and unresolved policy risks before
   requesting review.

## Claude Code

Generated Claude surfaces include:

- `CLAUDE.md`
- `.claude/skills/*/SKILL.md`
- `.claude/ethos/MEMORY.md`
- generated MCP and hook settings for the compiled policy runtime

Claude should use the same MCP-first lint workflow described in
[examples/mcp-lint-advice](../examples/mcp-lint-advice/README.md). Agent memory
and plan files are writable, while enforcement points remain protected.

## Gemini CLI

Generated Gemini surfaces include:

- `GEMINI.md`
- `.gemini/extensions/coding-ethos/gemini-extension.json`
- `.gemini/extensions/coding-ethos/skills/*/SKILL.md`
- `.code-ethos/gemini/prompt-pack.json`

Gemini review and hook prompts are generated from source templates and grounded
in `coding_ethos.yml`, `repo_ethos.yml`, and merged enforcement config.

## MCP Clients

The MCP server runs over stdio:

```bash
bin/coding-ethos-run mcp
```

High-value tools:

- `policy_check_command`
- `policy_check_edit`
- `lint_check`
- `lint_advice`
- `sarif_remediation_advice`
- `sarif_risk_summary`
- `sarif_trend_analysis`
- `sarif_policy_feedback`
- `tool_capabilities`
- `policy_explain`
- `skill_lookup`
- `skill_recommend`

Agents should prefer MCP calls for policy and lint context because MCP exposes
the same compiled policy, generated skills, managed tool metadata, and SARIF
evidence used by hooks and CI.

## GitHub Actions

Generated GitHub Actions workflows can run the SARIF gate and upload results to
code scanning. See [CI/CD SARIF](CI_CD_SARIF.md).

Use GitHub Actions for:

- branch and PR validation
- SARIF upload
- package build validation
- actionlint workflow checks
- artifact retention for policy and lint evidence

## GitLab CI

Generated GitLab CI files provide the same SARIF-oriented policy gate for
GitLab consumers. The generated config is controlled by
`generated_config.ci.gitlab.enabled` in the merged enforcement config.

## SARIF Consumers

SARIF output is useful beyond GitHub code scanning:

- editor diagnostics
- remediation advice
- risk summaries
- trend analysis
- policy feedback
- release quality gates

See [SARIF uses](SARIF_USES.md) and
[SARIF editor integration](SARIF_EDITOR_INTEGRATION.md).

## Static Analysis Tools

Managed tool capture currently focuses on routing lint and type-checker output
through generated config, normalized diagnostics, policy maps, traces, SARIF,
and MCP advice. Tools should declare capabilities such as network, Git,
sandbox, timeout, memory, CPU, seccomp, read paths, and write paths so CEL and
MCP can reason about runtime behavior.
