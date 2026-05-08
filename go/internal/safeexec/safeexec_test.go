// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package safeexec_test

import (
	"context"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

func TestSafeExecBuildsExecabsCommands(t *testing.T) {
	t.Parallel()

	cmd := safeexec.Command("sh", "-c", "true")
	if cmd.Path == "" || len(cmd.Args) != 3 {
		t.Fatalf("safe command = %#v", cmd)
	}

	ctxCmd := safeexec.CommandContext(context.Background(), "sh", "-c", "true")
	if ctxCmd.Path == "" || len(ctxCmd.Args) != 3 {
		t.Fatalf("safe context command = %#v", ctxCmd)
	}

	execCmd := ctxCmd
	if execCmd == nil {
		t.Fatal("safe command is not an exec.Cmd")
	}
}
