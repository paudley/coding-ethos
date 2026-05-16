// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
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
			name: "ls color optional argument does not consume path",
			argv: []string{"ls", "--color", "pkg"},
			tool: "ls",
			path: "pkg",
			ok:   true,
		},
		{
			name: "ls value option consumes next argument",
			argv: []string{"ls", "--block-size", "K", "pkg"},
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
			name: "tree size flag does not consume path",
			argv: []string{"tree", "-s", "go/internal"},
			tool: "tree",
			path: "go/internal",
			ok:   true,
		},
		{
			name: "ls directory entry mode rejected",
			argv: []string{"ls", "-d", "pkg"},
		},
		{
			name: "ls long directory entry mode rejected",
			argv: []string{"ls", "--directory", "pkg"},
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

func TestDetectShellDirectoryListingInvocationRejectsDynamicCommand(t *testing.T) {
	t.Parallel()

	commands, err := shellparse.Commands("ls $TARGET")
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	_, ok := agentproxy.DetectShellDirectoryListingInvocation(commands[0])
	if ok {
		t.Fatal("dynamic listing command was accepted")
	}
}
