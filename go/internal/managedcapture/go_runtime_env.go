// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const goRootProbeTimeout = 2 * time.Second

// managedGoRootCache avoids spawning `go env GOROOT` for every captured process.
//
//nolint:gochecknoglobals
var managedGoRootCache sync.Map

func managedGoRoot(ctx context.Context) string {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return ""
	}

	if cached, ok := managedGoRootCache.Load(goBin); ok {
		root, typeOK := cached.(string)
		if typeOK {
			return root
		}
	}

	ctx, cancel := context.WithTimeout(ctx, goRootProbeTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, goBin, "env", "GOROOT").Output()
	if err != nil {
		return ""
	}

	root := strings.TrimSpace(string(output))
	managedGoRootCache.Store(goBin, root)

	return root
}

func managedCCompiler() string {
	if compiler := strings.TrimSpace(os.Getenv("CC")); compiler != "" {
		path, err := exec.LookPath(compiler)
		if err == nil {
			return path
		}
	}

	for _, candidate := range []string{"gcc", "cc", "clang"} {
		path, err := exec.LookPath(candidate)
		if err == nil {
			return path
		}
	}

	return ""
}

func managedAssembler() string {
	if assembler := strings.TrimSpace(os.Getenv("AS")); assembler != "" {
		path, err := exec.LookPath(assembler)
		if err == nil {
			return path
		}
	}

	path, err := exec.LookPath("as")
	if err == nil {
		return path
	}

	return ""
}

func managedCompilerPath(ccPath, assemblerPath string) string {
	dirs := []string{}

	for _, path := range []string{ccPath, assemblerPath} {
		if path == "" {
			continue
		}

		dir := filepath.Dir(path)
		if dir == "." || slices.Contains(dirs, dir) {
			continue
		}

		dirs = append(dirs, dir)
	}

	return strings.Join(dirs, string(os.PathListSeparator))
}
