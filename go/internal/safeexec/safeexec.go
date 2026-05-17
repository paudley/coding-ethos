// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package safeexec

import (
	"context"
	"os/exec"

	"golang.org/x/sys/execabs"
)

func Command(name string, args ...string) *exec.Cmd {
	return execabs.Command(name, args...)
}

func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return execabs.CommandContext(ctx, name, args...)
}
