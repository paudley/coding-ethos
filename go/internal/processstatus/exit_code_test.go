// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package processstatus

import (
	"errors"
	"os/exec"
	"testing"
)

func TestExitCode(t *testing.T) {
	t.Parallel()

	if got := ExitCode(nil, 99); got != 0 {
		t.Fatalf("ExitCode(nil) = %d, want 0", got)
	}

	if got := ExitCode(errors.New("plain"), 99); got != 99 {
		t.Fatalf("ExitCode(plain) = %d, want fallback", got)
	}

	command := exec.Command("sh", "-c", "exit 7")
	err := command.Run()
	if err == nil {
		t.Fatal("fixture command unexpectedly passed")
	}

	if got := ExitCode(err, 99); got != 7 {
		t.Fatalf("ExitCode(exit 7) = %d, want 7", got)
	}
}
