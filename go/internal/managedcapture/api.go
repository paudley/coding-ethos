// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package managedcapture

import (
	"fmt"
	"io"
	"os"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const BlockedExitCode = 2

type PolicyContext struct {
	Skills       map[string]policy.Skill
	EvidenceMaps []diagnostics.EvidenceMap
	Policies     []policy.Policy
}

type CaptureOptions struct {
	PolicyContext PolicyContext
	Tool          string
	ToolPath      string
	Cwd           string
	TraceRoot     string
	OutputFormat  string
	Output        io.Writer
	Args          []string
}

func Capture(options CaptureOptions) int {
	return runCapturedTool(
		options.Tool,
		options.ToolPath,
		options.Cwd,
		options.TraceRoot,
		options.Args,
		options.PolicyContext,
		options.Output,
		options.OutputFormat,
	)
}

func ExecutableAvailable(path string) bool {
	return isExecutable(path)
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
