// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy

import (
	"path/filepath"
	"strings"
)

const (
	defaultDirectoryListingPath = "."
	shortOptionWithValueLength  = 2
)

// DirectoryListingInvocation is the normalized shape a future proxy interceptor
// needs after recognizing an ls/tree-style directory listing tool call.
type DirectoryListingInvocation struct {
	Tool string
	Path string
}

// DetectDirectoryListingInvocation recognizes simple ls/tree invocations and
// returns the directory whose output can be enriched with an anatomy map.
func DetectDirectoryListingInvocation(
	argv []string,
) (DirectoryListingInvocation, bool) {
	if len(argv) == 0 {
		return DirectoryListingInvocation{}, false
	}

	tool := filepath.Base(strings.TrimSpace(argv[0]))
	switch tool {
	case "ls":
		return detectLSInvocation(argv[1:])
	case "tree":
		return detectTreeInvocation(argv[1:])
	default:
		return DirectoryListingInvocation{}, false
	}
}

func detectLSInvocation(args []string) (DirectoryListingInvocation, bool) {
	targets := directoryListingPositionals(args, lsOptionsWithValue())
	if len(targets) > 1 {
		return DirectoryListingInvocation{}, false
	}

	return DirectoryListingInvocation{
		Tool: "ls",
		Path: firstDirectoryListingTarget(targets),
	}, true
}

func detectTreeInvocation(args []string) (DirectoryListingInvocation, bool) {
	targets := directoryListingPositionals(args, treeOptionsWithValue())
	if len(targets) > 1 {
		return DirectoryListingInvocation{}, false
	}

	return DirectoryListingInvocation{
		Tool: "tree",
		Path: firstDirectoryListingTarget(targets),
	}, true
}

func directoryListingPositionals(
	args []string,
	optionsWithValue map[string]struct{},
) []string {
	positionals := []string{}
	endOptions := false
	skipNext := false

	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}

		if endOptions {
			positionals = append(positionals, arg)

			continue
		}

		switch {
		case arg == "--":
			endOptions = true
		case strings.HasPrefix(arg, "--"):
			skipNext = longDirectoryListingOptionConsumesNext(arg, optionsWithValue)
		case strings.HasPrefix(arg, "-") && arg != "-":
			skipNext = shortDirectoryListingOptionConsumesNext(arg, optionsWithValue)
		default:
			positionals = append(positionals, arg)
		}
	}

	return positionals
}

func longDirectoryListingOptionConsumesNext(
	arg string,
	optionsWithValue map[string]struct{},
) bool {
	name, _, hasInlineValue := strings.Cut(arg, "=")
	if hasInlineValue {
		return false
	}

	_, consumes := optionsWithValue[name]

	return consumes
}

func shortDirectoryListingOptionConsumesNext(
	arg string,
	optionsWithValue map[string]struct{},
) bool {
	if len(arg) != shortOptionWithValueLength {
		return false
	}

	_, consumes := optionsWithValue[arg]

	return consumes
}

func firstDirectoryListingTarget(targets []string) string {
	if len(targets) == 0 || strings.TrimSpace(targets[0]) == "" {
		return defaultDirectoryListingPath
	}

	return targets[0]
}

func lsOptionsWithValue() map[string]struct{} {
	return map[string]struct{}{
		"--block-size": {},
		"--color":      {},
		"--format":     {},
		"--hide":       {},
		"--indicator":  {},
		"--quoting":    {},
		"--sort":       {},
		"--time":       {},
		"--width":      {},
		"-I":           {},
		"-T":           {},
		"-w":           {},
	}
}

func treeOptionsWithValue() map[string]struct{} {
	return map[string]struct{}{
		"--charset":   {},
		"--filelimit": {},
		"--fromfile":  {},
		"--gitfile":   {},
		"--infofile":  {},
		"--metafirst": {},
		"--timefmt":   {},
		"-H":          {},
		"-I":          {},
		"-L":          {},
		"-o":          {},
		"-P":          {},
		"-s":          {},
	}
}
