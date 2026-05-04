// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
	"blackcat.ca/coding-ethos/go/toolcatalog"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

type MetadataInput struct {
	AdminApproved bool   `json:"admin_approved"`
	SchemaVersion int64  `json:"schema_version"`
	Tool          string `json:"tool"`
}

type CommandInput struct {
	Argv         []string `json:"argv"`
	Lower        string   `json:"lower"`
	Raw          string   `json:"raw"`
	Tool         string   `json:"tool"`
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
	Argv                   []string `json:"argv"`
	Assignments            []string `json:"assignments"`
	Redirects              []string `json:"redirects"`
	WriteTargets           []string `json:"write_targets"`
	Command                string   `json:"command"`
	Name                   string   `json:"name"`
	Column                 int64    `json:"column"`
	Line                   int64    `json:"line"`
	Background             bool     `json:"background"`
	HasCommandSubstitution bool     `json:"has_command_substitution"`
	HasDynamicExpansion    bool     `json:"has_dynamic_expansion"`
	HasHeredoc             bool     `json:"has_heredoc"`
	HasInlineEnv           bool     `json:"has_inline_env"`
	HasProcessSubstitution bool     `json:"has_process_substitution"`
	HasRedirects           bool     `json:"has_redirects"`
	HasSubshell            bool     `json:"has_subshell"`
	HasWriteTargets        bool     `json:"has_write_targets"`
	IsFunctionDeclaration  bool     `json:"is_function_declaration"`
	IsGitMutation          bool     `json:"is_git_mutation"`
	IsGit                  bool     `json:"is_git"`
	IsLintTool             bool     `json:"is_lint_tool"`
	PipesToShell           bool     `json:"pipes_to_shell"`
	IsShellExec            bool     `json:"is_shell_exec"`
	UsesPathOverride       bool     `json:"uses_path_override"`
	WrapsGitMutation       bool     `json:"wraps_git_mutation"`
}

type EventInput struct {
	Name             string   `json:"name"`
	Matcher          string   `json:"matcher"`
	Mode             string   `json:"mode"`
	Provider         string   `json:"provider"`
	Scope            string   `json:"scope"`
	SessionID        string   `json:"session_id"`
	Source           string   `json:"source"`
	Tool             string   `json:"tool"`
	ToolInputKeys    []string `json:"tool_input_keys"`
	ToolResponseKeys []string `json:"tool_response_keys"`
	TranscriptPath   string   `json:"transcript_path"`
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
	Line     int64  `json:"line"`
	Column   int64  `json:"column"`
	Severity string `json:"severity"`
	PolicyID string `json:"policy_id"`
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
	Ignored     bool   `json:"ignored"`
	CheckFailed bool   `json:"check_failed"`
	Error       string `json:"error"`
}

type ConfigInput struct {
	Candidates []string `json:"candidates"`
	Present    []string `json:"present"`
}

type GitInput struct {
	CurrentBranch      string   `json:"current_branch"`
	OnProtectedBranch  bool     `json:"on_protected_branch"`
	ProtectedBranches  []string `json:"protected_branches"`
	ProtectedPathFiles []string `json:"protected_path_files"`
	StagedFiles        []string `json:"staged_files"`
	ChangedFiles       []string `json:"changed_files"`
}

type GitCommandInput struct {
	Args                       []string `json:"args"`
	Flags                      []string `json:"flags"`
	GlobalOptions              []string `json:"global_options"`
	HasChangeDir               bool     `json:"has_change_dir"`
	HasCheckoutProtectedBranch bool     `json:"has_checkout_protected_branch"`
	HasCleanForceDelete        bool     `json:"has_clean_force_delete"`
	HasForcePush               bool     `json:"has_force_push"`
	HasForcePushProtected      bool     `json:"has_force_push_protected"`
	HasHardReset               bool     `json:"has_hard_reset"`
	HasMergeStrategyShortcut   bool     `json:"has_merge_strategy_shortcut"`
	HasRestorePathspec         bool     `json:"has_restore_pathspec"`
	HasTheirsOursCheckout      bool     `json:"has_theirs_ours_checkout"`
	IsGit                      bool     `json:"is_git"`
	Subcommand                 string   `json:"subcommand"`
	Targets                    []string `json:"targets"`
}

