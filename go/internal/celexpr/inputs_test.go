// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
)

func TestValidateAcceptsPathDiagnosticFindingAndRepoInputs(t *testing.T) {
	t.Parallel()

	source := `
		metadata.schema_version == 1 &&
		event.provider == "codex" &&
		event.is_codex &&
		event.tool_input_keys.exists(key, key == "command") &&
		diff.has_changes &&
		diff.hunks.exists(hunk, hunk.file == "src/app.py") &&
		diff.added_lines.exists(line, line.text.contains("pass")) &&
		git_command.is_git &&
		git_command.subcommand == "status" &&
		paths.exists(path, path.ext == ".py" && path.is_test) &&
		file_changes.exists(file, file.ext == ".py" && file.line_count >= 0) &&
		shell_commands.exists(cmd, cmd.name == "git") &&
		diagnostics.exists(item, item.tool == "ruff") &&
		diagnostic.tool == "ruff" &&
		findings.exists(item, item.code == "F401") &&
		finding.file.endsWith("test_policy.py") &&
		repo.python_version == "3.13"
	`
	if err := Validate("test.path_scope", source); err != nil {
		t.Fatalf("validate CEL expression: %v", err)
	}
}

func TestValidateRejectsUnknownInputs(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`env.HOME != ""`,
		`path.fiel == "pkg/app.py"`,
		`diagnostic.line.contains("1")`,
	} {
		err := Validate("test.unknown_input", source)
		if err == nil {
			t.Fatalf("validate CEL expression %q succeeded, want compile error", source)
		}
	}
}

func TestValidateAcceptsReviewedHelperFunctions(t *testing.T) {
	t.Parallel()

	source := `
		has_prefix(path.file, "src/") &&
		has_suffix(path.file, ".py") &&
		glob_match("src/**/*.py", path.file) &&
		is_test_path(path.file) &&
		!is_generated_path(path.file) &&
		in_source_root(path.file, repo.source_roots) &&
		list_contains(files, path.file) &&
		paths.exists(item, item.file == path.file) &&
		any_has_prefix(files, "src/") &&
		any_has_suffix(files, ".py")
	`
	if err := Validate("test.helpers", source); err != nil {
		t.Fatalf("validate CEL expression: %v", err)
	}
}

func TestValidateAcceptsExpandedHelperFunctions(t *testing.T) {
	t.Parallel()

	source := `
		command_fact.has_inline_env &&
		command_invokes(command, "git") &&
		argv_invokes(argv, "git") &&
		argv_command_is(argv, "git") &&
		shell_commands.exists(cmd,
			cmd.name == "git" &&
			cmd.is_git &&
			list_contains(cmd.assignments, "CODE_ETHOS_CONSUMER_ROOT=/repo") &&
			!cmd.is_shell_exec &&
			!cmd.has_process_substitution &&
			!cmd.has_command_substitution
		) &&
		git_command.is_git &&
		list_contains(git_command.flags, "-f") &&
		has_inline_env(command, "CODE_ETHOS_CONSUMER_ROOT") &&
		lint_code_matches(diagnostic.code, "S*") &&
		repo_config_present(files, config.candidates) &&
		is_protected_path(path.file, repo.protected_paths) &&
		is_protected_branch(git.current_branch, repo.protected_branches) &&
		any_glob_match(["src/**/*.py"], path.file)
	`
	if err := Validate("test.expanded_helpers", source); err != nil {
		t.Fatalf("validate CEL expression: %v", err)
	}
}

func TestProgramEvaluatesRecursiveGlobHelper(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.glob_helper",
		`glob_match("src/**/*.py", path.file)`,
	)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		Files: []string{"src/package/nested/module.py"},
	}))
	if err != nil {
		t.Fatalf("evaluate CEL program: %v", err)
	}
	if matched, ok := output.Value().(bool); !ok || !matched {
		t.Fatalf("glob helper output = %#v, want true", output.Value())
	}
}

