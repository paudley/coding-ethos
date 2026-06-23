// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPublicMCPToolsAreDocumented(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	response := decodeResponse(t, output)
	result := mapValue(t, response["result"])

	var toolNames []string
	for _, toolValue := range listValue(t, result["tools"]) {
		tool := mapValue(t, toolValue)
		name, ok := tool["name"].(string)
		if !ok {
			t.Fatalf("tool name is not a string: %#v", tool)
		}
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	docPath := filepath.Join(mcpGoModuleRoot(t), "..", "docs", "MCP_SERVER.md")
	payload, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read MCP docs: %v", err)
	}

	docs := string(payload)
	var missing []string
	for _, name := range toolNames {
		if !strings.Contains(docs, "`"+name+"`") {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		t.Fatalf("MCP tools missing docs in %s: %s", docPath, strings.Join(missing, ", "))
	}
}
