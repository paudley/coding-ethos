// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
	"blackcat.ca/coding-ethos/go/internal/shellquote"
	"blackcat.ca/coding-ethos/go/internal/toolprotocol"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const (
	agentShellBlockedExitCode = 2
	agentShellCheckExitCode   = 0
	mountInfoMinimumFields    = 6
)

func run(paths runtimePaths, args []string) error {
	if len(args) == 0 {
		emitRunHelp()

		return nil
	}

	command := args[0]
	rest := args[1:]

	handler, found := runCommandHandler(command)
	if !found {
		return apperror.Wrapf(
			apperror.StaticError("unknown coding-ethos-run command"),
			"unknown coding-ethos-run command %q",
			command,
		)
	}

	return handler(paths, rest)
}

type runHandler func(runtimePaths, []string) error

type runCommandEntry struct {
	Handler runHandler
	Command string
}

func runCommandHandler(command string) (runHandler, bool) {
	for _, entry := range runCommandEntries() {
		if entry.Command == command {
			return entry.Handler, true
		}
	}

	return nil, false
}

func runCommandEntries() []runCommandEntry {
	return []runCommandEntry{
		{Command: "--help", Handler: runHelpHandler},
		{Command: "-h", Handler: runHelpHandler},
		{Command: "help", Handler: runHelpHandler},
		{Command: "agent-hook", Handler: runAgentHookHandler},
		{Command: "agent-proxy", Handler: runAgentProxyHandler},
		{Command: "agent-shell", Handler: runAgentShellHandler},
		{Command: "git-hook", Handler: runGitHook},
		{Command: "lfs-hook", Handler: runLFSHook},
		{Command: "agent-hooks", Handler: runAgentHooksHandler},
		{Command: "cutover", Handler: runCutover},
		{Command: "lint", Handler: runManagedLintHandler},
		{Command: "policy-lint", Handler: runPolicyLintHandler},
		{Command: "ci-sarif", Handler: runCISARIFHandler},
		{Command: "policy", Handler: runPolicyHandler},
		{Command: "code-intel", Handler: runCodeIntelHandler},
		{Command: "output", Handler: runOutputHandler},
		{Command: "web-guidance", Handler: runWebGuidanceHandler},
		{Command: "status", Handler: runStatusHandler},
		{Command: "policy-tool", Handler: runPolicyTool},
		{Command: "policy-tool-group", Handler: runPolicyToolGroup},
		{Command: "policy-git", Handler: runPolicyGitHandler},
		{Command: "parent-install", Handler: runParentInstall},
		{Command: "parent-runtime-sync", Handler: runParentRuntimeSync},
		{Command: "parent-check", Handler: runParentCheck},
		{Command: "parent-lint", Handler: runParentLint},
		{Command: "runtime-policy", Handler: runRuntimePolicy},
		{Command: "mcp", Handler: runMCPHandler},
	}
}

func runHelpHandler(_ runtimePaths, _ []string) error {
	emitRunHelp()

	return nil
}

func emitRunHelp() {
	feedback.Emit(os.Stdout, runHelpMessage(), feedback.FormatTOON)
}

func runHelpMessage() feedback.Message {
	return feedback.Message{
		Scalars: []feedback.Scalar{
			feedback.S("command", "coding-ethos-run"),
			feedback.S(
				"summary",
				"Managed coding-ethos runtime wrapper for parent repos and agents.",
			),
		},
		Tables: []feedback.Table{
			feedback.T(
				"common",
				[]string{"command", "purpose"},
				[][]string{
					{
						"lint --staged",
						"Run managed fixers, formatters, and lint checks for staged files.",
					},
					{
						"lint --changed",
						"Run managed fixers, formatters, and lint checks for changed files.",
					},
					{"lint --full", "Run all configured managed fixers, formatters, and lint checks."},
					{"bin/lint --staged", "Short wrapper for managed lint."},
					{"agent-proxy passthrough", "Run explicit Agent API proxy pass-through."},
					{"agent-shell -- <command>", "Run shell through policy rewrite."},
					{"output report", "Report managed output surfaces."},
					{"status", "Summarize operator health and write handoff reports."},
				},
			),
			feedback.T(
				"internal",
				[]string{"command", "purpose"},
				[][]string{
					{"policy-lint", "Internal compiled policy lint entrypoint."},
					{"policy-tool <tool>", "Run one managed captured tool."},
					{"policy-tool-group <group>", "Run a managed tool group."},
					{
						"runtime-policy sync|check --repo <path>",
						"Manage only the consumer-scoped hook policy bundle.",
					},
					{
						"parent-runtime-sync --repo <path>",
						"Project prebuilt hook executables into a consumer Git runtime.",
					},
					{"git-hook", "Git hook entrypoint; not for manual lint."},
				},
			),
		},
	}
}

