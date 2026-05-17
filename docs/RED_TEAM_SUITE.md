<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Red-Team Suite

`coding-ethos` red-team tests exercise bypass attempts as first-class policy
fixtures. They are not ordinary lint examples; they are regression tests for
the guardrail mission.

The initial harness lives in `go/internal/redteam`. It defines reusable
scenarios and runs them against the same compiled policy bundle used by hooks,
lint, Git wrappers, CI, SARIF, and MCP.

## Current Scenarios

- raw Git hook bypass with `--no-verify`;
- absolute Git binary invocation;
- nested shell Git execution;
- protected hook runtime writes;
- protected hook runtime path traversal;
- protected hook runtime symlink targets;
- hook runtime deletion attempts;
- managed toolchain or `PATH` evasion;
- generated config drift;
- Git-wrapper bypass checks;
- lint preflight bypass checks.

Each scenario records the bypass class, enforcement surface, status, policy
IDs, and whether the expected policy failed to fire. A missed scenario is a
test failure, not an advisory warning.

## Expansion Path

The next scenarios should cover live Claude/Codex/Gemini prompt attempts in
disposable repositories. Live agent runs should reuse the same scenario
definitions and report their results through the same result model instead of
creating a separate prompt-only test format.
