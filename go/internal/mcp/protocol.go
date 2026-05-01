// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type requestMessage struct {
	ID      any             `json:"id,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
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
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Name      string          `json:"name"`
}

func readFramedMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		header := strings.TrimRight(line, "\r\n")
		if header == "" {
			break
		}

		name, value, found := strings.Cut(header, ":")
		if !found {
			return nil, fmt.Errorf("invalid MCP header %q", header)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || parsed < 0 {
				return nil, fmt.Errorf("invalid MCP content length %q", value)
			}
			contentLength = parsed
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing MCP Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}

	return payload, nil
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
	Diagnostic lintAdviceInput `json:"diagnostic,omitempty"`
	Command    string          `json:"command,omitempty"`
	Intent     string          `json:"intent,omitempty"`
	Path       string          `json:"path,omitempty"`
	Limit      int             `json:"limit,omitempty"`
}

func writeResponse(
	writer io.Writer,
	id any,
	result any,
	responseErr *rpcError,
) error {
	response := responseMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   responseErr,
	}

	payload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode MCP response: %w", err)
	}

	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return fmt.Errorf("write MCP response header: %w", err)
	}
	if _, err := writer.Write(payload); err != nil {
		return fmt.Errorf("write MCP response: %w", err)
	}

	return nil
}

func initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{
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

func toolDefinitions() []map[string]any {
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
			"lint_check",
			"Run managed lint capture for a named tool, or compiled coding-ethos policy lint checks when no tool is provided.",
			map[string]any{
				"scope":          map[string]any{"type": "string"},
				"tool":           map[string]any{"type": "string"},
				"files":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"argv":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"command":        map[string]any{"type": "string"},
				"cwd":            map[string]any{"type": "string"},
				"admin_approved": map[string]any{"type": "boolean"},
			},
			nil,
			toolMetadata{
				Advisory:       false,
				ExecutesTools:  true,
				ReadsFiles:     true,
				PreferredUse:   "canonical lint path for agents instead of running linters directly",
				TracePersisted: true,
			},
		),
		toolDefinition(
			"lint_advice",
			"Map a linter diagnostic to ETHOS policy, remediation advice, rerun guidance, and skill hints.",
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
			"skill_recommend",
			"Recommend ETHOS-derived skills for a task, command, path, or lint diagnostic.",
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
	return map[string]any{
		"name":        name,
		"description": description,
		"coding_ethos": map[string]any{
			"advisory":        metadata.Advisory,
			"executes_tools":  metadata.ExecutesTools,
			"reads_files":     metadata.ReadsFiles,
			"preferred_use":   metadata.PreferredUse,
			"trace_persisted": metadata.TracePersisted,
		},
		"inputSchema": map[string]any{
			"type":                 "object",
			"properties":           properties,
			"required":             required,
			"additionalProperties": false,
		},
	}
}
