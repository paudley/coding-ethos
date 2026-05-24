// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

func TestCollectPythonASTFactsRecordsSuppressionCommentContext(t *testing.T) {
	t.Parallel()

	facts, err := collectPythonASTFacts(pythonSource{
		Path: "src/repository.py",
		Text: "class Repository:\n" +
			"    def write_record(self, value):\n" +
			"        self.backend.write(value)  # type: ignore\n",
	})
	if err != nil {
		t.Fatalf("collect python AST facts: %v", err)
	}

	for _, fact := range facts {
		if fact.IsSuppression {
			if fact.SuppressionLabel != "type: ignore" ||
				fact.EnclosingFunction != "write_record" {
				t.Fatalf("suppression fact = %#v", fact)
			}

			return
		}
	}

	t.Fatalf("missing suppression fact: %#v", facts)
}

func TestPythonWalkAllNodesVisitsCommentsWithoutPunctuation(t *testing.T) {
	t.Parallel()

	contents := []byte(
		"def write_record(value):\n" +
			"    return call(value)  # noqa\n",
	)
	tree, found, err := astfacts.Parse("src/repository.py", contents)
	if err != nil {
		t.Fatalf("parse python source: %v", err)
	}
	if !found {
		t.Fatal("missing Python parser")
	}
	defer tree.Close()

	visited := map[string]int{}
	pythonWalkAllNodes(tree.RootNode(), func(node *tree_sitter.Node) {
		visited[node.Kind()]++
	})

	if visited[pythonKindComment] != 1 {
		t.Fatalf(
			"comment visits = %d, want 1; visited=%#v",
			visited[pythonKindComment],
			visited,
		)
	}
	if visited["("] != 0 || visited[")"] != 0 || visited[":"] != 0 {
		t.Fatalf("walk should skip punctuation nodes: %#v", visited)
	}
}

func TestPythonWritePrefixesMatchSuppressionPolicy(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "..", "..", "coding_ethos.yml"))
	if err != nil {
		t.Fatalf("read coding_ethos.yml: %v", err)
	}

	counts := pythonSuppressionPolicyPrefixCounts(t, string(content))
	for _, prefix := range pythonWriteFunctionPrefixes {
		if counts[prefix] != 2 {
			t.Fatalf(
				"policy prefix %q count = %d, want base and wildcard entries; counts=%#v",
				prefix,
				counts[prefix],
				counts,
			)
		}

		delete(counts, prefix)
	}

	if len(counts) != 0 {
		t.Fatalf("policy has prefixes not recognized by Go helper: %#v", counts)
	}
}

func pythonSuppressionPolicyPrefixCounts(t *testing.T, content string) map[string]int {
	t.Helper()

	policyStart := strings.Index(content, "id: python.suppression_in_write_method")
	if policyStart < 0 {
		t.Fatal("missing python suppression policy")
	}

	globStart := strings.Index(content[policyStart:], "any_glob_match(")
	if globStart < 0 {
		t.Fatal("missing any_glob_match in python suppression policy")
	}

	listStart := strings.Index(content[policyStart+globStart:], "[")
	listEnd := strings.Index(content[policyStart+globStart+listStart:], "]")
	if listStart < 0 || listEnd < 0 {
		t.Fatal("missing suppression policy glob list")
	}

	absoluteListStart := policyStart + globStart + listStart
	list := content[absoluteListStart : absoluteListStart+listEnd]
	matches := regexp.MustCompile(`"([a-z]+)(?:_\*)?"`).FindAllStringSubmatch(list, -1)
	counts := map[string]int{}
	for _, match := range matches {
		counts[match[1]]++
	}

	return counts
}