func runAgentHookHandler(paths runtimePaths, rest []string) error {
	runAgentHook(paths, rest)

	return nil
}

func runAgentShellHandler(paths runtimePaths, rest []string) error {
	request, err := agentShellCommand(rest)
	if err != nil {
		return err
	}

	requireRuntimeFile(hookPolicyBundlePath(paths), "compiled policy bundle")

	if decision, blocked := agentShellEdgeDecision(request.Command); blocked {
		result := agentShellBlockedResult(decision)
		recordAgentShellExecution(
			paths,
			request,
			"blocked",
			agentShellBlockedExitCode,
			result,
		)
		emitAgentShellBlock(result)
	}

	command := request.Command
	if request.Rewrite {
		rewritten, err := rewriteAgentShellCommand(paths, request)
		if err != nil {
			return err
		}

		command = rewritten
	}

	request.Command = command
	if request.Check {
		result, err := inspectAgentShellCommand(paths, request)
		if err != nil {
			return err
		}

		recordAgentShellExecution(
			paths,
			request,
			result.Status,
			agentShellCheckExitCode,
			result,
		)

		return emitAgentShellCheck(result, request)
	}

	installGitWrapperShim(paths)
	installLintToolShims(paths)

	recordAgentShellExecution(paths, request, "started", -1, hooks.Result{
		Event:    "PreToolUse",
		Provider: agentShellProvider(),
		Status:   "started",
		Tool:     "Bash",
	})

	paths.executor().execAgentShell(paths, command)

	return nil
}

func runAgentHooksHandler(paths runtimePaths, rest []string) error {
	runAgentHooksCommand(paths, rest)

	return nil
}

type agentShellRequest struct {
	Command string
	Intent  string
	Check   bool
	Rewrite bool
}

func agentShellCommand(args []string) (agentShellRequest, error) {
	request, args, err := parseAgentShellFlags(args)
	if err != nil {
		return agentShellRequest{}, err
	}

	if len(args) < 2 || args[0] != "--" {
		return agentShellRequest{}, apperror.StaticError(
			"agent-shell requires [--no-rewrite] [--check] [--intent <intent>] -- <command>",
		)
	}

	commandArgs := args[1:]
	if len(commandArgs) == 1 {
		command := strings.TrimSpace(commandArgs[0])
		if command == "" {
			return agentShellRequest{}, apperror.StaticError(
				"agent-shell command is empty",
			)
		}

		request.Command = command

		return request, nil
	}

	request.Command = shellCommand(commandArgs)

	return request, nil
}

func parseAgentShellFlags(args []string) (agentShellRequest, []string, error) {
	request := agentShellRequest{Intent: agentShellStrategicIntent(), Rewrite: true}

	for len(args) > 0 {
		switch {
		case args[0] == "--rewrite":
			request.Rewrite = true
			args = args[1:]
		case args[0] == "--no-rewrite":
			request.Rewrite = false
			args = args[1:]
		case args[0] == "--check":
			request.Check = true
			args = args[1:]
		case args[0] == "--intent":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return agentShellRequest{}, nil, apperror.StaticError(
					"agent-shell --intent requires a value",
				)
			}

			request.Intent = strings.TrimSpace(args[1])
			args = args[2:]
		case strings.HasPrefix(args[0], "--intent="):
			request.Intent = strings.TrimSpace(strings.TrimPrefix(args[0], "--intent="))
			if request.Intent == "" {
				return agentShellRequest{}, nil, apperror.StaticError(
					"agent-shell --intent requires a value",
				)
			}

			args = args[1:]
		default:
			return request, args, nil
		}
	}

	return request, args, nil
}

func agentShellProvider() string {
	provider := hooks.Event{}.Provider()
	if provider != "" {
		return provider
	}

	return parentCheckoutCodingEthos
}

