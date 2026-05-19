// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package execguard prevents accidental recursive coding-ethos process trees.
package execguard

import (
	"fmt"
	"os"
	"strings"
)

const (
	EnvStack        = "CODING_ETHOS_EXEC_STACK"
	maxStackEntries = 32
	exitRecursive   = 96
)

// Enter records an executable entrypoint and terminates the process if the same
// coding-ethos executable re-enters through subprocess execution.
func Enter(name string) {
	normalized := strings.TrimSpace(name)
	if normalized == "" {
		normalized = "coding-ethos"
	}

	stack := currentStack()
	for _, entry := range stack {
		if entry == normalized {
			fmt.Fprintf(
				os.Stderr,
				"FATAL: recursive coding-ethos executable invocation blocked: %s\n",
				normalized,
			)
			fmt.Fprintf(os.Stderr, "stack: %s\n", strings.Join(stack, " -> "))
			os.Exit(exitRecursive)
		}
	}

	if len(stack) >= maxStackEntries {
		fmt.Fprintf(
			os.Stderr,
			"FATAL: coding-ethos executable nesting exceeded %d entries\n",
			maxStackEntries,
		)
		fmt.Fprintf(os.Stderr, "stack: %s\n", strings.Join(stack, " -> "))
		os.Exit(exitRecursive)
	}

	stack = append(stack, normalized)

	err := os.Setenv(EnvStack, strings.Join(stack, "\n"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: set %s: %v\n", EnvStack, err)
		os.Exit(exitRecursive)
	}
}

func currentStack() []string {
	raw := strings.TrimSpace(os.Getenv(EnvStack))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, "\n")

	stack := make([]string, 0, len(parts))
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry != "" {
			stack = append(stack, entry)
		}
	}

	return stack
}
