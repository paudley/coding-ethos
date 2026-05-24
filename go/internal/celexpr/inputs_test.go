// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/celexpr"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	testLanguagePython     = "python"
	testSourceAppPath      = "src/app.py"
	testSymbolKindFunction = "function"
)

func TestValidateAcceptsPathDiagnosticFindingAndRepoInputs(t *testing.T) {
	t.Parallel()

	source := `
		metadata.schema_version == 1 &&
		event.provider == "codex" &&
		event.is_codex &&
		event.tool_input_keys.exists(key, key == "command") &&
		content.has_git_token &&
		diff.has_changes &&
		diff.hunks.exists(hunk, hunk.file == "src/app.py") &&
		diff.added_lines.exists(line, line.text.contains("pass")) &&
		changed_symbols.exists(symbol,
			symbol.file == "src/app.py" || symbol.file == ""
		) &&
		git_command.is_git &&
		git_command.subcommand == "status" &&
		paths.exists(path, path.ext == ".py" && path.is_test) &&
		file_changes.exists(file, file.ext == ".py" && file.line_count >= 0) &&
		shell_commands.exists(cmd, cmd.name == "git") &&
		diagnostics.exists(item, item.tool == "ruff") &&
		diagnostic.tool == "ruff" &&
		findings.exists(item, item.code == "F401") &&
		finding.file.endsWith("test_policy.py") &&
		repo.python_version == "3.13" &&
		repo.required_ignores.all(ignore, ignore.ignored || ignore.check_failed)
	`

	err := Validate("test.path_scope", source)
	if err != nil {
		t.Fatalf("validate CEL expression: %v", err)
	}
}

func TestSchemasDocumentCoreInputsAndHelpers(t *testing.T) {
	t.Parallel()

	inputSchema := strings.Join(InputSchema(), "\n")
	for _, want := range []string{
		"argv: list(string)",
		"shell_commands: list(",
		"proposed_file_changes: list(",
		"proxy: {",
		"strategic_intent",
		"active_todo",
		"python_ast: list(",
		"hook_commands: list(",
		"tool_capabilities: list(",
	} {
		if !strings.Contains(inputSchema, want) {
			t.Fatalf("input schema missing %q:\n%s", want, inputSchema)
		}
	}

	helperSchema := strings.Join(HelperSchema(), "\n")
	for _, want := range []string{
		"glob_match(pattern, value)",
		"command_invokes(command, tool)",
		"sed_writes_files(argv)",
		"is_protected_path(path, protected_paths)",
		"any_contains(values, value)",
	} {
		if !strings.Contains(helperSchema, want) {
			t.Fatalf("helper schema missing %q:\n%s", want, helperSchema)
		}
	}
}

func TestHookCommandInputsDetectUnsafeDocstringCoverageFlow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "docstring_coverage.go")
	if err := os.WriteFile(
		source,
		[]byte(`package hookrunnercli

func checkDocstringCoverageCommand() int {
	exitCode := runDocstringCoverage()
	return exitCode
}

func checkedDocstringCoverageCommand() int {
	if !scopeDocstringCoverageForHook() {
		return 0
	}
	return runDocstringCoverage()
}
`),
		0o600,
	); err != nil {
		t.Fatalf("write source: %v", err)
	}

	facts := HookCommandInputs(root, []string{"docstring_coverage.go"})
	if len(facts) != 2 {
		t.Fatalf("hook command facts = %#v, want two facts", facts)
	}

	unsafe, found := hookCommandFactByName(facts, "checkDocstringCoverageCommand")
	if !found {
		t.Fatalf("missing unsafe command fact: %#v", facts)
	}
	if !unsafe.UnsafeUnscopedPathSensitiveRun {
		t.Fatalf("unsafe command fact did not flag unscoped run: %#v", unsafe)
	}

	safe, found := hookCommandFactByName(facts, "checkedDocstringCoverageCommand")
	if !found {
		t.Fatalf("missing safe command fact: %#v", facts)
	}
	if safe.UnsafeUnscopedPathSensitiveRun || !safe.ChangedFileScopeBeforeRun {
		t.Fatalf("safe command fact = %#v, want scoped before run", safe)
	}
}

func TestHookCommandsCELInputAllowsChangedFileScopePolicy(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.hook_scope",
		`hook_commands.exists(command,
			command.command_function &&
			command.runs_path_sensitive_check &&
			!command.changed_file_scope_before_run
		)`,
	)
	if err != nil {
		t.Fatalf("compile hook command CEL expression: %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		HookCommands: []HookCommandInput{{
			SymbolName:                "checkDocstringCoverageCommand",
			CommandFunction:           true,
			RunsPathSensitiveCheck:    true,
			ChangedFileScopeBeforeRun: false,
		}},
	}))
	if err != nil {
		t.Fatalf("evaluate hook command CEL expression: %v", err)
	}
	if matched, ok := output.Value().(bool); !ok || !matched {
		t.Fatalf("hook command CEL output = %#v, want true", output.Value())
	}
}

func hookCommandFactByName(
	facts []HookCommandInput,
	name string,
) (HookCommandInput, bool) {
	for _, fact := range facts {
		if fact.SymbolName == name {
			return fact, true
		}
	}

	return HookCommandInput{}, false
}

func TestActivationCarriesStrategicIntentFact(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		EventName:       "PreToolUse",
		Provider:        "gemini",
		StrategicIntent: "edit only pkg/auth.go",
		ActiveTodo:      "implement runner policy",
	})

	event, ok := activation["event"].(EventInput)
	if !ok {
		t.Fatalf("event activation = %#v", activation["event"])
	}
	if event.StrategicIntent != "edit only pkg/auth.go" {
		t.Fatalf("strategic intent = %q", event.StrategicIntent)
	}
	if event.ActiveTodo != "implement runner policy" {
		t.Fatalf("active todo = %q", event.ActiveTodo)
	}
}

