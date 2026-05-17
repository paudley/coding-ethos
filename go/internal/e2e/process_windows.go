// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package e2e

import "os/exec"

func configureCommandProcessGroup(_ *exec.Cmd) {}

func terminateCommandProcessGroup(_ *exec.Cmd) {}
