// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
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

func TestCollectPythonASTFactsSkipsNonSuppressionComments(t *testing.T) {
	t.Parallel()

	facts, err := collectPythonASTFacts(pythonSource{
		Path: "src/repository.py",
		Text: "def write_record(value):\n" +
			"    value = value + 1  # noquality marker\n" +
			"    return value  # ordinary comment\n",
	})
	if err != nil {
		t.Fatalf("collect python AST facts: %v", err)
	}

	for _, fact := range facts {
		if fact.NodeKind == pythonKindComment || fact.IsSuppression {
			t.Fatalf("non-suppression comments should not emit facts: %#v", facts)
		}
	}
}