func TestProxyInputExposesPolicyAndDLPFacts(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.proxy_dlp",
		`proxy.kind == "provider_call" &&
		 proxy.direction == "outbound" &&
		 proxy.payload_kind == "prompt" &&
		 proxy.total_tokens > 100 &&
		 proxy.has_dlp_facts &&
		 proxy.dlp_facts.exists(fact, fact.type == "credential_filename")`,
	)
	if err != nil {
		t.Fatalf("compile proxy CEL expression: %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		Proxy: ProxyInput{
			EventID:      "evt-1",
			SessionID:    "sess-1",
			Kind:         "provider_call",
			Direction:    "outbound",
			PayloadKind:  "prompt",
			TotalTokens:  128,
			PayloadBytes: 4096,
			DLPFacts: []ProxyDLPFactInput{{
				Type:       "credential_filename",
				Path:       ".env",
				Confidence: "high",
			}},
		},
	}))
	if err != nil {
		t.Fatalf("evaluate proxy CEL expression: %v", err)
	}

	matched, ok := output.Value().(bool)
	if !ok {
		t.Fatalf("proxy CEL expression returned %T", output.Value())
	}

	if !matched {
		t.Fatalf("proxy CEL expression did not match")
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

	err := Validate("test.helpers", source)
	if err != nil {
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
		content.has_absolute_git_path &&
		content.has_python_subprocess &&
		git_command.is_git &&
		list_contains(git_command.flags, "-f") &&
		has_inline_env(command, "CODE_ETHOS_CONSUMER_ROOT") &&
		lint_code_matches(diagnostic.code, "S*") &&
		is_linter(diagnostic.tool) &&
		advertising_filter("[codex] generated output") &&
		repo_config_present(files, config.candidates) &&
		is_protected_path(path.file, repo.protected_paths) &&
		is_protected_branch(git.current_branch, repo.protected_branches) &&
		tool_capabilities.exists(tool,
			tool.name == "gemini-check" &&
			tool.requires_network &&
			list_contains(tool.tags, "network")
		) &&
		tool_capabilities.exists(tool,
			tool.name == "ruff" &&
			!tool.requires_network &&
			!tool.requires_git &&
			list_contains(tool.tags, "no-network") &&
			list_contains(tool.tags, "no-git")
		) &&
		any_glob_match(["src/**/*.py"], path.file) &&
		any_contains(["git status"], command_fact.lower) &&
		referenced_files.exists(file, file.file == "src/package/module.py")
	`

	err := Validate("test.expanded_helpers", source)
	if err != nil {
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

	if matched, found := output.Value().(bool); !found || !matched {
		t.Fatalf("glob helper output = %#v, want true", output.Value())
	}
}

func TestProgramEvaluatesSourceASTHelpers(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.source_ast_helper_eval",
		`source.has_nearby_test() && source.has_doc_chunk()`,
	)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	activation := Activation(ActivationInput{
		Source: SourceActivation{
			HasNearbyTest: true,
			HasDocChunk:   true,
		},
	})

	output, _, evalErr := program.Eval(activation)
	if evalErr != nil {
		t.Fatalf("evaluate CEL program: %v", evalErr)
	}

	if matched, ok := output.Value().(bool); !ok || !matched {
		t.Fatalf("Source AST helper output = %#v, want true", output.Value())
	}
}

func TestProgramEvaluatesASTHelpers(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.ast_helper_eval",
		`
			proposed_symbol_changes.all(s,
				s.kind_is("function") &&
				s.name_matches("run*")
			)
		`,
	)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	repo := t.TempDir()
	file := filepath.Join(repo, "app.py")
	content := "def run_tool():\n    pass\n"

	err = os.WriteFile(file, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	// OldContent must match file content so the edit produces proposed_symbol_changes.
	// Previously OldContent="def old():" didn't exist in the file, so the edit was a
	// no-op and proposed_symbol_changes was empty, making .all(...) vacuously true.
	activation := Activation(ActivationInput{
		Cwd:        repo,
		Files:      []string{"app.py"},
		Tool:       "Edit",
		OldContent: "def run_tool():",
		Content:    "def run_task():",
	})

	// Verify proposed_symbol_changes is non-empty before evaluating the helpers.
	changes, found := activation["proposed_symbol_changes"].([]ProposedSymbolChangeInput)
	if !found || len(changes) == 0 {
		t.Fatalf(
			"proposed_symbol_changes is empty; test cannot exercise symbol helpers. "+
				"activation=%#v",
			activation["proposed_symbol_changes"],
		)
	}

	output, _, err := program.Eval(activation)
	if err != nil {
		t.Fatalf("evaluate CEL program: %v", err)
	}

	if matched, ok := output.Value().(bool); !ok || !matched {
		t.Fatalf("AST helper output = %#v, want true", output.Value())
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
			is_protected_path(
				"coding-ethos-hooks/bin/coding-ethos-policy",
				repo.protected_paths
			) &&
			is_protected_branch(git.current_branch, repo.protected_branches) &&
			tool_capabilities.exists(tool,
				tool.name == "gemini-check" &&
				list_contains(tool.command, "gemini") &&
				tool.requires_network &&
				tool.sandbox_profile == "agent-network"
			) &&
			paths.exists(item, any_glob_match(["src/**/*.py"], item.file))
		`,
	)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		Argv: []string{
			"git",
			"worktree",
			"remove",
			"-f",
			"../repo-old",
		},
		Command:           "CODE_ETHOS_CONSUMER_ROOT=/repo git status",
		Content:           `import subprocess; subprocess.run(["/usr/bin/git", "status"])`,
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

	if matched, found := output.Value().(bool); !found || !matched {
		t.Fatalf("expanded helper output = %#v, want true", output.Value())
	}
}

