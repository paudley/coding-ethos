// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooklog_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	. "blackcat.ca/coding-ethos/go/internal/hooklog"
	"blackcat.ca/coding-ethos/go/internal/testlock"
)

func TestRunWritesHookLogsAndMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeHookLogIgnore(t, root)
	git := fakeGit(t)

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	err := Run(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &stdout,
		Stderr:     &stderr,
		GitPath:    git,
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    commandThatPrints(t),
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("run hook log: %v", err)
	}

	runDirs, err := os.ReadDir(filepath.Join(root, ".coding-ethos", "hook-runs"))
	if err != nil {
		t.Fatalf("read hook runs: %v", err)
	}

	if len(runDirs) != 1 {
		t.Fatalf("hook run dirs = %d, want 1", len(runDirs))
	}

	runDir := filepath.Join(root, ".coding-ethos", "hook-runs", runDirs[0].Name())
	assertFileContains(t, filepath.Join(runDir, "stdout.log"), "hello stdout")
	assertFileContains(t, filepath.Join(runDir, "stderr.log"), "hello stderr")
	assertFileContains(
		t,
		filepath.Join(runDir, "metadata.env"),
		"started_at_utc='20260501T123456Z'",
	)
	assertFileContains(t, filepath.Join(runDir, "metadata.env"), "exit_code='0'")

	if !strings.Contains(stdout.String(), "hello stdout") {
		t.Fatalf("stdout was not mirrored: %q", stdout.String())
	}

	if !strings.Contains(stderr.String(), "hello stderr") {
		t.Fatalf("stderr was not mirrored: %q", stderr.String())
	}
}

func TestRunKeepsRepositoryIdentityWhileWritingToPrivateStateRoot(t *testing.T) {
	t.Parallel()

	repositoryRoot := t.TempDir()
	stateRoot := t.TempDir()
	err := Run(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		GitPath:    fakeGit(t),
		Root:       repositoryRoot,
		StateRoot:  stateRoot,
		BundleRoot: filepath.Join(repositoryRoot, "pre-commit"),
		Command:    commandThatPrints(t),
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("run hook log with private state: %v", err)
	}

	runDir := onlyHookRunDir(t, stateRoot)
	assertFileContains(
		t,
		filepath.Join(runDir, "metadata.env"),
		"repo_root='"+repositoryRoot+"'",
	)
	if _, err := os.Stat(
		filepath.Join(repositoryRoot, ".coding-ethos", "hook-runs"),
	); !os.IsNotExist(
		err,
	) {
		t.Fatalf("repository-local hook state was created: %v", err)
	}
}

func TestRunSuppressesDebugLogByDefault(t *testing.T) {
	root := t.TempDir()
	writeHookLogIgnore(t, root)

	err := Run(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		GitPath:    fakeGit(t),
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    commandThatPrints(t),
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("run hook log: %v", err)
	}

	runDir := onlyHookRunDir(t, root)
	_, err = os.Stat(filepath.Join(runDir, "debug.log"))
	if !os.IsNotExist(err) {
		t.Fatalf("debug.log should be absent by default: %v", err)
	}
	assertFileContains(t, filepath.Join(runDir, "metadata.env"), "debug='false'")
}

func TestRunWritesDebugLogAndStderrWhenEnabled(t *testing.T) {
	root := t.TempDir()
	writeHookLogIgnore(t, root)

	var stderr bytes.Buffer
	err := Run(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &bytes.Buffer{},
		Stderr:     &stderr,
		GitPath:    fakeGit(t),
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    fakeMake(t),
		Debug:      true,
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("run hook log: %v", err)
	}

	runDir := onlyHookRunDir(t, root)
	debugPath := filepath.Join(runDir, "debug.log")
	assertFileContains(t, debugPath, `"event":"hook.run.enter"`)
	assertFileContains(t, debugPath, `"event":"hook.run.exit"`)
	assertFileContains(t, debugPath, `"event":"make.process.enter"`)
	assertFileContains(t, debugPath, `"event":"make.process.exit"`)
	assertFileContains(t, filepath.Join(runDir, "metadata.env"), "debug='true'")

	if !strings.Contains(stderr.String(), `"event":"hook.run.enter"`) {
		t.Fatalf("stderr did not receive debug logs: %q", stderr.String())
	}
}

func TestRunChecksIgnoresWithoutIndex(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeHookLogIgnore(t, root)
	logPath := filepath.Join(t.TempDir(), "git-args.log")
	git := fakeGitWithLog(t, logPath)

	err := Run(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		GitPath:    git,
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    commandThatPrints(t),
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("run hook log: %v", err)
	}

	assertFileContains(t, logPath, "check-ignore --no-index --quiet")
}

