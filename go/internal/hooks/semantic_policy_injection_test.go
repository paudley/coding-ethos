// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

func TestShellCommandMutatesGitSkipsGlobalOptions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		command string
		want    bool
	}{
		{
			name:    "git -C commit",
			command: "git -C /repo commit -m test",
			want:    true,
		},
		{
			name:    "git -c commit",
			command: "git -c user.name=test commit -m test",
			want:    true,
		},
		{
			name:    "policy-git -C commit",
			command: "bin/coding-ethos-run policy-git -C /repo commit -m test",
			want:    true,
		},
		{
			name:    "coding-ethos-git -C commit",
			command: "coding-ethos-git -C /repo commit -m test",
			want:    true,
		},
		{
			name:    "git -C status",
			command: "git -C /repo status --short",
			want:    false,
		},
		{
			name:    "git -c alias value status",
			command: "git -c alias.status=commit status --short",
			want:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			commands, err := shellparse.Commands(tc.command)
			if err != nil {
				t.Fatalf("parse command: %v", err)
			}
			if len(commands) != 1 {
				t.Fatalf("commands = %#v, want one command", commands)
			}

			if got := shellCommandMutatesGit(commands[0]); got != tc.want {
				t.Fatalf("shellCommandMutatesGit() = %t, want %t", got, tc.want)
			}
		})
	}
}