type DiffInput struct {
	ChangedFiles   []string             `json:"changed_files"`
	Files          []string             `json:"files"`
	Hunks          []DiffHunkInput      `json:"hunks"`
	AddedLines     []DiffLineInput      `json:"added_lines"`
	RemovedLines   []DiffLineInput      `json:"removed_lines"`
	ChangedSymbols []ChangedSymbolInput `json:"changed_symbols"`
	HasChanges     bool                 `json:"has_changes"`
	StagedFiles    []string             `json:"staged_files"`
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
	Argv              []string
	Command           string
	Content           string
	OldContent        string
	ConfigCandidates  []string
	CurrentBranch     string
	Cwd               string
	EventName         string
	EventMatcher      string
	EventSource       string
	Files             []string
	ChangedFiles      []string
	StagedFiles       []string
	Scope             string
	Provider          string
	Mode              string
	SessionID         string
	Tool              string
	ToolInputKeys     []string
	ToolResponseKeys  []string
	TranscriptPath    string
	ReturnCode        int
	HasToolInput      bool
	HasToolResponse   bool
	AdminApproved     bool
	Diagnostic        *diagnostics.Diagnostic
	Diagnostics       []diagnostics.Diagnostic
	Finding           *FindingActivation
	Findings          []FindingActivation
	Source            SourceActivation
	ProtectedPaths    []string
	ProtectedBranches []string
	RequiredIgnores   []string
	SourceRoots       []string
	PythonVersion     string
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
		"metadata: {admin_approved, schema_version, tool}",
		"path: {file, dir, base, ext, symlink_target, is_symlink, is_test, is_generated, in_source_root}",
		"paths: list({file, dir, base, ext, symlink_target, is_symlink, is_test, is_generated, in_source_root})",
		"diagnostic: {tool, code, message, file, line, column, severity, policy_id}",
		"diagnostics: list({tool, code, message, file, line, column, severity, policy_id})",
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
			reflect.TypeOf(MetadataInput{}),
			reflect.TypeOf(CommandInput{}),
			reflect.TypeOf(ContentInput{}),
			reflect.TypeOf(ShellCommandInput{}),
			reflect.TypeOf(PathInput{}),
			reflect.TypeOf(DiagnosticInput{}),
			reflect.TypeOf(FindingInput{}),
			reflect.TypeOf(SourceInput{}),
			reflect.TypeOf(RepoInput{}),
			reflect.TypeOf(IgnoreInput{}),
			reflect.TypeOf(ConfigInput{}),
			reflect.TypeOf(GitInput{}),
			reflect.TypeOf(GitCommandInput{}),
			reflect.TypeOf(EventInput{}),
			reflect.TypeOf(DiffInput{}),
			reflect.TypeOf(DiffHunkInput{}),
			reflect.TypeOf(DiffLineInput{}),
			reflect.TypeOf(ChangedSymbolInput{}),
			reflect.TypeOf(FileChangeInput{}),
			reflect.TypeOf(ProposedFileChangeInput{}),
			reflect.TypeOf(ProposedSymbolChangeInput{}),
			reflect.TypeOf(ReferencedFileInput{}),
			reflect.TypeOf(ToolCapabilityInput{}),
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

func Validate(policyID string, source string) error {
	_, err := Program(policyID, source)

	return err
}

func Program(policyID string, source string) (cel.Program, error) {
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

func compileProgram(policyID string, source string) (cel.Program, error) {
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
			AdminApproved: input.AdminApproved,
			SchemaVersion: SchemaVersion,
			Tool:          input.Tool,
		},
		"scope":       input.Scope,
		"source":      sourceInput(input.Source, input.Finding, primaryPath),
		"path":        primaryPath,
		"paths":       paths,
		"diagnostic":  diagnosticInput(input.Diagnostic),
		"diagnostics": diagnosticInputs(input.Diagnostics, input.Diagnostic),
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

func gitCommandInput(argv []string, protectedBranches []string) GitCommandInput {
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
		HasForcePushProtected:      gitForcePushProtectedBranch(normalized, protectedBranches),
		HasHardReset:               normalized[subcommandIndex] == "reset" && listContains(flags, "--hard"),
		HasMergeStrategyShortcut:   gitMergeStrategyShortcut(args),
		HasRestorePathspec:         normalized[subcommandIndex] == "restore" && listContains(args, "--"),
		HasTheirsOursCheckout: normalized[subcommandIndex] == "checkout" &&
			(listContains(flags, "--theirs") || listContains(flags, "--ours")),
		IsGit:      true,
		Subcommand: normalized[subcommandIndex],
		Targets:    gitCommandTargets(normalized[subcommandIndex], args),
	}
}

func shellCommandInputs(command string) []ShellCommandInput {
	parsed, err := shellparse.Commands(command)
	if err != nil {
		return []ShellCommandInput{}
	}
	controlFields, _ := shellparse.ControlFields(command)

	inputs := make([]ShellCommandInput, 0, len(parsed))
	for _, parsedCommand := range parsed {
		name := shellCommandName(parsedCommand)
		writeTargets := shellWriteTargets(parsedCommand)
		inputs = append(inputs, ShellCommandInput{
			Argv:                   append([]string(nil), parsedCommand.Argv...),
			Assignments:            append([]string(nil), parsedCommand.Assignments...),
			Redirects:              append([]string(nil), parsedCommand.Redirects...),
			WriteTargets:           writeTargets,
			Command:                parsedCommand.Command,
			Name:                   name,
			Column:                 int64(parsedCommand.Column),
			Line:                   int64(parsedCommand.Line),
			Background:             parsedCommand.Background,
			HasCommandSubstitution: parsedCommand.HasCommandSubstitution,
			HasDynamicExpansion:    parsedCommand.HasDynamicExpansion,
			HasHeredoc:             parsedCommand.HasHeredoc,
			HasInlineEnv:           len(parsedCommand.Assignments) > 0,
			HasProcessSubstitution: parsedCommand.HasProcessSubstitution,
			HasRedirects:           len(parsedCommand.Redirects) > 0,
			HasSubshell:            parsedCommand.HasSubshell,
			HasWriteTargets:        len(writeTargets) > 0,
			IsFunctionDeclaration:  parsedCommand.IsFunctionDeclaration,
			IsGitMutation:          shellCommandIsGitMutation(parsedCommand),
			IsGit:                  shellCommandIsGit(parsedCommand),
			IsLintTool:             shellCommandIsLintTool(parsedCommand),
			PipesToShell:           shellCommandPipesToShell(parsedCommand, controlFields),
			IsShellExec:            shellCommandIsShellExec(parsedCommand),
			UsesPathOverride:       shellCommandUsesPathOverride(parsedCommand),
			WrapsGitMutation:       shellCommandWrapsGitMutation(parsedCommand),
		})
	}

	return inputs
}

func shellWriteTargets(command shellparse.Command) []string {
	assignments := shellAssignmentMap(command.Assignments)
	targets := []string{}
	for _, redirect := range command.Redirects {
		if target, ok := redirectWriteTarget(redirect, assignments); ok {
			targets = append(targets, target)
		}
	}
	targets = append(targets, commandWriteTargets(command, assignments)...)

	return cleanStringSlice(targets)
}

func shellAssignmentMap(assignments []string) map[string]string {
	values := map[string]string{}
	for _, assignment := range assignments {
		name, value, ok := strings.Cut(assignment, "=")
		if !ok || name == "" {
			continue
		}
		values[name] = strings.Trim(value, `"'`)
	}

	return values
}

func redirectWriteTarget(
	redirect string,
	assignments map[string]string,
) (string, bool) {
	operatorIndex := redirectWriteOperatorIndex(redirect)
	if operatorIndex < 0 {
		return "", false
	}

	operator := redirect[operatorIndex:]
	for _, prefix := range []string{">>|", ">|", ">>", "<>", ">"} {
		if strings.HasPrefix(operator, prefix) {
			target := strings.TrimSpace(operator[len(prefix):])
			if target == "" || strings.HasPrefix(target, "&") {
				return "", false
			}

			return resolveShellTarget(target, assignments), true
		}
	}

	return "", false
}

func redirectWriteOperatorIndex(redirect string) int {
	for index, char := range redirect {
		if char == '>' {
			return index
		}
		if char == '<' && strings.HasPrefix(redirect[index:], "<>") {
			return index
		}
	}

	return -1
}

func commandWriteTargets(
	command shellparse.Command,
	assignments map[string]string,
) []string {
	if len(command.Argv) == 0 {
		return nil
	}

	switch shellCommandName(command) {
	case "tee":
		return teeWriteTargets(command.Argv[1:], assignments)
	case "cp", "mv":
		return copyMoveWriteTargets(command.Argv[1:], assignments)
	default:
		return nil
	}
}

func teeWriteTargets(args []string, assignments map[string]string) []string {
	targets := []string{}
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}
		if arg == "--" {
			continue
		}
		if arg == "-a" || arg == "--append" ||
			arg == "-i" || arg == "--ignore-interrupts" {
			continue
		}
		if arg == "-p" || arg == "--output-error" {
			skipNext = true

			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		targets = append(targets, resolveShellTarget(arg, assignments))
	}

	return targets
}

