// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"os"
	"path/filepath"
	"strings"
)

func activeAgentShellSandboxCoversCapture(request captureRequest) bool {
	if os.Getenv("CODING_ETHOS_AGENT_SHELL_SANDBOX") != "1" ||
		os.Getenv("CODING_ETHOS_SANDBOX_ACTIVE") != "1" {
		return false
	}

	root := filepath.Clean(strings.TrimSpace(os.Getenv("CODING_ETHOS_SANDBOX_ROOT")))
	traceRoot := filepath.Clean(firstCaptureNonEmpty(request.TraceRoot, request.Cwd))

	cwd := filepath.Clean(request.Cwd)
	if root == "." || traceRoot == "." || cwd == "." {
		return false
	}

	return capturePathWithin(traceRoot, root) && capturePathWithin(cwd, root)
}

func capturePathWithin(path, parent string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(path))
	if err != nil {
		return false
	}

	return relative == "." ||
		(!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
			relative != "..")
}
