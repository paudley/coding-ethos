// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package shellparse

import "testing"

func FuzzShellParser(f *testing.F) {
	for _, seed := range []string{
		"git add file.py",
		"FILE=.claude/settings.json cat > ${FILE}",
		"git commit -m subject -m body",
		"ruff check . 2>&1 | tee lint.log",
		"python - <<'PY'\nprint('hello')\nPY",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = Fields(input)
		_, _ = ControlFields(input)
		_, _ = Commands(input)
	})
}