func shellCommand(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, shellquote.Arg(arg))
	}

	return strings.Join(parts, " ")
}

func rewriteAgentShellCommand(
	paths runtimePaths,
	request agentShellRequest,
) (string, error) {
	result, err := inspectAgentShellCommand(paths, request)
	if err != nil {
		return "", err
	}

	if result.Blocked() {
		emitAgentShellBlock(result)
	}

	if result.HookSpecificOutput == nil ||
		len(result.HookSpecificOutput.UpdatedInput) == 0 {
		return request.Command, nil
	}

	rewritten, ok := result.HookSpecificOutput.UpdatedInput["command"].(string)
	if !ok || strings.TrimSpace(rewritten) == "" {
		return request.Command, nil
	}

	return rewritten, nil
}

func inspectAgentShellCommand(
	paths runtimePaths,
	request agentShellRequest,
) (hooks.Result, error) {
	bundlePath := hookPolicyBundlePath(paths)

	bundleFile, err := os.Open(bundlePath)
	if err != nil {
		return hooks.Result{}, fmt.Errorf("open policy bundle: %w", err)
	}
	defer bundleFile.Close()

	bundle, err := policy.DecodeBundle(bundleFile)
	if err != nil {
		return hooks.Result{}, fmt.Errorf("decode policy bundle: %w", err)
	}

	result, err := hooks.Run(bundle, hooks.Options{Event: hooks.Event{
		ProviderHint:  "coding-ethos",
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		Cwd:           paths.InvocationCWD,
		ToolInput: map[string]any{
			"command":             request.Command,
			"agent_shell_rewrite": request.Rewrite,
			"strategic_intent":    request.Intent,
		},
	}})
	if err != nil {
		return hooks.Result{}, fmt.Errorf("inspect agent shell command: %w", err)
	}

	return result, nil
}

func emitAgentShellBlock(result hooks.Result) {
	err := hookoutput.EncodeLintResult(
		os.Stderr,
		agentShellBlockLintResult(result),
		hookoutput.FormatSARIF,
	)
	if err != nil {
		err = hooks.EncodeProviderResult(os.Stderr, result)
		if err != nil {
			feedback.Emit(
				os.Stderr,
				feedback.Text{Text: hooks.ProviderBlockMessage(result)},
				feedback.FormatTOON,
			)
		}
	}

	requestRuntimeExit(agentShellBlockedExitCode)
}

func agentShellBlockLintResult(result hooks.Result) lint.Result {
	items := make([]diagnostics.Diagnostic, 0, len(result.Decisions))
	for _, decision := range result.Decisions {
		items = append(items, diagnostics.Diagnostic{
			Tool:     "cerun",
			Code:     decision.PolicyID,
			File:     ".",
			PolicyID: decision.PolicyID,
			SkillID:  evidenceStringForDecision(decision, "skill_id"),
			Severity: "block",
			Message:  decision.Message,
			Advice:   decision.Suggestion,
		})
	}

	return lint.Result{
		Scope:       "agent-shell",
		Status:      "blocked",
		Decisions:   result.Decisions,
		Diagnostics: items,
	}
}

func evidenceStringForDecision(decision policy.Decision, key string) string {
	if decision.Evidence == nil {
		return ""
	}

	value, found := decision.Evidence[key]
	if !found {
		return ""
	}

	text, found := value.(string)
	if !found {
		return ""
	}

	return text
}

type agentShellCheckOutput struct {
	AgentRemediation []agentmsg.Remediation `json:"agent_remediation,omitempty"`
	CommandSHA256    string                 `json:"command_sha256"`
	StrategicIntent  string                 `json:"strategic_intent,omitempty"`
	Status           string                 `json:"status"`
	TraceID          string                 `json:"trace_id,omitempty"`
	TrackingID       string                 `json:"tracking_id,omitempty"`
	Decisions        []policy.Decision      `json:"decisions,omitempty"`
}

func emitAgentShellCheck(result hooks.Result, request agentShellRequest) error {
	if result.Blocked() {
		emitAgentShellBlock(result)
	}

	payload := agentShellCheckOutput{
		Status:          result.Status,
		CommandSHA256:   sha256Text(request.Command),
		StrategicIntent: request.Intent,
		TraceID:         result.TrackingID,
		TrackingID:      result.TrackingID,
		Decisions:       result.Decisions,
		AgentRemediation: agentmsg.FromDecisions(
			result.Decisions,
			"Bash",
		),
	}

	err := feedback.WriteJSON(os.Stdout, payload)
	if err != nil {
		return fmt.Errorf("write agent-shell check result: %w", err)
	}

	return nil
}