func TestRunIngestsHookTraceIntoCodeIntel(t *testing.T) {
	t.Parallel()
	testlock.ProcessState(t, "hooklog-global-streams")

	root := t.TempDir()
	writeHookLogIgnore(t, root)

	status, err := RunInProcess(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		GitPath:    fakeGit(t),
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    []string{"coding-ethos-run", "agent-hook"},
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
		},
	}, func() int {
		tracePath := filepath.Join(os.Getenv("CODE_ETHOS_HOOK_RUN_DIR"), "event.json")

		// #nosec G703 -- test path is created by the hook runner under t.TempDir.
		err := os.WriteFile(tracePath, []byte(`{
  "schema_version": 1,
  "trace_id": "hook-trace-1",
  "recorded_at_utc": "2026-05-01T12:34:56Z",
  "provider": "codex",
  "event": "PreToolUse",
  "tool": "Read",
  "status": "allow"
}`), 0o600)
		if err != nil {
			t.Fatalf("write hook trace: %v", err)
		}

		return 0
	})
	if err != nil {
		t.Fatalf("RunInProcess returned error: %v", err)
	}

	if status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}

	store, err := codeintel.Open(
		t.Context(),
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	defer store.Close()

	stats, err := store.Stats(t.Context())
	if err != nil {
		t.Fatalf("query code-intel stats: %v", err)
	}

	if stats.Traces != 1 || stats.HookEvents != 1 {
		t.Fatalf("stats = %#v, want one hook trace", stats)
	}
}

func TestRunForcesCodeIntelRefreshForPreCommit(t *testing.T) {
	t.Parallel()
	testlock.ProcessState(t, "hooklog-global-streams")

	root := t.TempDir()
	writeHookLogIgnore(t, root)

	err := os.WriteFile(
		filepath.Join(root, "app.js"),
		[]byte("export function run() { return 1; }\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write source: %v", err)
	}

	status, err := RunInProcess(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		GitPath:    fakeGit(t),
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    []string{"coding-ethos-run", "git-hook", "pre-commit"},
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
		},
	}, func() int {
		return 0
	})
	if err != nil {
		t.Fatalf("RunInProcess returned error: %v", err)
	}

	if status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}

	store, err := codeintel.Open(
		t.Context(),
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	defer store.Close()

	file, found, err := store.GetCodeFile(t.Context(), "app.js")
	if err != nil {
		t.Fatalf("query indexed file: %v", err)
	}

	if !found || file.Language != "javascript" || file.SourceModTimeUTC == "" {
		t.Fatalf("indexed file = %#v, found=%v", file, found)
	}
}

func TestRunInProcessRestoresGlobalStreamsAfterPanic(t *testing.T) {
	t.Parallel()
	testlock.ProcessState(t, "hooklog-global-streams")

	root := t.TempDir()
	writeHookLogIgnore(t, root)
	git := fakeGit(t)

	originalStdout := os.Stdout
	originalStderr := os.Stderr

	var recovered any

	func() {
		defer func() {
			recovered = recover()
		}()

		status, err := RunInProcess(Options{
			Stdin:      strings.NewReader(""),
			Stdout:     &bytes.Buffer{},
			Stderr:     &bytes.Buffer{},
			GitPath:    git,
			Root:       root,
			BundleRoot: filepath.Join(root, "pre-commit"),
			Command:    []string{"in-process"},
			Now: func() time.Time {
				return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
			},
		}, func() int {
			panic("boom")
		})
		if err != nil {
			t.Fatalf("RunInProcess returned unexpected error: %v", err)
		}

		if status != 0 {
			t.Fatalf("RunInProcess status = %d, want 0 before panic", status)
		}
	}()

	if recovered == nil {
		t.Fatal("RunInProcess panic was not propagated")
	}

	if os.Stdout != originalStdout {
		t.Fatal("RunInProcess did not restore os.Stdout after panic")
	}

	if os.Stderr != originalStderr {
		t.Fatal("RunInProcess did not restore os.Stderr after panic")
	}
}

func TestRunRejectsInvalidHookLogInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		want    string
		options Options
	}{
		{
			name:    "missing command",
			options: Options{Root: t.TempDir(), BundleRoot: "pre-commit"},
			want:    "command is required",
		},
		{
			name: "internal command",
			options: Options{
				Root:       t.TempDir(),
				BundleRoot: "pre-commit",
				Command:    []string{"coding-ethos-policy"},
			},
			want: "hook-log must not execute coding-ethos commands",
		},
		{
			name: "missing root",
			options: Options{
				BundleRoot: "pre-commit",
				Command:    []string{"echo"},
			},
			want: "root is required",
		},
		{
			name: "missing bundle root",
			options: Options{
				Root:    t.TempDir(),
				Command: []string{"echo"},
			},
			want: "bundle root is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := Run(test.options)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Run() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestRunRepairsMissingCodingEthosLogsIgnore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := Run(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		GitPath:    fakeFailingGit(t),
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    commandThatPrints(t),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want automatic missing ignore repair", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read repaired gitignore: %v", err)
	}
	if !strings.Contains(string(payload), ".coding-ethos/hook-runs/") {
		t.Fatalf("runtime hook log ignore missing:\n%s", payload)
	}
}

