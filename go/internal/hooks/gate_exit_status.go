// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const (
	gateExitStatusPolicyID = "shell.required_gate_exit_status"
	maxGateShellDepth      = 3
	preCommitExecutable    = "pre-commit"
	pytestExecutable       = "pytest"

	gateBackgroundReason = "Required repository gates must return their " +
		"terminal status, not run in the background."
	gateFallbackReason = "Required repository gate failure cannot be hidden " +
		"by a shell fallback."
	gatePipelineReason = "Required repository gate pipelines must enable " +
		"pipefail so the gate status remains authoritative."
	gateNestingReason = "Required repository gate inspection exceeded the " +
		"supported shell nesting depth."
	gateSequenceReason = "Commands after a required repository gate must " +
		"capture and return the gate's exact exit status."
)

func requiredGateExitStatusRouteFor(event Event) InspectionRoute {
	if event.HookEventName != eventPreToolUse || event.ToolName != toolBash {
		return InspectionRoute{}
	}

	reason, masked := maskedRequiredGateStatus(event.Command(), false, 0)
	if !masked {
		return InspectionRoute{}
	}

	return InspectionRoute{
		BlockPolicyID: gateExitStatusPolicyID,
		Reason:        reason,
		Block:         true,
	}
}

type gateShell struct {
	segments  [][]string
	operators []string
}

func maskedRequiredGateStatus(
	command string,
	inheritedPipefail bool,
	depth int,
) (string, bool) {
	if depth > maxGateShellDepth {
		return gateNestingReason, true
	}

	if strings.TrimSpace(command) == "" {
		return "", false
	}

	parsed, err := parseGateShell(command)
	if err != nil {
		return "", false
	}

	pipefail := inheritedPipefail

	for index := range parsed.segments {
		reason, masked := parsed.maskedSegmentStatus(index, pipefail, depth)
		if masked {
			return reason, true
		}

		if isPipefailCommand(gateExecutableArgv(parsed.segments[index])) {
			pipefail = true
		}
	}

	return "", false
}

func (parsed gateShell) maskedSegmentStatus(
	index int,
	pipefail bool,
	depth int,
) (string, bool) {
	argv := gateExecutableArgv(parsed.segments[index])
	nested, nestedPipefail, nestedFound := nestedShellScript(argv)

	if nestedFound {
		reason, masked := maskedRequiredGateStatus(
			nested,
			pipefail || nestedPipefail,
			depth+1,
		)
		if masked {
			return reason, true
		}
	}

	if !requiredGateArgv(argv) {
		return "", false
	}

	reason := maskedGateOperatorReason(parsed.operators[index:], pipefail)
	if reason != "" {
		return reason, true
	}

	if sequenceAfter(index, parsed.operators) &&
		!parsed.returnsStatusFrom(index) {
		return gateSequenceReason, true
	}

	return "", false
}

func maskedGateOperatorReason(operators []string, pipefail bool) string {
	for _, operator := range operators {
		switch operator {
		case "||":
			return gateFallbackReason
		case "&":
			return gateBackgroundReason
		case "|", "|&":
			if !pipefail {
				return gatePipelineReason
			}
		}
	}

	return ""
}

func parseGateShell(command string) (gateShell, error) {
	fields, err := shellparse.ControlFields(command)
	if err != nil {
		return gateShell{}, fmt.Errorf("parse shell control fields: %w", err)
	}

	parsed := gateShell{}
	segment := []string{}

	for _, field := range fields {
		if isShellControlToken(field) {
			if len(segment) > 0 {
				parsed.segments = append(parsed.segments, segment)
				segment = nil
			}

			parsed.operators = append(parsed.operators, field)

			continue
		}

		segment = append(segment, field)
	}

	if len(segment) > 0 {
		parsed.segments = append(parsed.segments, segment)
	}

	validTrailingBackground := len(parsed.operators) == len(parsed.segments) &&
		len(parsed.operators) > 0 &&
		parsed.operators[len(parsed.operators)-1] == "&"

	if len(parsed.segments) == 0 ||
		(len(parsed.operators)+1 != len(parsed.segments) &&
			!validTrailingBackground) {
		return gateShell{}, nil
	}

	return parsed, nil
}

func isPipefailCommand(argv []string) bool {
	return len(argv) >= 3 && argv[0] == "set" &&
		argv[1] == "-o" && argv[2] == "pipefail"
}

func sequenceAfter(gateIndex int, operators []string) bool {
	return slices.Contains(operators[gateIndex:], ";")
}

