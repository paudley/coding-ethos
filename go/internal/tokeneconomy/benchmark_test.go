// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package tokeneconomy

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestExtractBenchmarkArchiveRejectsTraversalThroughSymlink(t *testing.T) {
	t.Parallel()

	var payload bytes.Buffer
	writer := tar.NewWriter(&payload)
	if err := writer.WriteHeader(&tar.Header{
		Name:     "link",
		Linkname: "real",
		Mode:     0o777,
		Typeflag: tar.TypeSymlink,
	}); err != nil {
		t.Fatalf("write symlink header: %v", err)
	}
	content := []byte("escape\n")
	if err := writer.WriteHeader(&tar.Header{
		Name:     "link/result.txt",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write regular header: %v", err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatalf("write regular payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close test archive: %v", err)
	}

	root := t.TempDir()
	err := extractTarEntries(tar.NewReader(bytes.NewReader(payload.Bytes())), root)
	if err == nil || !strings.Contains(err.Error(), "traverses an extracted symlink") {
		t.Fatalf("expected symlink traversal rejection, got %v", err)
	}
}

func TestLoadBenchmarkManifestRejectsUnknownAndDriftedInputs(t *testing.T) {
	t.Parallel()

	fixture := newBenchmarkFixture(t)
	prepared, err := LoadBenchmarkManifest(context.Background(), fixture.manifestPath)
	if err != nil {
		t.Fatalf("load benchmark manifest: %v", err)
	}
	if prepared.TotalRuns != 6 || prepared.Manifest.ExperimentID != fixture.experimentID {
		t.Fatalf("unexpected prepared benchmark: %#v", prepared)
	}

	unknownPath := filepath.Join(fixture.root, "unknown.yaml")
	payload, err := os.ReadFile(fixture.manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	payload = append(payload, []byte("unknown_contract_field: true\n")...)
	if err = os.WriteFile(unknownPath, payload, 0o600); err != nil {
		t.Fatalf("write unknown-field manifest: %v", err)
	}
	if _, err = LoadBenchmarkManifest(context.Background(), unknownPath); err == nil ||
		!strings.Contains(err.Error(), "field unknown_contract_field not found") {
		t.Fatalf("expected unknown field failure, got %v", err)
	}

	multiplePath := filepath.Join(fixture.root, "multiple.yaml")
	payload = append(
		payload[:len(payload)-len("unknown_contract_field: true\n")],
		[]byte("---\n{}\n")...)
	if err = os.WriteFile(multiplePath, payload, 0o600); err != nil {
		t.Fatalf("write multiple-document manifest: %v", err)
	}
	if _, err = LoadBenchmarkManifest(context.Background(), multiplePath); err == nil ||
		!strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("expected multiple document failure, got %v", err)
	}

	if err = os.WriteFile(fixture.promptPath, []byte("changed\n"), 0o600); err != nil {
		t.Fatalf("drift prompt: %v", err)
	}
	if _, err = LoadBenchmarkManifest(
		context.Background(),
		fixture.manifestPath,
	); err == nil ||
		!strings.Contains(err.Error(), "task prompt hash") {
		t.Fatalf("expected prompt drift failure, got %v", err)
	}
}

func TestFreezeBenchmarkManifestCreatesVerifiedManifestWithoutOverwrite(t *testing.T) {
	t.Parallel()

	fixture := newBenchmarkFixture(t)
	output := filepath.Join(fixture.root, "frozen", "benchmark.yaml")
	prepared, err := FreezeBenchmarkManifest(
		context.Background(),
		fixture.manifestPath,
		output,
	)
	if err != nil {
		t.Fatalf("freeze benchmark manifest: %v", err)
	}
	if prepared.ManifestPath != output || prepared.TotalRuns != 6 {
		t.Fatalf("unexpected frozen manifest: %#v", prepared)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatalf("stat frozen manifest: %v", err)
	}
	if info.Mode().Perm() != storeFileMode {
		t.Fatalf("frozen manifest mode = %o, want %o", info.Mode().Perm(), storeFileMode)
	}

	if _, err = FreezeBenchmarkManifest(
		context.Background(),
		fixture.manifestPath,
		output,
	); err == nil || !strings.Contains(err.Error(), "file exists") {
		t.Fatalf("expected create-new failure, got %v", err)
	}
}

func TestBenchmarkScheduleIsDeterministicAndArmComplete(t *testing.T) {
	t.Parallel()

	manifest := BenchmarkManifest{
		ExperimentID:      "schedule-test",
		RandomizationSeed: "fixed-seed",
		Replicates:        2,
		BlockCheckpoints:  []int{2, 4},
		Tasks: []BenchmarkTask{
			{TaskID: "one"},
			{TaskID: "two"},
		},
	}

	first := benchmarkSchedule(manifest)
	second := benchmarkSchedule(manifest)
	if !slices.Equal(first, second) {
		t.Fatalf("schedule is not deterministic:\n%#v\n%#v", first, second)
	}
	if len(first) != 12 {
		t.Fatalf("schedule length = %d, want 12", len(first))
	}
	for index, run := range first {
		wantReplicate := index/(len(manifest.Tasks)*3) + 1
		if run.Replicate != wantReplicate {
			t.Fatalf(
				"schedule run %d replicate = %d, want layer %d",
				index,
				run.Replicate,
				wantReplicate,
			)
		}
	}
	for index := 0; index < len(first); index += 3 {
		arms := []Arm{first[index].Arm, first[index+1].Arm, first[index+2].Arm}
		slices.Sort(arms)
		if !slices.Equal(arms, []Arm{ArmFull, ArmOff, ArmStatic}) {
			t.Fatalf("block %d arms = %#v", index/3, arms)
		}
	}
}

func TestValidateAnalysisBlockCheckpointsCoversEveryReplicate(t *testing.T) {
	t.Parallel()

	if err := validateAnalysisBlockCheckpoints([]int{10, 20}, 20); err != nil {
		t.Fatalf("valid analysis block checkpoints: %v", err)
	}
	if err := validateAnalysisBlockCheckpoints([]int{10}, 20); err == nil ||
		!strings.Contains(err.Error(), "must equal block count 20") {
		t.Fatalf("expected incomplete checkpoint plan failure, got %v", err)
	}
}

func TestBenchmarkEnvironmentUsesBenignAllowlist(t *testing.T) {
	t.Parallel()

	clean := scrubBenchmarkEnvironment([]string{
		"PATH=/usr/bin",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"AWS_ACCESS_KEY_ID=must-not-leak",
		"HTTP_PROXY=http://credential@proxy.invalid",
		"HOME=/private/home",
		"CODE_ETHOS_STATE_ROOT=/ambient/state",
	})
	if !slices.Equal(clean, []string{
		"PATH=/usr/bin",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}) {
		t.Fatalf("scrubbed benchmark environment = %#v", clean)
	}
}

func TestBenchmarkCodexArgumentsDisableSandboxNetwork(t *testing.T) {
	t.Parallel()

	arguments := benchmarkCodexArguments(BenchmarkManifest{
		Provider: BenchmarkProvider{
			Model:           "fixed-model",
			ReasoningEffort: "high",
		},
	}, ArmOff, "/frozen/workspace")
	if !slices.Contains(arguments, "sandbox_workspace_write.network_access=false") {
		t.Fatalf("Codex benchmark arguments do not disable sandbox network: %#v", arguments)
	}
}

func TestRunBenchmarkUsesIsolatedArmsAndResumesWithoutReplacement(t *testing.T) {
	fixture := newBenchmarkFixture(t)
	prepared, err := LoadBenchmarkManifest(context.Background(), fixture.manifestPath)
	if err != nil {
		t.Fatalf("load benchmark manifest: %v", err)
	}

	stateRoot := filepath.Join(fixture.root, "private-state")
	options := BenchmarkRunOptions{StateRoot: stateRoot, ApprovedMaxRuns: 6}
	first, err := RunBenchmark(context.Background(), prepared, options)
	if err != nil {
		t.Fatalf("run fake benchmark: %v", err)
	}
	if first.NewlyRecorded != 6 || first.PreviouslyRecorded != 0 {
		t.Fatalf("unexpected first run summary: %#v", first)
	}

	store, err := Open(context.Background(), DefaultDBPath(stateRoot))
	if err != nil {
		t.Fatalf("open benchmark evidence: %v", err)
	}
	runs, err := store.Runs(context.Background(), fixture.experimentID)
	closeErr := store.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read benchmark evidence: runs=%v close=%v", err, closeErr)
	}
	if len(runs) != 6 {
		t.Fatalf("recorded runs = %d, want 6", len(runs))
	}

	for _, run := range runs {
		if !run.Accepted || run.Status != "completed" || run.Usage.TotalTokens != 15 {
			t.Fatalf("unexpected recorded run: %#v", run)
		}
		resultPath := filepath.Join(
			stateRoot,
			".coding-ethos",
			"token-economy-runs",
			fixture.experimentID,
			run.RunID,
			"workspace",
			"result.txt",
		)
		payload, readErr := os.ReadFile(resultPath)
		if readErr != nil {
			t.Fatalf("read %s arm result: %v", run.Arm, readErr)
		}
		want := "context static\n"
		if run.Arm == ArmFull {
			want = "context full\n"
		} else if run.Arm == ArmOff {
			want = "off static\n"
		}
		if string(payload) != want {
			t.Fatalf("%s arm isolation marker = %q, want %q", run.Arm, payload, want)
		}
	}

	second, err := RunBenchmark(context.Background(), prepared, options)
	if err != nil {
		t.Fatalf("resume fake benchmark: %v", err)
	}
	if second.NewlyRecorded != 0 || second.PreviouslyRecorded != 6 ||
		len(second.Executions) != 0 {
		t.Fatalf("unexpected resumed summary: %#v", second)
	}
}

