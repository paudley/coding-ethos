<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

<!-- Source: go/internal/agenthooks/provider_capabilities.go.
Regenerate with make sync-provider-matrix.
-->
# Provider Capability Matrix

This report is generated from the provider capability registry.
It lists supported, partially supported, and unsupported adapter surfaces.

## Coverage Summary

| Provider | Display name | Coverage |
| --- | --- | --- |
| `claude` | Claude Code | full |
| `codex` | Codex | partial |
| `gemini` | Gemini CLI | partial |
| `generic` | Generic fallback | unsupported |

## Provider Details

### Claude Code

- Provider id: `claude`
- Coverage: full
- Settings target: .claude/settings.local.json
- MCP setup: project .mcp.json stdio server
- Block response shape: hookSpecificOutput.permissionDecision = deny
- Context/advice shape: hookSpecificOutput additionalContext and updatedInput
- Memory interception: provider memory imports into .coding-ethos/memories
- Memory fallback: central memory guidance when writes target managed paths
- Verification: `TestSyncAndVerifySettingsRunsProviderSmokePayloads`

Native settings:

- .claude/settings.local.json
- .mcp.json

Hook events:

- PreToolUse
- PostToolUse
- PostToolBatch
- PreCompact
- SessionStart
- UserPromptSubmit
- Stop
- SessionEnd
- SubagentStart
- SubagentStop

Generated targets:

- CLAUDE.md
- .claude/skills/*/SKILL.md
- .claude/ethos/MEMORY.md
- .mcp.json

Supported surfaces:

- PreToolUse block
- PreToolUse updatedInput rewrite
- PostToolUse additionalContext
- PostToolUse edit verification advice
- PostToolBatch additionalContext
- PreCompact capture
- SessionStart additionalContext
- UserPromptSubmit additionalContext
- Stop additionalContext
- SessionEnd additionalContext
- SubagentStart additionalContext
- SubagentStop additionalContext
- MCP stdio server

Partially supported surfaces:

- none

Unsupported surfaces:

- none

Safety caveats:

- none

### Codex

- Provider id: `codex`
- Coverage: partial
- Settings target: .codex/config.toml
- MCP setup: .codex/config.toml mcp_servers.coding-ethos stdio server
- Block response shape: decision = block plus permissionDecision = deny for PreToolUse
- Context/advice shape: additionalContext where native; compact systemMessage otherwise
- Memory interception: provider memory imports into .coding-ethos/memories
- Memory fallback: memory.centralized denial points at the central memory file
- Verification: `TestSyncAndVerifySettingsRunsProviderSmokePayloads`

Native settings:

- .codex/config.toml

Hook events:

- PreToolUse
- PostToolUse
- SessionStart
- UserPromptSubmit
- Stop

Generated targets:

- AGENTS.md
- .codex/skills/*/SKILL.md
- .codex/config.toml

Supported surfaces:

- PreToolUse block
- PreToolUse native command hook
- PreToolUse apply_patch/edit policy hook
- PostToolUse compact additionalContext
- PostToolUse edit verification advice
- SessionStart additionalContext
- UserPromptSubmit additionalContext
- Stop compact systemMessage
- MCP stdio server

Partially supported surfaces:

- lifecycle context is compacted because Codex flattens multiline allowed context

Unsupported surfaces:

- PreToolUse updatedInput rewrite
- PostToolBatch additionalContext
- SessionEnd additionalContext
- SubagentStart additionalContext
- SubagentStop additionalContext

Safety caveats:

- none

### Gemini CLI

- Provider id: `gemini`
- Coverage: partial
- Settings target: .gemini/settings.json
- MCP setup: .gemini/settings.json mcpServers.coding-ethos stdio server
- Block response shape: decision = deny plus systemMessage
- Context/advice shape: additionalContext on supported lifecycle hooks
- Memory interception: provider memory imports into .coding-ethos/memories
- Memory fallback: memory.centralized denial points at the central memory file
- Verification: `TestSyncAndVerifySettingsRunsProviderSmokePayloads`

Native settings:

- .gemini/settings.json

Hook events:

- BeforeTool
- AfterTool
- BeforeAgent
- AfterAgent
- SessionStart
- SessionEnd

Generated targets:

- GEMINI.md
- .gemini/extensions/coding-ethos/gemini-extension.json
- .gemini/extensions/coding-ethos/skills/*/SKILL.md
- .coding-ethos/gemini/prompt-pack.json
- .gemini/settings.json

Supported surfaces:

- BeforeTool deny
- BeforeTool systemMessage
- PreToolUse updatedInput rewrite
- AfterTool additionalContext
- AfterTool edit verification advice
- BeforeAgent additionalContext
- AfterAgent additionalContext
- SessionStart additionalContext
- SessionEnd additionalContext
- MCP stdio server

Partially supported surfaces:

- BeforeTool maps to PreToolUse for run_shell_command, write_file, replace, and MultiEdit
- AfterTool maps to PostToolUse for run_shell_command, write_file, replace, and MultiEdit

Unsupported surfaces:

- PostToolBatch additionalContext
- PreCompact capture
- SubagentStart additionalContext
- SubagentStop additionalContext

Safety caveats:

- none

### Generic fallback

- Provider id: `generic`
- Coverage: unsupported
- Settings target: none
- MCP setup: manual stdio MCP client configuration
- Block response shape: none; no provider-native hook decision shape
- Context/advice shape: portable Markdown and MCP responses only
- Memory interception: none; providers must write central memory directly
- Memory fallback: read and write .coding-ethos/memories/MEMORY.md
- Verification: `TestProviderCapabilityMatrixSyncAndCheckDetectDrift`

Native settings:

- none

Hook events:

- none

Generated targets:

- AGENTS.md
- ETHOS.md
- .agents/ethos/*.md
- .agents/skills/*/SKILL.md

Supported surfaces:

- portable root guidance
- portable ETHOS.md guidance
- portable skill surfaces
- manual MCP stdio server configuration

Partially supported surfaces:

- none

Unsupported surfaces:

- native hook settings generation
- provider-native block response
- provider-native context injection
- provider-native updatedInput rewrite
- automatic memory write interception

Safety caveats:

- generic fallback providers must route policy checks through MCP or explicit CLI commands
- generic fallback providers do not receive automatic provider-native hook enforcement