func TestProgramEvaluatesNetworkCapabilityPolicy(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.network_capability",
		`
			!metadata.admin_approved &&
			tool_capabilities.exists(tool,
				tool.requires_network &&
				shell_commands.exists(command,
					command.name == tool.name ||
					list_contains(tool.command, command.name)
				)
			)
		`,
	)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		Command: "gemini --prompt check",
		Tool:    "Bash",
	}))
	if err != nil {
		t.Fatalf("evaluate CEL program: %v", err)
	}

	if matched, found := output.Value().(bool); !found || !matched {
		t.Fatalf("network capability output = %#v, want true", output.Value())
	}
}

func TestProgramEvaluatesProtectedSymlinkPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	linkPath := filepath.Join(root, "hook-link")

	inlineErr0 := os.Symlink(
		".git/coding-ethos-hooks/coding-ethos-git-hook",
		linkPath,
	)
	if inlineErr0 != nil {
		t.Fatalf("create symlink: %v", inlineErr0)
	}

	program, err := Program(
		"test.protected_symlink_path",
		`
			paths.exists(path,
				path.is_symlink &&
				is_protected_path(path.symlink_target, repo.protected_paths)
			)
		`,
	)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		Cwd:            root,
		Files:          []string{"hook-link"},
		ProtectedPaths: []string{".git/coding-ethos-hooks/coding-ethos-git-hook"},
	}))
	if err != nil {
		t.Fatalf("evaluate CEL program: %v", err)
	}

	if matched, found := output.Value().(bool); !found || !matched {
		t.Fatalf("symlink helper output = %#v, want true", output.Value())
	}
}

func TestProgramEvaluatesArgvCommandHelperAgainstLeadingAssignments(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.argv_command_helper_eval",
		`argv_command_is(argv, "git") &&
			!argv_command_is(["echo", "git"], "git")`,
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

	if matched, found := output.Value().(bool); !found || !matched {
		t.Fatalf("argv command helper output = %#v, want true", output.Value())
	}
}

func TestProgramEvaluatesArgvCommandHelperAgainstEmptyAssignment(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"test.argv_command_helper_empty_assignment_eval",
		`argv_command_is(argv, "git")`,
	)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		Argv: []string{"CODE_ETHOS_CONSUMER_ROOT=", "git", "status"},
	}))
	if err != nil {
		t.Fatalf("evaluate CEL program: %v", err)
	}

	if matched, found := output.Value().(bool); !found || !matched {
		t.Fatalf("argv command helper output = %#v, want true", output.Value())
	}
}

func TestProgramEvaluatesSedWritesFilesHelper(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		argv []string
		want bool
	}{
		{
			name: "read only print",
			argv: []string{"sed", "-n", "1,120p", "repo_config.yaml"},
			want: false,
		},
		{
			name: "in place long option",
			argv: []string{"sed", "--in-place=.bak", "s/old/new/", "app.py"},
			want: true,
		},
		{
			name: "combined in place short option",
			argv: []string{"sed", "-Ei", "s/old/new/", "app.py"},
			want: true,
		},
		{
			name: "explicit expression write command",
			argv: []string{"sed", "-n", "-e", "/old/w output.txt", "app.py"},
			want: true,
		},
		{
			name: "substitute write flag",
			argv: []string{"sed", "-n", "s/old/new/w output.txt", "app.py"},
			want: true,
		},
		{
			name: "script file does not inspect target",
			argv: []string{"sed", "-f", "script.sed", "app.py"},
			want: false,
		},
	}

	program, err := Program("test.sed_writes_files", `sed_writes_files(argv)`)
	if err != nil {
		t.Fatalf("compile CEL program: %v", err)
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, _, evalErr := program.Eval(Activation(ActivationInput{
				Argv: testCase.argv,
			}))
			if evalErr != nil {
				t.Fatalf("evaluate CEL program: %v", evalErr)
			}

			matched, found := output.Value().(bool)
			if !found || matched != testCase.want {
				t.Fatalf("sed_writes_files output = %#v, want %t", output.Value(), testCase.want)
			}
		})
	}
}

func TestActivationPopulatesShellCommandInputs(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Command: "FOO=bar git status -s 2>&1 | grep file && ruff check .",
	})

	commands, found := activation["shell_commands"].([]ShellCommandInput)
	if !found {
		t.Fatalf("shell commands input = %#v", activation["shell_commands"])
	}

	if len(commands) != 3 {
		t.Fatalf("shell command count = %d, want 3: %#v", len(commands), commands)
	}

	if commands[0].Name != "git" ||
		!commands[0].HasInlineEnv ||
		!commands[0].IsGit ||
		!slices.Contains(commands[0].Assignments, "FOO=bar") ||
		!slices.Contains(commands[0].Redirects, "2>&1") {
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

	commands, found := activation["shell_commands"].([]ShellCommandInput)
	if !found {
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

	gitCommand, found := activation["git_command"].(GitCommandInput)
	if !found {
		t.Fatalf("git command input = %#v", activation["git_command"])
	}

	if !gitCommand.IsGit ||
		!gitCommand.HasChangeDir ||
		gitCommand.Subcommand != "worktree" ||
		len(gitCommand.Args) != 3 ||
		!slices.Contains(gitCommand.Flags, "-f") ||
		!slices.Contains(gitCommand.Flags, "-d") ||
		len(gitCommand.Targets) != 2 ||
		gitCommand.Targets[0] != "remove" ||
		gitCommand.Targets[1] != "../repo-old" {
		t.Fatalf("git command input = %#v", gitCommand)
	}
}

func TestActivationPopulatesGitCommandGlobalOptionValues(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Argv: []string{
			"git",
			"-C",
			"/repo",
			"-c",
			"core.hooksPath=.git/hooks",
			"status",
		},
	})

	gitCommand, found := activation["git_command"].(GitCommandInput)
	if !found {
		t.Fatalf("git command input = %#v", activation["git_command"])
	}

	for _, want := range []string{"-C", "/repo", "-c", "core.hooksPath=.git/hooks"} {
		if !slices.Contains(gitCommand.GlobalOptions, want) {
			t.Fatalf(
				"git global options = %#v, missing %q",
				gitCommand.GlobalOptions,
				want,
			)
		}
	}
}

