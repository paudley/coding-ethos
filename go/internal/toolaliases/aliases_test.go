// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolaliases_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/toolaliases"
)

func TestActiveCanonicalMatchesProviderAliasesAndSuffixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "Bash", want: toolaliases.CanonicalShell},
		{name: "functions.exec_command", want: toolaliases.CanonicalShell},
		{name: "tool.functions.write_stdin", want: toolaliases.CanonicalShell},
		{name: "functions.apply_patch", want: toolaliases.CanonicalEdit},
		{name: "tool.functions.apply_patch", want: toolaliases.CanonicalEdit},
		{name: "Write", want: toolaliases.CanonicalWrite},
		{name: "MultiEdit", want: toolaliases.CanonicalMultiEdit},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, found := toolaliases.ActiveCanonical(test.name)
			if !found || got != test.want {
				t.Fatalf(
					"ActiveCanonical(%q) = %q, %v; want %q, true",
					test.name,
					got,
					found,
					test.want,
				)
			}
		})
	}
}

func TestActiveCanonicalRejectsBlankAndUnknownTools(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", "   ", "Read"} {
		if got, found := toolaliases.ActiveCanonical(name); found || got != "" {
			t.Fatalf("ActiveCanonical(%q) = %q, %v; want empty false", name, got, found)
		}
	}
}

func TestProviderAliasesAndNoopCanonical(t *testing.T) {
	t.Parallel()

	aliases := toolaliases.ProviderAliases(
		toolaliases.ProviderCodex,
		toolaliases.CanonicalShell,
	)
	if len(aliases) == 0 {
		t.Fatal("Codex shell aliases should be registered")
	}

	kimiAliases := toolaliases.ProviderAliases(
		toolaliases.ProviderKimi,
		toolaliases.CanonicalNoop,
	)
	if len(kimiAliases) == 0 {
		t.Fatal("Kimi no-op aliases should be registered")
	}

	if !toolaliases.NoopCanonical("functions.update_plan") {
		t.Fatal("known no-op tool should be recognized")
	}

	if toolaliases.NoopCanonical("Bash") {
		t.Fatal("active tool must not be treated as no-op")
	}
}
