// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

type messageFraming string

const (
	framingContentLength messageFraming = "content-length"
	framingJSONLine      messageFraming = "json-line"
)

type requestMessage struct {
	ID      any             `json:"id,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type responseMessage struct {
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
	JSONRPC string    `json:"jsonrpc"`
}

type rpcError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type resourceReadParams struct {
	URI string `json:"uri"`
}

func toolText(parts ...string) string {
	return strings.Join(parts, " ")
}

func readMessage(reader *bufio.Reader) ([]byte, messageFraming, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, "", fmt.Errorf("read MCP message header: %w", err)
	}

	header := strings.TrimRight(line, "\r\n")
	if header == "" {
		return nil, "", apperror.StaticError("empty MCP message")
	}

	if !strings.HasPrefix(
		strings.ToLower(strings.TrimSpace(header)),
		"content-length:",
	) {
		return []byte(header), framingJSONLine, nil
	}

	contentLength, err := readContentLengthHeaders(reader, header)
	if err != nil {
		return nil, "", err
	}

	if contentLength < 0 {
		return nil, "", apperror.StaticError("missing MCP Content-Length header")
	}

	payload := make([]byte, contentLength)

	_, inlineErrA := io.ReadFull(reader, payload)
	if inlineErrA != nil {
		return nil, "", fmt.Errorf("read MCP message payload: %w", inlineErrA)
	}

	return payload, framingContentLength, nil
}

func readContentLengthHeaders(reader *bufio.Reader, header string) (int, error) {
	contentLength := -1

	for header != "" {
		nextLength, err := parseContentLengthHeader(header)
		if err != nil {
			return -1, err
		}

		if nextLength >= 0 {
			contentLength = nextLength
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			return -1, fmt.Errorf("read MCP message header: %w", err)
		}

		header = strings.TrimRight(line, "\r\n")
	}

	return contentLength, nil
}

func parseContentLengthHeader(header string) (int, error) {
	name, value, found := strings.Cut(header, ":")
	if !found {
		return -1, apperror.Wrapf(
			apperror.StaticError("invalid MCP header %q"),
			"invalid MCP header %q",
			header,
		)
	}

	if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
		return -1, nil
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return -1, apperror.Wrapf(
			apperror.StaticError("invalid MCP content length %q"),
			"invalid MCP content length %q",
			value,
		)
	}

	return parsed, nil
}

type commandCheckInput struct {
	Command  string `json:"command"`
	Cwd      string `json:"cwd,omitempty"`
	Provider string `json:"provider,omitempty"`
}

type editCheckInput struct {
	After    string `json:"after"`
	Before   string `json:"before,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
	Path     string `json:"path"`
	Provider string `json:"provider,omitempty"`
}

