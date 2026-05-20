// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package shellquote

import "testing"

func TestArg(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "status", want: "status"},
		{name: "empty", value: "", want: "''"},
		{name: "space", value: "a b", want: "'a b'"},
		{name: "quote", value: "can't", want: "'can'\\''t'"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := Arg(test.value); got != test.want {
				t.Fatalf("Arg(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestCommand(t *testing.T) {
	t.Parallel()

	got := Command("git", "commit", "-m", "fix bug")
	want := "git commit -m 'fix bug'"
	if got != want {
		t.Fatalf("Command() = %q, want %q", got, want)
	}
}