func copyMoveWriteTargets(args []string, assignments map[string]string) []string {
	candidates := []string{}
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}
		if arg == "--" {
			continue
		}
		if copyMoveOptionHasValue(arg) {
			skipNext = !strings.Contains(arg, "=")

			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		candidates = append(candidates, arg)
	}
	if len(candidates) == 0 {
		return nil
	}

	return []string{resolveShellTarget(candidates[len(candidates)-1], assignments)}
}

func copyMoveOptionHasValue(arg string) bool {
	return strings.HasPrefix(arg, "--target-directory") ||
		strings.HasPrefix(arg, "--backup") ||
		strings.HasPrefix(arg, "--suffix") ||
		arg == "-t" || arg == "-S"
}

func resolveShellTarget(target string, assignments map[string]string) string {
	cleaned := strings.Trim(target, `"'`)
	if variable, ok := shellVariableReference(cleaned); ok {
		if resolved := assignments[variable]; resolved != "" {
			return resolved
		}
	}

	return cleaned
}

func shellVariableReference(value string) (string, bool) {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}"), true
	}
	if strings.HasPrefix(value, "$") && len(value) > 1 {
		return strings.TrimPrefix(value, "$"), true
	}

	return "", false
}

func shellCommandName(command shellparse.Command) string {
	if command.Name != "" {
		return path.Base(command.Name)
	}
	if len(command.Argv) == 0 {
		return ""
	}

	return path.Base(command.Argv[0])
}