func agentShellBlockedResult(decision policy.Decision) hooks.Result {
	return hooks.Result{
		Event:     "PreToolUse",
		Provider:  agentShellProvider(),
		Status:    "blocked",
		Tool:      "Bash",
		Decisions: []policy.Decision{decision},
	}
}

func agentShellEdgeDecision(command string) (policy.Decision, bool) {
	for _, finding := range agentShellEdgeFindings(command) {
		policyDef := policy.Policy{
			ID:              finding.PolicyID,
			Category:        "security",
			DefaultSeverity: "block",
			Message:         finding.Message,
			Suggestion:      finding.Suggestion,
			SupportedModes:  []string{"block"},
		}
		decision := policy.NewDecision("block", policyDef)
		decision.Severity = "block"
		decision.Message = finding.Message
		decision.Suggestion = finding.Suggestion
		decision.Evidence = map[string]any{
			"implementation": "agent-shell edge scanner",
			"command_sha256": sha256Text(command),
		}

		return decision, true
	}

	return policy.Decision{}, false
}

type agentShellEdgeFinding struct {
	PolicyID   string
	Message    string
	Suggestion string
}

var (
	agentShellSecretPattern = regexp.MustCompile(
		`(?i)(sk-[A-Za-z0-9_-]{16,}|` +
			`api[_-]?key\s*[=:]\s*['"]?[A-Za-z0-9_-]{16,}|` +
			`token\s*[=:]\s*['"]?[A-Za-z0-9_-]{16,}|` +
			`authorization:\s*(bearer|token)\s+[A-Za-z0-9_-]{16,})`,
	)
	agentShellLocalPIIPattern = regexp.MustCompile(
		`(?i)(^|[[:space:]'"])(/home/[^[:space:]'"]+|` +
			`/users/[^[:space:]'"]+|[A-Z]:\\Users\\[^[:space:]'"]+)`,
	)
)

func agentShellEdgeFindings(command string) []agentShellEdgeFinding {
	if agentShellRecurses(command) {
		return []agentShellEdgeFinding{{
			PolicyID:   "runner.recursive_invocation",
			Message:    "cerun blocked a recursive runner invocation.",
			Suggestion: "Run the target command directly after the single cerun boundary.",
		}}
	}

	if agentShellSecretPattern.MatchString(command) {
		return []agentShellEdgeFinding{
			{
				PolicyID: "runner.argv_secret",
				Message:  "cerun blocked command argv that appears to contain a secret.",
				Suggestion: "Remove secrets from command arguments and use an approved " +
					"non-logged channel.",
			},
		}
	}

	if agentShellLocalPIIPattern.MatchString(command) {
		return []agentShellEdgeFinding{
			{
				PolicyID: "runner.argv_local_pii",
				Message: "cerun blocked command argv that appears to expose a local-machine " +
					"path.",
				Suggestion: "Use repo-relative paths or redact local-machine details before " +
					"running the command.",
			},
		}
	}

	return nil
}

func agentShellRecurses(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return false
	}

	for _, parsed := range commands {
		if len(parsed.Argv) == 0 {
			continue
		}

		name := filepath.Base(parsed.Name)
		if name == "cerun" {
			return true
		}

		if name == "coding-ethos-run" && len(parsed.Argv) > 1 &&
			parsed.Argv[1] == "agent-shell" {
			return true
		}
	}

	return false
}

func recordAgentShellExecution(
	paths runtimePaths,
	request agentShellRequest,
	status string,
	exitCode int,
	result hooks.Result,
) {
	event := agentShellAuditEvent(paths, request, status, exitCode, result)

	err := appendAgentShellAuditEvent(paths.effectiveStateRoot(), event)
	if err != nil {
		debuglog.Debug(
			"agent-shell.audit.failed",
			zap.String("phase", "append_event_log"),
			zap.Error(err),
		)
	}
}