func TestProgramEvaluatesExpandedHelpers(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.expanded_helper_eval",
		`
			command_fact.has_inline_env &&
			command_invokes(command, "git") &&
			argv_invokes(argv, "git") &&
			argv_command_is(argv, "git") &&
			git_command.is_git &&
			git_command.subcommand == "worktree" &&
			list_contains(git_command.flags, "-f") &&
			has_inline_env(command, "CODE_ETHOS_CONSUMER_ROOT") &&
			lint_code_matches(diagnostic.code, "S*") &&
			repo_config_present(files, config.candidates) &&
			is_protected_path("coding-ethos-hooks/bin/coding-ethos-policy", repo.protected_paths) &&
			is_protected_branch(git.current_branch, repo.protected_branches) &&
			paths.exists(item, any_glob_match(["src/**/*.py"], item.file))
		`,
	)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		Argv:              []string{"git", "worktree", "remove", "-f", "../repo-old"},
		Command:           "CODE_ETHOS_CONSUMER_ROOT=/repo git status",
		ConfigCandidates:  []string{"repo_config.yaml"},
		CurrentBranch:     "main",
		Diagnostic:        &diagnostics.Diagnostic{Code: "S101"},
		Files:             []string{"src/package/module.py", "repo_config.yaml"},
		ProtectedBranches: []string{"main"},
		ProtectedPaths:    []string{"coding-ethos-hooks/bin/coding-ethos-policy"},
		Tool:              "Bash",
	}))
	if err != nil {
		t.Fatalf("evaluate CEL program: %v", err)
	}
	if matched, ok := output.Value().(bool); !ok || !matched {
		t.Fatalf("expanded helper output = %#v, want true", output.Value())
	}
}

func TestProgramEvaluatesArgvCommandHelperAgainstLeadingAssignments(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.argv_command_helper_eval",
		`argv_command_is(argv, "git") && !argv_command_is(["echo", "git"], "git")`,
	)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		Argv: []string{"CODE_ETHOS_CONSUMER_ROOT=/repo", "/usr/bin/git", "status"},
	}))
	if err != nil {
		t.Fatalf("evaluate CEL program: %v", err)
	}
	if matched, ok := output.Value().(bool); !ok || !matched {
		t.Fatalf("argv command helper output = %#v, want true", output.Value())
	}
}

func TestActivationPopulatesShellCommandInputs(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Command: "FOO=bar git status -s 2>&1 | grep file && ruff check .",
	})

	commands, ok := activation["shell_commands"].([]ShellCommandInput)
	if !ok {
		t.Fatalf("shell commands input = %#v", activation["shell_commands"])
	}
	if len(commands) != 3 {
		t.Fatalf("shell command count = %d, want 3: %#v", len(commands), commands)
	}
	if commands[0].Name != "git" ||
		!commands[0].HasInlineEnv ||
		!commands[0].IsGit ||
		!listContains(commands[0].Assignments, "FOO=bar") ||
		!listContains(commands[0].Redirects, "2>&1") {
		t.Fatalf("first shell command = %#v", commands[0])
	}
	if commands[1].Name != "grep" ||
		commands[2].Name != "ruff" ||
		!commands[2].IsLintTool {
		t.Fatalf("shell commands = %#v", commands)
	}
}

func TestActivationPopulatesRichShellCommandInputs(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Command: "PATH=/tmp:$PATH bash -c 'git status' && env FOO=bar ruff check .",
	})

	commands, ok := activation["shell_commands"].([]ShellCommandInput)
	if !ok {
		t.Fatalf("shell commands input = %#v", activation["shell_commands"])
	}
	if len(commands) != 2 {
		t.Fatalf("shell command count = %d, want 2: %#v", len(commands), commands)
	}
	if !commands[0].IsShellExec ||
		!commands[0].UsesPathOverride ||
		!commands[0].HasDynamicExpansion {
		t.Fatalf("shell exec facts mismatch: %#v", commands[0])
	}
	if !commands[1].IsLintTool {
		t.Fatalf("wrapped lint command mismatch: %#v", commands[1])
	}
}

