// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/hooks"
)

const (
	providerCapabilityMatrixDirMode  = 0o755
	providerCapabilityMatrixFileMode = 0o644

	// ProviderCapabilityMatrixRelativePath is the generated provider report path.
	ProviderCapabilityMatrixRelativePath = "docs/PROVIDER_CAPABILITY_MATRIX.md"
)

// ProviderCapabilities returns the provider adapter capability registry.
func ProviderCapabilities() []ProviderCapability {
	return []ProviderCapability{
		claudeProviderCapability(),
		codexProviderCapability(),
		geminiProviderCapability(),
		kimiProviderCapability(),
		genericProviderCapability(),
	}
}

// AgentHooksAPIVersion identifies the capability-discovery response schema.
const AgentHooksAPIVersion = "coding-ethos.agent-hooks/v1"

// CapabilityReport is the machine-readable agent-hook capability response.
type CapabilityReport struct {
	APIVersion           string `json:"api_version"`
	RuntimeVersion       string `json:"runtime_version"`
	SettingsRootFlag     string `json:"settings_root_flag"`
	RepositoryRootFlag   string `json:"repository_root_flag"`
	StateRootFlag        string `json:"state_root_flag"`
	MCPCommandFlag       string `json:"mcp_command_flag"`
	HookTimeoutFlag      string `json:"hook_timeout_flag"`
	RuntimePolicyCommand string `json:"runtime_policy_command"`

	HookContracts []hooks.HookContractCapability `json:"hook_contracts"`
	Providers     []ProviderCapability           `json:"providers"`

	SupportsPrivateOverlay bool `json:"supports_private_overlay"`
}

// Capabilities returns versioned hook contracts and provider adapters.
func Capabilities(runtimeVersion string) CapabilityReport {
	return CapabilityReport{
		APIVersion:     AgentHooksAPIVersion,
		RuntimeVersion: runtimeVersion,
		HookContracts: []hooks.HookContractCapability{
			hooks.HookContractV1Capability(),
		},
		Providers:              ProviderCapabilities(),
		SettingsRootFlag:       "--root",
		RepositoryRootFlag:     "--repo-root",
		StateRootFlag:          "--state-root",
		MCPCommandFlag:         "--mcp-command",
		HookTimeoutFlag:        "--hook-timeout-seconds",
		RuntimePolicyCommand:   "runtime-policy",
		SupportsPrivateOverlay: true,
	}
}

func claudeProviderCapability() ProviderCapability {
	return ProviderCapability{
		Provider:    string(ProviderClaude),
		DisplayName: "Claude Code",
		Coverage:    "full",
		NativeFiles: []string{".claude/settings.local.json", ".mcp.json"},
		HookEvents: []string{
			"PreToolUse",
			"PostToolUse",
			"PostToolBatch",
			"PreCompact",
			"SessionStart",
			"UserPromptSubmit",
			"Stop",
			"SessionEnd",
			"SubagentStart",
			"SubagentStop",
		},
		BlockResponseShape: "hookSpecificOutput.permissionDecision = deny",
		ContextAdviceShape: "hookSpecificOutput additionalContext and updatedInput",
		MCPSetup:           "project .mcp.json stdio server",
		SettingsTarget:     ".claude/settings.local.json",
		GeneratedTargets: []string{
			"CLAUDE.md",
			".claude/skills/*/SKILL.md",
			".claude/ethos/MEMORY.md",
			".mcp.json",
		},
		MemoryInterception: "provider memory imports into .coding-ethos/memories",
		MemoryFallback:     "central memory guidance when writes target managed paths",
		Supported: []string{
			"PreToolUse block",
			"PreToolUse updatedInput rewrite",
			"PostToolUse additionalContext",
			"PostToolUse edit verification advice",
			"PostToolBatch additionalContext",
			"PreCompact capture",
			"SessionStart additionalContext",
			"UserPromptSubmit additionalContext",
			"Stop additionalContext",
			"SessionEnd additionalContext",
			"SubagentStart additionalContext",
			"SubagentStop additionalContext",
			"MCP stdio server",
		},
		VerificationFixture: "TestSyncAndVerifySettingsRunsProviderSmokePayloads",
	}
}