func (parsed gateShell) returnsStatusFrom(gateIndex int) bool {
	relativeIndex := slices.Index(parsed.operators[gateIndex:], ";")
	if relativeIndex < 0 {
		return true
	}

	sequenceIndex := gateIndex + relativeIndex
	commandIndex := sequenceIndex + 1

	if commandIndex >= len(parsed.segments) {
		return true
	}

	first := parsed.segments[commandIndex]
	if directStatusExit(first, commandIndex, len(parsed.segments)) {
		return true
	}

	statusName, captured := capturedStatusName(first)
	if !captured {
		return false
	}

	last := gateExecutableArgv(parsed.segments[len(parsed.segments)-1])
	if len(last) < 2 || last[0] != "exit" {
		return false
	}

	return last[1] == "$"+statusName ||
		last[1] == "${"+statusName+"}"
}

func directStatusExit(first []string, commandIndex, segmentCount int) bool {
	return len(first) >= 2 && first[0] == "exit" && first[1] == "$?" &&
		commandIndex == segmentCount-1
}

func capturedStatusName(first []string) (string, bool) {
	if len(first) == 0 {
		return "", false
	}

	name, value, found := strings.Cut(first[0], "=")

	return name, found && name != "" && value == "$?"
}

func gateExecutableArgv(segment []string) []string {
	argv := gateExecutableFields(segment)

	for len(argv) > 0 {
		unwrapped, changed := unwrapGateWrapper(argv)
		if !changed {
			return argv
		}

		argv = unwrapped
	}

	return argv
}

func gateExecutableFields(segment []string) []string {
	argv := make([]string, 0, len(segment))

	for _, field := range segment {
		if shellRedirectField(field) ||
			(len(argv) == 0 && shellAssignment(field)) {
			continue
		}

		argv = append(argv, field)
	}

	return argv
}

func unwrapGateWrapper(argv []string) ([]string, bool) {
	switch filepath.Base(argv[0]) {
	case tokenEnv:
		return unwrapEnvArgv(argv[1:]), true
	case tokenCommand, tokenExec, "nohup", "time":
		return unwrapSimpleArgv(argv[1:]), true
	case "nice":
		return unwrapNiceArgv(argv[1:]), true
	case "timeout":
		return unwrapTimeoutArgv(argv[1:]), true
	case "flock":
		return unwrapFlockArgv(argv[1:]), true
	case "cerun":
		separator := slices.Index(argv, "--")
		if separator < 0 {
			return argv, false
		}

		return argv[separator+1:], true
	default:
		return argv, false
	}
}

func shellRedirectField(field string) bool {
	trimmed := strings.TrimLeft(field, "0123456789")

	return strings.HasPrefix(trimmed, ">") ||
		strings.HasPrefix(trimmed, "<")
}

func unwrapEnvArgv(argv []string) []string {
	for len(argv) > 0 {
		if shellAssignment(argv[0]) {
			argv = argv[1:]

			continue
		}

		if envOptionConsumesValue(argv[0]) && len(argv) > 1 {
			argv = argv[2:]

			continue
		}

		if strings.HasPrefix(argv[0], "-") {
			argv = argv[1:]

			continue
		}

		break
	}

	return argv
}

func envOptionConsumesValue(argument string) bool {
	return slices.Contains(
		[]string{
			"-u", "--unset", "-C", "--chdir", "-S", "--split-string",
		},
		argument,
	)
}

func unwrapSimpleArgv(argv []string) []string {
	for len(argv) > 0 && strings.HasPrefix(argv[0], "-") {
		argv = argv[1:]
	}

	return argv
}

func unwrapNiceArgv(argv []string) []string {
	for len(argv) > 0 {
		if slices.Contains(
			[]string{"-n", "--adjustment"},
			argv[0],
		) && len(argv) > 1 {
			argv = argv[2:]

			continue
		}

		if strings.HasPrefix(argv[0], "-") {
			argv = argv[1:]

			continue
		}

		break
	}

	return argv
}

func unwrapTimeoutArgv(argv []string) []string {
	for len(argv) > 0 {
		if timeoutOptionConsumesValue(argv[0]) && len(argv) > 1 {
			argv = argv[2:]

			continue
		}

		if strings.HasPrefix(argv[0], "-") {
			argv = argv[1:]

			continue
		}

		break
	}

	return discardWrapperOperand(argv)
}

func timeoutOptionConsumesValue(argument string) bool {
	return slices.Contains(
		[]string{"-k", "--kill-after", "-s", "--signal"},
		argument,
	)
}

func unwrapFlockArgv(argv []string) []string {
	for len(argv) > 0 {
		if flockOptionConsumesValue(argv[0]) && len(argv) > 1 {
			argv = argv[2:]

			continue
		}

		if strings.HasPrefix(argv[0], "-") {
			argv = argv[1:]

			continue
		}

		break
	}

	return discardWrapperOperand(argv)
}

func flockOptionConsumesValue(argument string) bool {
	return slices.Contains(
		[]string{"-E", "--conflict-exit-code", "-w", "--wait"},
		argument,
	)
}

func discardWrapperOperand(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}

	return argv[1:]
}

