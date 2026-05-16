// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy

import (
	"path/filepath"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const (
	defaultDirectoryListingPath = "."
	shortOptionWithValueLength  = 2
)

// DirectoryListingInvocation is the normalized shape a future proxy interceptor
// needs after recognizing an ls/tree-style directory listing tool call.
type DirectoryListingInvocation struct {
	Tool      string
	Path      string
	Recursive bool
	MaxDepth  int
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

// DetectShellDirectoryListingInvocation recognizes static, single-target
// ls/tree shell commands whose output can safely be paired with a directory
// anatomy map.
func DetectShellDirectoryListingInvocation(
	command shellparse.Command,
) (DirectoryListingInvocation, bool) {
	if command.HasCommandSubstitution ||
		command.HasDynamicExpansion ||
		command.HasProcessSubstitution ||
		command.HasSubshell {
		return DirectoryListingInvocation{}, false
	}

	return DetectDirectoryListingInvocation(command.Argv)
}

func detectLSInvocation(args []string) (DirectoryListingInvocation, bool) {
	targets, ok := directoryListingPositionals(
		args,
		lsOptionsWithValue(),
		lsUnsupportedListingOptions(),
	)
	if !ok {
		return DirectoryListingInvocation{}, false
	}

	if len(targets) > 1 {
		return DirectoryListingInvocation{}, false
	}

	return DirectoryListingInvocation{
		Tool: "ls",
		Path: firstDirectoryListingTarget(targets),
	}, true
}

func detectTreeInvocation(args []string) (DirectoryListingInvocation, bool) {
	maxDepth, depthDetected := treeMaxDepth(args)
	if !depthDetected {
		return DirectoryListingInvocation{}, false
	}

	targets, positionalsDetected := directoryListingPositionals(
		args,
		treeOptionsWithValue(),
		nil,
	)
	if !positionalsDetected {
		return DirectoryListingInvocation{}, false
	}

	if len(targets) > 1 {
		return DirectoryListingInvocation{}, false
	}

	return DirectoryListingInvocation{
		Tool:      "tree",
		Path:      firstDirectoryListingTarget(targets),
		Recursive: true,
		MaxDepth:  maxDepth,
	}, true
}

func treeMaxDepth(args []string) (int, bool) {
	for index := range args {
		arg := args[index]
		switch {
		case arg == "-L":
			if index+1 >= len(args) {
				return 0, false
			}

			depth, ok := parseTreeMaxDepth(args[index+1])
			if !ok {
				return 0, false
			}

			return depth, true
		case strings.HasPrefix(arg, "-L") && arg != "-L":
			return parseTreeMaxDepth(strings.TrimPrefix(arg, "-L"))
		}
	}

	return 0, true
}

func parseTreeMaxDepth(value string) (int, bool) {
	depth, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || depth <= 0 {
		return 0, false
	}

	return depth, true
}

func directoryListingPositionals(
	args []string,
	optionsWithValue map[string]struct{},
	unsupportedOptions map[string]struct{},
) ([]string, bool) {
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
			if longDirectoryListingOptionUnsupported(arg, unsupportedOptions) {
				return nil, false
			}

			skipNext = longDirectoryListingOptionConsumesNext(arg, optionsWithValue)
		case strings.HasPrefix(arg, "-") && arg != "-":
			if shortDirectoryListingOptionUnsupported(arg, unsupportedOptions) {
				return nil, false
			}

			skipNext = shortDirectoryListingOptionConsumesNext(arg, optionsWithValue)
		default:
			positionals = append(positionals, arg)
		}
	}

	return positionals, true
}

func longDirectoryListingOptionUnsupported(
	arg string,
	unsupportedOptions map[string]struct{},
) bool {
	name, _, _ := strings.Cut(arg, "=")
	_, unsupported := unsupportedOptions[name]

	return unsupported
}

func shortDirectoryListingOptionUnsupported(
	arg string,
	unsupportedOptions map[string]struct{},
) bool {
	if len(unsupportedOptions) == 0 {
		return false
	}

	for _, option := range strings.TrimPrefix(arg, "-") {
		if _, unsupported := unsupportedOptions["-"+string(option)]; unsupported {
			return true
		}
	}

	return false
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

func lsUnsupportedListingOptions() map[string]struct{} {
	return map[string]struct{}{
		"--directory": {},
		"-d":          {},
	}
}

func treeOptionsWithValue() map[string]struct{} {
	return map[string]struct{}{
		"--charset":   {},
		"--filelimit": {},
		"--fromfile":  {},
		"--gitfile":   {},
		"--infofile":  {},
		"--timefmt":   {},
		"-H":          {},
		"-I":          {},
		"-L":          {},
		"-o":          {},
		"-P":          {},
	}
}
