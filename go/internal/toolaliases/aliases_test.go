// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolaliases

import "testing"

func TestActiveCanonicalMatchesProviderAliasesAndSuffixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "Bash", want: CanonicalShell},
		{name: "functions.exec_command", want: CanonicalShell},
		{name: "tool.functions.write_stdin", want: CanonicalShell},
		{name: "functions.apply_patch", want: CanonicalEdit},
		{name: "tool.functions.apply_patch", want: CanonicalEdit},
		{name: "Write", want: CanonicalWrite},
		{name: "MultiEdit", want: CanonicalMultiEdit},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := ActiveCanonical(test.name)
			if !ok || got != test.want {
				t.Fatalf("ActiveCanonical(%q) = %q, %v; want %q, true", test.name, got, ok, test.want)
			}
		})
	}
}

func TestActiveCanonicalRejectsBlankAndUnknownTools(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", "   ", "Read"} {
		if got, ok := ActiveCanonical(name); ok || got != "" {
			t.Fatalf("ActiveCanonical(%q) = %q, %v; want empty false", name, got, ok)
		}
	}
}

func TestProviderAliasesAndNoopCanonical(t *testing.T) {
	t.Parallel()

	aliases := ProviderAliases(ProviderCodex, CanonicalShell)
	if len(aliases) == 0 {
		t.Fatal("Codex shell aliases should be registered")
	}
	if !NoopCanonical("functions.update_plan") {
		t.Fatal("known no-op tool should be recognized")
	}
	if NoopCanonical("Bash") {
		t.Fatal("active tool must not be treated as no-op")
	}
}
