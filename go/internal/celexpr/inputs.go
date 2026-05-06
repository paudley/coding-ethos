// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"

	"blackcat.ca/coding-ethos/go/diagnostics"
)

type MetadataInput struct {
	Tool               string `json:"tool"`
	SchemaVersion      int64  `json:"schema_version"`
	AdminApproved      bool   `json:"admin_approved"`
	ReadOnlyInspection bool   `json:"read_only_inspection"`
}

type CommandInput struct {
	Lower        string   `json:"lower"`
	Raw          string   `json:"raw"`
	Tool         string   `json:"tool"`
	Argv         []string `json:"argv"`
	HasInlineEnv bool     `json:"has_inline_env"`
}

type ContentInput struct {
	Raw                 string `json:"raw"`
	Lower               string `json:"lower"`
	HasAbsoluteGitPath  bool   `json:"has_absolute_git_path"`
	HasGitToken         bool   `json:"has_git_token"`
	HasPathOverride     bool   `json:"has_path_override"`
	HasPythonSubprocess bool   `json:"has_python_subprocess"`
	HasShellExec        bool   `json:"has_shell_exec"`
}

type ShellCommandInput struct {
	Command                string   `json:"command"`
	Name                   string   `json:"name"`
	Argv                   []string `json:"argv"`
	Assignments            []string `json:"assignments"`
	Redirects              []string `json:"redirects"`
	WriteTargets           []string `json:"write_targets"`
	Column                 int64    `json:"column"`
	Line                   int64    `json:"line"`
	HasInlineEnv           bool     `json:"has_inline_env"`
	IsFunctionDeclaration  bool     `json:"is_function_declaration"`
	HasDynamicExpansion    bool     `json:"has_dynamic_expansion"`
	HasHeredoc             bool     `json:"has_heredoc"`
	Background             bool     `json:"background"`
	HasProcessSubstitution bool     `json:"has_process_substitution"`
	HasRedirects           bool     `json:"has_redirects"`
	HasSubshell            bool     `json:"has_subshell"`
	HasWriteTargets        bool     `json:"has_write_targets"`
	HasCommandSubstitution bool     `json:"has_command_substitution"`
	IsGitMutation          bool     `json:"is_git_mutation"`
	IsGit                  bool     `json:"is_git"`
	IsLintTool             bool     `json:"is_lint_tool"`
	PipesToShell           bool     `json:"pipes_to_shell"`
	IsShellExec            bool     `json:"is_shell_exec"`
	UsesPathOverride       bool     `json:"uses_path_override"`
	WrapsGitMutation       bool     `json:"wraps_git_mutation"`
}

type EventInput struct {
	TranscriptPath   string   `json:"transcript_path"`
	Matcher          string   `json:"matcher"`
	Mode             string   `json:"mode"`
	Provider         string   `json:"provider"`
	Scope            string   `json:"scope"`
	SessionID        string   `json:"session_id"`
	Source           string   `json:"source"`
	Tool             string   `json:"tool"`
	Name             string   `json:"name"`
	ToolInputKeys    []string `json:"tool_input_keys"`
	ToolResponseKeys []string `json:"tool_response_keys"`
	ReturnCode       int64    `json:"return_code"`
	HasToolInput     bool     `json:"has_tool_input"`
	HasToolResponse  bool     `json:"has_tool_response"`
	IsClaude         bool     `json:"is_claude"`
	IsCodex          bool     `json:"is_codex"`
	IsGemini         bool     `json:"is_gemini"`
}

type PathInput struct {
	File          string `json:"file"`
	Dir           string `json:"dir"`
	Base          string `json:"base"`
	Ext           string `json:"ext"`
	SymlinkTarget string `json:"symlink_target"`
	IsSymlink     bool   `json:"is_symlink"`
	IsTest        bool   `json:"is_test"`
	IsGenerated   bool   `json:"is_generated"`
	InSourceRoot  bool   `json:"in_source_root"`
}

