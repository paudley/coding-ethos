// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

// blockPolicyIDPattern finds the policy IDs this package declares for blocks.
var blockPolicyIDPattern = regexp.MustCompile(`PolicyID\s*=\s*"([^"]+)"`)

// TestEveryBlockPolicyIDResolvesInTheBundle keeps a block explainable.
//
// A block names the policy that caused it, and everything an agent does next
// depends on being able to look that name up: policy_explain describes it,
// remediation_explain says what to do instead, and an operator counting blocks
// can tell which rule is costing the most. An ID with no record behind it
// gives none of that -- remediation can only repeat "fix the violation" -- and
// the gap is silent, because enforcement works perfectly well without it.
//
// Four IDs had drifted out of the bundle this way. One lane spent over half
// its refusals on shell.file_tool_emulation and could not see what it was.
func TestEveryBlockPolicyIDResolvesInTheBundle(t *testing.T) {
	t.Parallel()

	root := repoRootForBundle(t)

	bundle, _, err := policy.Compile(policy.CompileOptions{
		Primary: filepath.Join(root, "coding_ethos.yml"),
		Config:  filepath.Join(root, "config.yaml"),
	})
	if err != nil {
		t.Fatalf("compile repo policy bundle: %v", err)
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read hooks package: %v", err)
	}

	seen := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" {
			continue
		}

		source, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}

		for _, match := range blockPolicyIDPattern.FindAllSubmatch(source, -1) {
			seen[string(match[1])] = true
		}
	}

	if len(seen) == 0 {
		t.Fatal("no block policy IDs found; the pattern no longer matches how " +
			"this package declares them, so the check is passing vacuously")
	}

	for id := range seen {
		if _, ok := bundle.Policies[id]; !ok {
			t.Errorf("hook route blocks with policy %q, which is not in the "+
				"bundle: the block cannot be explained, attributed or tuned", id)
		}
	}
}

func repoRootForBundle(tb testing.TB) string {
	tb.Helper()

	dir, err := filepath.Abs(".")
	if err != nil {
		tb.Fatalf("resolve cwd: %v", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "coding_ethos.yml")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("coding_ethos.yml not found above the hooks package")
		}

		dir = parent
	}
}