func nestedShellScript(argv []string) (string, bool, bool) {
	if len(argv) == 0 || !shellExecutable(argv[0]) {
		return "", false, false
	}

	pipefail := false

	for index := 1; index < len(argv); index++ {
		if argv[index] == "-o" && index+1 < len(argv) &&
			argv[index+1] == "pipefail" {
			pipefail = true
			index++

			continue
		}

		if shellCommandStringOption(argv[index]) && index+1 < len(argv) {
			return argv[index+1], pipefail, true
		}
	}

	return "", pipefail, false
}

func shellCommandStringOption(argument string) bool {
	return strings.HasPrefix(argument, "-") &&
		!strings.HasPrefix(argument, "--") &&
		strings.Contains(argument[1:], "c")
}

func shellExecutable(argument string) bool {
	return slices.Contains(
		[]string{"bash", "dash", "sh"},
		filepath.Base(argument),
	)
}

func requiredGateArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}

	name := filepath.Base(argv[0])
	arguments := argv[1:]

	if name == "make" {
		return slices.ContainsFunc(arguments, requiredMakeGateTarget)
	}

	return requiredExecutableGate(name, arguments)
}

func requiredExecutableGate(name string, arguments []string) bool {
	if isPythonCommand(name) {
		return len(arguments) > 1 && arguments[0] == "-m" &&
			arguments[1] == pytestExecutable
	}

	switch name {
	case "cargo":
		return requiredCargoGate(arguments)
	case "go":
		return requiredGoGate(arguments)
	case pytestExecutable, "ghprsq":
		return true
	case preCommitExecutable:
		return commandOperation(
			arguments,
			map[string]bool{"--color": true},
		) == "run"
	case "uv":
		return uvRunsPytest(arguments)
	case tokenGit:
		return gitArgvOperation(arguments) == gitCommitOperation
	default:
		return false
	}
}

func requiredMakeGateTarget(argument string) bool {
	switch argument {
	case "acceptance", "bench", "build", "check", "check-sync",
		gitCommitOperation,
		"go-e2e-test", "go-test", "heavy", "install", "lint",
		"maint-rust-heavy", "mutants", "nextest", preCommitExecutable,
		"purrdf-extractor-check", "reason", "reason-verify", "release",
		"rust-coverage", "rust-gate", "rust-test", testOperation, "validate",
		"wasm-parity":
		return true
	default:
		return false
	}
}

func requiredGoGate(argv []string) bool {
	return commandOperation(argv, map[string]bool{"-C": true}) ==
		testOperation
}

func requiredCargoGate(argv []string) bool {
	operation := commandOperation(argv, map[string]bool{
		"--color":         true,
		"--config":        true,
		"--manifest-path": true,
		"--target-dir":    true,
	})

	return slices.Contains(
		[]string{
			"bench", "build", "check", "clippy", "nextest", testOperation,
		},
		operation,
	)
}

func commandOperation(
	argv []string,
	optionsWithValues map[string]bool,
) string {
	for index := 0; index < len(argv); index++ {
		argument := argv[index]
		name, _, hasInlineValue := strings.Cut(argument, "=")

		if optionsWithValues[name] && !hasInlineValue {
			index++

			continue
		}

		if strings.HasPrefix(argument, "+") ||
			strings.HasPrefix(argument, "-") {
			continue
		}

		return argument
	}

	return ""
}

func uvRunsPytest(argv []string) bool {
	operation := commandOperation(argv, map[string]bool{
		"--config-file": true,
		"--directory":   true,
		"--project":     true,
	})
	if operation != "run" {
		return false
	}

	runIndex := slices.Index(argv, "run")
	if runIndex < 0 {
		return false
	}

	operation = commandOperation(argv[runIndex+1:], map[string]bool{
		"--directory": true,
		"--env-file":  true,
		"--index":     true,
		"--index-url": true,
		"--python":    true,
	})

	return filepath.Base(operation) == pytestExecutable
}

func gitArgvOperation(argv []string) string {
	for index := 0; index < len(argv); index++ {
		argument := argv[index]

		if gitGlobalOptionConsumesValue(argument) {
			index++

			continue
		}

		if strings.HasPrefix(argument, "-") {
			continue
		}

		return argument
	}

	return ""
}

func gitGlobalOptionConsumesValue(argument string) bool {
	return slices.Contains(
		[]string{
			"-c", "-C", "--git-dir", "--work-tree", "--namespace",
			"--config-env",
		},
		argument,
	)
}

func shellAssignment(value string) bool {
	name, _, found := strings.Cut(value, "=")
	if !found || name == "" {
		return false
	}

	for index, character := range name {
		if validShellNameCharacter(index, character) {
			continue
		}

		return false
	}

	return true
}

func validShellNameCharacter(index int, character rune) bool {
	return character == '_' || character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		index > 0 && character >= '0' && character <= '9'
}
