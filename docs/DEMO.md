<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Demo

This demo shows the core `coding-ethos` loop:

1. Ask MCP for policy context.
2. Block unsafe commands before execution.
3. Run policy lint through the managed path.
4. Emit SARIF for CI, editor, and remediation workflows.

The excerpts below were verified against the local repo. They are intentionally
short so they can be reused in README screenshots, release notes, or a recorded
terminal demo.

## Recording

![coding-ethos MCP and SARIF demo](assets/coding-ethos-demo.gif)

The source recording is [assets/coding-ethos-demo.cast](assets/coding-ethos-demo.cast).

## Start The MCP Server

```bash
bin/coding-ethos-run mcp
```

The server speaks MCP over stdio with `Content-Length` framing. MCP clients
should call `tools/list` first, then use `policy_check_command`, `lint_check`,
`lint_advice`, `sarif_remediation_advice`, or `tool_capabilities` depending on
the task.

## MCP Command Block

An MCP client can ask whether a command is safe before running it:

```json
{
  "method": "tools/call",
  "params": {
    "name": "policy_check_command",
    "arguments": {
      "provider": "codex",
      "command": "git commit --no-verify -m test"
    }
  }
}
```

Expected result:

```json
{
  "blocked": true,
  "scope": "command",
  "status": "blocked",
  "decisions": [
    {
      "policy_id": "git.hook_bypass",
      "decision": "block",
      "severity": "block",
      "message": "Hook bypass is forbidden.",
      "skill_id": "safe-git-workflow",
      "suggestion": "Run the configured gate and fix the underlying failure."
    }
  ]
}
```

## MCP Lint Check

Agents should use MCP `lint_check` instead of guessing raw linter commands:

```json
{
  "method": "tools/call",
  "params": {
    "name": "lint_check",
    "arguments": {
      "scope": "files",
      "files": ["examples/mcp-lint-advice/README.md"]
    }
  }
}
```

Expected result shape:

```json
{
  "blocked": false,
  "engine": "compiled_policy_lint",
  "files": ["examples/mcp-lint-advice/README.md"],
  "diagnostics": [],
  "findings": [
    {
      "policy_id": "syntax.file_syntax",
      "status": "pass",
      "skill_id": "managed-toolchain"
    },
    {
      "policy_id": "filesystem.line_limits",
      "status": "pass",
      "skill_id": "agent-operating-discipline"
    }
  ]
}
```

## SARIF Output

The same policy path can emit SARIF:

```bash
bin/coding-ethos-run policy-lint \
  --scope files \
  --files examples/mcp-lint-advice/README.md \
  --sarif
```

Expected SARIF shape:

```json
{
  "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
  "version": "2.1.0",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "coding-ethos",
          "informationUri": "https://github.com/paudley/coding-ethos"
        }
      },
      "automationDetails": {
        "id": "coding-ethos/files"
      },
      "results": [],
      "properties": {
        "scope": "files",
        "policy_coverage": {
          "policy_count": 21,
          "ethos_count": 15
        }
      }
    }
  ]
}
```

## Recording Plan

The checked-in GIF was recorded with `asciinema` and rendered with `agg`:

```bash
asciinema rec docs/assets/coding-ethos-demo.cast
agg docs/assets/coding-ethos-demo.cast docs/assets/coding-ethos-demo.gif
```

Recommended sequence:

1. Show `tools/list` from MCP.
2. Call `policy_check_command` with a blocked hook-bypass command.
3. Call `lint_check` for one example file.
4. Emit SARIF for the same file.
5. End on the README quick-start commands.