func codexProviderCapability() ProviderCapability {
	return ProviderCapability{
		Provider:    string(ProviderCodex),
		DisplayName: "Codex",
		Coverage:    "partial",
		NativeFiles: []string{".codex/config.toml"},
		HookEvents: []string{
			"PreToolUse",
			"PostToolUse",
			"SessionStart",
			"UserPromptSubmit",
			"Stop",
		},
		SettingsTarget: ".codex/config.toml",
		BlockResponseShape: "decision = block plus permissionDecision = deny " +
			"for PreToolUse",
		ContextAdviceShape: "additionalContext where native; compact " +
			"systemMessage otherwise",
		MCPSetup: ".codex/config.toml mcp_servers.coding-ethos stdio server",
		GeneratedTargets: []string{
			"AGENTS.md",
			".codex/skills/*/SKILL.md",
			".codex/config.toml",
		},
		MemoryInterception: "provider memory imports into .coding-ethos/memories",
		MemoryFallback:     "memory.centralized denial points at the central memory file",
		Supported: []string{
			"PreToolUse block",
			"PreToolUse native command hook",
			"PreToolUse apply_patch/edit policy hook",
			"PostToolUse compact additionalContext",
			"PostToolUse edit verification advice",
			"SessionStart additionalContext",
			"UserPromptSubmit additionalContext",
			"Stop compact systemMessage",
			"MCP stdio server",
		},
		ProviderLimited: []string{
			"lifecycle context is compacted because Codex flattens multiline allowed context",
		},
		Unsupported: []string{
			"PreToolUse updatedInput rewrite",
			"PostToolBatch additionalContext",
			"SessionEnd additionalContext",
			"SubagentStart additionalContext",
			"SubagentStop additionalContext",
		},
		VerificationFixture: "TestSyncAndVerifySettingsRunsProviderSmokePayloads",
	}
}

func geminiProviderCapability() ProviderCapability {
	return ProviderCapability{
		Provider:    string(ProviderGemini),
		DisplayName: "Gemini CLI",
		Coverage:    "partial",
		NativeFiles: []string{".gemini/settings.json"},
		HookEvents: []string{
			"BeforeTool",
			"AfterTool",
			"BeforeAgent",
			"AfterAgent",
			"SessionStart",
			"SessionEnd",
		},
		SettingsTarget:     ".gemini/settings.json",
		BlockResponseShape: "decision = deny plus systemMessage",
		ContextAdviceShape: "additionalContext on supported lifecycle hooks",
		MCPSetup:           ".gemini/settings.json mcpServers.coding-ethos stdio server",
		GeneratedTargets: []string{
			"GEMINI.md",
			".gemini/extensions/coding-ethos/gemini-extension.json",
			".gemini/extensions/coding-ethos/skills/*/SKILL.md",
			".coding-ethos/gemini/prompt-pack.json",
			".gemini/settings.json",
		},
		MemoryInterception: "provider memory imports into .coding-ethos/memories",
		MemoryFallback:     "memory.centralized denial points at the central memory file",
		Supported: []string{
			"BeforeTool deny",
			"BeforeTool systemMessage",
			"PreToolUse updatedInput rewrite",
			"AfterTool additionalContext",
			"AfterTool edit verification advice",
			"BeforeAgent additionalContext",
			"AfterAgent additionalContext",
			"SessionStart additionalContext",
			"SessionEnd additionalContext",
			"MCP stdio server",
		},
		ProviderLimited: []string{
			providerLimitedToolMapping("BeforeTool", "PreToolUse"),
			providerLimitedToolMapping("AfterTool", "PostToolUse"),
		},
		Unsupported: []string{
			"PostToolBatch additionalContext",
			"PreCompact capture",
			"SubagentStart additionalContext",
			"SubagentStop additionalContext",
		},
		VerificationFixture: "TestSyncAndVerifySettingsRunsProviderSmokePayloads",
	}
}