func agentShellAuditEvent(
	paths runtimePaths,
	request agentShellRequest,
	status string,
	exitCode int,
	result hooks.Result,
) agentproxy.ProviderEvent {
	decision, policyID := firstAgentShellDecision(result)

	event := agentproxy.ProviderEvent{
		ID: "cerun-" + sha256Text(
			time.Now().UTC().Format(time.RFC3339Nano)+request.Command,
		),
		SessionID: firstNonEmptyString(
			os.Getenv("CODEX_SESSION_ID"),
			os.Getenv("GEMINI_SESSION_ID"),
			os.Getenv("CLAUDE_SESSION_ID"),
			"cerun-local",
		),
		Kind:          agentproxy.EventToolCall,
		Provider:      "coding-ethos",
		Tool:          "cerun",
		RepoRoot:      paths.Root,
		Cwd:           paths.InvocationCWD,
		RecordedAtUTC: time.Now().UTC(),
		Direction:     agentproxy.DirectionLocal,
		PayloadKind:   agentproxy.PayloadToolCall,
		InputHash:     sha256Text(request.Command),
		Payload: agentproxy.PayloadMeasurement{
			Bytes: len(request.Command),
			Lines: strings.Count(request.Command, "\n") + 1,
		},
		PolicyID: policyID,
		Decision: decision,
		Metadata: agentShellAuditMetadata(request, status, exitCode),
	}
	if request.Intent != "" {
		event.Policy = agentproxy.PolicyEvidence{
			Reason: "strategic intent captured for contextual sandbox policy",
		}
	}

	return event
}

func firstAgentShellDecision(result hooks.Result) (string, string) {
	if len(result.Decisions) == 0 {
		return "", ""
	}

	return result.Decisions[0].Decision, result.Decisions[0].PolicyID
}

func agentShellAuditMetadata(
	request agentShellRequest,
	status string,
	exitCode int,
) map[string]string {
	metadata := map[string]string{
		"status":           status,
		"rewrite":          strconv.FormatBool(request.Rewrite),
		"check":            strconv.FormatBool(request.Check),
		"strategic_intent": request.Intent,
		"sandbox_profile":  agentShellSandboxProfile(runtime.GOOS),
		"sandbox_enforced": strconv.FormatBool(agentShellSandboxEnforced(runtime.GOOS)),
		"goos":             runtime.GOOS,
	}
	if exitCode >= 0 {
		metadata["exit_code"] = strconv.Itoa(exitCode)
	}

	return metadata
}

