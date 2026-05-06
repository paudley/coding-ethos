// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package shellparse_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

func FuzzShellParser(f *testing.F) {
	for _, seed := range []string{
		"git add file.py",
		"FILE=.claude/settings.json cat > ${FILE}",
		"git commit -m subject -m body",
		"ruff check . 2>&1 | tee lint.log",
		"python - <<'PY'\nprint('hello')\nPY",
		"bash -c 'git reset --hard'",
		"env PATH=/tmp/bin:$PATH git status",
		"FILE=.claude/settings.json cat > ${FILE}",
		"printf %s data > >(tee audit.log)",
		"git submodule update --init coding-ethos",
		"python -c 'import subprocess; subprocess.run([\"git\", \"status\"])'",
		"git commit -m 'subject' -m 'body with\nnewline'",
		"if [ -f file ]; then git add file; fi",
		"unterminated 'quote",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		fields, fieldsErr := shellparse.Fields(input)
		controlFields, controlErr := shellparse.ControlFields(input)

		commands, commandsErr := shellparse.Commands(input)
		if (fieldsErr == nil) != (controlErr == nil) ||
			(fieldsErr == nil) != (commandsErr == nil) {
			t.Fatalf(
				"parser entrypoints disagree: fields=%v control=%v commands=%v",
				fieldsErr,
				controlErr,
				commandsErr,
			)
		}

		if fieldsErr != nil {
			return
		}

		_ = fields
		_ = controlFields

		for _, command := range commands {
			if command.Line < 0 || command.Column < 0 {
				t.Fatalf("invalid position for %#v", command)
			}
		}
	})
}