type lintCheckInput struct {
	Command       string   `json:"command,omitempty"`
	Cwd           string   `json:"cwd,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	Tool          string   `json:"tool,omitempty"`
	Files         []string `json:"files,omitempty"`
	Argv          []string `json:"argv,omitempty"`
	AdminApproved bool     `json:"admin_approved,omitempty"`
}

type lintAdviceInput struct {
	Code     string `json:"code,omitempty"`
	File     string `json:"file,omitempty"`
	Message  string `json:"message"`
	Severity string `json:"severity,omitempty"`
	Tool     string `json:"tool"`
	Column   int    `json:"column,omitempty"`
	Line     int    `json:"line,omitempty"`
}

type policyExplainInput struct {
	PolicyID string `json:"policy_id"`
}

type skillLookupInput struct {
	SkillID string `json:"skill_id"`
}

type skillRecommendInput struct {
	Command    string          `json:"command,omitempty"`
	Intent     string          `json:"intent,omitempty"`
	Path       string          `json:"path,omitempty"`
	Diagnostic lintAdviceInput `json:"diagnostic,omitzero"`
	Limit      int             `json:"limit,omitempty"`
}

type remediationExplainInput struct {
	Message      string               `json:"message,omitempty"`
	SkillID      string               `json:"skill_id,omitempty"`
	Command      string               `json:"command,omitempty"`
	FailedAction string               `json:"failed_action,omitempty"`
	File         string               `json:"file,omitempty"`
	ID           string               `json:"id,omitempty"`
	PolicyID     string               `json:"policy_id,omitempty"`
	Path         string               `json:"path,omitempty"`
	Code         string               `json:"code,omitempty"`
	Severity     string               `json:"severity,omitempty"`
	Tool         string               `json:"tool,omitempty"`
	Remediation  agentmsg.Remediation `json:"remediation,omitzero"`
	Column       int                  `json:"column,omitempty"`
	Line         int                  `json:"line,omitempty"`
}

type sarifRemediationInput struct {
	SARIF       string `json:"sarif"`
	TraceID     string `json:"trace_id,omitempty"`
	ResultIndex int    `json:"result_index,omitempty"`
}

type sarifRiskSummaryInput struct {
	SARIF   string `json:"sarif,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

type sarifTrendInput struct {
	BaselineSARIF   string   `json:"baseline_sarif,omitempty"`
	CurrentSARIF    string   `json:"current_sarif,omitempty"`
	BaselineTraceID string   `json:"baseline_trace_id,omitempty"`
	CurrentTraceID  string   `json:"current_trace_id,omitempty"`
	HistorySARIF    []string `json:"history_sarif,omitempty"`
	HistoryTraceIDs []string `json:"history_trace_ids,omitempty"`
}

type sarifPolicyFeedbackInput struct {
	SARIF   string `json:"sarif,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

type codeIntelSearchInput struct {
	Filters    map[string]string `json:"filters,omitempty"`
	Text       string            `json:"text,omitempty"`
	Query      string            `json:"query,omitempty"`
	RecordKind string            `json:"record_kind,omitempty"`
	Collection string            `json:"collection,omitempty"`
	ModelID    string            `json:"model_id,omitempty"`
	PolicyID   string            `json:"policy_id,omitempty"`
	SkillID    string            `json:"skill_id,omitempty"`
	Path       string            `json:"path,omitempty"`
	Vector     []float32         `json:"vector,omitempty"`
	Limit      int               `json:"limit,omitempty"`
}

type codeIntelIndexStatusInput struct {
	Collection string `json:"collection,omitempty"`
	ModelID    string `json:"model_id,omitempty"`
}

type codeIntelHookUsageInput struct {
	Provider      string `json:"provider,omitempty"`
	Status        string `json:"status,omitempty"`
	PolicyID      string `json:"policy_id,omitempty"`
	SkillID       string `json:"skill_id,omitempty"`
	OperationKind string `json:"operation_kind,omitempty"`
	TargetKind    string `json:"target_kind,omitempty"`
	RiskCategory  string `json:"risk_category,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type codeIntelIndexCodeInput struct {
	Paths []string `json:"paths,omitempty"`
}

type codeIntelEmbeddingCandidatesInput struct {
	RecordKind string `json:"record_kind,omitempty"`
	PolicyID   string `json:"policy_id,omitempty"`
	SkillID    string `json:"skill_id,omitempty"`
	Path       string `json:"path,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type codeIntelCodeChunksInput struct {
	Path       string `json:"path,omitempty"`
	Language   string `json:"language,omitempty"`
	SymbolKind string `json:"symbol_kind,omitempty"`
	SymbolName string `json:"symbol_name,omitempty"`
	SymbolPath string `json:"symbol_path,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type codeIntelCodeContextInput struct {
	ChunkID    string `json:"chunk_id,omitempty"`
	Path       string `json:"path,omitempty"`
	SymbolPath string `json:"symbol_path,omitempty"`
	Line       int    `json:"line,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type codeIntelRepoMapInput struct {
	Path           string `json:"path,omitempty"`
	Language       string `json:"language,omitempty"`
	Format         string `json:"format,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	SymbolsPerFile int    `json:"symbols_per_file,omitempty"`
}

type codeSimilarityCheckInput struct {
	Code      string  `json:"code"`
	Path      string  `json:"path,omitempty"`
	Language  string  `json:"language,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
	Limit     int     `json:"limit,omitempty"`
}

func writeResponse(
	writer io.Writer,
	framing messageFraming,
	requestID any,
	result any,
	responseErr *rpcError,
) error {
	response := responseMessage{
		JSONRPC: "2.0",
		ID:      requestID,
		Result:  result,
		Error:   responseErr,
	}

	payload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode MCP response: %w", err)
	}

	if framing == framingJSONLine {
		_, inlineErrB := writer.Write(append(payload, '\n'))
		if inlineErrB != nil {
			return fmt.Errorf("write MCP response: %w", inlineErrB)
		}

		return nil
	}

	_, inlineErrC := fmt.Fprintf(
		writer,
		"Content-Length: %d\r\n\r\n",
		len(payload),
	)
	if inlineErrC != nil {
		return fmt.Errorf("write MCP response header: %w", inlineErrC)
	}

	_, inlineErrD := writer.Write(payload)
	if inlineErrD != nil {
		return fmt.Errorf("write MCP response: %w", inlineErrD)
	}

	return nil
}

func initializeResult(params json.RawMessage) map[string]any {
	version := protocolVersion

	var parsed map[string]string

	err := json.Unmarshal(params, &parsed)
	if err == nil && strings.TrimSpace(parsed["protocolVersion"]) != "" {
		version = strings.TrimSpace(parsed["protocolVersion"])
	}

	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools": map[string]any{
				"listChanged": false,
			},
			"resources": map[string]any{
				"listChanged": false,
			},
		},
		"serverInfo": map[string]any{
			"name":    "coding-ethos",
			"version": "0.1.0",
		},
	}
}

func toolResult(result any) map[string]any {
	payload, err := json.Marshal(result)
	if err != nil {
		payload = []byte(`{"error":"encode tool result"}`)
	}

	return map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": string(payload),
		}},
		"structuredContent": result,
	}
}

const (
	toolDefinitionCapacity          = 25
	codeIntelToolDefinitionCapacity = 11
)

func toolDefinitions() []map[string]any {
	definitions := make([]map[string]any, 0, toolDefinitionCapacity)
	definitions = append(definitions, policyPreflightToolDefinitions()...)
	definitions = append(definitions, lintToolDefinitions()...)
	definitions = append(definitions, sarifToolDefinitions()...)
	definitions = append(definitions, skillToolDefinitions()...)
	definitions = append(definitions, codeIntelToolDefinitions()...)

	return definitions
}

func resourceDefinitions() []map[string]any {
	return []map[string]any{
		{
			"uri":         repoMapResourceURI,
			"name":        "coding_ethos_repo_map",
			"description": "Compact repository-wide AST map for session orientation.",
			"mimeType":    "text/vnd.coding-ethos.toon",
		},
	}
}

func policyPreflightToolDefinitions() []map[string]any {
	return []map[string]any{
		toolDefinition(
			"policy_check_command",
			"Check whether a proposed shell command would violate compiled coding-ethos policy.",
			map[string]any{
				"command":  map[string]any{"type": "string"},
				"cwd":      map[string]any{"type": "string"},
				"provider": map[string]any{"type": "string"},
			},
			[]string{"command"},
			toolMetadata{
				Advisory:       true,
				ExecutesTools:  false,
				ReadsFiles:     false,
				PreferredUse:   "preflight shell commands before Bash",
				TracePersisted: false,
			},
		),
		toolDefinition(
			"policy_check_edit",
			"Check whether a proposed file edit would violate compiled coding-ethos policy.",
			map[string]any{
				"path":     map[string]any{"type": "string"},
				"before":   map[string]any{"type": "string"},
				"after":    map[string]any{"type": "string"},
				"cwd":      map[string]any{"type": "string"},
				"provider": map[string]any{"type": "string"},
			},
			[]string{"path", "after"},
			toolMetadata{
				Advisory:       true,
				ExecutesTools:  false,
				ReadsFiles:     false,
				PreferredUse:   "preflight generated or high-risk edits before writing",
				TracePersisted: false,
			},
		),
		toolDefinition(
			"policy_explain",
			"Explain a compiled policy, including ETHOS grounding and CEL details when present.",
			map[string]any{
				"policy_id": map[string]any{"type": "string"},
			},
			[]string{"policy_id"},
			toolMetadata{
				Advisory:       true,
				ExecutesTools:  false,
				ReadsFiles:     false,
				PreferredUse:   "understand a policy before changing related code",
				TracePersisted: false,
			},
		),
	}
}

func lintToolDefinitions() []map[string]any {
	return []map[string]any{
		toolDefinition(
			"lint_check",
			toolText(
				"Run managed lint capture for a named tool, or compiled",
				"coding-ethos policy lint checks when no tool is provided.",
			),
			map[string]any{
				"scope": map[string]any{"type": "string"},
				"tool":  map[string]any{"type": "string"},
				"files": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
				"argv": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
				"command":        map[string]any{"type": "string"},
				"cwd":            map[string]any{"type": "string"},
				"admin_approved": map[string]any{"type": "boolean"},
			},
			nil,
			toolMetadata{
				Advisory:      false,
				ExecutesTools: true,
				ReadsFiles:    true,
				PreferredUse: toolText(
					"canonical lint path for agents instead of running",
					"linters directly",
				),
				TracePersisted: true,
			},
		),
		toolDefinition(
			"lint_advice",
			toolText(
				"Map a linter diagnostic to ETHOS policy, remediation advice,",
				"rerun guidance, and skill hints.",
			),
			map[string]any{
				"tool":     map[string]any{"type": "string"},
				"code":     map[string]any{"type": "string"},
				"file":     map[string]any{"type": "string"},
				"line":     map[string]any{"type": "integer"},
				"column":   map[string]any{"type": "integer"},
				"severity": map[string]any{"type": "string"},
				"message":  map[string]any{"type": "string"},
			},
			[]string{"tool", "message"},
			toolMetadata{
				Advisory:       true,
				ExecutesTools:  false,
				ReadsFiles:     false,
				PreferredUse:   "turn an existing lint finding into repair guidance",
				TracePersisted: false,
			},
		),
	}
}

func sarifToolDefinitions() []map[string]any {
	return []map[string]any{
		sarifRemediationAdviceToolDefinition(),
		sarifRiskSummaryToolDefinition(),
		sarifTrendAnalysisToolDefinition(),
		sarifPolicyFeedbackToolDefinition(),
	}
}

func sarifRemediationAdviceToolDefinition() map[string]any {
	return toolDefinition(
		"sarif_remediation_advice",
		"Turn a SARIF result into ETHOS-grounded repair guidance for an agent.",
		map[string]any{
			"sarif":        map[string]any{"type": "string"},
			"trace_id":     map[string]any{"type": "string"},
			"result_index": map[string]any{"type": "integer"},
		},
		nil,
		toolMetadata{
			Advisory:      true,
			ExecutesTools: false,
			ReadsFiles:    false,
			PreferredUse: toolText(
				"repair a code-scanning or SARIF finding without",
				"rerunning lint first",
			),
			TracePersisted: false,
		},
	)
}

func sarifRiskSummaryToolDefinition() map[string]any {
	return toolDefinition(
		"sarif_risk_summary",
		toolText(
			"Summarize a SARIF run into compact policy, skill, tool, file,",
			"and finding-group risk signals.",
		),
		map[string]any{
			"sarif":    map[string]any{"type": "string"},
			"trace_id": map[string]any{"type": "string"},
		},
		nil,
		toolMetadata{
			Advisory:       true,
			ExecutesTools:  false,
			ReadsFiles:     false,
			PreferredUse:   "triage a SARIF run before choosing remediation order",
			TracePersisted: false,
		},
	)
}

func sarifTrendAnalysisToolDefinition() map[string]any {
	return toolDefinition(
		"sarif_trend_analysis",
		toolText(
			"Compare two SARIF runs and identify introduced, fixed, and",
			"persisting findings.",
		),
		sarifTrendAnalysisInputSchema(),
		nil,
		toolMetadata{
			Advisory:      true,
			ExecutesTools: false,
			ReadsFiles:    false,
			PreferredUse: toolText(
				"prioritize introduced or reopened SARIF findings over",
				"historical noise",
			),
			TracePersisted: false,
		},
	)
}

func sarifTrendAnalysisInputSchema() map[string]any {
	return map[string]any{
		"baseline_sarif":    map[string]any{"type": "string"},
		"current_sarif":     map[string]any{"type": "string"},
		"baseline_trace_id": map[string]any{"type": "string"},
		"current_trace_id":  map[string]any{"type": "string"},
		"history_sarif": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
		"history_trace_ids": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
	}
}

func sarifPolicyFeedbackToolDefinition() map[string]any {
	return toolDefinition(
		"sarif_policy_feedback",
		toolText(
			"Report unmapped, noisy, weakly mapped, or under-advised",
			"SARIF diagnostics for policy authors.",
		),
		map[string]any{
			"sarif":    map[string]any{"type": "string"},
			"trace_id": map[string]any{"type": "string"},
		},
		nil,
		toolMetadata{
			Advisory:      true,
			ExecutesTools: false,
			ReadsFiles:    false,
			PreferredUse: toolText(
				"improve evidence maps, skill linkage, and severity",
				"mappings after a SARIF run",
			),
			TracePersisted: false,
		},
	)
}

func skillToolDefinitions() []map[string]any {
	return []map[string]any{
		toolDefinition(
			"tool_capabilities",
			toolText(
				"List managed tool sandbox capabilities, tags, resource limits,",
				"and network/Git posture.",
			),
			map[string]any{},
			nil,
			toolMetadata{
				Advisory:      true,
				ExecutesTools: false,
				ReadsFiles:    false,
				PreferredUse: toolText(
					"choose MCP lint_check over direct linter execution and",
					"inspect sandbox posture",
				),
				TracePersisted: false,
			},
		),
		toolDefinition(
			"skill_lookup",
			"Return ETHOS-derived skill guidance for a generated coding-ethos skill ID.",
			map[string]any{
				"skill_id": map[string]any{"type": "string"},
			},
			[]string{"skill_id"},
			toolMetadata{
				Advisory:       true,
				ExecutesTools:  false,
				ReadsFiles:     false,
				PreferredUse:   "load the full remediation playbook for a known skill",
				TracePersisted: false,
			},
		),
		toolDefinition(
			"remediation_explain",
			toolText(
				"Expand an agent_remediation payload into policy, principle,",
				"skill, and retry guidance.",
			),
			map[string]any{
				"remediation": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
				"id":            map[string]any{"type": "string"},
				"policy_id":     map[string]any{"type": "string"},
				"skill_id":      map[string]any{"type": "string"},
				"message":       map[string]any{"type": "string"},
				"failed_action": map[string]any{"type": "string"},
				"command":       map[string]any{"type": "string"},
				"file":          map[string]any{"type": "string"},
				"path":          map[string]any{"type": "string"},
				"tool":          map[string]any{"type": "string"},
				"code":          map[string]any{"type": "string"},
				"severity":      map[string]any{"type": "string"},
				"line":          map[string]any{"type": "integer"},
				"column":        map[string]any{"type": "integer"},
			},
			nil,
			toolMetadata{
				Advisory:      true,
				ExecutesTools: false,
				ReadsFiles:    false,
				PreferredUse: toolText(
					"turn an emitted agent_remediation item into grounded",
					"next-action guidance",
				),
				TracePersisted: false,
			},
		),
	}
}

func codeIntelToolDefinitions() []map[string]any {
	definitions := make([]map[string]any, 0, codeIntelToolDefinitionCapacity)
	definitions = append(definitions, codeIntelSearchToolDefinitions()...)
	definitions = append(definitions, codeIntelHookToolDefinitions()...)
	definitions = append(definitions, codeIntelCodeToolDefinitions()...)
	definitions = append(definitions, codeIntelEmbeddingToolDefinitions()...)

	return definitions
}

func codeIntelSearchToolDefinitions() []map[string]any {
	return []map[string]any{
		codeIntelSearchToolDefinition(),
		semanticSearchToolDefinition(),
		toolDefinition(
			"code_intel_index_status",
			toolText(
				"Report code-intelligence store freshness, embedding",
				"metadata counts, and sqlite-vec row counts.",
			),
			map[string]any{
				"collection": map[string]any{"type": "string"},
				"model_id":   map[string]any{"type": "string"},
			},
			nil,
			toolMetadata{
				Advisory:      true,
				ExecutesTools: false,
				ReadsFiles:    true,
				PreferredUse: toolText(
					"check whether remediation and SARIF retrieval has",
					"fresh vector coverage",
				),
				TracePersisted: false,
			},
		),
	}
}

func codeIntelSearchToolDefinition() map[string]any {
	return toolDefinition(
		"code_intel_search",
		toolText(
			"Search stored remediation, SARIF, policy, and embedding",
			"evidence with FTS plus sqlite-vec when a query vector is",
			"supplied.",
		),
		map[string]any{
			"text":        map[string]any{"type": "string"},
			"query":       aliasSchema("text"),
			"record_kind": map[string]any{"type": "string"},
			"vector":      vectorSchema(),
			"collection":  map[string]any{"type": "string"},
			"model_id":    map[string]any{"type": "string"},
			"policy_id":   map[string]any{"type": "string"},
			"skill_id":    map[string]any{"type": "string"},
			"path":        map[string]any{"type": "string"},
			"limit":       map[string]any{"type": "integer"},
			"filters":     stringMapSchema(),
		},
		nil,
		toolMetadata{
			Advisory:      true,
			ExecutesTools: false,
			ReadsFiles:    true,
			PreferredUse: toolText(
				"retrieve prior fixes, related SARIF findings, and",
				"policy evidence before broad file reads",
			),
			TracePersisted: false,
		},
	)
}

func semanticSearchToolDefinition() map[string]any {
	return toolDefinition(
		"semantic_search",
		toolText(
			"Search indexed repository code semantically and return",
			"exact code chunks with path and line metadata.",
		),
		map[string]any{
			"query":      map[string]any{"type": "string"},
			"text":       aliasSchema("query"),
			"vector":     vectorSchema(),
			"collection": map[string]any{"type": "string"},
			"model_id":   map[string]any{"type": "string"},
			"path":       map[string]any{"type": "string"},
			"limit":      map[string]any{"type": "integer"},
			"filters":    stringMapSchema(),
		},
		[]string{"query"},
		toolMetadata{
			Advisory:      true,
			ExecutesTools: false,
			ReadsFiles:    true,
			PreferredUse: toolText(
				"find exact indexed code blocks before broad grep or",
				"repo-wide file reads",
			),
			TracePersisted: false,
		},
	)
}

func aliasSchema(name string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "Alias for " + name + ".",
	}
}

func vectorSchema() map[string]any {
	return map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "number"},
	}
}

func stringMapSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": map[string]any{"type": "string"},
	}
}

func codeIntelHookToolDefinitions() []map[string]any {
	return []map[string]any{
		toolDefinition(
			"code_intel_hook_usage",
			toolText(
				"Summarize normalized hook usage by provider, operation,",
				"target, risk, status, policy, and skill.",
			),
			map[string]any{
				"provider":       map[string]any{"type": "string"},
				"status":         map[string]any{"type": "string"},
				"policy_id":      map[string]any{"type": "string"},
				"skill_id":       map[string]any{"type": "string"},
				"operation_kind": map[string]any{"type": "string"},
				"target_kind":    map[string]any{"type": "string"},
				"risk_category":  map[string]any{"type": "string"},
				"limit":          map[string]any{"type": "integer"},
			},
			nil,
			toolMetadata{
				Advisory:      true,
				ExecutesTools: false,
				ReadsFiles:    true,
				PreferredUse: toolText(
					"identify recurring hook friction, bypass attempts,",
					"rewrites, and remediation opportunities",
				),
				TracePersisted: false,
			},
		),
	}
}

func codeIntelCodeToolDefinitions() []map[string]any {
	return []map[string]any{
		codeSimilarityToolDefinition(),
		codeIntelRepoMapToolDefinition(),
		toolDefinition(
			"code_intel_index_code",
			toolText(
				"Parse repository code with Tree-sitter and persist",
				"symbol-level code chunks for search and embedding.",
			),
			map[string]any{
				"paths": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			nil,
			toolMetadata{
				Advisory:      true,
				ExecutesTools: false,
				ReadsFiles:    true,
				PreferredUse: toolText(
					"refresh symbol-level code context before asking for",
					"related code or remediation history",
				),
				TracePersisted: true,
			},
		),
		toolDefinition(
			"code_intel_code_chunks",
			toolText(
				"Return Tree-sitter code chunks filtered by path, language,",
				"symbol kind, or symbol name.",
			),
			map[string]any{
				"path":        map[string]any{"type": "string"},
				"language":    map[string]any{"type": "string"},
				"symbol_kind": map[string]any{"type": "string"},
				"symbol_name": map[string]any{"type": "string"},
				"symbol_path": map[string]any{"type": "string"},
				"limit":       map[string]any{"type": "integer"},
			},
			nil,
			toolMetadata{
				Advisory:      true,
				ExecutesTools: false,
				ReadsFiles:    true,
				PreferredUse: toolText(
					"retrieve focused symbol-level code context before",
					"reading whole files",
				),
				TracePersisted: false,
			},
		),
		toolDefinition(
			"code_intel_code_context",
			toolText(
				"Expand a Tree-sitter code chunk into parent, children,",
				"graph edges, and linked SARIF/CEL findings.",
			),
			map[string]any{
				"chunk_id":    map[string]any{"type": "string"},
				"path":        map[string]any{"type": "string"},
				"symbol_path": map[string]any{"type": "string"},
				"line":        map[string]any{"type": "integer"},
				"limit":       map[string]any{"type": "integer"},
			},
			nil,
			toolMetadata{
				Advisory:      true,
				ExecutesTools: false,
				ReadsFiles:    true,
				PreferredUse: toolText(
					"expand a known symbol into related context before",
					"broad code reads",
				),
				TracePersisted: false,
			},
		),
	}
}

func codeIntelRepoMapToolDefinition() map[string]any {
	return toolDefinition(
		"code_intel_repo_map",
		toolText(
			"Return a compact repository-wide AST map with ranked files,",
			"important symbols, and signatures for startup orientation.",
		),
		map[string]any{
			"path":             map[string]any{"type": "string"},
			"language":         map[string]any{"type": "string"},
			"limit":            map[string]any{"type": "integer"},
			"symbols_per_file": map[string]any{"type": "integer"},
			"format":           map[string]any{"type": "string"},
		},
		nil,
		toolMetadata{
			Advisory:      true,
			ExecutesTools: false,
			ReadsFiles:    true,
			PreferredUse: toolText(
				"orient a session before broad directory listings or",
				"whole-file reads",
			),
			TracePersisted: false,
		},
	)
}

func codeSimilarityToolDefinition() map[string]any {
	return toolDefinition(
		"code_similarity_check",
		toolText(
			"Check whether proposed code is structurally similar to",
			"indexed repository symbols using normalized hashes and",
			"MinHash LSH.",
		),
		map[string]any{
			"code":      map[string]any{"type": "string"},
			"path":      map[string]any{"type": "string"},
			"language":  map[string]any{"type": "string"},
			"threshold": map[string]any{"type": "number"},
			"limit":     map[string]any{"type": "integer"},
		},
		[]string{"code"},
		toolMetadata{
			Advisory:      true,
			ExecutesTools: false,
			ReadsFiles:    true,
			PreferredUse: toolText(
				"preflight generated code before writing a duplicate",
				"implementation",
			),
			TracePersisted: false,
		},
	)
}

func codeIntelEmbeddingToolDefinitions() []map[string]any {
	return []map[string]any{
		toolDefinition(
			"code_intel_embedding_candidates",
			toolText(
				"Return compact SARIF and remediation records that are",
				"ready to embed and write into sqlite-vec.",
			),
			map[string]any{
				"record_kind": map[string]any{"type": "string"},
				"policy_id":   map[string]any{"type": "string"},
				"skill_id":    map[string]any{"type": "string"},
				"path":        map[string]any{"type": "string"},
				"limit":       map[string]any{"type": "integer"},
			},
			nil,
			toolMetadata{
				Advisory:      true,
				ExecutesTools: false,
				ReadsFiles:    true,
				PreferredUse: toolText(
					"feed an approved embedding producer with traceable",
					"SARIF/remediation text",
				),
				TracePersisted: false,
			},
		),
		toolDefinition(
			"skill_recommend",
			toolText(
				"Recommend ETHOS-derived skills for a task, command, path,",
				"or lint diagnostic.",
			),
			map[string]any{
				"intent":  map[string]any{"type": "string"},
				"command": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string"},
				"limit":   map[string]any{"type": "integer"},
				"diagnostic": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tool":     map[string]any{"type": "string"},
						"code":     map[string]any{"type": "string"},
						"file":     map[string]any{"type": "string"},
						"line":     map[string]any{"type": "integer"},
						"column":   map[string]any{"type": "integer"},
						"severity": map[string]any{"type": "string"},
						"message":  map[string]any{"type": "string"},
					},
					"additionalProperties": false,
				},
			},
			nil,
			toolMetadata{
				Advisory:       true,
				ExecutesTools:  false,
				ReadsFiles:     false,
				PreferredUse:   "choose the relevant skill before starting or repairing work",
				TracePersisted: false,
			},
		),
	}
}

type toolMetadata struct {
	PreferredUse   string
	Advisory       bool
	ExecutesTools  bool
	ReadsFiles     bool
	TracePersisted bool
}

func toolDefinition(
	name string,
	description string,
	properties map[string]any,
	required []string,
	metadata toolMetadata,
) map[string]any {
	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		inputSchema["required"] = required
	}

	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": inputSchema,
		"annotations": map[string]any{
			"readOnlyHint":    !metadata.ExecutesTools && !metadata.TracePersisted,
			"destructiveHint": false,
			"idempotentHint":  metadata.Advisory,
			"openWorldHint":   metadata.ExecutesTools,
		},
		"_meta": map[string]any{
			"coding_ethos": map[string]any{
				"advisory":        metadata.Advisory,
				"executes_tools":  metadata.ExecutesTools,
				"reads_files":     metadata.ReadsFiles,
				"preferred_use":   metadata.PreferredUse,
				"trace_persisted": metadata.TracePersisted,
			},
		},
	}
}