func TestActivationPopulatesGitCommandInput(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Argv: []string{
			"CODE_ETHOS_CONSUMER_ROOT=/repo",
			"/usr/bin/git",
			"-C",
			"/repo",
			"worktree",
			"remove",
			"-fd",
			"../repo-old",
		},
	})

	gitCommand, ok := activation["git_command"].(GitCommandInput)
	if !ok {
		t.Fatalf("git command input = %#v", activation["git_command"])
	}
	if !gitCommand.IsGit ||
		!gitCommand.HasChangeDir ||
		gitCommand.Subcommand != "worktree" ||
		len(gitCommand.Args) != 3 ||
		!listContains(gitCommand.Flags, "-f") ||
		!listContains(gitCommand.Flags, "-d") ||
		len(gitCommand.Targets) != 2 ||
		gitCommand.Targets[0] != "remove" ||
		gitCommand.Targets[1] != "../repo-old" {
		t.Fatalf("git command input = %#v", gitCommand)
	}
}

func TestActivationPopulatesFileChangeInputs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	runTestGit(t, repo, "config", "user.name", "Test User")

	file := filepath.Join(repo, "src", "app.py")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(file, []byte("print('one')\nprint('two')\n"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	runTestGit(t, repo, "add", "src/app.py")

	activation := Activation(ActivationInput{
		Cwd:            repo,
		Files:          []string{"src/app.py"},
		ProtectedPaths: []string{"src/app.py"},
	})

	fileChanges, ok := activation["file_changes"].([]FileChangeInput)
	if !ok || len(fileChanges) != 1 {
		t.Fatalf("file_changes input = %#v", activation["file_changes"])
	}
	fileChange := fileChanges[0]
	if fileChange.File != "src/app.py" ||
		fileChange.Status != "A" ||
		!fileChange.IsAdded ||
		!fileChange.IsProtected ||
		fileChange.LineCount != 2 ||
		fileChange.SizeBytes == 0 ||
		fileChange.OriginalLineCount != -1 {
		t.Fatalf("file change input = %#v", fileChange)
	}
}

func TestActivationPopulatesDiffHunkInputs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	runTestGit(t, repo, "config", "user.name", "Test User")

	file := filepath.Join(repo, "src", "app.py")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(file, []byte("print('one')\nprint('two')\n"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	runTestGit(t, repo, "add", "src/app.py")
	runTestGit(t, repo, "commit", "-m", "feat: seed")

	if err := os.WriteFile(
		file,
		[]byte("print('one')\nprint('two changed')\nprint('three')\n"),
		0o600,
	); err != nil {
		t.Fatalf("rewrite source file: %v", err)
	}
	runTestGit(t, repo, "add", "src/app.py")

	activation := Activation(ActivationInput{
		Cwd:   repo,
		Files: []string{"src/app.py"},
	})

	diff, ok := activation["diff"].(DiffInput)
	if !ok || len(diff.Hunks) != 1 {
		t.Fatalf("diff input = %#v", activation["diff"])
	}
	hunk := diff.Hunks[0]
	if hunk.File != "src/app.py" ||
		hunk.OldStart != 2 ||
		hunk.NewStart != 2 ||
		len(hunk.AddedLines) != 2 ||
		len(hunk.RemovedLines) != 1 ||
		hunk.AddedLines[0].Line != 2 ||
		hunk.AddedLines[0].Text != "print('two changed')" ||
		hunk.AddedLines[1].Line != 3 ||
		hunk.RemovedLines[0].Line != 2 ||
		hunk.RemovedLines[0].Text != "print('two')" {
		t.Fatalf("hunk input = %#v", hunk)
	}
	if len(diff.AddedLines) != 2 ||
		len(diff.RemovedLines) != 1 ||
		diff.AddedLines[1].NewLine != 3 {
		t.Fatalf("diff line summaries = %#v", diff)
	}
}

func TestActivationBuildsStablePathAndRepoInputs(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Cwd:           "/repo",
		Files:         []string{"./src/tests/test_policy.py"},
		Tool:          "ruff",
		SourceRoots:   []string{"src"},
		PythonVersion: "3.13",
	})

	pathInput, ok := activation["path"].(PathInput)
	if !ok {
		t.Fatalf("path input = %#v", activation["path"])
	}
	if pathInput.File != "src/tests/test_policy.py" ||
		pathInput.Dir != "src/tests" ||
		pathInput.Base != "test_policy.py" ||
		pathInput.Ext != ".py" ||
		!pathInput.IsTest ||
		!pathInput.InSourceRoot {
		t.Fatalf("path input = %#v", pathInput)
	}

	diagnostic, ok := activation["diagnostic"].(DiagnosticInput)
	if !ok || diagnostic.Tool != "" || diagnostic.File != "" {
		t.Fatalf("diagnostic input = %#v", activation["diagnostic"])
	}

	repo, ok := activation["repo"].(RepoInput)
	if !ok || repo.Root != "/repo" || repo.PythonVersion != "3.13" {
		t.Fatalf("repo input = %#v", activation["repo"])
	}

	paths, ok := activation["paths"].([]PathInput)
	if !ok || len(paths) != 1 || paths[0] != pathInput {
		t.Fatalf("paths input = %#v", activation["paths"])
	}

	metadata, ok := activation["metadata"].(MetadataInput)
	if !ok || metadata.SchemaVersion != SchemaVersion {
		t.Fatalf("metadata input = %#v", activation["metadata"])
	}
}

func runTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func TestActivationBuildsConfigGitAndCommandFacts(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Argv:              []string{"git", "status"},
		Command:           "CODE_ETHOS_CONSUMER_ROOT=/repo git status",
		ConfigCandidates:  []string{"repo_config.yaml", "repo_config.yml"},
		CurrentBranch:     "main",
		EventName:         "PreToolUse",
		EventMatcher:      "Bash",
		EventSource:       "codex-cli",
		Files:             []string{"repo_config.yaml", "pkg/app.py"},
		ChangedFiles:      []string{"pkg/app.py"},
		StagedFiles:       []string{"repo_config.yaml"},
		Provider:          "codex",
		SessionID:         "session-123",
		ProtectedBranches: []string{"main"},
		ProtectedPaths:    []string{"coding-ethos-hooks/bin/coding-ethos-policy"},
		Tool:              "Bash",
		ToolInputKeys:     []string{"command"},
		ToolResponseKeys:  []string{"stdout"},
		TranscriptPath:    "/tmp/transcript.jsonl",
		ReturnCode:        7,
		HasToolInput:      true,
		HasToolResponse:   true,
	})

	commandFact, ok := activation["command_fact"].(CommandInput)
	if !ok || commandFact.Raw == "" || !commandFact.HasInlineEnv {
		t.Fatalf("command_fact input = %#v", activation["command_fact"])
	}

	config, ok := activation["config"].(ConfigInput)
	if !ok || len(config.Candidates) != 2 || len(config.Present) != 1 ||
		config.Present[0] != "repo_config.yaml" {
		t.Fatalf("config input = %#v", activation["config"])
	}

	git, ok := activation["git"].(GitInput)
	if !ok || git.CurrentBranch != "main" || !git.OnProtectedBranch {
		t.Fatalf("git input = %#v", activation["git"])
	}
	if len(git.ChangedFiles) != 1 || len(git.StagedFiles) != 1 {
		t.Fatalf("git file facts = %#v", git)
	}

	diff, ok := activation["diff"].(DiffInput)
	if !ok || !diff.HasChanges || len(diff.ChangedFiles) != 1 || len(diff.StagedFiles) != 1 {
		t.Fatalf("diff input = %#v", activation["diff"])
	}

	event, ok := activation["event"].(EventInput)
	if !ok || event.Name != "PreToolUse" || event.Provider != "codex" ||
		event.Tool != "Bash" ||
		event.Matcher != "Bash" ||
		event.Source != "codex-cli" ||
		event.SessionID != "session-123" ||
		event.TranscriptPath != "/tmp/transcript.jsonl" ||
		event.ReturnCode != 7 ||
		!event.HasToolInput ||
		!event.HasToolResponse ||
		!event.IsCodex ||
		event.IsClaude ||
		event.IsGemini ||
		!listContains(event.ToolInputKeys, "command") ||
		!listContains(event.ToolResponseKeys, "stdout") {
		t.Fatalf("event input = %#v", activation["event"])
	}

	repo, ok := activation["repo"].(RepoInput)
	if !ok || len(repo.ProtectedBranches) != 1 || len(repo.ProtectedPaths) != 1 {
		t.Fatalf("repo input = %#v", activation["repo"])
	}
}