func shellCommandIsGit(command shellparse.Command) bool {
	return commandTokenMatchesTool(shellCommandName(command), "git") ||
		shellCommandWrappedTool(command, "git")
}

func shellCommandIsLintTool(command shellparse.Command) bool {
	if _, ok := toolcatalog.CapturedLintTool(shellCommandName(command)); ok {
		return true
	}
	for _, arg := range command.Argv {
		if _, ok := toolcatalog.CapturedLintTool(path.Base(arg)); ok {
			return true
		}
	}

	return false
}

func shellCommandIsShellExec(command shellparse.Command) bool {
	switch shellCommandName(command) {
	case "bash", "sh", "zsh", "dash":
		return true
	default:
		return false
	}
}

func shellCommandUsesPathOverride(command shellparse.Command) bool {
	for _, assignment := range command.Assignments {
		if strings.HasPrefix(assignment, "PATH=") {
			return true
		}
	}
	if len(command.Argv) > 0 && shellCommandName(command) == "env" {
		for _, arg := range command.Argv[1:] {
			if strings.HasPrefix(arg, "PATH=") {
				return true
			}
		}
	}

	return false
}

func shellCommandIsGitMutation(command shellparse.Command) bool {
	if !shellCommandIsGit(command) || len(command.Argv) < 2 {
		return false
	}
	switch command.Argv[1] {
	case "commit", "push":
		return true
	default:
		return false
	}
}

func shellCommandWrapsGitMutation(command shellparse.Command) bool {
	if shellCommandName(command) != "timeout" {
		return false
	}
	for index, arg := range command.Argv {
		if path.Base(arg) != "git" || index+1 >= len(command.Argv) {
			continue
		}
		switch command.Argv[index+1] {
		case "commit", "push":
			return true
		}
	}

	return false
}

func shellCommandPipesToShell(
	command shellparse.Command,
	controlFields []string,
) bool {
	name := shellCommandName(command)
	switch name {
	case "curl", "wget":
	default:
		return false
	}

	for index, field := range controlFields {
		if path.Base(field) != name {
			continue
		}
	scan:
		for cursor := index + 1; cursor < len(controlFields)-1; cursor++ {
			switch controlFields[cursor] {
			case "|":
				if isShellInterpreter(controlFields[cursor+1]) {
					return true
				}
			case "&&", ";", "||":
				break scan
			}
		}
	}

	return false
}

func isShellInterpreter(value string) bool {
	switch path.Base(value) {
	case "bash", "sh", "zsh", "dash":
		return true
	default:
		return false
	}
}

func shellCommandWrappedTool(command shellparse.Command, tool string) bool {
	if len(command.Argv) < 2 {
		return false
	}
	switch shellCommandName(command) {
	case "command", "env":
		for _, arg := range command.Argv[1:] {
			if strings.Contains(arg, "=") {
				continue
			}

			return commandTokenMatchesTool(arg, tool)
		}
	}

	return false
}

func gitSubcommandIndex(argv []string) int {
	for idx := 1; idx < len(argv); idx++ {
		arg := argv[idx]
		if arg == "--" {
			return -1
		}
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			return idx
		}
		if gitGlobalOptionHasValue(arg) && idx+1 < len(argv) {
			idx++
		}
	}

	return -1
}

func gitGlobalOptions(args []string) []string {
	options := []string{}
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			break
		}
		options = append(options, arg)
		if gitGlobalOptionHasValue(arg) && idx+1 < len(args) {
			idx++
			options = append(options, args[idx])
		}
	}

	return options
}

func gitGlobalOptionHasValue(arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}
	switch arg {
	case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--config-env":
		return true
	default:
		return false
	}
}

func gitFlags(args []string) []string {
	flags := []string{}
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "" || !strings.HasPrefix(arg, "-") {
			continue
		}
		flags = append(flags, arg)
		if strings.HasPrefix(arg, "--") {
			continue
		}
		for _, flag := range strings.TrimPrefix(arg, "-") {
			flags = append(flags, "-"+string(flag))
		}
	}

	return uniqueStrings(flags)
}

func gitTargets(args []string) []string {
	targets := []string{}
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}
		if arg == "--" {
			continue
		}
		if gitArgOptionHasValue(arg) {
			skipNext = true

			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		targets = append(targets, arg)
	}

	return targets
}

func gitArgOptionHasValue(arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}
	switch arg {
	case "-m", "-F", "-X", "-C", "-c", "-b", "-B", "--message", "--file",
		"--branch", "--orphan", "--strategy-option", "--strategy",
		"--pathspec-from-file":
		return true
	default:
		return false
	}
}

func gitCleanForceDelete(flags []string) bool {
	return (listContains(flags, "--force") || listContains(flags, "-f")) &&
		listContains(flags, "-d")
}