type benchmarkFixture struct {
	root         string
	experimentID string
	manifestPath string
	promptPath   string
}

func newBenchmarkFixture(t *testing.T) benchmarkFixture {
	t.Helper()

	root := t.TempDir()
	repository := filepath.Join(root, "source")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatalf("create source repository: %v", err)
	}
	writeTestFile(t, filepath.Join(repository, "baseline.txt"), "baseline\n", 0o600)
	runTestGit(t, repository, "init", "--quiet", "--initial-branch=main")
	runTestGit(t, repository, "add", "--all")
	runTestGit(
		t,
		repository,
		"-c",
		"user.name=Benchmark Test",
		"-c",
		"user.email=benchmark@invalid",
		"-c",
		"commit.gpgSign=false",
		"commit",
		"--quiet",
		"--no-gpg-sign",
		"-m",
		"baseline",
	)
	commit := strings.TrimSpace(runTestGit(t, repository, "rev-parse", "HEAD"))

	providerPath := filepath.Join(root, "fake-codex")
	writeTestFile(t, providerPath, fakeCodexScript, 0o700)
	authPath := filepath.Join(root, "auth.json")
	writeTestFile(t, authPath, "{}\n", 0o600)
	staticPath := filepath.Join(root, "managed-agents.md")
	writeTestFile(t, staticPath, "# Managed Coding Ethos benchmark context\n", 0o600)
	promptPath := filepath.Join(root, "prompt.txt")
	writeTestFile(t, promptPath, "Create result.txt.\n", 0o600)
	validatorPath := filepath.Join(root, "validate-result")
	writeTestFile(t, validatorPath, fakeValidatorScript, 0o700)

	sourceHash, err := benchmarkSourceArchiveSHA256(
		context.Background(),
		repository,
		commit,
	)
	if err != nil {
		t.Fatalf("hash source archive: %v", err)
	}
	validators := [][]string{{validatorPath}}
	validatorHash, err := benchmarkValidatorSHA256(validators)
	if err != nil {
		t.Fatalf("hash validator: %v", err)
	}

	manifest := BenchmarkManifest{
		ExperimentID: "fake-controlled-experiment",
		CreatedAtUTC: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC).
			Format(time.RFC3339Nano),
		RandomizationSeed: "fixture-seed",
		StaticContext: BenchmarkStaticContext{
			Path:   staticPath,
			SHA256: fileSHA256(t, staticPath),
		},
		Provider: BenchmarkProvider{
			Kind:             ProviderCodex,
			Executable:       providerPath,
			ExecutableSHA256: fileSHA256(t, providerPath),
			RuntimeVersion:   "fake-codex 1.0",
			Model:            "fake-model",
			ReasoningEffort:  "medium",
			AuthFile:         authPath,
		},
		Tasks: []BenchmarkTask{
			{
				TaskID:              "diagnostic-one",
				Kind:                "diagnostic",
				RepositoryPath:      repository,
				Commit:              commit,
				SourceArchiveSHA256: sourceHash,
				PromptPath:          promptPath,
				PromptSHA256:        fileSHA256(t, promptPath),
				ValidatorSHA256:     validatorHash,
				AgentTimeout:        "30s",
				ValidationTimeout:   "30s",
				AllowedPaths:        []string{"result.txt"},
				Validators:          validators,
			},
			{
				TaskID:              "real-one",
				Kind:                "real",
				RepositoryPath:      repository,
				Commit:              commit,
				SourceArchiveSHA256: sourceHash,
				PromptPath:          promptPath,
				PromptSHA256:        fileSHA256(t, promptPath),
				ValidatorSHA256:     validatorHash,
				AgentTimeout:        "30s",
				ValidationTimeout:   "30s",
				AllowedPaths:        []string{"result.txt"},
				Validators:          validators,
			},
		},
		FullConfigOverrides: []string{"features.code_intel=true"},
		SchemaVersion:       BenchmarkManifestVersion,
		Replicates:          1,
		BlockCheckpoints:    []int{2},
	}
	payload, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	manifestPath := filepath.Join(root, "benchmark.yaml")
	if err = os.WriteFile(manifestPath, payload, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	return benchmarkFixture{
		root:         root,
		experimentID: manifest.ExperimentID,
		manifestPath: manifestPath,
		promptPath:   promptPath,
	}
}

