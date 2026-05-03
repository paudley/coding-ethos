<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Runtime Sandbox Example

Runtime sandboxing complements CEL policy. CEL decides whether a tool should be
allowed to run; sandboxing limits what the tool can do after it starts.

## Tool Capability Shape

Managed tools should declare runtime needs explicitly:

```yaml
runtime:
  sandbox_profile: lint-offline
  requires_network: false
  requires_git: false
  timeout_seconds: 30
  memory_mb: 512
  cpu_quota_percent: 100
  seccomp_profile: lint-basic
  read_paths:
    - "."
  write_paths:
    - ".code-ethos/cache"
```

## Expected Behavior

For an offline linter, sandbox evidence should show:

- read-only root
- read-only repository and `.git` mounts
- hidden home credential directories
- disconnected network
- declared writable paths only
- timeout and resource-limit requests
- seccomp profile metadata

## Agent Workflow

Before choosing a managed tool, agents should call MCP `tool_capabilities`.
That response exposes the same capability facts used by CEL, traces, and SARIF.

If sandbox mode is required and unavailable, the run should fail closed. If
sandbox mode is advisory, the result must record degraded evidence rather than
claiming enforcement happened.

## Related Docs

- [Runtime sandboxing](../../docs/RUNTIME_SANDBOXING.md)
- [Threat model](../../docs/THREAT_MODEL.md)
- [Tool integrations](../../docs/INTEGRATIONS.md)