func gitHasForcePush(flags []string) bool {
	return listContains(flags, "--force") ||
		listContains(flags, "--force-with-lease") ||
		listContains(flags, "-f")
}

func gitForcePushProtectedBranch(argv []string, protectedBranches []string) bool {
	input := gitCommandInputWithoutDerived(argv)
	if input.Subcommand != "push" || !gitHasForcePush(input.Flags) {
		return false
	}

	for _, arg := range input.Args {
		if gitProtectedBranchRef(arg, protectedBranches) {
			return true
		}
	}

	return false
}

func gitCheckoutProtectedBranch(argv []string, protectedBranches []string) bool {
	input := gitCommandInputWithoutDerived(argv)
	switch input.Subcommand {
	case "checkout":
		return gitTargetsContainLocalProtected(
			checkoutBranchTargets(input.Args),
			protectedBranches,
		)
	case "switch":
		return gitTargetsContainLocalProtected(
			switchBranchTargets(input.Args),
			protectedBranches,
		)
	default:
		return false
	}
}

func gitTargetsContainLocalProtected(targets []string, protectedBranches []string) bool {
	for _, target := range targets {
		if gitLocalProtectedBranchRef(target, protectedBranches) {
			return true
		}
	}

	return false
}

func gitLocalProtectedBranchRef(value string, protectedBranches []string) bool {
	if value == "" {
		return false
	}
	if len(protectedBranches) == 0 {
		protectedBranches = []string{"main", "master"}
	}
	for _, branch := range protectedBranches {
		if value == branch {
			return true
		}
	}

	return false
}

func gitCommandInputWithoutDerived(argv []string) GitCommandInput {
	normalized := stripLeadingAssignments(argv)
	if len(normalized) == 0 || !commandTokenMatchesTool(normalized[0], "git") {
		return GitCommandInput{}
	}
	subcommandIndex := gitSubcommandIndex(normalized)
	if subcommandIndex == -1 {
		return GitCommandInput{}
	}
	args := append([]string(nil), normalized[subcommandIndex+1:]...)

	return GitCommandInput{
		Args:       args,
		Flags:      gitFlags(args),
		IsGit:      true,
		Subcommand: normalized[subcommandIndex],
		Targets:    gitCommandTargets(normalized[subcommandIndex], args),
	}
}

func gitCommandTargets(subcommand string, args []string) []string {
	switch subcommand {
	case "checkout":
		return checkoutBranchTargets(args)
	case "switch":
		return switchBranchTargets(args)
	default:
		return gitTargets(args)
	}
}

func gitMergeStrategyShortcut(args []string) bool {
	for index, arg := range args {
		if arg == "-X" && index+1 < len(args) && isTheirsOrOurs(args[index+1]) {
			return true
		}
		if strings.HasPrefix(arg, "-X") &&
			isTheirsOrOurs(strings.TrimPrefix(arg, "-X")) {
			return true
		}
	}

	return false
}

func checkoutBranchTargets(args []string) []string {
	targets := []string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-b" || arg == "-B":
			if index+1 < len(args) {
				targets = append(targets, args[index+1])
				index++
			}
			if index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
				targets = append(targets, args[index+1])
			}
		case arg == "--branch" || arg == "--orphan":
			if index+1 < len(args) {
				targets = append(targets, args[index+1])
				index++
			}
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			targets = append(targets, arg)
		}
	}

	return targets
}

func switchBranchTargets(args []string) []string {
	targets := []string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-c" || arg == "-C" || arg == "--create" || arg == "--force-create":
			if index+1 < len(args) {
				targets = append(targets, args[index+1])
				index++
			}
			if index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
				targets = append(targets, args[index+1])
			}
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			targets = append(targets, arg)
		}
	}

	return targets
}

func gitTargetsContainProtected(targets []string, protectedBranches []string) bool {
	for _, target := range targets {
		if gitProtectedBranchRef(target, protectedBranches) {
			return true
		}
	}

	return false
}

func gitProtectedBranchRef(value string, protectedBranches []string) bool {
	if value == "" {
		return false
	}
	if len(protectedBranches) == 0 {
		protectedBranches = []string{"main", "master"}
	}
	for _, branch := range protectedBranches {
		if value == branch ||
			value == "origin/"+branch ||
			value == "remotes/origin/"+branch {
			return true
		}
	}

	return false
}

func isTheirsOrOurs(value string) bool {
	return value == "theirs" || value == "ours"
}

func listContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}

	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}

	return unique
}

func fileChangeInputs(
	cwd string,
	files []string,
	protectedPaths []string,
) []FileChangeInput {
	statuses := gitFileStatuses(cwd)
	inputs := make([]FileChangeInput, 0, len(files))
	for _, file := range files {
		cleanFile := cleanInputFile(file)
		if cleanFile == "" {
			continue
		}
		inputs = append(
			inputs,
			fileChangeInput(cwd, cleanFile, statuses[cleanFile], protectedPaths),
		)
	}

	return inputs
}