func TestActivationPopulatesGitHistoryRewriteFacts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		argv                 []string
		wantCommitAmend      bool
		wantForcePush        bool
		wantForcedBranchMove bool
	}{
		"commit amend": {
			argv:            []string{"git", "commit", "--amend", "-m", "fix"},
			wantCommitAmend: true,
		},
		"force push": {
			argv:          []string{"git", "push", "--force-with-lease", "origin", "topic"},
			wantForcePush: true,
		},
		"checkout forced branch move": {
			argv:                 []string{"git", "checkout", "-B", "topic", "main"},
			wantForcedBranchMove: true,
		},
		"branch forced move": {
			argv:                 []string{"git", "branch", "--force", "topic", "HEAD~1"},
			wantForcedBranchMove: true,
		},
		"rebase remains allowed": {
			argv: []string{"git", "rebase", "main"},
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			activation := Activation(ActivationInput{
				Argv: testCase.argv,
			})

			gitCommand, found := activation["git_command"].(GitCommandInput)
			if !found {
				t.Fatalf("git command input = %#v", activation["git_command"])
			}

			if gitCommand.HasCommitAmend != testCase.wantCommitAmend ||
				gitCommand.HasForcePush != testCase.wantForcePush ||
				gitCommand.HasForcedBranchMove != testCase.wantForcedBranchMove {
				t.Fatalf("git command input = %#v", gitCommand)
			}
		})
	}
}

func TestActivationPopulatesGitResetRewriteFacts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		argv                   []string
		protectedBranches      []string
		wantBranchRewriteReset bool
	}{
		"reset moves branch": {
			argv:                   []string{"git", "reset", "--soft", "HEAD~1"},
			wantBranchRewriteReset: true,
		},
		"reset protected branch": {
			argv:                   []string{"git", "reset", "main"},
			protectedBranches:      []string{"main"},
			wantBranchRewriteReset: true,
		},
		"reset protected remote branch": {
			argv:                   []string{"git", "reset", "origin/main"},
			protectedBranches:      []string{"main"},
			wantBranchRewriteReset: true,
		},
		"reset commit sha": {
			argv:                   []string{"git", "reset", "1234abc"},
			wantBranchRewriteReset: true,
		},
		"reset index to head": {
			argv: []string{"git", "reset", "HEAD"},
		},
		"reset single pathspec": {
			argv: []string{"git", "reset", "file.txt"},
		},
		"reset head pathspec without separator": {
			argv: []string{"git", "reset", "HEAD", "file.txt"},
		},
		"reset pathspec": {
			argv: []string{"git", "reset", "HEAD", "--", "file.txt"},
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			activation := Activation(ActivationInput{
				Argv:              testCase.argv,
				ProtectedBranches: testCase.protectedBranches,
			})

			gitCommand, found := activation["git_command"].(GitCommandInput)
			if !found {
				t.Fatalf("git command input = %#v", activation["git_command"])
			}

			if gitCommand.HasBranchRewriteReset != testCase.wantBranchRewriteReset {
				t.Fatalf("git command input = %#v", gitCommand)
			}
		})
	}
}

func TestActivationStopsGitFlagParsingAtPathspecSeparator(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Argv: []string{"git", "add", "--", "-not-a-flag"},
	})

	gitCommand, found := activation["git_command"].(GitCommandInput)
	if !found {
		t.Fatalf("git command input = %#v", activation["git_command"])
	}

	if slices.Contains(gitCommand.Flags, "-not-a-flag") {
		t.Fatalf("git flags include pathspec after --: %#v", gitCommand.Flags)
	}
}

func TestActivationPreservesCheckoutAndSwitchStartPoints(t *testing.T) {
	t.Parallel()

	for name, argv := range map[string][]string{
		"checkout": {"git", "checkout", "-b", "feature", "main"},
		"switch":   {"git", "switch", "-c", "feature", "main"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			activation := Activation(ActivationInput{
				Argv:              argv,
				ProtectedBranches: []string{"main"},
			})

			gitCommand, found := activation["git_command"].(GitCommandInput)
			if !found {
				t.Fatalf("git command input = %#v", activation["git_command"])
			}

			if !slices.Contains(gitCommand.Targets, "feature") ||
				!slices.Contains(gitCommand.Targets, "main") ||
				!gitCommand.HasCheckoutProtectedBranch {
				t.Fatalf("git command input = %#v", gitCommand)
			}
		})
	}
}

