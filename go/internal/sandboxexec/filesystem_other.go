// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package sandboxexec

import "syscall"

func applyFilesystemPolicy(options options) error {
	return nil
}

func sandboxedCommandSysProcAttr() *syscall.SysProcAttr {
	return nil
}
