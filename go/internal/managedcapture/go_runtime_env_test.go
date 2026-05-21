// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"
	"os/exec"
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

	if !anyToolAvailable("gcc", "cc", "clang") {
		t.Skip("C compiler is not available on PATH")
	}

	if got := managedCCompiler(); got == "" {
		t.Fatal("managedCCompiler() returned empty")
	}
}

func TestManagedAssemblerResolvesAssembler(t *testing.T) {
	t.Parallel()

	if !anyToolAvailable("as") {
		t.Skip("assembler is not available on PATH")
	}

	if got := managedAssembler(); got == "" {
		t.Fatal("managedAssembler() returned empty")
	}
}

func anyToolAvailable(names ...string) bool {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}

	return false
}

func TestManagedCompilerPathIncludesCompilerAndAssemblerDirs(t *testing.T) {
	t.Parallel()

	got := managedCompilerPath("/usr/bin/gcc", "/usr/bin/as")
	if got != "/usr/bin" {
		t.Fatalf("managedCompilerPath() = %q, want /usr/bin", got)
	}
}