func TestActivationDoesNotTreatRemoteStartPointAsProtectedCheckout(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Argv: []string{
			"git",
			"checkout",
			"-b",
			"feature",
			"origin/main",
		},
		ProtectedBranches: []string{"main"},
	})

	gitCommand, found := activation["git_command"].(GitCommandInput)
	if !found {
		t.Fatalf("git command input = %#v", activation["git_command"])
	}

	if !slices.Contains(gitCommand.Targets, "origin/main") ||
		gitCommand.HasCheckoutProtectedBranch {
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

	err := os.MkdirAll(filepath.Dir(file), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(file, []byte("print('one')\nprint('two')\n"), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	runTestGit(t, repo, "add", testSourceAppPath)

	activation := Activation(ActivationInput{
		Cwd:            repo,
		Files:          []string{testSourceAppPath},
		ProtectedPaths: []string{testSourceAppPath},
	})

	fileChanges, found := activation["file_changes"].([]FileChangeInput)
	if !found || len(fileChanges) != 1 {
		t.Fatalf("file_changes input = %#v", activation["file_changes"])
	}

	fileChange := fileChanges[0]
	if fileChange.File != testSourceAppPath ||
		fileChange.Status != "A" ||
		!fileChange.IsAdded ||
		!fileChange.IsProtected ||
		fileChange.LineCount != 2 ||
		fileChange.SizeBytes == 0 ||
		fileChange.OriginalLineCount != -1 {
		t.Fatalf("file change input = %#v", fileChange)
	}
}

func TestActivationPopulatesProposedWriteFileChangeInputs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	file := filepath.Join(repo, "src", "app.py")

	err := os.MkdirAll(filepath.Dir(file), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(file, []byte("one\ntwo\n"), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	activation := Activation(ActivationInput{
		Cwd:     repo,
		Files:   []string{testSourceAppPath},
		Tool:    "Write",
		Content: "one\ntwo\nthree\n",
	})

	changes, found := activation["proposed_file_changes"].([]ProposedFileChangeInput)
	if !found || len(changes) != 1 {
		t.Fatalf(
			"proposed_file_changes input = %#v",
			activation["proposed_file_changes"],
		)
	}

	change := changes[0]
	if change.File != testSourceAppPath ||
		change.CurrentLineCount != 2 ||
		change.ProposedLineCount != 3 ||
		change.LineDelta != 1 ||
		!change.LineCountGrows ||
		change.LineCountShrinks {
		t.Fatalf("proposed change input = %#v", change)
	}
}

func TestActivationPopulatesProposedApplyPatchFileChangeInputs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	file := filepath.Join(repo, "go", "cmd", "coding-ethos-hook-runner", "main.go")

	err := os.MkdirAll(filepath.Dir(file), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	activation := Activation(ActivationInput{
		Cwd:  repo,
		Tool: "Edit",
		Command: `*** Begin Patch
*** Update File: go/cmd/coding-ethos-hook-runner/main.go
@@
-two
+two
+four
+five
*** End Patch
`,
	})

	changes, found := activation["proposed_file_changes"].([]ProposedFileChangeInput)
	if !found || len(changes) != 1 {
		t.Fatalf(
			"proposed_file_changes input = %#v",
			activation["proposed_file_changes"],
		)
	}

	assertApplyPatchFileChange(t, changes[0])
}

func TestActivationPopulatesProposedShrinkingEditFileChangeInputs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	file := filepath.Join(repo, "src", "app.py")

	err := os.MkdirAll(filepath.Dir(file), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	activation := Activation(ActivationInput{
		Cwd:        repo,
		Files:      []string{testSourceAppPath},
		Tool:       "Edit",
		OldContent: "two\nthree\n",
		Content:    "two\n",
	})

	changes, found := activation["proposed_file_changes"].([]ProposedFileChangeInput)
	if !found || len(changes) != 1 {
		t.Fatalf(
			"proposed_file_changes input = %#v",
			activation["proposed_file_changes"],
		)
	}

	change := changes[0]
	if change.ProposedLineCount != 2 ||
		change.LineDelta != -1 ||
		change.LineCountGrows ||
		!change.LineCountShrinks ||
		!change.ReplacementMatched ||
		change.ReplacementAmbiguous {
		t.Fatalf("proposed change input = %#v", change)
	}
}

func assertApplyPatchFileChange(t *testing.T, change ProposedFileChangeInput) {
	t.Helper()

	got := []any{
		change.File,
		change.CurrentLineCount,
		change.ProposedLineCount,
		change.LineDelta,
		change.NonBlankLineDelta,
		change.LineCountGrows,
		change.NonBlankLineCountGrows,
		change.HasProposedContent,
	}

	want := []any{
		"go/cmd/coding-ethos-hook-runner/main.go",
		int64(3),
		int64(5),
		int64(2),
		int64(2),
		true,
		true,
		false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("proposed apply_patch change fields = %#v, want %#v", got, want)
	}
}

func assertGrowingSymbolChange(t *testing.T, change ProposedSymbolChangeInput) {
	t.Helper()

	got := []any{
		change.File,
		change.Language,
		change.SymbolKind,
		change.SymbolName,
		change.Action,
		change.CurrentLineCount,
		change.ProposedLineCount,
		change.LineDelta,
		change.LineCountGrows,
		change.LineCountShrinks,
	}

	want := []any{
		testSourceAppPath,
		testLanguagePython,
		testSymbolKindFunction,
		"grow",
		"modified",
		int64(2),
		int64(3),
		int64(1),
		true,
		false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("symbol change fields = %#v, want %#v", got, want)
	}
}

func TestActivationPopulatesProposedSymbolChangeInputs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	file := filepath.Join(repo, "src", "app.py")

	err := os.MkdirAll(filepath.Dir(file), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	current := "def keep():\n    return 1\n\n"

	current += "def grow():\n    return 1\n"

	err = os.WriteFile(file, []byte(current), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	activation := Activation(ActivationInput{
		Cwd:        repo,
		Files:      []string{testSourceAppPath},
		Tool:       "Edit",
		OldContent: "def grow():\n    return 1\n",
		Content:    "def grow():\n    value = 1\n    return value\n",
	})

	changes, found := activation["proposed_symbol_changes"].([]ProposedSymbolChangeInput)
	if !found || len(changes) != 1 {
		t.Fatalf(
			"proposed_symbol_changes input = %#v",
			activation["proposed_symbol_changes"],
		)
	}

	assertGrowingSymbolChange(t, changes[0])
}

func TestActivationEstimatesAmbiguousEditAcrossAllMatches(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	file := filepath.Join(repo, "src", "app.py")

	err := os.MkdirAll(filepath.Dir(file), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(file, []byte("line\nline\n"), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	activation := Activation(ActivationInput{
		Cwd:        repo,
		Files:      []string{testSourceAppPath},
		Tool:       "Edit",
		OldContent: "line\n",
		Content:    "line\nextra\n",
	})

	changes, found := activation["proposed_file_changes"].([]ProposedFileChangeInput)
	if !found || len(changes) != 1 {
		t.Fatalf(
			"proposed_file_changes input = %#v",
			activation["proposed_file_changes"],
		)
	}

	change := changes[0]
	if !change.ReplacementMatched ||
		!change.ReplacementAmbiguous ||
		!change.LineCountGrows ||
		change.ProposedLineCount != 4 ||
		change.LineDelta != 2 {
		t.Fatalf("ambiguous proposed change input = %#v", change)
	}
}

func TestActivationPopulatesDiffHunkInputs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	runTestGit(t, repo, "config", "user.name", "Test User")

	file := filepath.Join(repo, "src", "app.py")

	err := os.MkdirAll(filepath.Dir(file), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(file, []byte("print('one')\nprint('two')\n"), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	runTestGit(t, repo, "add", testSourceAppPath)
	runTestGit(t, repo, "commit", "-m", "feat: seed")

	err = os.WriteFile(
		file,
		[]byte("print('one')\nprint('two changed')\nprint('three')\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("rewrite source file: %v", err)
	}

	runTestGit(t, repo, "add", testSourceAppPath)

	activation := Activation(ActivationInput{
		Cwd:   repo,
		Files: []string{testSourceAppPath},
	})

	diff, found := activation["diff"].(DiffInput)
	if !found || len(diff.Hunks) != 1 {
		t.Fatalf("diff input = %#v", activation["diff"])
	}

	assertDiffHunkInput(t, diff)
}

func TestActivationPopulatesChangedSymbolInputs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	runTestGit(t, repo, "config", "user.name", "Test User")

	file := filepath.Join(repo, "src", "app.py")

	err := os.MkdirAll(filepath.Dir(file), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(file, []byte("def build():\n    return 1\n"), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	runTestGit(t, repo, "add", testSourceAppPath)
	runTestGit(t, repo, "commit", "-m", "feat: seed")

	err = os.WriteFile(
		file,
		[]byte("def build():\n    value = 1\n    return value\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("rewrite source file: %v", err)
	}

	runTestGit(t, repo, "add", testSourceAppPath)

	activation := Activation(ActivationInput{
		Cwd:   repo,
		Files: []string{testSourceAppPath},
	})

	symbols, found := activation["changed_symbols"].([]ChangedSymbolInput)
	if !found || len(symbols) != 1 {
		t.Fatalf("changed_symbols = %#v", activation["changed_symbols"])
	}

	assertChangedSymbolInput(t, symbols[0])

	diff, found := activation["diff"].(DiffInput)
	if !found || len(diff.ChangedSymbols) != 1 ||
		diff.ChangedSymbols[0].SymbolName != "build" {
		t.Fatalf("diff changed symbols = %#v", activation["diff"])
	}
}

func assertDiffHunkInput(t *testing.T, diff DiffInput) {
	t.Helper()

	hunk := diff.Hunks[0]
	got := []any{
		hunk.File,
		hunk.OldStart,
		hunk.NewStart,
		len(hunk.AddedLines),
		len(hunk.RemovedLines),
		hunk.AddedLines[0].Line,
		hunk.AddedLines[0].Text,
		hunk.AddedLines[1].Line,
		hunk.RemovedLines[0].Line,
		hunk.RemovedLines[0].Text,
		len(diff.AddedLines),
		len(diff.RemovedLines),
		diff.AddedLines[1].NewLine,
	}

	want := []any{
		testSourceAppPath,
		int64(2),
		int64(2),
		2,
		1,
		int64(2),
		"print('two changed')",
		int64(3),
		int64(2),
		"print('two')",
		2,
		1,
		int64(3),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff hunk fields = %#v, want %#v", got, want)
	}
}

func assertChangedSymbolInput(t *testing.T, symbol ChangedSymbolInput) {
	t.Helper()

	got := []any{
		symbol.File,
		symbol.Language,
		symbol.SymbolKind,
		symbol.SymbolName,
		symbol.Action,
		symbol.LineCountGrows,
		symbol.OriginalLineCount,
		symbol.CurrentLineCount,
		symbol.LineDelta,
		len(symbol.ChangedLines) > 0,
	}

	want := []any{
		testSourceAppPath,
		testLanguagePython,
		testSymbolKindFunction,
		"build",
		"modified",
		true,
		int64(2),
		int64(3),
		int64(1),
		true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed symbol fields = %#v, want %#v", got, want)
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

	pathInput, found := activation["path"].(PathInput)
	if !found {
		t.Fatalf("path input = %#v", activation["path"])
	}

	assertStablePathInput(t, pathInput)
	assertStableRepoInputs(t, activation, pathInput)
}

func assertStablePathInput(t *testing.T, pathInput PathInput) {
	t.Helper()

	got := []any{
		pathInput.File,
		pathInput.Dir,
		pathInput.Base,
		pathInput.Ext,
		pathInput.IsTest,
		pathInput.InSourceRoot,
	}

	want := []any{
		"src/tests/test_policy.py",
		"src/tests",
		"test_policy.py",
		".py",
		true,
		true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("path input fields = %#v, want %#v", got, want)
	}
}

func assertStableRepoInputs(
	t *testing.T,
	activation map[string]any,
	pathInput PathInput,
) {
	t.Helper()

	diagnostic, found := activation["diagnostic"].(DiagnosticInput)
	if !found || diagnostic.Tool != "" || diagnostic.File != "" {
		t.Fatalf("diagnostic input = %#v", activation["diagnostic"])
	}

	repo, found := activation["repo"].(RepoInput)
	if !found || repo.Root != "/repo" || repo.PythonVersion != "3.13" {
		t.Fatalf("repo input = %#v", activation["repo"])
	}

	paths, found := activation["paths"].([]PathInput)
	if !found || len(paths) != 1 || paths[0] != pathInput {
		t.Fatalf("paths input = %#v", activation["paths"])
	}

	metadata, found := activation["metadata"].(MetadataInput)
	if !found || metadata.SchemaVersion != SchemaVersion {
		t.Fatalf("metadata input = %#v", activation["metadata"])
	}
}

func runTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), gitPath, args...)
	cmd.Dir = dir

	cmd.Env = append(
		os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)

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

	assertCommandFactInput(t, activation)
	assertConfigInput(t, activation)
	assertGitAndDiffInputs(t, activation)
	assertEventInput(t, activation)
	assertProtectedRepoInput(t, activation)
}

func assertCommandFactInput(t *testing.T, activation map[string]any) {
	t.Helper()

	commandFact, found := activation["command_fact"].(CommandInput)
	if !found || commandFact.Raw == "" || !commandFact.HasInlineEnv {
		t.Fatalf("command_fact input = %#v", activation["command_fact"])
	}
}

func assertConfigInput(t *testing.T, activation map[string]any) {
	t.Helper()

	config, found := activation["config"].(ConfigInput)
	if !found || len(config.Candidates) != 2 || len(config.Present) != 1 ||
		config.Present[0] != "repo_config.yaml" {
		t.Fatalf("config input = %#v", activation["config"])
	}
}

func assertGitAndDiffInputs(t *testing.T, activation map[string]any) {
	t.Helper()

	git, found := activation["git"].(GitInput)
	if !found || git.CurrentBranch != "main" || !git.OnProtectedBranch {
		t.Fatalf("git input = %#v", activation["git"])
	}

	if len(git.ChangedFiles) != 1 || len(git.StagedFiles) != 1 {
		t.Fatalf("git file facts = %#v", git)
	}

	diff, found := activation["diff"].(DiffInput)
	if !found || !diff.HasChanges || len(diff.ChangedFiles) != 1 ||
		len(diff.StagedFiles) != 1 {
		t.Fatalf("diff input = %#v", activation["diff"])
	}
}

func assertEventInput(t *testing.T, activation map[string]any) {
	t.Helper()

	event, found := activation["event"].(EventInput)
	if !found {
		t.Fatalf("event input = %#v", activation["event"])
	}

	got := []any{
		event.Name,
		event.Provider,
		event.Tool,
		event.Matcher,
		event.Source,
		event.SessionID,
		event.TranscriptPath,
		event.ReturnCode,
		event.HasToolInput,
		event.HasToolResponse,
		event.IsCodex,
		event.IsClaude,
		event.IsGemini,
		slices.Contains(event.ToolInputKeys, "command"),
		slices.Contains(event.ToolResponseKeys, "stdout"),
	}

	want := []any{
		"PreToolUse",
		"codex",
		"Bash",
		"Bash",
		"codex-cli",
		"session-123",
		"/tmp/transcript.jsonl",
		int64(7),
		true,
		true,
		true,
		false,
		false,
		true,
		true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event fields = %#v, want %#v", got, want)
	}
}

func assertProtectedRepoInput(t *testing.T, activation map[string]any) {
	t.Helper()

	repo, found := activation["repo"].(RepoInput)
	if !found || len(repo.ProtectedBranches) != 1 || len(repo.ProtectedPaths) != 1 {
		t.Fatalf("repo input = %#v", activation["repo"])
	}
}

func TestActivationExposesShellWriteTargets(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Command: `FILE=.claude/settings.json cat > ${FILE}; ` +
			`printf ok | tee -a .codex/config.toml; ` +
			`cp source.txt .gemini/settings.json`,
	})

	commands, found := activation["shell_commands"].([]ShellCommandInput)
	if !found || len(commands) != 4 {
		t.Fatalf("shell_commands input = %#v", activation["shell_commands"])
	}

	if !commands[0].HasWriteTargets ||
		!slices.Contains(commands[0].WriteTargets, ".claude/settings.json") {
		t.Fatalf("redirect write targets = %#v", commands[0])
	}

	if !slices.Contains(commands[2].WriteTargets, ".codex/config.toml") {
		t.Fatalf("tee write targets = %#v", commands[2])
	}

	if !slices.Contains(commands[3].WriteTargets, ".gemini/settings.json") {
		t.Fatalf("copy write targets = %#v", commands[3])
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

	diagnostic, found := activation["diagnostic"].(DiagnosticInput)
	if !found {
		t.Fatalf("diagnostic input = %#v", activation["diagnostic"])
	}

	assertExplicitDiagnosticInput(t, diagnostic)

	diagnostics, found := activation["diagnostics"].([]DiagnosticInput)
	if !found || len(diagnostics) != 1 || diagnostics[0] != diagnostic {
		t.Fatalf("diagnostics input = %#v", activation["diagnostics"])
	}
}

func TestActivationPopulatesCoverageInputsFromDiagnostics(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Diagnostics: []diagnostics.Diagnostic{
			{
				Metadata: map[string]any{
					"coverage_percent": 79.5,
				},
				Tool: "pytest",
				Code: "coverage-total",
			},
			{
				Metadata: map[string]any{
					"coverage_percent": json.Number("88.25"),
				},
				Tool: "pytest",
				File: "./pkg/app.py",
				Code: "coverage-file",
			},
			{
				Metadata: map[string]any{
					"coverage_percent": 82.4,
					"package":          "blackcat.ca/coding-ethos/go/pkg",
				},
				Tool: "go-test",
				Code: "coverage-package",
			},
		},
	})

	coverage, found := activation["coverage"].([]CoverageInput)
	if !found || len(coverage) != 3 {
		t.Fatalf("coverage input = %#v", activation["coverage"])
	}

	assertCoverageInput(t, coverage[0], CoverageInput{
		Tool:    "pytest",
		Code:    "coverage-total",
		Total:   true,
		Percent: 79.5,
	})
	assertCoverageInput(t, coverage[1], CoverageInput{
		Tool:    "pytest",
		File:    "pkg/app.py",
		Code:    "coverage-file",
		Percent: 88.25,
	})
	assertCoverageInput(t, coverage[2], CoverageInput{
		Tool:    "go-test",
		Code:    "coverage-package",
		Package: "blackcat.ca/coding-ethos/go/pkg",
		Percent: 82.4,
	})
}

func assertCoverageInput(t *testing.T, got, want CoverageInput) {
	t.Helper()

	if got != want {
		t.Fatalf("coverage input = %#v, want %#v", got, want)
	}
}

func TestProgramCanEvaluateCoverageInputs(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"testing.coverage_floor",
		"coverage.exists(item, item.tool == 'pytest' && item.total && item.percent < 80.0)",
	)
	if err != nil {
		t.Fatalf("Program() error = %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		Diagnostics: []diagnostics.Diagnostic{{
			Metadata: map[string]any{
				"coverage_percent": 79.5,
			},
			Tool: "pytest",
			Code: "coverage-total",
		}},
	}))
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	if output.Value() != true {
		t.Fatalf("coverage policy result = %v, want true", output.Value())
	}
}

func TestProgramCanEvaluateCoverageThresholdInputs(t *testing.T) {
	t.Parallel()

	program, err := Program(
		"testing.coverage_thresholds",
		`coverage.exists(item,
			item.tool == "go-test" &&
			item.total &&
			item.percent < coverage_thresholds.project.floor
		)`,
	)
	if err != nil {
		t.Fatalf("Program() error = %v", err)
	}

	output, _, err := program.Eval(Activation(ActivationInput{
		CoverageThresholds: CoverageThresholdsInput{
			Project: CoverageThresholdBandInput{Floor: 85.0},
		},
		Diagnostics: []diagnostics.Diagnostic{{
			Metadata: map[string]any{
				"coverage_percent": 84.5,
			},
			Tool: "go-test",
			Code: "coverage-total",
		}},
	}))
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}

	if output.Value() != true {
		t.Fatalf("coverage threshold policy result = %v, want true", output.Value())
	}
}

