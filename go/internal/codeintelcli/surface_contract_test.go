// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestPublicCodeIntelCommandsAreDocumented(t *testing.T) {
	t.Parallel()

	var commands []string
	for name := range commandHandlers() {
		commands = append(commands, name)
	}
	sort.Strings(commands)

	docPath := filepath.Join(codeIntelRepoRoot(t), "docs", "CODE_INTEL.md")
	payload, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read code-intel docs: %v", err)
	}

	docs := string(payload)
	var missing []string
	for _, command := range commands {
		if !strings.Contains(docs, "`"+command+"`") &&
			!strings.Contains(docs, "code-intel "+command) {
			missing = append(missing, command)
		}
	}

	if len(missing) > 0 {
		t.Fatalf(
			"code-intel commands missing docs in %s: %s",
			docPath,
			strings.Join(missing, ", "),
		)
	}
}

func codeIntelRepoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
