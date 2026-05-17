// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/celexpr"
)

func FuzzActivationShellAndProposedFileChange(f *testing.F) {
	for _, seed := range []struct {
		command string
		tool    string
		oldText string
		newText string
	}{
		{"git add file.py", "Bash", "alpha\n", "alpha\nbeta\n"},
		{"FILE=.claude/settings.json cat > ${FILE}", "Write", "", "memory\n"},
		{"git commit -m subject -m body", "Edit", "old\n", "new\n"},
		{"python - <<'PY'\nprint('hello')\nPY", "MultiEdit", "same\n", "same\nmore\n"},
		{"unterminated 'quote", "Edit", "old\n", "new\n"},
	} {
		f.Add(seed.command, seed.tool, seed.oldText, seed.newText)
	}

	f.Fuzz(func(t *testing.T, command, tool, oldText, newText string) {
		if len(command) > 4096 || len(oldText) > 4096 || len(newText) > 4096 {
			t.Skip("bounded fuzz input size")
		}

		activation := Activation(ActivationInput{
			Command:    command,
			Content:    newText,
			OldContent: oldText,
			Files:      []string{"pkg/app.py"},
			Tool:       tool,
			SourceRoots: []string{
				"pkg",
			},
			ProtectedPaths: []string{".git/**", "pre-commit/hooks/**"},
		})
		if activation["command_fact"] == nil {
			t.Fatal("activation missing command facts")
		}

		if activation["shell_commands"] == nil {
			t.Fatal("activation missing shell command facts")
		}

		if activation["proposed_file_changes"] == nil {
			t.Fatal("activation missing proposed file change facts")
		}
	})
}