func TestActivationPopulatesExplicitDiagnosticInput(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Diagnostic: &diagnostics.Diagnostic{
			Tool:     "ruff",
			Code:     "F401",
			Message:  "unused import",
			File:     "./src/app.py",
			Line:     7,
			Column:   3,
			Severity: "error",
			PolicyID: "python.direct_imports",
		},
	})

	diagnostic, ok := activation["diagnostic"].(DiagnosticInput)
	if !ok {
		t.Fatalf("diagnostic input = %#v", activation["diagnostic"])
	}
	if diagnostic.Tool != "ruff" ||
		diagnostic.Code != "F401" ||
		diagnostic.Message != "unused import" ||
		diagnostic.File != "src/app.py" ||
		diagnostic.Line != 7 ||
		diagnostic.Column != 3 ||
		diagnostic.Severity != "error" ||
		diagnostic.PolicyID != "python.direct_imports" {
		t.Fatalf("diagnostic input = %#v", diagnostic)
	}

	diagnostics, ok := activation["diagnostics"].([]DiagnosticInput)
	if !ok || len(diagnostics) != 1 || diagnostics[0] != diagnostic {
		t.Fatalf("diagnostics input = %#v", activation["diagnostics"])
	}
}

func TestActivationPopulatesExplicitFindingInput(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Finding: &FindingActivation{
			Tool:         "mypy",
			Code:         "no-any-return",
			Message:      "Returning Any",
			File:         "./src/app.py",
			Line:         12,
			Severity:     "error",
			PolicyID:     "python.typing",
			SkillID:      "lint-remediation",
			PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
		},
	})

	finding, ok := activation["finding"].(FindingInput)
	if !ok {
		t.Fatalf("finding input = %#v", activation["finding"])
	}
	if finding.Tool != "mypy" ||
		finding.Code != "no-any-return" ||
		finding.Message != "Returning Any" ||
		finding.File != "src/app.py" ||
		finding.Line != 12 ||
		finding.Severity != "error" ||
		finding.PolicyID != "python.typing" ||
		finding.SkillID != "lint-remediation" ||
		len(finding.PrincipleIDs) != 1 {
		t.Fatalf("finding input = %#v", finding)
	}

	findings, ok := activation["findings"].([]FindingInput)
	if !ok || len(findings) != 1 || findings[0].Tool != "mypy" {
		t.Fatalf("findings input = %#v", activation["findings"])
	}
}

func TestActivationMarksTopLevelGeneratedDirectory(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Files: []string{"generated/client.py"},
	})

	pathInput, ok := activation["path"].(PathInput)
	if !ok {
		t.Fatalf("path input = %#v", activation["path"])
	}
	if !pathInput.IsGenerated {
		t.Fatalf("path input = %#v, want generated", pathInput)
	}
}

func TestActivationUsesExplicitPathsForMultipleFiles(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Files:       []string{"src/app.py", "tests/test_app.py"},
		SourceRoots: []string{"src"},
	})

	pathInput, ok := activation["path"].(PathInput)
	if !ok {
		t.Fatalf("path input = %#v", activation["path"])
	}
	if pathInput.File != "" {
		t.Fatalf("path input = %#v, want empty compatibility path", pathInput)
	}

	paths, ok := activation["paths"].([]PathInput)
	if !ok || len(paths) != 2 {
		t.Fatalf("paths input = %#v", activation["paths"])
	}
	if paths[0].File != "src/app.py" || !paths[0].InSourceRoot {
		t.Fatalf("first path input = %#v", paths[0])
	}
	if paths[1].File != "tests/test_app.py" || !paths[1].IsTest {
		t.Fatalf("second path input = %#v", paths[1])
	}
}
