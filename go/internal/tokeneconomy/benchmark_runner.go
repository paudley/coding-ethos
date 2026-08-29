// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:cyclop,funlen,gocyclo,govet,lll,mnd,noinlineerr,wsl_v5 // Runner stages remain auditable.
package tokeneconomy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	maximumBenchmarkPromptBytes = 1 << 20
	benchmarkProjectDocMaxBytes = 131_072
	benchmarkCompletedStatus    = "completed"
	codexConfigFlag             = "--config"
)

var errBenchmarkRun = errors.New("token-economy benchmark run error")

// BenchmarkRunOptions are explicit operator controls for a model-backed run.
type BenchmarkRunOptions struct {
	StateRoot       string
	ApprovedMaxRuns int
}

// BenchmarkExecution summarizes one immutable scheduled run.
type BenchmarkExecution struct {
	RunID    string `json:"run_id"`
	TaskID   string `json:"task_id"`
	Arm      Arm    `json:"arm"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	Accepted bool   `json:"accepted"`
}

// BenchmarkRunSummary reports the bounded work completed by one invocation.
type BenchmarkRunSummary struct {
	ExperimentID       string               `json:"experiment_id"`
	ManifestSHA256     string               `json:"manifest_sha256"`
	ProtocolSHA256     string               `json:"protocol_sha256"`
	ScheduledRuns      int                  `json:"scheduled_runs"`
	PreviouslyRecorded int                  `json:"previously_recorded"`
	NewlyRecorded      int                  `json:"newly_recorded"`
	Executions         []BenchmarkExecution `json:"executions"`
}

type benchmarkRunSpec struct {
	TaskID    string
	BlockID   string
	RunID     string
	Arm       Arm
	Replicate int
}

// RunBenchmark executes pending three-arm blocks up to an explicit run cap.
func RunBenchmark(
	ctx context.Context,
	prepared PreparedBenchmark,
	options BenchmarkRunOptions,
) (BenchmarkRunSummary, error) {
	stateRoot, err := validateBenchmarkRunOptions(prepared, options)
	if err != nil {
		return BenchmarkRunSummary{}, err
	}

	store, err := Open(ctx, DefaultDBPath(stateRoot))
	if err != nil {
		return BenchmarkRunSummary{}, err
	}
	defer store.Close()

	err = registerPreparedBenchmark(ctx, store, prepared)
	if err != nil {
		return BenchmarkRunSummary{}, err
	}

	schedule := benchmarkSchedule(prepared.Manifest)
	summary := BenchmarkRunSummary{
		ExperimentID:   prepared.Manifest.ExperimentID,
		ManifestSHA256: prepared.ManifestSHA256,
		ProtocolSHA256: prepared.ProtocolSHA256,
		ScheduledRuns:  len(schedule),
		Executions:     []BenchmarkExecution{},
	}

	remaining := options.ApprovedMaxRuns
	for index := 0; index < len(schedule); index += 3 {
		block := schedule[index : index+3]
		pending, recorded, blockErr := pendingBenchmarkBlock(ctx, store, block)
		if blockErr != nil {
			return summary, blockErr
		}
		summary.PreviouslyRecorded += recorded
		if len(pending) == 0 {
			continue
		}
		if len(pending) > remaining {
			break
		}

		for _, spec := range pending {
			run, execution, fatalErr := executeBenchmarkRun(
				ctx,
				prepared,
				spec,
				stateRoot,
			)
			if fatalErr != nil {
				return summary, fatalErr
			}

			if err = store.RecordRun(ctx, run); err != nil {
				return summary, err
			}
			summary.Executions = append(summary.Executions, execution)
			summary.NewlyRecorded++
			remaining--
		}
	}

	return summary, nil
}

func validateBenchmarkRunOptions(
	prepared PreparedBenchmark,
	options BenchmarkRunOptions,
) (string, error) {
	if options.ApprovedMaxRuns <= 0 ||
		options.ApprovedMaxRuns > maximumBenchmarkRuns ||
		options.ApprovedMaxRuns%3 != 0 {
		return "", fmt.Errorf(
			"%w: approved run count must be a positive multiple of 3 no greater than %d",
			errBenchmarkRun,
			maximumBenchmarkRuns,
		)
	}
	if options.ApprovedMaxRuns > prepared.TotalRuns {
		return "", fmt.Errorf(
			"%w: approved run count %d exceeds manifest schedule %d",
			errBenchmarkRun,
			options.ApprovedMaxRuns,
			prepared.TotalRuns,
		)
	}
	if !filepath.IsAbs(options.StateRoot) {
		return "", fmt.Errorf("%w: state root must be absolute", errBenchmarkRun)
	}

	stateRoot := filepath.Clean(options.StateRoot)
	err := os.MkdirAll(filepath.Join(stateRoot, ".coding-ethos"), storeDirMode)
	if err != nil {
		return "", fmt.Errorf("create benchmark state root: %w", err)
	}

	return stateRoot, nil
}

func registerPreparedBenchmark(
	ctx context.Context,
	store *Store,
	prepared PreparedBenchmark,
) error {
	manifest := prepared.Manifest
	experiment := Experiment{
		ExperimentID:             manifest.ExperimentID,
		ManifestSHA256:           prepared.ManifestSHA256,
		ProtocolSHA256:           prepared.ProtocolSHA256,
		Provider:                 manifest.Provider.Kind,
		Model:                    manifest.Provider.Model,
		RuntimeVersion:           manifest.Provider.RuntimeVersion,
		CreatedAtUTC:             manifest.CreatedAtUTC,
		RandomizationSeed:        manifest.RandomizationSeed,
		Status:                   "registered",
		AnalysisBlockCheckpoints: slices.Clone(manifest.BlockCheckpoints),
		Randomized:               true,
		ArmIsolationVerified:     true,
	}
	if err := store.RecordExperiment(ctx, experiment); err != nil {
		return err
	}

	for _, task := range manifest.Tasks {
		record := Task{
			ExperimentID:    manifest.ExperimentID,
			TaskID:          task.TaskID,
			Kind:            task.Kind,
			SourceSHA256:    task.SourceArchiveSHA256,
			PromptSHA256:    task.PromptSHA256,
			ValidatorSHA256: task.ValidatorSHA256,
		}
		if err := store.RecordTask(ctx, record); err != nil {
			return err
		}
	}

	return nil
}

func benchmarkSchedule(manifest BenchmarkManifest) []benchmarkRunSpec {
	type block struct {
		TaskID    string
		ID        string
		Score     string
		Replicate int
	}

	blocks := make([]block, 0, len(manifest.Tasks)*manifest.Replicates)
	for replicate := 1; replicate <= manifest.Replicates; replicate++ {
		layer := make([]block, 0, len(manifest.Tasks))
		for _, task := range manifest.Tasks {
			id := fmt.Sprintf("%s:%d", task.TaskID, replicate)
			layer = append(layer, block{
				TaskID:    task.TaskID,
				ID:        id,
				Score:     benchmarkOrderScore(manifest.RandomizationSeed, "block", id),
				Replicate: replicate,
			})
		}
		slices.SortFunc(layer, func(left, right block) int {
			return strings.Compare(left.Score+left.ID, right.Score+right.ID)
		})
		blocks = append(blocks, layer...)
	}

	schedule := make([]benchmarkRunSpec, 0, len(blocks)*3)
	for _, current := range blocks {
		arms := []Arm{ArmFull, ArmStatic, ArmOff}
		slices.SortFunc(arms, func(left, right Arm) int {
			leftScore := benchmarkOrderScore(
				manifest.RandomizationSeed,
				current.ID,
				string(left),
			)
			rightScore := benchmarkOrderScore(
				manifest.RandomizationSeed,
				current.ID,
				string(right),
			)

			return strings.Compare(leftScore+string(left), rightScore+string(right))
		})

		for _, arm := range arms {
			runDigest := sha256.Sum256([]byte(
				manifest.ExperimentID + "\x00" + current.ID + "\x00" + string(arm),
			))
			schedule = append(schedule, benchmarkRunSpec{
				TaskID:    current.TaskID,
				BlockID:   current.ID,
				RunID:     "run-" + hex.EncodeToString(runDigest[:12]),
				Arm:       arm,
				Replicate: current.Replicate,
			})
		}
	}

	return schedule
}

func benchmarkOrderScore(seed, scope, value string) string {
	digest := sha256.Sum256([]byte(seed + "\x00" + scope + "\x00" + value))

	return hex.EncodeToString(digest[:])
}

func pendingBenchmarkBlock(
	ctx context.Context,
	store *Store,
	block []benchmarkRunSpec,
) ([]benchmarkRunSpec, int, error) {
	pending := []benchmarkRunSpec{}
	recorded := 0
	for _, spec := range block {
		found, err := store.RunRecorded(ctx, spec.RunID)
		if err != nil {
			return nil, 0, err
		}
		if found {
			recorded++
		} else {
			pending = append(pending, spec)
		}
	}

	return pending, recorded, nil
}

func executeBenchmarkRun(
	ctx context.Context,
	prepared PreparedBenchmark,
	spec benchmarkRunSpec,
	stateRoot string,
) (Run, BenchmarkExecution, error) {
	task, found := benchmarkTaskByID(prepared.Manifest.Tasks, spec.TaskID)
	if !found {
		return Run{}, BenchmarkExecution{}, fmt.Errorf(
			"%w: scheduled task %q is missing",
			errBenchmarkRun,
			spec.TaskID,
		)
	}

	err := revalidateBenchmarkInputs(prepared.Manifest, task)
	if err != nil {
		return Run{}, BenchmarkExecution{}, err
	}

	runRoot := filepath.Join(
		stateRoot,
		".coding-ethos",
		"token-economy-runs",
		prepared.Manifest.ExperimentID,
		spec.RunID,
	)
	workspace, err := prepareBenchmarkWorkspace(
		ctx,
		task,
		filepath.Join(runRoot, "workspace"),
	)
	if err != nil {
		return Run{}, BenchmarkExecution{}, err
	}

	provider := runCodexBenchmark(
		ctx,
		prepared.Manifest,
		task,
		spec,
		runRoot,
		workspace.Path,
	)
	validation := validateBenchmarkWorkspace(
		ctx,
		task,
		workspace,
		runRoot,
		provider.ExitCode == 0 && !provider.TimedOut && provider.EvidenceError == nil,
	)

	receiptSHA, receiptErr := writeBenchmarkValidationReceipt(runRoot, validation)
	if receiptErr != nil {
		return Run{}, BenchmarkExecution{}, receiptErr
	}

	mechanisms := MechanismMetrics{}
	if spec.Arm == ArmFull && provider.Ledger.SessionID != "" {
		mechanisms, err = readRunMechanisms(
			ctx,
			filepath.Join(provider.FullStateRoot, ".coding-ethos", "code-intel.duckdb"),
			provider.Ledger.SessionID,
		)
		if err != nil {
			provider.Errors = append(provider.Errors, err.Error())
		}
	}

	status := benchmarkCompletedStatus
	if provider.TimedOut {
		status = "timed_out"
	} else if provider.ExitCode != 0 || provider.EvidenceError != nil {
		status = "failed"
	}
	failureReason := strings.Join(provider.Errors, "; ")

	run := Run{
		RunID:                     spec.RunID,
		ExperimentID:              prepared.Manifest.ExperimentID,
		TaskID:                    spec.TaskID,
		Arm:                       spec.Arm,
		Provider:                  prepared.Manifest.Provider.Kind,
		Model:                     prepared.Manifest.Provider.Model,
		ProviderSessionID:         provider.Ledger.SessionID,
		LedgerSHA256:              provider.Ledger.SourceSHA256,
		ValidationReceiptSHA256:   receiptSHA,
		StartedAtUTC:              provider.StartedAtUTC,
		CompletedAtUTC:            provider.CompletedAtUTC,
		Status:                    status,
		FailureReason:             failureReason,
		Replicate:                 spec.Replicate,
		DurationMilliseconds:      provider.Duration.Milliseconds(),
		Accepted:                  validation.Accepted,
		SevereGovernanceViolation: len(validation.OutOfScopePaths) > 0,
		Usage:                     provider.Ledger.Usage,
		UsageEvents:               provider.Ledger.Events,
		Mechanisms:                mechanisms,
	}
	execution := BenchmarkExecution{
		RunID:    spec.RunID,
		TaskID:   spec.TaskID,
		Arm:      spec.Arm,
		Status:   status,
		Error:    failureReason,
		Accepted: validation.Accepted,
	}

	return run, execution, nil
}

func benchmarkTaskByID(tasks []BenchmarkTask, taskID string) (BenchmarkTask, bool) {
	for _, task := range tasks {
		if task.TaskID == taskID {
			return task, true
		}
	}

	return BenchmarkTask{}, false
}

func revalidateBenchmarkInputs(manifest BenchmarkManifest, task BenchmarkTask) error {
	for _, input := range []struct {
		path     string
		expected string
		label    string
	}{
		{manifest.Provider.Executable, manifest.Provider.ExecutableSHA256, "provider executable"},
		{manifest.StaticContext.Path, manifest.StaticContext.SHA256, "static context"},
		{task.PromptPath, task.PromptSHA256, "task prompt"},
	} {
		if err := validateExpectedFileHash(
			input.path,
			input.expected,
			input.label,
		); err != nil {
			return err
		}
	}

	validatorHash, err := benchmarkValidatorSHA256(task.Validators)
	if err != nil {
		return err
	}
	if validatorHash != task.ValidatorSHA256 {
		return fmt.Errorf(
			"%w: validator identity changed after manifest validation",
			errBenchmarkRun,
		)
	}

	return nil
}

type codexBenchmarkOutcome struct {
	Ledger         Ledger
	FullStateRoot  string
	StartedAtUTC   string
	CompletedAtUTC string
	Errors         []string
	Duration       time.Duration
	EvidenceError  error
	ExitCode       int
	TimedOut       bool
}

func runCodexBenchmark(
	ctx context.Context,
	manifest BenchmarkManifest,
	task BenchmarkTask,
	spec benchmarkRunSpec,
	runRoot string,
	workspace string,
) codexBenchmarkOutcome {
	return runCodexBenchmarkWithHomeRemover(
		ctx,
		manifest,
		task,
		spec,
		runRoot,
		workspace,
		os.RemoveAll,
	)
}

func runCodexBenchmarkWithHomeRemover(
	ctx context.Context,
	manifest BenchmarkManifest,
	task BenchmarkTask,
	spec benchmarkRunSpec,
	runRoot string,
	workspace string,
	removeHome func(string) error,
) codexBenchmarkOutcome {
	started := time.Now().UTC()
	outcome := codexBenchmarkOutcome{
		StartedAtUTC: started.Format(time.RFC3339Nano),
		Errors:       []string{},
		ExitCode:     -1,
	}

	agentTimeout, err := time.ParseDuration(task.AgentTimeout)
	if err != nil {
		outcome.Errors = append(outcome.Errors, err.Error())

		return finishCodexOutcome(outcome, started)
	}

	codexHome, err := os.MkdirTemp(runRoot, ".codex-home-")
	if err != nil {
		outcome.Errors = append(
			outcome.Errors,
			fmt.Sprintf("create isolated Codex home: %v", err),
		)

		return finishCodexOutcome(outcome, started)
	}
	finish := func() codexBenchmarkOutcome {
		removeErr := removeHome(codexHome)
		if removeErr != nil {
			cleanupErr := fmt.Errorf("remove isolated Codex home: %w", removeErr)
			outcome.EvidenceError = errors.Join(outcome.EvidenceError, cleanupErr)
			outcome.Errors = append(outcome.Errors, cleanupErr.Error())
		}

		return finishCodexOutcome(outcome, started)
	}

	err = populateBenchmarkCodexHome(codexHome, manifest, spec.Arm)
	if err != nil {
		outcome.Errors = append(outcome.Errors, err.Error())

		return finish()
	}

	prompt, err := os.ReadFile(task.PromptPath)
	if err != nil {
		outcome.Errors = append(outcome.Errors, fmt.Sprintf("read benchmark prompt: %v", err))

		return finish()
	}
	if len(prompt) > maximumBenchmarkPromptBytes {
		outcome.Errors = append(outcome.Errors, "benchmark prompt exceeds size limit")

		return finish()
	}

	stdout, stderr, err := createAgentLogs(runRoot)
	if err != nil {
		outcome.Errors = append(outcome.Errors, err.Error())

		return finish()
	}

	runContext, cancel := context.WithTimeout(ctx, agentTimeout)
	defer cancel()

	arguments := benchmarkCodexArguments(manifest, spec.Arm, workspace)
	command := safeexec.CommandContext(
		runContext,
		manifest.Provider.Executable,
		arguments...,
	)
	command.Dir = workspace
	command.Env = benchmarkAgentEnvironment(codexHome, runRoot, workspace, spec.Arm)
	command.Stdin = bytes.NewReader(prompt)
	command.Stdout = stdout
	command.Stderr = stderr

	runErr := command.Run()
	closeErr := errors.Join(stdout.Close(), stderr.Close())
	if closeErr != nil {
		outcome.Errors = append(outcome.Errors, "close agent logs: "+closeErr.Error())
	}
	outcome.ExitCode = benchmarkProcessExitCode(runErr)
	outcome.TimedOut = errors.Is(runContext.Err(), context.DeadlineExceeded)
	if runErr != nil {
		outcome.Errors = append(outcome.Errors, "provider process: "+runErr.Error())
	}

	ledgerPath, ledgerErr := findCodexSessionLedger(codexHome)
	if ledgerErr == nil {
		outcome.Ledger, ledgerErr = ParseLedger(ProviderCodex, ledgerPath)
	}
	if ledgerErr != nil {
		outcome.EvidenceError = ledgerErr
		outcome.Errors = append(outcome.Errors, "provider ledger: "+ledgerErr.Error())
	}
	if spec.Arm == ArmFull {
		outcome.FullStateRoot = filepath.Join(runRoot, "coding-ethos-state")
	}

	return finish()
}

func finishCodexOutcome(
	outcome codexBenchmarkOutcome,
	started time.Time,
) codexBenchmarkOutcome {
	completed := time.Now().UTC()
	outcome.CompletedAtUTC = completed.Format(time.RFC3339Nano)
	outcome.Duration = completed.Sub(started)

	return outcome
}

func populateBenchmarkCodexHome(
	codexHome string,
	manifest BenchmarkManifest,
	arm Arm,
) error {
	auth, err := os.ReadFile(manifest.Provider.AuthFile)
	if err != nil {
		return fmt.Errorf("read provider auth file: %w", err)
	}
	// codexHome is created by os.MkdirTemp under the validated private run root.
	if err = os.WriteFile( //nolint:gosec // The destination is not provider-controlled.
		filepath.Join(codexHome, "auth.json"),
		auth,
		0o600,
	); err != nil {
		return fmt.Errorf("write isolated provider auth file: %w", err)
	}

	if arm == ArmOff {
		return nil
	}

	staticContext, err := os.ReadFile(manifest.StaticContext.Path)
	if err != nil {
		return fmt.Errorf("read benchmark static context: %w", err)
	}
	if err = os.WriteFile( //nolint:gosec // codexHome is a validated private temp directory.
		filepath.Join(codexHome, "AGENTS.md"),
		staticContext,
		0o600,
	); err != nil {
		return fmt.Errorf("write benchmark static context: %w", err)
	}

	return nil
}

func createAgentLogs(runRoot string) (*os.File, *os.File, error) {
	stdout, err := os.OpenFile(
		filepath.Join(runRoot, "agent.stdout.jsonl"),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create agent stdout log: %w", err)
	}

	stderr, err := os.OpenFile(
		filepath.Join(runRoot, "agent.stderr.log"),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		_ = stdout.Close()

		return nil, nil, fmt.Errorf("create agent stderr log: %w", err)
	}

	return stdout, stderr, nil
}

func benchmarkCodexArguments(
	manifest BenchmarkManifest,
	arm Arm,
	workspace string,
) []string {
	arguments := []string{
		"exec",
		"--json",
		"--ignore-user-config",
		"--ignore-rules",
		"--strict-config",
		"--model",
		manifest.Provider.Model,
		"--sandbox",
		"workspace-write",
		"--cd",
		workspace,
		codexConfigFlag,
		"approval_policy=\"never\"",
		codexConfigFlag,
		fmt.Sprintf("model_reasoning_effort=%q", manifest.Provider.ReasoningEffort),
		codexConfigFlag,
		fmt.Sprintf("project_doc_max_bytes=%d", benchmarkProjectDocMaxBytes),
		codexConfigFlag,
		"sandbox_workspace_write.network_access=false",
	}
	if arm == ArmFull {
		for _, override := range manifest.FullConfigOverrides {
			arguments = append(arguments, codexConfigFlag, override)
		}
	}

	return append(arguments, "-")
}

func benchmarkAgentEnvironment(
	codexHome string,
	runRoot string,
	workspace string,
	arm Arm,
) []string {
	environment := scrubBenchmarkEnvironment(os.Environ())
	environment = append(environment,
		"CODEX_HOME="+codexHome,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GH_PROMPT_DISABLED=1",
	)
	if arm == ArmFull {
		environment = append(environment,
			"CODE_ETHOS_STATE_ROOT="+filepath.Join(runRoot, "coding-ethos-state"),
			"CODE_ETHOS_CONSUMER_ROOT="+workspace,
		)
	}

	return environment
}

func scrubBenchmarkEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if !found || !benchmarkAllowedEnvironmentKey(key) {
			continue
		}

		result = append(result, entry)
	}

	return result
}

func benchmarkAllowedEnvironmentKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	if strings.HasPrefix(upper, "LC_") {
		return true
	}

	return slices.Contains([]string{
		"COLORTERM",
		"LANG",
		"LANGUAGE",
		"PATH",
		"SSL_CERT_DIR",
		"SSL_CERT_FILE",
		"TEMP",
		"TERM",
		"TMP",
		"TMPDIR",
		"TZ",
	}, upper)
}

func benchmarkProcessExitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}

	return -1
}

func findCodexSessionLedger(codexHome string) (string, error) {
	root := filepath.Join(codexHome, "sessions")
	ledgers := []string{}

	err := filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
				ledgers = append(ledgers, path)
			}

			return nil
		},
	)
	if err != nil {
		return "", fmt.Errorf("find Codex session ledger: %w", err)
	}
	if len(ledgers) != 1 {
		return "", fmt.Errorf(
			"%w: expected one Codex session ledger, found %d",
			errBenchmarkRun,
			len(ledgers),
		)
	}

	return ledgers[0], nil
}