func kimiProviderCapability() ProviderCapability {
	return ProviderCapability{
		Provider:    string(ProviderKimi),
		DisplayName: "Kimi Code CLI",
		Coverage:    "partial",
		NativeFiles: []string{
			".kimi-code/config.toml",
			".kimi-code/mcp.json",
		},
		HookEvents: []string{
			"PreToolUse",
			"PostToolUse",
			"PostToolUseFailure",
			"PermissionRequest",
			"PermissionResult",
			"UserPromptSubmit",
			"Stop",
			"StopFailure",
			"Interrupt",
			"SessionStart",
			"SessionEnd",
			"SubagentStart",
			"SubagentStop",
			"PreCompact",
			"PostCompact",
			"Notification",
		},
		SettingsTarget: ".kimi-code/config.toml",
		BlockResponseShape: "exit 2 with stderr reason or " +
			"hookSpecificOutput.permissionDecision = deny",
		ContextAdviceShape: "message for context; Stop deny continues the turn once",
		MCPSetup: ".kimi-code/mcp.json stdio server in the generated " +
			"KIMI_CODE_HOME overlay",
		GeneratedTargets: []string{
			"AGENTS.md",
			".agents/skills/*/SKILL.md",
			".kimi-code/config.toml",
			".kimi-code/mcp.json",
		},
		MemoryInterception: "central memory guidance through portable AGENTS.md",
		MemoryFallback:     "read and write .coding-ethos/memories/MEMORY.md",
		Supported: []string{
			"PreToolUse block",
			"PostToolUse context",
			"PostToolUseFailure observation",
			"PermissionRequest observation",
			"PermissionResult observation",
			"UserPromptSubmit block and context",
			"Stop continuation through deny",
			"SessionStart context",
			"SessionEnd observation",
			"SubagentStart observation",
			"SubagentStop observation",
			"PreCompact observation",
			"PostCompact observation",
			"Notification observation",
			"MCP stdio server",
		},
		ProviderLimited: []string{
			"hook command failures other than exit 2 are fail-open in Kimi",
			"only PreToolUse, UserPromptSubmit, and Stop are blockable by Kimi",
		},
		Unsupported: []string{
			"PreToolUse updatedInput rewrite",
			"provider-native skill generation",
		},
		SafetyCaveats: []string{
			"launch Kimi with KIMI_CODE_HOME set to the generated .kimi-code overlay",
			"Kimi hooks are not a substitute for provider permission approval",
		},
		VerificationFixture: "TestSyncAndVerifySettingsRunsProviderSmokePayloads",
	}
}

func genericProviderCapability() ProviderCapability {
	return ProviderCapability{
		Provider:           string(ProviderGeneric),
		DisplayName:        "Generic fallback",
		Coverage:           "unsupported",
		NativeFiles:        []string{},
		HookEvents:         []string{},
		BlockResponseShape: "none; no provider-native hook decision shape",
		ContextAdviceShape: "portable Markdown and MCP responses only",
		MCPSetup:           "manual stdio MCP client configuration",
		SettingsTarget:     "none",
		GeneratedTargets: []string{
			"AGENTS.md",
			"ETHOS.md",
			".agents/ethos/*.md",
			".agents/skills/*/SKILL.md",
		},
		MemoryInterception: "none; providers must write central memory directly",
		MemoryFallback:     "read and write .coding-ethos/memories/MEMORY.md",
		Supported: []string{
			"portable root guidance",
			"portable ETHOS.md guidance",
			"portable skill surfaces",
			"manual MCP stdio server configuration",
		},
		Unsupported: []string{
			"native hook settings generation",
			"provider-native block response",
			"provider-native context injection",
			"provider-native updatedInput rewrite",
			"automatic memory write interception",
		},
		SafetyCaveats: []string{
			"generic fallback providers must route policy checks through MCP " +
				"or explicit CLI commands",
			"generic fallback providers do not receive automatic " +
				"provider-native hook enforcement",
		},
		VerificationFixture: "TestProviderCapabilityMatrixSyncAndCheckDetectDrift",
	}
}

// ProviderCapabilityMatrixMarkdown renders the generated provider report.
func ProviderCapabilityMatrixMarkdown() string {
	var builder strings.Builder

	builder.WriteString("<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. " +
		"<paudley@blackcat.ca> -->\n")
	builder.WriteString("<!-- SPDX-License-Identifier: AGPL-3.0-only -->\n\n")
	builder.WriteString("<!-- Source: go/internal/agenthooks/provider_capabilities.go.\n")
	builder.WriteString("Regenerate with make sync-provider-matrix.\n")
	builder.WriteString("-->\n")
	builder.WriteString("# Provider Capability Matrix\n\n")
	builder.WriteString(
		"This report is generated from the provider capability registry.\n",
	)
	builder.WriteString("It lists supported, partially supported, and unsupported " +
		"adapter surfaces.\n\n")
	builder.WriteString("## Coverage Summary\n\n")
	builder.WriteString("| Provider | Display name | Coverage |\n")
	builder.WriteString("| --- | --- | --- |\n")

	for _, capability := range ProviderCapabilities() {
		appendCapabilitySummaryRow(&builder, capability)
	}

	builder.WriteString("\n## Provider Details\n")

	for _, capability := range ProviderCapabilities() {
		appendCapabilityDetail(&builder, capability)
	}

	return builder.String()
}

