<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Examples

These examples show how `coding-ethos` should be used by humans and AI agents.
They are intentionally small so the important path is visible: agents should ask
the MCP server for policy and lint context instead of running raw tooling and
guessing at remediation.

## Available Examples

- [MCP lint advice](mcp-lint-advice/README.md): run the local MCP server,
  inspect available tools, and use `lint_check`/`lint_advice` as the preferred
  agent path for static-analysis work.
- [CEL policy](cel-policy/README.md): keep custom policy expressions attached
  to the ETHOS principle they enforce.
- [GitHub SARIF CI](github-sarif-ci/README.md): generated code-scanning gates
  and agent remediation flow.
- [Agent command block](agent-command-block/README.md): blocked unsafe command
  handling and MCP follow-up.
- [Runtime sandbox](runtime-sandbox/README.md): tool capabilities, sandbox
  evidence, and degraded-mode reporting.

## Planned Examples

- Editor SARIF integration.
- Red-team regression authoring.
- Consumer repo cutover with generated agent settings.