func writeTestFile(t *testing.T, path, payload string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(payload), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	digest := sha256.Sum256(payload)

	return hex.EncodeToString(digest[:])
}

func runTestGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.CommandContext( //nolint:gosec // Test arguments are fixed local fixtures.
		context.Background(),
		"git",
		append([]string{"-C", root}, arguments...)...,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, output)
	}

	return string(output)
}

const fakeCodexScript = `#!/bin/sh
set -eu
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'fake-codex 1.0'
  exit 0
fi
context=off
if [ -f "$CODEX_HOME/AGENTS.md" ]; then
  context=context
fi
dynamic=static
if [ -n "${CODE_ETHOS_STATE_ROOT:-}" ]; then
  dynamic=full
fi
printf '%s %s\n' "$context" "$dynamic" > result.txt
mkdir -p "$CODEX_HOME/sessions/2026/08/28"
printf '%s\n' \
  '{"type":"session_meta","payload":{"id":"fake-session"}}' \
  '{"type":"turn_context","payload":{"model":"fake-model"}}' \
  '{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}' \
  > "$CODEX_HOME/sessions/2026/08/28/rollout.jsonl"
printf '%s\n' '{"type":"turn.completed"}'
`

const fakeValidatorScript = `#!/bin/sh
set -eu
test -f result.txt
`
