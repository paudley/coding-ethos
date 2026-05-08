// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package execguard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEveryCommandMainEntersExecGuard(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	cmdRoot := filepath.Join(root, "cmd")

	entries, err := os.ReadDir(cmdRoot)
	if err != nil {
		t.Fatalf("read cmd root: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		mainPath := filepath.Join(cmdRoot, name, "main.go")

		payload, err := os.ReadFile(mainPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			t.Fatalf("read %s: %v", mainPath, err)
		}

		content := string(payload)

		want := `execguard.Enter("` + name + `")`
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %s", mainPath, want)
		}
	}
}

func TestInternalCLIPackagesDoNotExposeMainFunctions(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	internalRoot := filepath.Join(root, "internal")

	for _, name := range []string{
		"agenthookscli",
		"codeintelcli",
		"githookcli",
		"hookcli",
		"hooklogcli",
		"hookrunnercli",
		"lintcli",
		"mcpcli",
		"policycli",
		"policygitcli",
		"toolchaincli",
	} {
		mainPath := filepath.Join(internalRoot, name, "main.go")

		payload, err := os.ReadFile(mainPath)
		if err != nil {
			t.Fatalf("read %s: %v", mainPath, err)
		}

		if strings.Contains(string(payload), "func main(") {
			t.Fatalf(
				"%s exposes func main; executable entrypoints must live under go/cmd",
				mainPath,
			)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