func TestRunRepairsBroadCodingEthosIgnore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := os.WriteFile(
		filepath.Join(root, ".gitignore"),
		[]byte(".coding-ethos/**\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	err = Run(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		GitPath:    fakeGit(t),
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    commandThatPrints(t),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want automatic broad ignore repair", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read repaired gitignore: %v", err)
	}
	text := string(payload)
	if strings.Contains(text, ".coding-ethos/**") {
		t.Fatalf("broad ignore still present:\n%s", text)
	}
	if !strings.Contains(text, ".coding-ethos/hook-runs/") {
		t.Fatalf("runtime hook log ignore missing:\n%s", text)
	}
}

func TestRunReturnsCommandExitCode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeHookLogIgnore(t, root)

	err := Run(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		GitPath:    fakeGit(t),
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    []string{os.Args[0], "-test.run=^$", "--", "exit-7"},
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
		},
	})

	var exitCoder interface{ ExitCode() int }
	if !errors.As(err, &exitCoder) || exitCoder.ExitCode() != 7 {
		t.Fatalf("Run() error = %v, want command exit code 7", err)
	}
}

func fakeGit(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "git")

	script := fakeGitCheckIgnoreScript("")

	writeExecutableTestFile(t, path, script)

	return path
}

func writeHookLogIgnore(t *testing.T, root string) {
	t.Helper()

	err := os.WriteFile(
		filepath.Join(root, ".gitignore"),
		[]byte(".coding-ethos/hook-runs/\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
}

func fakeFailingGit(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "git")

	script := "#!/usr/bin/env bash\nexit 1\n"

	writeExecutableTestFile(t, path, script)

	return path
}

func fakeGitWithLog(t *testing.T, logPath string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "git")

	script := fakeGitCheckIgnoreScript(logPath)

	writeExecutableTestFile(t, path, script)

	return path
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func fakeGitCheckIgnoreScript(logPath string) string {
	logLine := ""
	if logPath != "" {
		logLine = "printf '%s\\n' \"$*\" >> " + shellQuoteForTest(logPath) + "\n"
	}

	return `#!/usr/bin/env bash
` + logLine + `root=""
target=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-C" ]; then
    root="$2"
    shift 2
    continue
  fi
  target="$1"
  shift
done

gitignore="$root/.gitignore"
if [ ! -f "$gitignore" ]; then
  exit 1
fi

if [ "$target" = ".coding-ethos/memories/MEMORY.md" ]; then
  grep -Eq '^[[:space:]]*(\.coding-ethos/?|\.coding-ethos/\*\*|\.coding-ethos/\*)[[:space:]]*$' "$gitignore"
  exit $?
fi

if [ "$target" = ".coding-ethos/hook-runs/" ]; then
  grep -Eq '^[[:space:]]*(\.coding-ethos/hook-runs/?|\.coding-ethos/?|\.coding-ethos/\*\*|\.coding-ethos/\*)[[:space:]]*$' "$gitignore"
  exit $?
fi

exit 1
`
}

func commandThatPrints(t *testing.T) []string {
	t.Helper()

	return []string{os.Args[0], "-test.run=^$", "--", "print"}
}

func fakeMake(t *testing.T) []string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "make")

	script := "#!/usr/bin/env bash\nexit 0\n"

	writeExecutableTestFile(t, path, script)

	return []string{path, "check"}
}

func writeExecutableTestFile(t *testing.T, path, script string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		t.Fatalf("create executable fixture: %v", err)
	}

	if _, err = file.WriteString(script); err != nil {
		_ = file.Close()
		t.Fatalf("write executable fixture: %v", err)
	}
	if err = file.Close(); err != nil {
		t.Fatalf("close executable fixture: %v", err)
	}
}

func onlyHookRunDir(t *testing.T, root string) string {
	t.Helper()

	runDirs, err := os.ReadDir(filepath.Join(root, ".coding-ethos", "hook-runs"))
	if err != nil {
		t.Fatalf("read hook runs: %v", err)
	}

	if len(runDirs) != 1 {
		t.Fatalf("hook run dirs = %d, want 1", len(runDirs))
	}

	return filepath.Join(root, ".coding-ethos", "hook-runs", runDirs[0].Name())
}

func assertFileContains(t *testing.T, path, substring string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	if !strings.Contains(string(content), substring) {
		t.Fatalf("%s does not contain %q:\n%s", path, substring, string(content))
	}
}

func TestMain(m *testing.M) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-1] != "print" {
		if len(os.Args) >= 3 && os.Args[len(os.Args)-1] == "exit-7" {
			os.Exit(7)
		}

		os.Exit(m.Run())
	}

	os.Stdout.WriteString("hello stdout\n")
	os.Stderr.WriteString("hello stderr\n")
	os.Exit(0)
}
