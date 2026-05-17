<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Agent Remediation Payloads

`coding-ethos` emits a normalized `agent_remediation` payload anywhere an
agent is likely to consume failures: provider-native blocked hook output,
JSON/TOON lint output, SARIF result properties, hook traces, and lint traces.

The payload is designed for agents to call MCP instead of guessing from terminal
text. Each item has a stable `id`, ETHOS-grounded policy and principle IDs when
known, skill hints, the failed action or file location, concrete next steps, and
the next MCP call to make.

## Shape

```json
{
  "id": "rem-2faf26561f4e1312",
  "policy_id": "git.hook_bypass",
  "skill_id": "safe-git-workflow",
  "skill_use": "Load the safe-git-workflow skill before editing or retrying.",
  "failed_action": "Bash",
  "message": "Hook bypass is forbidden.",
  "next_steps": [
    "Run the configured gate and fix the underlying failure.",
    "Call MCP policy_explain with policy_id=git.hook_bypass before retrying.",
    "Call MCP skill_lookup with skill_id=safe-git-workflow for the repair playbook."
  ],
  "mcp": {
    "tool": "policy_explain",
    "arguments": {
      "policy_id": "git.hook_bypass"
    }
  }
}
```

## MCP Flow

Agents should use the embedded `mcp` object first. When the agent has a full
remediation item and wants richer context, call:

```json
{
  "name": "remediation_explain",
  "arguments": {
    "remediation": {
      "id": "rem-2faf26561f4e1312",
      "policy_id": "git.hook_bypass",
      "skill_id": "safe-git-workflow",
      "message": "Hook bypass is forbidden.",
      "failed_action": "Bash"
    }
  }
}
```

`remediation_explain` returns the normalized item plus policy summary,
principle summaries, skill guidance, and action context when available.

## Examples

Blocked Git bypass:

```json
{
  "policy_id": "git.hook_bypass",
  "failed_action": "Bash",
  "next_steps": [
    "Run the configured gate and fix the underlying failure.",
    "Call MCP policy_explain with policy_id=git.hook_bypass before retrying.",
    "Call MCP skill_lookup with skill_id=safe-git-workflow for the repair playbook."
  ]
}
```

Protected write:

```json
{
  "policy_id": "agent.enforcement_point_write",
  "failed_action": "Write",
  "path": ".claude/settings.json",
  "next_steps": [
    "Call MCP policy_explain with policy_id=agent.enforcement_point_write before retrying."
  ]
}
```

Lint diagnostic:

```json
{
  "policy_id": "python.conditional_imports",
  "skill_id": "conditional-imports",
  "tool": "ruff",
  "code": "PLC0415",
  "file": "pkg/app.py",
  "line": 12,
  "next_steps": [
    "Move the import to the top of the file.",
    "Call MCP policy_explain with policy_id=python.conditional_imports before retrying.",
    "Call MCP skill_lookup with skill_id=conditional-imports for the repair playbook."
  ]
}
```

SARIF findings carry the same payload under result `properties`, allowing code
scanning, editor integrations, and retained traces to call the same MCP tools.

## Trace Summary

Hook and lint traces include `remediation_summary` with stable remediation IDs,
policy IDs, skill IDs, and repeated policy counts inside the run. This is the
local trace substrate for measuring whether future remediation storage reduces
repeat failures over time.

Traces and SARIF result properties also include normalized evidence records:
`finding`, `source_span`, `search_text`, and `remediation_events`. These fields
are the ingestion contract for future code intelligence storage and vector
search; agents should treat them as derived context and continue using MCP for
policy explanation.

The normalized finding ID is the stable join key across CEL decisions, SARIF
fingerprints, retained traces, and future code-intelligence indexes.