func fileChangeInput(
	cwd string,
	file string,
	status gitFileStatus,
	protectedPaths []string,
) FileChangeInput {
	statusCode := strings.TrimSpace(status.Code)
	sizeBytes, lineCount, binary := fileSizeAndLines(cwd, file)
	nonBlankLineCount := currentNonBlankLineCount(cwd, file, binary)
	originalNonBlankLineCount := originalNonBlankLineCount(cwd, file)

	return FileChangeInput{
		Base:                      path.Base(file),
		Dir:                       path.Dir(file),
		Ext:                       strings.ToLower(path.Ext(file)),
		File:                      file,
		OldFile:                   status.OldFile,
		Status:                    statusCode,
		IsAdded:                   strings.Contains(statusCode, "A"),
		IsBinary:                  binary,
		IsDeleted:                 strings.Contains(statusCode, "D"),
		IsGenerated:               isGeneratedPath(file),
		IsModified:                strings.Contains(statusCode, "M"),
		IsProtected:               isProtectedPath(file, protectedPaths),
		IsRenamed:                 strings.Contains(statusCode, "R"),
		IsTest:                    isTestPath(file),
		LineCount:                 int64(lineCount),
		OriginalLineCount:         int64(originalLineCount(cwd, file)),
		NonBlankLineCount:         int64(nonBlankLineCount),
		OriginalNonBlankLineCount: int64(originalNonBlankLineCount),
		NonBlankLineDelta:         int64(nonBlankLineCount - originalNonBlankLineCount),
		SizeBytes:                 sizeBytes,
		NonBlankLineCountGrows: originalNonBlankLineCount >= 0 &&
			nonBlankLineCount > originalNonBlankLineCount,
		NonBlankLineCountShrinks: originalNonBlankLineCount >= 0 &&
			nonBlankLineCount < originalNonBlankLineCount,
	}
}

type gitFileStatus struct {
	Code    string
	OldFile string
}

func gitFileStatuses(cwd string) map[string]gitFileStatus {
	output, err := gitOutput(cwd, "diff", "--cached", "--name-status", "-M")
	if err != nil {
		return nil
	}

	statuses := map[string]gitFileStatus{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}

		status := gitFileStatus{Code: fields[0]}
		file := fields[1]
		if strings.HasPrefix(status.Code, "R") && len(fields) >= 3 {
			status.OldFile = fields[1]
			file = fields[2]
		}
		statuses[cleanInputFile(file)] = status
	}

	return statuses
}

func fileSizeAndLines(cwd string, file string) (int64, int, bool) {
	path := resolveFilePath(cwd, file)
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, -1, false
	}
	if bytes.Contains(content, []byte{0}) {
		return int64(len(content)), -1, true
	}

	return int64(len(content)), countLines(string(content)), false
}

func originalLineCount(cwd string, file string) int {
	output, err := gitOutput(cwd, "show", "HEAD:"+file)
	if err != nil {
		return -1
	}

	return countLines(output)
}

func currentNonBlankLineCount(cwd string, file string, binary bool) int {
	if binary {
		return -1
	}
	path := resolveFilePath(cwd, file)
	content, err := os.ReadFile(path)
	if err != nil || bytes.Contains(content, []byte{0}) {
		return -1
	}

	return countNonBlankLines(string(content))
}

func originalNonBlankLineCount(cwd string, file string) int {
	output, err := gitOutput(cwd, "show", "HEAD:"+file)
	if err != nil {
		return -1
	}

	return countNonBlankLines(output)
}

func countLines(text string) int {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return 0
	}

	return strings.Count(trimmed, "\n") + 1
}

func countNonBlankLines(text string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if !isBlankLine(line) {
			count++
		}
	}

	return count
}

func isBlankLine(text string) bool {
	return strings.TrimSpace(text) == ""
}

