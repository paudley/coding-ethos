<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Agent Command Block Example

This example demonstrates a core `coding-ethos` behavior: unsafe agent tool use
is blocked before the shell command runs.

## Example Scenario

An agent attempts a raw Git command that would bypass the documented wrapper or
tamper with protected hook behavior. The PreToolUse hook evaluates the command
and returns a blocking policy decision.

Expected agent response:

1. Treat the block as policy feedback.
2. Stop the unsafe command path.
3. Use the documented coding-ethos Git wrapper.
4. Ask for admin approval only when the protected workflow requires it.
5. Report the policy ID and next safe command path.

## Why This Matters

AI agents often try to recover from blocked commands by changing the shell
shape: alternate shells, direct binary paths, subprocesses, PATH edits, aliases,
or wrapper bypasses. `coding-ethos` treats those as the same class of risk. The
policy follows intent, not just one command spelling.

## MCP Follow-Up

Agents can use MCP to understand the block:

- `policy_check_command`: evaluate a proposed command before running it.
- `policy_explain`: explain the blocking policy ID.
- `skill_lookup`: load the safe Git workflow remediation skill.

The MCP response is advisory context. The actual enforcement remains in the
agent hook and Git hook paths.

## Related Docs

- [MCP server](../../docs/MCP_SERVER.md)
- [Threat model](../../docs/THREAT_MODEL.md)
- [Comparison](../../docs/COMPARISON.md)