func assertExplicitDiagnosticInput(t *testing.T, diagnostic DiagnosticInput) {
	t.Helper()

	got := []any{
		diagnostic.Tool,
		diagnostic.Code,
		diagnostic.Message,
		diagnostic.File,
		diagnostic.Line,
		diagnostic.Column,
		diagnostic.Severity,
		diagnostic.PolicyID,
	}

	want := []any{
		"ruff",
		"F401",
		"unused import",
		testSourceAppPath,
		int64(7),
		int64(3),
		"error",
		"python.direct_imports",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostic fields = %#v, want %#v", got, want)
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
			Language:     testLanguagePython,
			SymbolName:   "build_value",
			SymbolKind:   testSymbolKindFunction,
			ChunkHash:    "sha256:abc",
			Line:         12,
			LineCount:    20,
			ChangedLines: 3,
			Severity:     "error",
			PolicyID:     "python.typing",
			SkillID:      "lint-remediation",
			PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
		},
	})

	finding, found := activation["finding"].(FindingInput)
	if !found {
		t.Fatalf("finding input = %#v", activation["finding"])
	}

	assertExplicitFindingInput(t, finding)

	findings, found := activation["findings"].([]FindingInput)
	if !found || len(findings) != 1 || findings[0].Tool != "mypy" {
		t.Fatalf("findings input = %#v", activation["findings"])
	}

	assertExplicitSourceInput(t, activation)
}