func appendAgentShellAuditEvent(root string, event agentproxy.ProviderEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal agent-shell audit event: %w", err)
	}

	err = codeintel.NewEventLog(codeintel.DefaultEventLogDir(root)).Append(
		event.ID,
		[]codeintel.EventRecord{
			{
				ID:            event.ID,
				Kind:          "proxy_event",
				RecordedAtUTC: event.RecordedAtUTC.UTC().Format(time.RFC3339Nano),
				SourceRunID:   event.ID,
				TraceID:       event.TraceID,
				Provider:      event.Provider,
				Tool:          event.Tool,
				CommandShape:  event.InputHash,
				PolicyID:      event.PolicyID,
				Path:          event.TargetPath,
				Payload:       payload,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("append agent-shell audit event: %w", err)
	}

	return nil
}

func agentShellStrategicIntent() string {
	for _, name := range []string{
		"CODING_ETHOS_STRATEGIC_INTENT",
		"GEMINI_STRATEGIC_INTENT",
		"GEMINI_UPDATE_TOPIC",
		"UPDATE_TOPIC",
	} {
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" {
			return value
		}
	}

	return ""
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func runPolicyLintHandler(paths runtimePaths, rest []string) error {
	requirePolicyBundle(paths)
	runtimeExecLint(paths, append([]string{"--bundle", paths.PolicyBundle}, rest...)...)

	return nil
}

func runManagedLintHandler(paths runtimePaths, rest []string) error {
	requirePolicyBundle(paths)

	scope, err := managedLintScopeFromArgs(paths, rest)
	if err != nil {
		return err
	}

	for _, group := range []string{"formatters", "autofixers"} {
		exitCode := runScopedPolicyToolGroupByName(paths, group, true, scope)
		if exitCode != 0 {
			requestRuntimeExit(exitCode)

			return nil
		}
	}

	runtimeExecLint(
		paths,
		append([]string{"--bundle", paths.PolicyBundle, "--code-intel"}, rest...)...,
	)

	return nil
}

func runCISARIFHandler(paths runtimePaths, rest []string) error {
	requirePolicyBundle(paths)

	return runCISARIF(paths, rest)
}

func runPolicyHandler(paths runtimePaths, rest []string) error {
	runtimeExecTool(paths, "coding-ethos-policy", rest...)

	return nil
}

func runCodeIntelHandler(paths runtimePaths, rest []string) error {
	stateRoot := firstNonEmptyString(paths.StateRoot, paths.Root)
	runtimeExecTool(
		paths,
		"coding-ethos-code-intel",
		codeIntelArgs(paths.Root, stateRoot, rest)...)

	return nil
}

func runOutputHandler(paths runtimePaths, rest []string) error {
	runtimeExecTool(
		paths,
		"coding-ethos-output",
		outputArgs(paths.Root, rest)...)

	return nil
}

func runWebGuidanceHandler(paths runtimePaths, rest []string) error {
	runtimeExecTool(
		paths,
		"coding-ethos-web-guidance",
		webGuidanceArgs(paths.Root, rest)...)

	return nil
}

func runPolicyGitHandler(paths runtimePaths, rest []string) error {
	bundlePath := hookPolicyBundlePath(paths)
	requireRuntimeFile(bundlePath, "compiled policy bundle")

	realGitPath := paths.RealGit
	if agentShellNativeGitBindActive(paths) {
		realGitPath = strings.TrimSpace(os.Getenv(realgit.Env))
	} else {
		installGitWrapperShim(paths)
	}

	runtimeExecTool(
		paths,
		"coding-ethos-git",
		append(
			[]string{"--bundle", bundlePath, "--real-git", realGitPath},
			rest...,
		)...)

	return nil
}

func agentShellNativeGitBindActive(paths runtimePaths) bool {
	if runtime.GOOS != linuxGOOS {
		return false
	}

	realGitBind := strings.TrimSpace(os.Getenv(realgit.Env))
	if realGitBind == "" {
		return false
	}

	resolvedBind := filepath.Clean(realGitBind)
	if !executableFile(resolvedBind) {
		debuglog.Debug(
			"agent-shell.git-bind.inactive",
			zap.String("reason", "not_executable"),
			zap.String("path", resolvedBind),
		)

		return false
	}

	cacheRoot := filepath.Join(paths.Root, ".coding-ethos", "cache", "agent-shell")
	if !pathInside(cacheRoot, resolvedBind) {
		debuglog.Debug(
			"agent-shell.git-bind.inactive",
			zap.String("reason", "outside_agent_shell_cache"),
			zap.String("path", resolvedBind),
			zap.String("cache_root", cacheRoot),
		)

		return false
	}

	mountInfo, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		debuglog.Debug(
			"agent-shell.git-bind.inactive",
			zap.String("reason", "read_mountinfo"),
			zap.Error(err),
		)

		return false
	}

	if !readOnlyMountInfoForPath(string(mountInfo), resolvedBind) {
		debuglog.Debug(
			"agent-shell.git-bind.inactive",
			zap.String("reason", "no_readonly_mount"),
			zap.String("path", resolvedBind),
		)

		return false
	}

	return true
}

func executableFile(path string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || !filepath.IsAbs(cleaned) {
		return false
	}

	// #nosec G703 -- cleaned is only probed as an absolute executable path.
	info, err := os.Stat(cleaned)

	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}

	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}

func readOnlyMountInfoForPath(content, path string) bool {
	cleanPath := filepath.Clean(path)

	for line := range strings.Lines(content) {
		fields := strings.Fields(line)
		if len(fields) < mountInfoMinimumFields {
			continue
		}

		mountPoint := strings.ReplaceAll(fields[4], `\040`, " ")
		if filepath.Clean(mountPoint) != cleanPath {
			continue
		}

		options := strings.Split(fields[5], ",")

		return slices.Contains(options, "ro")
	}

	return false
}

func runMCPHandler(paths runtimePaths, rest []string) error {
	runMCP(paths, rest)

	return nil
}

