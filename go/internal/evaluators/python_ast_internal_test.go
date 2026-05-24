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

func TestCollectPythonASTFactsRecordsOptionalReturn(t *testing.T) {
	t.Parallel()

	fact := firstPythonASTFact(
		t,
		"def load_service() -> Service | None:\n    return None\n",
		func(fact pythonASTFact) bool { return fact.IsOptionalReturn },
	)

	if fact.SymbolName != "load_service" ||
		fact.ReturnAnnotation != "Service | None" {
		t.Fatalf("optional return fact = %#v", fact)
	}
}

func TestCollectPythonASTFactsRecordsSilentException(t *testing.T) {
	t.Parallel()

	fact := firstPythonASTFact(
		t,
		"try:\n    run()\nexcept RuntimeError:\n    return None\n",
		func(fact pythonASTFact) bool { return fact.IsSilentExcept },
	)

	if fact.ExceptionType != "RuntimeError" ||
		fact.ExceptionAction != "return None" {
		t.Fatalf("silent exception fact = %#v", fact)
	}
}

func TestCollectPythonASTFactsRecordsStructuredLoggerCall(t *testing.T) {
	t.Parallel()

	fact := firstPythonASTFact(
		t,
		"logger.info(f'user={user_id}')\n",
		func(fact pythonASTFact) bool { return fact.IsStructuredLog },
	)

	if fact.LoggerName != "logger" || fact.LoggerMethod != "info" {
		t.Fatalf("structured log fact = %#v", fact)
	}
}

func TestCollectPythonASTFactsRecordsUnexplainedTypeIgnore(t *testing.T) {
	t.Parallel()

	fact := firstPythonASTFact(
		t,
		"value = load()  # type: ignore\n",
		func(fact pythonASTFact) bool { return fact.IsUnexplainedTypeIgnore },
	)

	if fact.SuppressionLabel != "type: ignore" {
		t.Fatalf("type ignore fact = %#v", fact)
	}
}

func firstPythonASTFact(
	t *testing.T,
	text string,
	match func(pythonASTFact) bool,
) pythonASTFact {
	t.Helper()

	facts, err := collectPythonASTFacts(pythonSource{
		Path: "src/app.py",
		Text: text,
	})
	if err != nil {
		t.Fatalf("collect python AST facts: %v", err)
	}

	for _, fact := range facts {
		if match(fact) {
			return fact
		}
	}

	t.Fatalf("missing matching fact: %#v", facts)

	return pythonASTFact{}
}
