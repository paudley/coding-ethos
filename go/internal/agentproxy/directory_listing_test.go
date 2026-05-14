// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

func TestDetectDirectoryListingInvocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		tool string
		path string
		ok   bool
	}{
		{
			name: "ls default path",
			argv: []string{"ls", "-la"},
			tool: "ls",
			path: ".",
			ok:   true,
		},
		{
			name: "ls explicit path after value option",
			argv: []string{"ls", "--color", "always", "pkg"},
			tool: "ls",
			path: "pkg",
			ok:   true,
		},
		{
			name: "tree explicit path",
			argv: []string{"tree", "-L", "2", "go/internal"},
			tool: "tree",
			path: "go/internal",
			ok:   true,
		},
		{
			name: "multiple targets rejected",
			argv: []string{"ls", "pkg", "docs"},
		},
		{
			name: "non listing command rejected",
			argv: []string{"find", ".", "-maxdepth", "1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := agentproxy.DetectDirectoryListingInvocation(test.argv)
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v: %#v", ok, test.ok, got)
			}

			if !test.ok {
				return
			}

			if got.Tool != test.tool || got.Path != test.path {
				t.Fatalf("invocation = %#v", got)
			}
		})
	}
}
