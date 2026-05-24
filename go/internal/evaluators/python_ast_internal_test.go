// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import "testing"

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