func appendCapabilitySummaryRow(
	builder *strings.Builder,
	capability ProviderCapability,
) {
	builder.WriteString("| `")
	builder.WriteString(markdownCell(capability.Provider))
	builder.WriteString("` | ")
	builder.WriteString(markdownCell(capability.DisplayName))
	builder.WriteString(" | ")
	builder.WriteString(markdownCell(capability.Coverage))
	builder.WriteString(" |\n")
}

// SyncProviderCapabilityMatrix writes the generated provider report to disk.
func SyncProviderCapabilityMatrix(root string) ([]string, error) {
	path := providerCapabilityMatrixPath(root)

	err := os.MkdirAll(filepath.Dir(path), providerCapabilityMatrixDirMode)
	if err != nil {
		return nil, fmt.Errorf("create provider matrix dir: %w", err)
	}

	err = os.WriteFile(
		path,
		[]byte(ProviderCapabilityMatrixMarkdown()),
		providerCapabilityMatrixFileMode,
	)
	if err != nil {
		return nil, fmt.Errorf("write provider capability matrix: %w", err)
	}

	return []string{path}, nil
}

// CheckProviderCapabilityMatrix reports generated provider report drift.
func CheckProviderCapabilityMatrix(root string) ([]string, error) {
	path := providerCapabilityMatrixPath(root)

	current, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read provider capability matrix: %w", err)
		}

		return []string{path}, nil
	}

	if string(current) != ProviderCapabilityMatrixMarkdown() {
		return []string{path}, nil
	}

	return []string{}, nil
}

func providerCapabilityMatrixPath(root string) string {
	return filepath.Join(
		root,
		filepath.FromSlash(ProviderCapabilityMatrixRelativePath),
	)
}

func appendCapabilityDetail(
	builder *strings.Builder,
	capability ProviderCapability,
) {
	builder.WriteString("\n### " + capability.DisplayName + "\n\n")
	appendKeyValue(builder, "Provider id", "`"+markdownCell(capability.Provider)+"`")
	appendKeyValue(builder, "Coverage", markdownCell(capability.Coverage))
	appendKeyValue(
		builder,
		"Settings target",
		markdownCell(capability.SettingsTarget),
	)
	appendKeyValue(builder, "MCP setup", markdownCell(capability.MCPSetup))
	appendKeyValue(
		builder,
		"Block response shape",
		markdownCell(capability.BlockResponseShape),
	)
	appendKeyValue(
		builder,
		"Context/advice shape",
		markdownCell(capability.ContextAdviceShape),
	)
	appendKeyValue(
		builder,
		"Memory interception",
		markdownCell(capability.MemoryInterception),
	)
	appendKeyValue(
		builder,
		"Memory fallback",
		markdownCell(capability.MemoryFallback),
	)
	appendKeyValue(
		builder,
		"Verification",
		"`"+markdownCell(capability.VerificationFixture)+"`",
	)

	builder.WriteString("\nNative settings:\n")
	appendBulletList(builder, capability.NativeFiles)

	builder.WriteString("\nHook events:\n")
	appendBulletList(builder, capability.HookEvents)

	builder.WriteString("\nGenerated targets:\n")
	appendBulletList(builder, capability.GeneratedTargets)

	builder.WriteString("\nSupported surfaces:\n")
	appendBulletList(builder, capability.Supported)

	builder.WriteString("\nPartially supported surfaces:\n")
	appendBulletList(builder, capability.ProviderLimited)

	builder.WriteString("\nUnsupported surfaces:\n")
	appendBulletList(builder, capability.Unsupported)

	builder.WriteString("\nSafety caveats:\n")
	appendBulletList(builder, capability.SafetyCaveats)
}

func appendKeyValue(
	builder *strings.Builder,
	label string,
	value string,
) {
	builder.WriteString("- " + label + ": " + value + "\n")
}

func appendBulletList(builder *strings.Builder, values []string) {
	if len(values) == 0 {
		builder.WriteString("\n- none\n")

		return
	}

	builder.WriteString("\n")

	for _, value := range values {
		builder.WriteString("- " + markdownCell(value) + "\n")
	}
}

func markdownCell(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "none"
	}

	escaped := strings.ReplaceAll(trimmed, "|", "\\|")

	return escaped
}

func providerLimitedToolMapping(event, target string) string {
	return event + " maps to " + target + " for run_shell_command, " +
		"write_file, replace, and MultiEdit"
}