func assertExplicitFindingInput(t *testing.T, finding FindingInput) {
	t.Helper()

	got := []any{
		finding.Tool,
		finding.Code,
		finding.Message,
		finding.File,
		finding.Language,
		finding.SymbolName,
		finding.SymbolKind,
		finding.ChunkHash,
		finding.Line,
		finding.LineCount,
		finding.ChangedLines,
		finding.Severity,
		finding.PolicyID,
		finding.SkillID,
		len(finding.PrincipleIDs),
	}

	want := []any{
		"mypy",
		"no-any-return",
		"Returning Any",
		testSourceAppPath,
		testLanguagePython,
		"build_value",
		testSymbolKindFunction,
		"sha256:abc",
		int64(12),
		int64(20),
		int64(3),
		"error",
		"python.typing",
		"lint-remediation",
		1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("finding fields = %#v, want %#v", got, want)
	}
}

func assertExplicitSourceInput(t *testing.T, activation map[string]any) {
	t.Helper()

	source, found := activation["source"].(SourceInput)
	if !found {
		t.Fatalf("source input = %#v", activation["source"])
	}

	got := []any{
		source.Path,
		source.Language,
		source.SymbolName,
		source.SymbolKind,
		source.ChunkHash,
		source.LineCount,
		source.ChangedLines,
	}

	want := []any{
		testSourceAppPath,
		testLanguagePython,
		"build_value",
		testSymbolKindFunction,
		"sha256:abc",
		int64(20),
		int64(3),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source fields = %#v, want %#v", got, want)
	}
}

func TestActivationMarksTopLevelGeneratedDirectory(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Files: []string{"generated/client.py"},
	})

	pathInput, found := activation["path"].(PathInput)
	if !found {
		t.Fatalf("path input = %#v", activation["path"])
	}

	if !pathInput.IsGenerated {
		t.Fatalf("path input = %#v, want generated", pathInput)
	}
}

func TestActivationUsesExplicitPathsForMultipleFiles(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Files:       []string{testSourceAppPath, "tests/test_app.py"},
		SourceRoots: []string{"src"},
	})

	pathInput, found := activation["path"].(PathInput)
	if !found {
		t.Fatalf("path input = %#v", activation["path"])
	}

	if pathInput.File != "" {
		t.Fatalf("path input = %#v, want empty compatibility path", pathInput)
	}

	paths, found := activation["paths"].([]PathInput)
	if !found || len(paths) != 2 {
		t.Fatalf("paths input = %#v", activation["paths"])
	}

	if paths[0].File != testSourceAppPath || !paths[0].InSourceRoot {
		t.Fatalf("first path input = %#v", paths[0])
	}

	if paths[1].File != "tests/test_app.py" || !paths[1].IsTest {
		t.Fatalf("second path input = %#v", paths[1])
	}
}