func gitOutput(cwd string, args ...string) (string, error) {
	cmd := exec.Command(gitExecutable(), args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = cleanGitLocalEnv(os.Environ())
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func gitExecutable() string {
	if configured := strings.TrimSpace(os.Getenv("CODING_ETHOS_REAL_GIT")); configured != "" {
		return configured
	}

	return "git"
}

func cleanGitLocalEnv(source []string) []string {
	cleaned := make([]string, 0, len(source))
	for _, entry := range source {
		name, _, ok := strings.Cut(entry, "=")
		if ok && gitLocalEnvName(name) {
			continue
		}
		cleaned = append(cleaned, entry)
	}

	return cleaned
}

func gitLocalEnvName(name string) bool {
	return name == "GIT_DIR" ||
		name == "GIT_WORK_TREE" ||
		name == "GIT_INDEX_FILE" ||
		name == "GIT_OBJECT_DIRECTORY" ||
		strings.HasPrefix(name, "GIT_ALTERNATE_OBJECT_DIRECTORIES")
}

func resolveFilePath(cwd string, file string) string {
	if filepath.IsAbs(file) {
		return file
	}

	return filepath.Join(cwd, filepath.FromSlash(file))
}

func diagnosticInputs(
	diagnosticList []diagnostics.Diagnostic,
	primary *diagnostics.Diagnostic,
) []DiagnosticInput {
	inputs := make([]DiagnosticInput, 0, len(diagnosticList)+1)
	for _, diagnostic := range diagnosticList {
		inputs = append(inputs, diagnosticInput(&diagnostic))
	}
	if primary != nil && !diagnosticAlreadyPresent(inputs, primary) {
		inputs = append(inputs, diagnosticInput(primary))
	}

	return inputs
}

func diagnosticAlreadyPresent(
	inputs []DiagnosticInput,
	diagnostic *diagnostics.Diagnostic,
) bool {
	candidate := diagnosticInput(diagnostic)
	for _, input := range inputs {
		if input == candidate {
			return true
		}
	}

	return false
}

func findingInputs(
	findings []FindingActivation,
	primary *FindingActivation,
) []FindingInput {
	inputs := make([]FindingInput, 0, len(findings)+1)
	for _, finding := range findings {
		finding := finding
		inputs = append(inputs, findingInput(&finding))
	}
	if primary != nil {
		inputs = append(inputs, findingInput(primary))
	}

	return inputs
}

func requiredIgnoreInputs(cwd string, paths []string) []IgnoreInput {
	inputs := make([]IgnoreInput, 0, len(paths))
	for _, requiredPath := range cleanStringValues(paths) {
		ignored, err := gitCheckIgnore(cwd, requiredPath)
		item := IgnoreInput{
			Path:    requiredPath,
			Ignored: ignored,
		}
		if err != nil {
			item.CheckFailed = true
			item.Error = err.Error()
		}
		inputs = append(inputs, item)
	}

	return inputs
}

func gitCheckIgnore(cwd string, path string) (bool, error) {
	if cwd == "" || path == "" {
		return false, nil
	}

	cmd := exec.Command(gitExecutable(), "check-ignore", "--quiet", "--no-index", path)
	cmd.Dir = cwd
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	exitError, ok := err.(*exec.ExitError)
	if ok && exitError.ExitCode() == 1 {
		return false, nil
	}

	return false, err
}

func pathInputs(cwd string, files []string, sourceRoots []string) []PathInput {
	paths := make([]PathInput, 0, len(files))
	for _, file := range files {
		pathInput := newPathInput(cwd, file, sourceRoots)
		if pathInput.File != "" {
			paths = append(paths, pathInput)
		}
	}

	return paths
}

func newPathInput(cwd string, file string, sourceRoots []string) PathInput {
	cleanFile := strings.TrimPrefix(path.Clean(strings.TrimSpace(file)), "./")
	if cleanFile == "." || cleanFile == "/" {
		cleanFile = ""
	}

	dir := ""
	base := ""
	ext := ""
	if cleanFile != "" {
		dir = path.Dir(cleanFile)
		if dir == "." {
			dir = ""
		}
		base = path.Base(cleanFile)
		ext = path.Ext(cleanFile)
	}
	symlinkTarget, isSymlink := symlinkTargetInput(cwd, cleanFile)

	return PathInput{
		File:          cleanFile,
		Dir:           dir,
		Base:          base,
		Ext:           ext,
		SymlinkTarget: symlinkTarget,
		IsSymlink:     isSymlink,
		IsGenerated:   isGeneratedPath(cleanFile),
		IsTest:        isTestPath(cleanFile),
		InSourceRoot:  inSourceRoot(cleanFile, sourceRoots),
	}
}

func symlinkTargetInput(cwd string, cleanFile string) (string, bool) {
	if cwd == "" || cleanFile == "" {
		return "", false
	}

	resolved := filepath.FromSlash(cleanFile)
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}
	resolved = filepath.Clean(resolved)

	info, err := os.Lstat(resolved)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", false
	}

	target, err := os.Readlink(resolved)
	if err != nil {
		return "", true
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(resolved), target)
	}
	target = filepath.Clean(target)

	relative, err := filepath.Rel(cwd, target)
	if err == nil && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != ".." {
		return filepath.ToSlash(relative), true
	}

	return filepath.ToSlash(target), true
}

func diagnosticInput(diagnostic *diagnostics.Diagnostic) DiagnosticInput {
	if diagnostic == nil {
		return DiagnosticInput{}
	}

	return DiagnosticInput{
		Tool:     diagnostic.Tool,
		Code:     diagnostic.Code,
		Message:  diagnostic.Message,
		File:     cleanInputFile(diagnostic.File),
		Line:     int64(diagnostic.Line),
		Column:   int64(diagnostic.Column),
		Severity: diagnostic.Severity,
		PolicyID: diagnostic.PolicyID,
	}
}

func cleanInputFile(file string) string {
	cleaned := strings.TrimPrefix(path.Clean(strings.TrimSpace(file)), "./")
	if cleaned == "." || cleaned == "/" {
		return ""
	}

	return cleaned
}

