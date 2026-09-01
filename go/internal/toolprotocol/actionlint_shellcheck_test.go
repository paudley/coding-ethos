// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolprotocol

import "testing"

func TestIsActionlintShellcheckJSONStdin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		marker string
		tool   string
		args   []string
		want   bool
	}{
		{
			name:   "marked actionlint json stdin",
			marker: ActionlintShellcheckJSONStdinV1,
			tool:   ShellcheckTool,
			args:   []string{"--norc", "-f", "json", "-x", "--shell", "bash", "-"},
			want:   true,
		},
		{
			name:   "marked long json format",
			marker: ActionlintShellcheckJSONStdinV1,
			tool:   ShellcheckTool,
			args:   []string{"--format=json", "-"},
			want:   true,
		},
		{
			name: "marker absent",
			tool: ShellcheckTool,
			args: []string{"-f", "json", "-"},
		},
		{
			name:   "unknown marker",
			marker: "json-stdin-v2",
			tool:   ShellcheckTool,
			args:   []string{"-f", "json", "-"},
		},
		{
			name:   "wrong tool",
			marker: ActionlintShellcheckJSONStdinV1,
			tool:   ActionlintTool,
			args:   []string{"-f", "json", "-"},
		},
		{
			name:   "not stdin",
			marker: ActionlintShellcheckJSONStdinV1,
			tool:   ShellcheckTool,
			args:   []string{"-f", "json", "script.sh"},
		},
		{
			name:   "not json",
			marker: ActionlintShellcheckJSONStdinV1,
			tool:   ShellcheckTool,
			args:   []string{"-f", "gcc", "-"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := IsActionlintShellcheckJSONStdin(test.marker, test.tool, test.args)
			if got != test.want {
				t.Fatalf("IsActionlintShellcheckJSONStdin() = %v, want %v", got, test.want)
			}
		})
	}
}
