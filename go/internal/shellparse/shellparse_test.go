// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package shellparse

import (
	"reflect"
	"testing"
)

func TestControlFieldsParsesMultilineGitAdd(t *testing.T) {
	t.Parallel()

	fields, err := ControlFields(
		"# stage files\n" +
			"git add \\\n" +
			"  'path with spaces.py' \\\n" +
			"  lbox-platform/tests/test_example.py",
	)
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	want := []string{
		"git",
		"add",
		"path with spaces.py",
		"lbox-platform/tests/test_example.py",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields mismatch:\n got %#v\nwant %#v", fields, want)
	}
}

func TestControlFieldsPreservesOperatorsAndRedirects(t *testing.T) {
	t.Parallel()

	fields, err := ControlFields("git status -s 2>&1 | grep file && ruff check .")
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	want := []string{
		"git", "status", "-s", "2>&1",
		"|",
		"grep", "file",
		"&&",
		"ruff", "check", ".",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields mismatch:\n got %#v\nwant %#v", fields, want)
	}
}

func TestCommandsExposeStructuredFacts(t *testing.T) {
	t.Parallel()

	commands, err := Commands("FOO=bar python -m ruff check . &")
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("command count mismatch: got %d", len(commands))
	}

	command := commands[0]
	if !reflect.DeepEqual(command.Assignments, []string{"FOO=bar"}) {
		t.Fatalf("assignments mismatch: %#v", command.Assignments)
	}
	if !reflect.DeepEqual(command.Argv, []string{"python", "-m", "ruff", "check", "."}) {
		t.Fatalf("argv mismatch: %#v", command.Argv)
	}
	if !command.Background {
		t.Fatalf("expected background command")
	}
}
