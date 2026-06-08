// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

func TestPolicyToolCaptureToolClassifiesGolangciLintMutatingCommands(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		name string
		want string
	}{
		{
			name: "autofix",
			args: []string{"run", "--fix", "./internal/..."},
			want: golangciLintAutofixTool,
		},
		{
			name: "format",
			args: []string{"fmt", "./internal/..."},
			want: golangciLintFormatTool,
		},
		{
			name: "analysis",
			args: []string{"run", "./internal/..."},
			want: golangciLintTool,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := policyToolCaptureTool(golangciLintTool, test.args)
			if got != test.want {
				t.Fatalf("policyToolCaptureTool() = %q, want %q", got, test.want)
			}
		})
	}
}
