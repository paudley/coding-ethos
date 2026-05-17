// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

func TestDetectFileReadInvocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		path string
		ok   bool
	}{
		{
			name: "cat single file",
			argv: []string{"cat", "pkg/app.py"},
			path: "pkg/app.py",
			ok:   true,
		},
		{
			name: "cat explicit option terminator",
			argv: []string{"cat", "--", "pkg/app.py"},
			path: "pkg/app.py",
			ok:   true,
		},
		{
			name: "cat literal option-looking filename",
			argv: []string{"cat", "--", "-n"},
			path: "-n",
			ok:   true,
		},
		{
			name: "cat literal double-dash filename",
			argv: []string{"cat", "--", "--"},
			path: "--",
			ok:   true,
		},
		{
			name: "cat formatter option rejected",
			argv: []string{"cat", "-n", "pkg/app.py"},
		},
		{
			name: "cat stdin rejected",
			argv: []string{"cat", "-"},
		},
		{
			name: "cat multiple files rejected",
			argv: []string{"cat", "pkg/app.py", "pkg/other.py"},
		},
		{
			name: "non file-read command rejected",
			argv: []string{"sed", "-n", "1,100p", "pkg/app.py"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := agentproxy.DetectFileReadInvocation(test.argv)
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v: %#v", ok, test.ok, got)
			}

			if !test.ok {
				return
			}

			if got.Path != test.path {
				t.Fatalf("invocation = %#v", got)
			}
		})
	}
}

func TestDetectShellFileReadInvocationRejectsDynamicCommand(t *testing.T) {
	t.Parallel()

	commands, err := shellparse.Commands("cat $TARGET")
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	_, ok := agentproxy.DetectShellFileReadInvocation(commands[0])
	if ok {
		t.Fatal("dynamic file-read command was accepted")
	}
}
