// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
)

func logCapturedToolResult(
	cwd string,
	result lint.Result,
) string {
	tracePath, err := lint.LogResult(cwd, result)
	if err != nil {
		emitManagedCaptureText("warning: lint trace not written: " + err.Error())

		return ""
	}

	err = outputsurface.AutoPruneSurface(
		context.Background(),
		cwd,
		"lint_traces",
		false,
	)
	if err != nil {
		emitManagedCaptureText("warning: lint trace auto-prune failed: " + err.Error())
	}

	return tracePath
}

func refreshCapturedToolCodeIntel(root, tracePath string, changedFiles []string) {
	ctx := context.Background()

	err := codeintel.IngestLintTraceFile(ctx, root, tracePath)
	if err != nil {
		emitManagedCaptureText(
			"warning: captured lint trace not ingested into code-intel: " + err.Error(),
		)

		return
	}

	if len(changedFiles) == 0 {
		return
	}

	_, err = codeintel.RefreshLintFiles(ctx, root, changedFiles)
	if err != nil {
		emitManagedCaptureText(
			"warning: captured lint code-intel refresh failed: " + err.Error(),
		)
	}
}
