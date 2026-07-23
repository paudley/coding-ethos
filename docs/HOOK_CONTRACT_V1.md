<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Provider-Neutral Hook Contract v1

The provider-neutral hook contract is the stable process boundary for
supervisors that need coding-ethos decisions without depending on Claude,
Codex, Gemini, or Kimi response schemas.

Provider-native output remains the default. Select v1 explicitly:

```bash
bin/coding-ethos-run agent-hook --json --contract neutral-v1 < event.json
```

The equivalent validated environment setting is
`CODE_ETHOS_HOOK_CONTRACT=neutral-v1`. An unknown selector fails before policy
evaluation.

## Request

The request is the existing normalized hook event object. These two fields are
optional additions:

- `contract_version`: `coding-ethos.hook/v1`; declaring it enables strict
  canonical-field validation.
- `correlation_id`: an operator-provided identifier of at most 128 bytes. The
  runtime generates a `hook-...` identifier when it is absent.

Canonical v1 field names are:

```json
{
  "contract_version": "coding-ethos.hook/v1",
  "correlation_id": "lane-01-turn-42-hook-03",
  "provider": "claude",
  "hook_event_name": "PreToolUse",
  "session_id": "provider-session-id",
  "cwd": "/path/to/repo",
  "tool_name": "Bash",
  "tool_input": {
    "command": "git status --short"
  }
}
```

Requests are bounded to 1 MiB before decoding. A declared v1 request rejects
unknown top-level fields, unknown providers/events, overlong identifiers,
control characters in identifiers and paths, trailing JSON values, and an
unsupported contract version. Requests that do not declare
`contract_version` retain the provider alias normalization used by current
hooks.

## Response

Every v1 response is a single JSON object:

```json
{
  "contract_version": "coding-ethos.hook/v1",
  "correlation_id": "lane-01-turn-42-hook-03",
  "event": {
    "name": "PreToolUse",
    "provider": "claude",
    "tool": "Bash"
  },
  "decision": "deny",
  "effect": {
    "action": "block",
    "reason": "policy-grounded denial"
  },
  "status": "blocked",
  "tracking_id": "hook-0123456789abcdef",
  "decisions": [],
  "advice": {},
  "runtime_ms": 4
}
```

`decision` is `allow` or `deny`. `effect.action` is one of:

- `allow`: proceed without changing provider input.
- `rewrite`: use `effect.updated_input` before the provider executes the tool.
- `block`: reject the requested operation using `effect.reason`.
- `continue`: reject a premature `Stop` and continue the current turn using
  `effect.reason`.

`effect.additional_context` is advisory context for the current event.
`tracking_id` is present on policy denials and connects the response to
coding-ethos remediation and trace evidence.

The neutral process exits 0 for `allow` and 1 for `deny`, independent of the
source provider. Provider-native modes retain their provider-specific exit
semantics.

## Capability Discovery

Discover the runtime version, contract selector, input limit, supported events,
effects, provider adapters, and private-overlay flags without loading DuckDB:

```bash
bin/coding-ethos-run agent-hooks capabilities --json
```

The response schema is `coding-ethos.agent-hooks/v1`. `runtime_version` comes
from the checkout's `pyproject.toml`. The command is read-only and does not
require a policy bundle or code-intelligence store. The report advertises
`mcp_command_flag: "--mcp-command"` alongside the settings and repository root
flags.

## Kimi Native Semantics

Kimi settings are generated under `.kimi-code/` in the selected settings root.
Set `KIMI_CODE_HOME` to that directory when starting Kimi.

- Policy denials exit with code 2 and write the reason to stderr.
- A structured `hookSpecificOutput.permissionDecision = deny` is also emitted.
- Stop checkpoint guidance exits successfully with a structured deny. Kimi
  injects the reason and continues the model once.
- Other non-zero Kimi hook exits are fail-open by provider design.

For a settings overlay separate from the repository:

```bash
bin/coding-ethos-run agent-hooks sync \
  --root /private/settings-overlay \
  --repo-root /path/to/repo
bin/coding-ethos-run agent-hooks verify \
  --root /private/settings-overlay \
  --repo-root /path/to/repo
```

Provider settings and install state are written under the first path. Skill
checks and runnable hook probes use the second path.

When a provider-neutral supervisor owns hook execution, keep Coding Ethos as
the MCP and code-intelligence owner with separate commands:

```bash
bin/coding-ethos-run runtime-policy sync --repo /path/to/repo
bin/coding-ethos-run agent-hooks sync \
  --root /private/settings-overlay \
  --repo-root /path/to/repo \
  --hook-command 'env NYAR_HOME=/private/nyar NYAR_CODING_ETHOS_ROOT=/opt/coding-ethos /absolute/path/nyar hook' \
  --mcp-command '/opt/coding-ethos/bin/coding-ethos-run mcp'
bin/coding-ethos-run agent-hooks verify \
  --root /private/settings-overlay \
  --repo-root /path/to/repo \
  --hook-command 'env NYAR_HOME=/private/nyar NYAR_CODING_ETHOS_ROOT=/opt/coding-ethos /absolute/path/nyar hook' \
  --mcp-command '/opt/coding-ethos/bin/coding-ethos-run mcp'
bin/coding-ethos-run runtime-policy check --repo /path/to/repo
```

Pass both flags unchanged to `doctor` as well. The external hook form is one
statically parsed command with an absolute executable and `hook` subcommand.
A leading `env KEY=value ...` is supported. Operators, substitutions,
redirects, background execution, raw leading assignments, and relative
external executables are rejected. The explicit MCP form is exactly an
absolute `coding-ethos-run mcp`; omitting it preserves the existing derivation
from `coding-ethos-run agent-hook`. Verification sends all provider-native
smoke payloads through the external supervisor command, while generated
Claude, Codex, Gemini, and Kimi MCP entries continue to invoke Coding Ethos
directly. `runtime-policy sync/check` owns only the consumer-scoped compiled
bundle below Git metadata; it does not generate or rewrite tracked repository
configuration.
