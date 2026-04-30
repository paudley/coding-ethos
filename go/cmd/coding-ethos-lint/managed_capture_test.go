// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"blackcat.ca/coding-ethos/go/toolcatalog"
)

func TestManagedRuffFormatDoesNotForceJsonOutput(t *testing.T) {
	t.Parallel()

	ethosRoot := filepath.Join("tmp", "coding-ethos")
	tool, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff tool")
	}

	enforced := enforceManagedToolArgs(
		tool,
		[]string{"format", "--check", "lib/python/pkg.py"},
		"/repo",
		ethosRoot,
	)
	got := capturedToolArgs("ruff", enforced)
	want := []string{
		"format",
		"--config",
		filepath.Join(ethosRoot, "ruff.toml"),
		"--check",
		"lib/python/pkg.py",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managed ruff format args = %#v, want %#v", got, want)
	}
}

func TestManagedSubcommandConfigPlacement(t *testing.T) {
	t.Parallel()

	ethosRoot := filepath.Join("tmp", "coding-ethos")
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "sqlfluff",
			args: []string{"lint", "queries/report.sql"},
			want: []string{
				"lint",
				"--format",
				"json",
				"--config",
				filepath.Join(ethosRoot, ".sqlfluff"),
				"queries/report.sql",
			},
		},
		{
			name: "golangci-lint",
			args: []string{"run", "./..."},
			want: []string{
				"run",
				"--output.json.path=stdout",
				"--output.text.path=stderr",
				"--config",
				filepath.Join(ethosRoot, ".golangci.yml"),
				"./...",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tool, found := toolcatalog.HookOwnedTool(test.name)
			if !found {
				t.Fatalf("missing %s tool", test.name)
			}

			enforced := enforceManagedToolArgs(tool, test.args, "/repo", ethosRoot)
			got := capturedToolArgs(test.name, enforced)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("managed %s args = %#v, want %#v", test.name, got, test.want)
			}
		})
	}
}
