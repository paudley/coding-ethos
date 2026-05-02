// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestGitCommitReadsMessageFromStdin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{
			name: "separate file flag stdin",
			argv: []string{"commit", "-F", "-"},
			want: true,
		},
		{
			name: "compact short file flag stdin",
			argv: []string{"commit", "-F-"},
			want: true,
		},
		{
			name: "long file flag stdin",
			argv: []string{"commit", "--file=-"},
			want: true,
		},
		{
			name: "message flag does not read stdin",
			argv: []string{"commit", "-m", "fix(wrapper): avoid stdin"},
		},
		{
			name: "non commit does not read stdin",
			argv: []string{"status", "--short"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := gitCommitReadsMessageFromStdin(test.argv); got != test.want {
				t.Fatalf("gitCommitReadsMessageFromStdin(%#v) = %v, want %v", test.argv, got, test.want)
			}
		})
	}
}
