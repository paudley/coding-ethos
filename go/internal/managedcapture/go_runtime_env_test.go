// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"
	"testing"
)

func TestManagedGoRootResolvesRuntimeRoot(t *testing.T) {
	t.Parallel()

	if got := managedGoRoot(context.Background()); got == "" {
		t.Fatal("managedGoRoot() returned empty")
	}
}

func TestManagedCCompilerResolvesCompiler(t *testing.T) {
	t.Parallel()

	if got := managedCCompiler(); got == "" {
		t.Fatal("managedCCompiler() returned empty")
	}
}

func TestManagedAssemblerResolvesAssembler(t *testing.T) {
	t.Parallel()

	if got := managedAssembler(); got == "" {
		t.Fatal("managedAssembler() returned empty")
	}
}

func TestManagedCompilerPathIncludesCompilerAndAssemblerDirs(t *testing.T) {
	t.Parallel()

	got := managedCompilerPath("/usr/bin/gcc", "/usr/bin/as")
	if got != "/usr/bin" {
		t.Fatalf("managedCompilerPath() = %q, want /usr/bin", got)
	}
}