type DiagnosticInput struct {
	Tool     string `json:"tool"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	File     string `json:"file"`
	Severity string `json:"severity"`
	PolicyID string `json:"policy_id"`
	Line     int64  `json:"line"`
	Column   int64  `json:"column"`
}

type RepoInput struct {
	ConfigCandidates  []string      `json:"config_candidates"`
	ProtectedBranches []string      `json:"protected_branches"`
	ProtectedPaths    []string      `json:"protected_paths"`
	PythonVersion     string        `json:"python_version"`
	RequiredIgnores   []IgnoreInput `json:"required_ignores"`
	Root              string        `json:"root"`
	SourceRoots       []string      `json:"source_roots"`
}

type IgnoreInput struct {
	Path        string `json:"path"`
	Error       string `json:"error"`
	Ignored     bool   `json:"ignored"`
	CheckFailed bool   `json:"check_failed"`
}

type ConfigInput struct {
	Candidates []string `json:"candidates"`
	Present    []string `json:"present"`
}

type GitInput struct {
	CurrentBranch      string   `json:"current_branch"`
	ProtectedBranches  []string `json:"protected_branches"`
	ProtectedPathFiles []string `json:"protected_path_files"`
	StagedFiles        []string `json:"staged_files"`
	ChangedFiles       []string `json:"changed_files"`
	OnProtectedBranch  bool     `json:"on_protected_branch"`
}

type GitCommandInput struct {
	Subcommand                 string   `json:"subcommand"`
	Args                       []string `json:"args"`
	Flags                      []string `json:"flags"`
	GlobalOptions              []string `json:"global_options"`
	Targets                    []string `json:"targets"`
	HasForcePush               bool     `json:"has_force_push"`
	HasCleanForceDelete        bool     `json:"has_clean_force_delete"`
	HasForcePushProtected      bool     `json:"has_force_push_protected"`
	HasHardReset               bool     `json:"has_hard_reset"`
	HasMergeStrategyShortcut   bool     `json:"has_merge_strategy_shortcut"`
	HasRestorePathspec         bool     `json:"has_restore_pathspec"`
	HasTheirsOursCheckout      bool     `json:"has_theirs_ours_checkout"`
	IsGit                      bool     `json:"is_git"`
	HasCheckoutProtectedBranch bool     `json:"has_checkout_protected_branch"`
	HasChangeDir               bool     `json:"has_change_dir"`
}

type DiffInput struct {
	ChangedFiles   []string             `json:"changed_files"`
	Files          []string             `json:"files"`
	Hunks          []DiffHunkInput      `json:"hunks"`
	AddedLines     []DiffLineInput      `json:"added_lines"`
	RemovedLines   []DiffLineInput      `json:"removed_lines"`
	ChangedSymbols []ChangedSymbolInput `json:"changed_symbols"`
	StagedFiles    []string             `json:"staged_files"`
	HasChanges     bool                 `json:"has_changes"`
}

type DiffHunkInput struct {
	AddedLines   []DiffLineInput `json:"added_lines"`
	File         string          `json:"file"`
	Header       string          `json:"header"`
	RemovedLines []DiffLineInput `json:"removed_lines"`
	NewLines     int64           `json:"new_lines"`
	NewStart     int64           `json:"new_start"`
	OldLines     int64           `json:"old_lines"`
	OldStart     int64           `json:"old_start"`
}

type DiffLineInput struct {
	File    string `json:"file"`
	Text    string `json:"text"`
	Line    int64  `json:"line"`
	NewLine int64  `json:"new_line"`
	OldLine int64  `json:"old_line"`
	IsBlank bool   `json:"is_blank"`
}

type FileChangeInput struct {
	Base                      string `json:"base"`
	Dir                       string `json:"dir"`
	Ext                       string `json:"ext"`
	File                      string `json:"file"`
	OldFile                   string `json:"old_file"`
	Status                    string `json:"status"`
	IsAdded                   bool   `json:"is_added"`
	IsBinary                  bool   `json:"is_binary"`
	IsDeleted                 bool   `json:"is_deleted"`
	IsGenerated               bool   `json:"is_generated"`
	IsModified                bool   `json:"is_modified"`
	IsProtected               bool   `json:"is_protected"`
	IsRenamed                 bool   `json:"is_renamed"`
	IsTest                    bool   `json:"is_test"`
	LineCount                 int64  `json:"line_count"`
	OriginalLineCount         int64  `json:"original_line_count"`
	NonBlankLineCount         int64  `json:"nonblank_line_count"`
	OriginalNonBlankLineCount int64  `json:"original_nonblank_line_count"`
	NonBlankLineDelta         int64  `json:"nonblank_line_delta"`
	SizeBytes                 int64  `json:"size_bytes"`
	NonBlankLineCountGrows    bool   `json:"nonblank_line_count_grows"`
	NonBlankLineCountShrinks  bool   `json:"nonblank_line_count_shrinks"`
}

type ReferencedFileInput struct {
	Base             string `json:"base"`
	Dir              string `json:"dir"`
	File             string `json:"file"`
	Lower            string `json:"lower"`
	Exists           bool   `json:"exists"`
	InAgentWorkspace bool   `json:"in_agent_workspace"`
	IsRegular        bool   `json:"is_regular"`
	SizeBytes        int64  `json:"size_bytes"`
}

type ActivationInput struct {
	Diagnostic         *diagnostics.Diagnostic
	Finding            *FindingActivation
	EventSource        string
	Tool               string
	PythonVersion      string
	CurrentBranch      string
	Cwd                string
	EventName          string
	EventMatcher       string
	Command            string
	Content            string
	OldContent         string
	TranscriptPath     string
	Scope              string
	Provider           string
	Mode               string
	SessionID          string
	ChangedFiles       []string
	ProtectedBranches  []string
	ToolResponseKeys   []string
	StagedFiles        []string
	ConfigCandidates   []string
	SourceRoots        []string
	RequiredIgnores    []string
	ProtectedPaths     []string
	Files              []string
	Diagnostics        []diagnostics.Diagnostic
	Argv               []string
	Findings           []FindingActivation
	PythonASTFacts     []PythonASTFactInput
	ToolInputKeys      []string
	Source             SourceActivation
	ReturnCode         int
	AdminApproved      bool
	ReadOnlyInspection bool
	HasToolResponse    bool
	HasToolInput       bool
}

const SchemaVersion int64 = 1

var (
	environmentOnce sync.Once
	environment     *cel.Env
	environmentErr  error
	programCache    sync.Map
)

type programCacheKey struct {
	PolicyID string
	Source   string
}

func InputSchema() []string {
	return []string{
		"argv: list(string)",
		"command: string",
		"command_fact: {raw, lower, tool, argv, has_inline_env}",
		"content: {raw, lower, has_git_token, has_absolute_git_path, has_path_override, has_python_subprocess, has_shell_exec}",
		"shell_commands: list({command, name, argv, assignments, redirects, write_targets, line, column, background, has_inline_env, has_redirects, has_write_targets, has_heredoc, has_command_substitution, has_process_substitution, has_dynamic_expansion, has_subshell, is_function_declaration, is_git, is_lint_tool, is_shell_exec, uses_path_override})",
		"config: {candidates, present}",
		"cwd: string",
		"diff: {files, changed_files, staged_files, has_changes, hunks, added_lines, removed_lines, changed_symbols}",
		"diff.hunks[]: {file, old_start, old_lines, new_start, new_lines, header, added_lines, removed_lines}",
		"diff.added_lines[]/removed_lines[]: {file, line, old_line, new_line, text, is_blank}",
		"changed_symbols: list({file, dir, base, ext, language, node_kind, symbol_kind, symbol_name, symbol_path, action, changed_lines, is_generated, is_test, original_line_count, current_line_count, line_delta, original_nonblank_line_count, current_nonblank_line_count, nonblank_line_delta, line_count_grows, line_count_shrinks, nonblank_line_count_grows, nonblank_line_count_shrinks, original_start_line, original_end_line, current_start_line, current_end_line, original_content_hash, current_content_hash})",
		"event: {name, provider, tool, scope, mode, source, matcher, session_id, transcript_path, tool_input_keys, tool_response_keys, return_code, has_tool_input, has_tool_response, is_claude, is_codex, is_gemini}",
		"files: list(string)",
		"file_changes: list({file, old_file, status, dir, base, ext, is_added, is_modified, is_deleted, is_renamed, is_generated, is_test, is_protected, is_binary, size_bytes, line_count, original_line_count, nonblank_line_count, original_nonblank_line_count, nonblank_line_delta, nonblank_line_count_grows, nonblank_line_count_shrinks})",
		"proposed_file_changes: list({file, dir, base, ext, exists, has_proposed_content, is_binary, is_generated, is_test, current_size_bytes, proposed_size_bytes, size_delta, current_line_count, proposed_line_count, line_delta, current_nonblank_line_count, proposed_nonblank_line_count, nonblank_line_delta, size_grows, size_shrinks, line_count_grows, line_count_shrinks, nonblank_line_count_grows, nonblank_line_count_shrinks, replacement_matched, replacement_ambiguous})",
		"proposed_symbol_changes: list({file, dir, base, ext, language, node_kind, symbol_kind, symbol_name, symbol_path, action, is_generated, is_test, current_line_count, proposed_line_count, line_delta, current_nonblank_line_count, proposed_nonblank_line_count, nonblank_line_delta, line_count_grows, line_count_shrinks, nonblank_line_count_grows, nonblank_line_count_shrinks, current_start_line, current_end_line, proposed_start_line, proposed_end_line, current_content_hash, proposed_content_hash})",
		"git: {current_branch, on_protected_branch, protected_branches, protected_path_files, staged_files, changed_files}",
		"git_command: {is_git, subcommand, args, flags, targets, global_options, has_change_dir}",
		"scope: string",
		"source: {path, language, symbol_name, symbol_kind, chunk_hash, line_count, changed_lines, prior_failures, recent_remediations}",
		"metadata: {admin_approved, read_only_inspection, schema_version, tool}",
		"path: {file, dir, base, ext, symlink_target, is_symlink, is_test, is_generated, in_source_root}",
		"paths: list({file, dir, base, ext, symlink_target, is_symlink, is_test, is_generated, in_source_root})",
		"diagnostic: {tool, code, message, file, line, column, severity, policy_id}",
		"diagnostics: list({tool, code, message, file, line, column, severity, policy_id})",
		"python_ast: list({file, language, node_kind, symbol_kind, symbol_name, symbol_path, parent_symbol_path, text, import_module, call_name, annotation_role, line, column, end_line, parameter_count, has_varargs, has_kwargs, module_level, under_class, under_conditional, under_function, under_try, under_type_checking, is_import, is_import_fallback, is_dynamic_import, is_assigned_lambda, is_closure_factory})",
		"finding: {tool, code, message, file, language, symbol_name, symbol_kind, chunk_hash, line, line_count, changed_lines, severity, policy_id, skill_id, principle_ids}",
		"findings: list({tool, code, message, file, language, symbol_name, symbol_kind, chunk_hash, line, line_count, changed_lines, severity, policy_id, skill_id, principle_ids})",
		"repo: {root, source_roots, python_version, config_candidates, protected_paths, protected_branches}",
		"referenced_files: list({file, dir, base, lower, exists, is_regular, in_agent_workspace, size_bytes})",
		"tool_capabilities: list({name, command, tags, read_paths, write_paths, sandbox_profile, timeout_seconds, memory_mb, cpu_quota_percent, requires_network, requires_git, requires_env, requires_processes, seccomp_profile})",
	}
}

func HelperSchema() []string {
	return []string{
		"has_prefix(value, prefix)",
		"has_suffix(value, suffix)",
		"glob_match(pattern, value)",
		"is_test_path(path)",
		"is_generated_path(path)",
		"is_protected_path(path, protected_paths)",
		"in_source_root(path, source_roots)",
		"lint_code_matches(code, pattern)",
		"command_invokes(command, tool)",
		"argv_invokes(argv, tool)",
		"argv_command_is(argv, tool)",
		"has_inline_env(command, name)",
		"repo_config_present(files, candidates)",
		"is_protected_branch(branch, protected_branches)",
		"list_contains(values, value)",
		"any_glob_match(patterns, value)",
		"any_has_prefix(values, prefix)",
		"any_has_suffix(values, suffix)",
		"any_contains(values, value)",
	}
}

func Environment() (*cel.Env, error) {
	environmentOnce.Do(func() {
		environment, environmentErr = newEnvironment()
	})

	return environment, environmentErr
}

func newEnvironment() (*cel.Env, error) {
	options := []cel.EnvOption{
		ext.NativeTypes(
			reflect.TypeFor[MetadataInput](),
			reflect.TypeFor[CommandInput](),
			reflect.TypeFor[ContentInput](),
			reflect.TypeFor[ShellCommandInput](),
			reflect.TypeFor[PathInput](),
			reflect.TypeFor[DiagnosticInput](),
			reflect.TypeFor[PythonASTFactInput](),
			reflect.TypeFor[FindingInput](),
			reflect.TypeFor[SourceInput](),
			reflect.TypeFor[RepoInput](),
			reflect.TypeFor[IgnoreInput](),
			reflect.TypeFor[ConfigInput](),
			reflect.TypeFor[GitInput](),
			reflect.TypeFor[GitCommandInput](),
			reflect.TypeFor[EventInput](),
			reflect.TypeFor[DiffInput](),
			reflect.TypeFor[DiffHunkInput](),
			reflect.TypeFor[DiffLineInput](),
			reflect.TypeFor[ChangedSymbolInput](),
			reflect.TypeFor[FileChangeInput](),
			reflect.TypeFor[ProposedFileChangeInput](),
			reflect.TypeFor[ProposedSymbolChangeInput](),
			reflect.TypeFor[ReferencedFileInput](),
			reflect.TypeFor[ToolCapabilityInput](),
			ext.ParseStructTag("json"),
		),
		cel.Variable("argv", cel.ListType(cel.StringType)),
		cel.Variable("command", cel.StringType),
		cel.Variable("content", cel.ObjectType("celexpr.ContentInput")),
		cel.Variable("cwd", cel.StringType),
		cel.Variable("files", cel.ListType(cel.StringType)),
		cel.Variable(
			"changed_symbols",
			cel.ListType(cel.ObjectType("celexpr.ChangedSymbolInput")),
		),
		cel.Variable(
			"file_changes",
			cel.ListType(cel.ObjectType("celexpr.FileChangeInput")),
		),
		cel.Variable(
			"proposed_file_changes",
			cel.ListType(cel.ObjectType("celexpr.ProposedFileChangeInput")),
		),
		cel.Variable(
			"proposed_symbol_changes",
			cel.ListType(cel.ObjectType("celexpr.ProposedSymbolChangeInput")),
		),
		cel.Variable(
			"referenced_files",
			cel.ListType(cel.ObjectType("celexpr.ReferencedFileInput")),
		),
		cel.Variable(
			"tool_capabilities",
			cel.ListType(cel.ObjectType("celexpr.ToolCapabilityInput")),
		),
		cel.Variable("scope", cel.StringType),
		cel.Variable("source", cel.ObjectType("celexpr.SourceInput")),
		cel.Variable("metadata", cel.ObjectType("celexpr.MetadataInput")),
		cel.Variable("command_fact", cel.ObjectType("celexpr.CommandInput")),
		cel.Variable(
			"shell_commands",
			cel.ListType(cel.ObjectType("celexpr.ShellCommandInput")),
		),
		cel.Variable("event", cel.ObjectType("celexpr.EventInput")),
		cel.Variable("diff", cel.ObjectType("celexpr.DiffInput")),
		cel.Variable("path", cel.ObjectType("celexpr.PathInput")),
		cel.Variable(
			"paths",
			cel.ListType(cel.ObjectType("celexpr.PathInput")),
		),
		cel.Variable("diagnostic", cel.ObjectType("celexpr.DiagnosticInput")),
		cel.Variable(
			"diagnostics",
			cel.ListType(cel.ObjectType("celexpr.DiagnosticInput")),
		),
		cel.Variable(
			"python_ast",
			cel.ListType(cel.ObjectType("celexpr.PythonASTFactInput")),
		),
		cel.Variable("finding", cel.ObjectType("celexpr.FindingInput")),
		cel.Variable(
			"findings",
			cel.ListType(cel.ObjectType("celexpr.FindingInput")),
		),
		cel.Variable("repo", cel.ObjectType("celexpr.RepoInput")),
		cel.Variable("config", cel.ObjectType("celexpr.ConfigInput")),
		cel.Variable("git", cel.ObjectType("celexpr.GitInput")),
		cel.Variable("git_command", cel.ObjectType("celexpr.GitCommandInput")),
	}
	options = append(options, helperFunctions()...)

	return cel.NewEnv(options...)
}

func Validate(policyID, source string) error {
	_, err := Program(policyID, source)

	return err
}

func Program(policyID, source string) (cel.Program, error) {
	key := programCacheKey{
		PolicyID: policyID,
		Source:   strings.TrimSpace(source),
	}
	if cached, ok := programCache.Load(key); ok {
		return cached.(cel.Program), nil
	}

	program, err := compileProgram(key.PolicyID, key.Source)
	if err != nil {
		return nil, err
	}

	cached, _ := programCache.LoadOrStore(key, program)

	return cached.(cel.Program), nil
}

func compileProgram(policyID, source string) (cel.Program, error) {
	if source == "" {
		return nil, fmt.Errorf("CEL expression policy %q missing when", policyID)
	}

	env, err := Environment()
	if err != nil {
		return nil, fmt.Errorf("prepare CEL environment for %q: %w", policyID, err)
	}

	ast, issues := env.Compile(source)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile CEL policy %q: %w", policyID, issues.Err())
	}

	if !ast.OutputType().IsExactType(cel.BoolType) {
		return nil, fmt.Errorf(
			"compile CEL policy %q: when expression must return bool, got %s",
			policyID,
			ast.OutputType(),
		)
	}

	program, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("prepare CEL program for %q: %w", policyID, err)
	}

	return program, nil
}

func Activation(input ActivationInput) map[string]any {
	sourceRoots := cleanSourceRoots(input.SourceRoots)
	paths := pathInputs(input.Cwd, input.Files, sourceRoots)
	files := cleanStringSlice(input.Files)
	changedFiles := cleanStringSlice(input.ChangedFiles)
	stagedFiles := cleanStringSlice(input.StagedFiles)
	protectedPaths := cleanStringSlice(input.ProtectedPaths)
	protectedBranches := cleanStringSlice(input.ProtectedBranches)
	configCandidates := cleanStringSlice(input.ConfigCandidates)
	presentConfigs := presentRepoConfigs(files, configCandidates)

	primaryPath := PathInput{}
	if len(paths) == 1 {
		primaryPath = paths[0]
	} else if len(input.Files) == 1 {
		primaryPath = newPathInput(input.Cwd, input.Files[0], sourceRoots)
	}

	diffHunks := diffHunkInputs(input.Cwd, files)
	addedLines, removedLines := diffLines(diffHunks)
	changedSymbols := changedSymbolInputs(input.Cwd, files, diffHunks)
	hasChanges := len(files) > 0 ||
		len(changedFiles) > 0 ||
		len(stagedFiles) > 0 ||
		len(diffHunks) > 0
	provider := strings.ToLower(strings.TrimSpace(input.Provider))

	return map[string]any{
		"argv":    append([]string(nil), input.Argv...),
		"command": input.Command,
		"command_fact": CommandInput{
			Argv:         append([]string(nil), input.Argv...),
			Lower:        strings.ToLower(input.Command),
			Raw:          input.Command,
			Tool:         input.Tool,
			HasInlineEnv: commandHasInlineEnv(input.Command, ""),
		},
		"content":        contentInput(input.Content),
		"shell_commands": shellCommandInputs(input.Command),
		"config": ConfigInput{
			Candidates: configCandidates,
			Present:    presentConfigs,
		},
		"cwd": input.Cwd,
		"diff": DiffInput{
			ChangedFiles:   changedFiles,
			Files:          files,
			Hunks:          diffHunks,
			AddedLines:     addedLines,
			RemovedLines:   removedLines,
			ChangedSymbols: changedSymbols,
			HasChanges:     hasChanges,
			StagedFiles:    stagedFiles,
		},
		"event": EventInput{
			Name:             input.EventName,
			Matcher:          input.EventMatcher,
			Mode:             input.Mode,
			Provider:         input.Provider,
			Scope:            input.Scope,
			SessionID:        input.SessionID,
			Source:           input.EventSource,
			Tool:             input.Tool,
			ToolInputKeys:    cleanStringValues(input.ToolInputKeys),
			ToolResponseKeys: cleanStringValues(input.ToolResponseKeys),
			TranscriptPath:   input.TranscriptPath,
			ReturnCode:       int64(input.ReturnCode),
			HasToolInput:     input.HasToolInput,
			HasToolResponse:  input.HasToolResponse,
			IsClaude:         provider == "claude",
			IsCodex:          provider == "codex",
			IsGemini:         provider == "gemini",
		},
		"files":           files,
		"changed_symbols": changedSymbols,
		"file_changes": fileChangeInputs(
			input.Cwd,
			files,
			protectedPaths,
		),
		"proposed_file_changes":   proposedFileChangeInputs(input),
		"proposed_symbol_changes": proposedSymbolChangeInputs(input),
		"referenced_files":        referencedFileInputs(input.Cwd, files, input.Argv),
		"tool_capabilities":       toolCapabilityInputs(),
		"git": GitInput{
			CurrentBranch:      input.CurrentBranch,
			OnProtectedBranch:  isProtectedBranch(input.CurrentBranch, protectedBranches),
			ChangedFiles:       changedFiles,
			ProtectedBranches:  protectedBranches,
			ProtectedPathFiles: protectedPathFiles(files, protectedPaths),
			StagedFiles:        stagedFiles,
		},
		"git_command": gitCommandInput(input.Argv, protectedBranches),
		"metadata": MetadataInput{
			AdminApproved:      input.AdminApproved,
			ReadOnlyInspection: input.ReadOnlyInspection,
			SchemaVersion:      SchemaVersion,
			Tool:               input.Tool,
		},
		"scope":       input.Scope,
		"source":      sourceInput(input.Source, input.Finding, primaryPath),
		"path":        primaryPath,
		"paths":       paths,
		"diagnostic":  diagnosticInput(input.Diagnostic),
		"diagnostics": diagnosticInputs(input.Diagnostics, input.Diagnostic),
		"python_ast":  append([]PythonASTFactInput(nil), input.PythonASTFacts...),
		"finding":     findingInput(input.Finding),
		"findings":    findingInputs(input.Findings, input.Finding),
		"repo": RepoInput{
			ConfigCandidates:  configCandidates,
			ProtectedBranches: protectedBranches,
			ProtectedPaths:    protectedPaths,
			PythonVersion:     input.PythonVersion,
			RequiredIgnores:   requiredIgnoreInputs(input.Cwd, input.RequiredIgnores),
			Root:              input.Cwd,
			SourceRoots:       sourceRoots,
		},
	}
}

func contentInput(content string) ContentInput {
	lower := strings.ToLower(content)

	return ContentInput{
		Raw:   content,
		Lower: lower,
		HasAbsoluteGitPath: strings.Contains(lower, "/usr/bin/git") ||
			strings.Contains(lower, "/bin/git") ||
			strings.Contains(lower, "/usr/local/bin/git"),
		HasGitToken:     strings.Contains(lower, "git"),
		HasPathOverride: strings.Contains(lower, "path="),
		HasPythonSubprocess: strings.Contains(lower, "subprocess") ||
			strings.Contains(lower, "os.system") ||
			strings.Contains(lower, "os.popen"),
		HasShellExec: strings.Contains(lower, "bash -c") ||
			strings.Contains(lower, "sh -c") ||
			strings.Contains(lower, "zsh -c") ||
			strings.Contains(lower, "dash -c") ||
			strings.Contains(lower, "eval ") ||
			strings.Contains(lower, "exec "),
	}
}

func gitCommandInput(argv, protectedBranches []string) GitCommandInput {
	normalized := stripLeadingAssignments(argv)
	if len(normalized) == 0 || !commandTokenMatchesTool(normalized[0], "git") {
		return GitCommandInput{
			Args:          []string{},
			Flags:         []string{},
			GlobalOptions: []string{},
			Targets:       []string{},
		}
	}

	subcommandIndex := gitSubcommandIndex(normalized)
	if subcommandIndex == -1 {
		return GitCommandInput{
			GlobalOptions: gitGlobalOptions(normalized[1:]),
			Args:          []string{},
			Flags:         []string{},
			IsGit:         true,
			Targets:       []string{},
			HasChangeDir:  listContains(normalized[1:], "-C"),
		}
	}

	args := append([]string(nil), normalized[subcommandIndex+1:]...)
	flags := gitFlags(args)

	return GitCommandInput{
		Args:                       args,
		Flags:                      flags,
		GlobalOptions:              gitGlobalOptions(normalized[1:subcommandIndex]),
		HasChangeDir:               listContains(normalized[1:subcommandIndex], "-C"),
		HasCheckoutProtectedBranch: gitCheckoutProtectedBranch(normalized, protectedBranches),
		HasCleanForceDelete:        gitCleanForceDelete(flags),
		HasForcePush:               gitHasForcePush(flags),
		HasForcePushProtected: gitForcePushProtectedBranch(
			normalized,
			protectedBranches,
		),
		HasHardReset: normalized[subcommandIndex] == "reset" &&
			listContains(flags, "--hard"),
		HasMergeStrategyShortcut: gitMergeStrategyShortcut(args),
		HasRestorePathspec: normalized[subcommandIndex] == "restore" &&
			listContains(args, "--"),
		HasTheirsOursCheckout: normalized[subcommandIndex] == "checkout" &&
			(listContains(flags, "--theirs") || listContains(flags, "--ours")),
		IsGit:      true,
		Subcommand: normalized[subcommandIndex],
		Targets:    gitCommandTargets(normalized[subcommandIndex], args),
	}
}
