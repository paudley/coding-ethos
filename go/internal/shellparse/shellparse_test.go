// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package shellparse_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

func TestControlFieldsParsesMultilineGitAdd(t *testing.T) {
	t.Parallel()

	fields, err := shellparse.ControlFields(
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

	fields, err := shellparse.ControlFields(
		"git status -s 2>&1 | grep file && ruff check .",
	)
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

func TestCatHeredocCommandSubstitutionExtractsRenderedCommand(t *testing.T) {
	t.Parallel()

	fields, err := shellparse.Fields(
		"git commit -m \"$(cat <<'EOF'\nfix(test): subject\n\nBody.\nEOF\n)\"",
	)
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	if len(fields) != 4 {
		t.Fatalf("fields mismatch: %#v", fields)
	}

	message, ok := shellparse.CatHeredocCommandSubstitution(fields[3])
	if !ok {
		t.Fatalf("expected extractable heredoc command substitution: %#v", fields[3])
	}

	want := "fix(test): subject\n\nBody.\n"
	if message != want {
		t.Fatalf("message mismatch:\n got %#v\nwant %#v", message, want)
	}
}

func TestFieldsDecodeANSICQuotedWords(t *testing.T) {
	t.Parallel()

	fields, err := shellparse.Fields(
		"git commit -m $'fix(commitlint): subject\\n\\nBody.\\nFixes #54' $'\\xff\\377\\u263a\\ud800'",
	)
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	want := []string{
		"git",
		"commit",
		"-m",
		"fix(commitlint): subject\n\nBody.\nFixes #54",
		string([]byte{0xff, 0xff}) + "☺\\ud800",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields mismatch:\n got %#v\nwant %#v", fields, want)
	}

	wantBytes := []byte{0xff, 0xff, 0xe2, 0x98, 0xba, '\\', 'u', 'd', '8', '0', '0'}
	if !reflect.DeepEqual([]byte(fields[4]), wantBytes) {
		t.Fatalf(
			"ANSI-C byte escape mismatch:\n got %#v\nwant %#v",
			[]byte(fields[4]),
			wantBytes,
		)
	}
}

func TestCommandsExposeStructuredFacts(t *testing.T) {
	t.Parallel()

	commands, err := shellparse.Commands("FOO=bar python -m ruff check . &")
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

	if !reflect.DeepEqual(
		command.Argv,
		[]string{"python", "-m", "ruff", "check", "."},
	) {
		t.Fatalf("argv mismatch: %#v", command.Argv)
	}

	if !command.Background {
		t.Fatalf("expected background command")
	}
}

func TestCommandsExposeDynamicShellFacts(t *testing.T) {
	t.Parallel()

	commands, err := shellparse.Commands(
		"PATH=$(pwd):$PATH bash -c 'git status' <(cat file)",
	)
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	if len(commands) != 1 {
		t.Fatalf("command count mismatch: got %d", len(commands))
	}

	command := commands[0]
	if command.Name != "bash" || command.Line != 1 || command.Column != 1 {
		t.Fatalf("command identity mismatch: %#v", command)
	}

	if !command.HasCommandSubstitution ||
		!command.HasProcessSubstitution ||
		!command.HasDynamicExpansion {
		t.Fatalf("dynamic flags mismatch: %#v", command)
	}
}

func TestCommandsExposeFunctionsAndNestedStatements(t *testing.T) {
	t.Parallel()

	commands, err := shellparse.Commands(
		"stage_files() { git add one.py; git add two.py; }\nstage_files",
	)
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	if len(commands) != 4 {
		t.Fatalf("command count mismatch: got %d: %#v", len(commands), commands)
	}

	if !commands[0].IsFunctionDeclaration || commands[0].Name != "stage_files" {
		t.Fatalf("function declaration missing: %#v", commands[0])
	}

	if !reflect.DeepEqual(commands[1].Argv, []string{"git", "add", "one.py"}) ||
		!reflect.DeepEqual(commands[2].Argv, []string{"git", "add", "two.py"}) ||
		!reflect.DeepEqual(commands[3].Argv, []string{"stage_files"}) {
		t.Fatalf("nested command facts mismatch: %#v", commands)
	}

	fields, err := shellparse.ControlFields("(git status; ruff check .)")
	if err != nil {
		t.Fatalf("parse shell subshell block: %v", err)
	}

	if !reflect.DeepEqual(
		fields,
		[]string{"git", "status", ";", "ruff", "check", "."},
	) {
		t.Fatalf("subshell fields mismatch: %#v", fields)
	}
}

func TestParseErrorsPreserveLocationAndCause(t *testing.T) {
	t.Parallel()

	_, err := shellparse.Fields("git commit &&")
	if err == nil {
		t.Fatal("expected malformed shell command to fail")
	}

	var parseErr shellparse.Error
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected shellparse.Error, got %T: %v", err, err)
	}

	if parseErr.Line == 0 || parseErr.Column == 0 {
		t.Fatalf("parse error location missing: %#v", parseErr)
	}

	if !strings.Contains(parseErr.Error(), "parse shell command") {
		t.Fatalf("parse error should include wrapped message: %v", parseErr)
	}

	if parseErr.Unwrap() == nil {
		t.Fatalf("parse error should expose wrapped cause: %#v", parseErr)
	}
}
