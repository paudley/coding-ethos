// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"fmt"
	"os"
)

// Run executes the hook runner command family.
func Run(args []string) int {
	if len(args) < minCollectionItems-1 {
		usage()

		return 1
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	command, ok := defaultHookCommandRegistry().Commands[args[0]]
	if !ok {
		usage()

		return 1
	}

	return command(cfg, args[1:])
}