func cleanSourceRoots(sourceRoots []string) []string {
	return cleanStringSlice(sourceRoots)
}

func cleanStringSlice(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = cleanInputFile(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}

	return cleaned
}

func cleanStringValues(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}

	return cleaned
}

func presentRepoConfigs(files []string, candidates []string) []string {
	present := []string{}
	for _, candidate := range candidates {
		if listContainsCleanPath(files, candidate) {
			present = append(present, candidate)
		}
	}

	return present
}

func listContainsCleanPath(files []string, candidate string) bool {
	cleanCandidate := cleanInputFile(candidate)
	for _, file := range files {
		if cleanInputFile(file) == cleanCandidate {
			return true
		}
	}

	return false
}

func protectedPathFiles(files []string, protectedPaths []string) []string {
	matched := []string{}
	for _, file := range files {
		cleanFile := cleanInputFile(file)
		if isProtectedPath(cleanFile, protectedPaths) {
			matched = append(matched, cleanFile)
		}
	}

	return matched
}

func referencedFileInputs(
	cwd string,
	files []string,
	argv []string,
) []ReferencedFileInput {
	referenced := append([]string{}, files...)
	for _, arg := range argv {
		if arg == "" || strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}
		referenced = append(referenced, arg)
	}

	result := []ReferencedFileInput{}
	seen := map[string]bool{}
	for _, file := range referenced {
		cleanFile := cleanInputFile(file)
		if cleanFile == "" || seen[cleanFile] {
			continue
		}
		seen[cleanFile] = true
		result = append(result, referencedFileInput(cwd, file))
	}

	return result
}

func referencedFileInput(cwd string, file string) ReferencedFileInput {
	cleanFile := cleanInputFile(file)
	resolved := file
	if !filepath.IsAbs(resolved) && cwd != "" {
		resolved = filepath.Join(cwd, resolved)
	}
	resolved = filepath.Clean(resolved)

	input := ReferencedFileInput{
		Base:             path.Base(cleanFile),
		Dir:              path.Dir(cleanFile),
		File:             cleanFile,
		InAgentWorkspace: inAgentWorkspacePath(cleanFile),
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return input
	}
	input.Exists = true
	input.IsRegular = info.Mode().IsRegular()
	input.SizeBytes = info.Size()
	if !input.IsRegular || info.Size() > maxReferencedFileFactBytes {
		return input
	}

	content, err := os.ReadFile(resolved)
	if err != nil || strings.ContainsRune(string(content), 0) {
		return input
	}
	input.Lower = strings.ToLower(string(content))

	return input
}

const maxReferencedFileFactBytes = 1 << 20

func inAgentWorkspacePath(file string) bool {
	return strings.Contains(file, "/.claude/") ||
		strings.HasPrefix(file, ".claude/") ||
		strings.Contains(file, "/.codex/") ||
		strings.HasPrefix(file, ".codex/") ||
		strings.Contains(file, "/.gemini/") ||
		strings.HasPrefix(file, ".gemini/")
}

func isGeneratedPath(file string) bool {
	return strings.HasPrefix(file, "generated/") ||
		strings.Contains(file, "/generated/") ||
		strings.HasPrefix(file, ".generated/") ||
		strings.Contains(file, "/.generated/") ||
		strings.Contains(path.Base(file), ".generated.")
}

func isTestPath(file string) bool {
	base := path.Base(file)

	return strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.go") ||
		strings.Contains(file, "/tests/") ||
		strings.HasPrefix(file, "tests/")
}

func inSourceRoot(file string, sourceRoots []string) bool {
	if file == "" {
		return false
	}

	for _, sourceRoot := range sourceRoots {
		if file == sourceRoot || strings.HasPrefix(file, sourceRoot+"/") {
			return true
		}
	}

	return len(sourceRoots) == 0
}

func isProtectedPath(file string, protectedPaths []string) bool {
	cleanFile := cleanInputFile(file)
	for _, protectedPath := range protectedPaths {
		cleanProtectedPath := cleanInputFile(protectedPath)
		if cleanProtectedPath == "" {
			continue
		}
		if cleanFile == cleanProtectedPath ||
			strings.HasPrefix(cleanFile, cleanProtectedPath+"/") ||
			strings.Contains(cleanFile, "/"+cleanProtectedPath) {
			return true
		}
	}

	return false
}

func isProtectedBranch(branch string, protectedBranches []string) bool {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false
	}
	for _, protectedBranch := range protectedBranches {
		if branch == strings.TrimSpace(protectedBranch) {
			return true
		}
	}

	return false
}

func commandHasInlineEnv(command string, name string) bool {
	fields := strings.Fields(command)
	for _, field := range fields {
		if !strings.Contains(field, "=") {
			return false
		}
		if name == "" {
			return true
		}
		if strings.HasPrefix(field, name+"=") {
			return true
		}
	}

	return false
}