func runAgentHook(paths runtimePaths, rest []string) {
	bundlePath := hookPolicyBundlePath(paths)
	requireRuntimeFile(bundlePath, "compiled policy bundle")
	installGitWrapperShim(paths)
	installLintToolShims(paths)
	persistAgentEnvironment(paths)
	_ = os.Setenv("CODING_ETHOS_GIT_SHIM_DIR", paths.BinDir)
	paths.executor().execAgentHook(
		append(
			[]string{"--bundle", bundlePath, "--json"},
			withoutFlags(rest, "--repo-root", "--state-root")...,
		)...)
}

func runAgentHooksCommand(paths runtimePaths, rest []string) {
	if len(rest) == 0 || rest[0] != agentHookCapabilitiesCommand {
		installGitWrapperShim(paths)
		installLintToolShims(paths)
	}

	settingsRoot := rootFlagValue(rest, paths.Root)
	repoRoot := flagValue(rest, "--repo-root", paths.Root)
	_ = os.Setenv(
		"CODE_ETHOS_CONSUMER_ROOT",
		repoRoot,
	)
	_ = os.Setenv(
		envStateRoot,
		flagValue(rest, "--state-root", settingsRoot),
	)
	runtimeExecTool(
		paths,
		"coding-ethos-agent-hooks",
		agentHooksArgs(paths, rest)...)
}

func runPolicyTool(paths runtimePaths, rest []string) error {
	parentExecutable := ""

	resolvedParent, err := os.Readlink(
		filepath.Join("/proc", strconv.Itoa(os.Getppid()), "exe"),
	)
	if err == nil {
		parentExecutable = resolvedParent
	}

	return runPolicyToolForProtocol(
		paths,
		rest,
		os.Getenv(toolprotocol.ActionlintShellcheckEnv),
		parentExecutable,
	)
}

func runPolicyToolForProtocol(
	paths runtimePaths,
	rest []string,
	protocolMarker string,
	parentExecutable string,
) error {
	if len(rest) == 0 {
		return apperror.StaticError("policy-tool requires a tool name")
	}

	requirePolicyBundle(paths)

	if toolprotocol.IsActionlintShellcheckJSONStdin(
		protocolMarker,
		rest[0],
		rest[1:],
	) {
		if !managedActionlintParent(paths, parentExecutable) {
			return apperror.StaticError(
				"actionlint ShellCheck protocol requires the managed actionlint parent",
			)
		}

		tool, found := toolcatalog.HookOwnedTool("shellcheck")
		if !found {
			return apperror.StaticError("managed shellcheck tool is not registered")
		}

		shellcheck := tool.ManagedExecutablePath(paths.EthosRoot)
		requireRuntimeBinary(shellcheck, "managed shellcheck dependency")
		paths.executor().execPath(shellcheck, rest[1:]...)

		return nil
	}

	runtimeExecLint(paths, policyToolLintArgs(paths, rest[0], rest[1:])...)

	return nil
}

func managedActionlintParent(paths runtimePaths, parentExecutable string) bool {
	tool, found := toolcatalog.HookOwnedTool(toolprotocol.ActionlintTool)
	if !found || strings.TrimSpace(parentExecutable) == "" {
		return false
	}

	expected := tool.ManagedExecutablePath(paths.EthosRoot)

	expectedInfo, err := os.Lstat(expected)
	if err != nil || !expectedInfo.Mode().IsRegular() ||
		expectedInfo.Mode()&0o111 == 0 {
		return false
	}

	parentInfo, err := os.Stat(parentExecutable)
	if err != nil || !parentInfo.Mode().IsRegular() ||
		parentInfo.Mode()&0o111 == 0 {
		return false
	}

	return os.SameFile(expectedInfo, parentInfo)
}

func runMCP(paths runtimePaths, rest []string) {
	bundlePath := hookPolicyBundlePath(paths)
	requireRuntimeFile(bundlePath, "compiled policy bundle")

	rest = withoutFlags(rest, "--repo-root", "--state-root")

	stateRoot := firstNonEmptyString(paths.StateRoot, paths.Root)
	runtimeExecTool(paths, "coding-ethos-mcp", append([]string{
		"--bundle", bundlePath,
		"--cerun", filepath.Join(paths.BinDir, "cerun"),
		"--ethos-root", paths.EthosRoot,
		"--consumer-root", paths.Root,
		"--state-root", stateRoot,
		"--invocation-cwd", paths.InvocationCWD,
	}, rest...)...)
}
