// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthookscli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/feedback"
)

const (
	commandArgsOffset = 2
)

var (
	errProviderMatrixDrift = apperror.StaticError(
		"provider capability matrix out of sync",
	)
	errUnknownCommand = apperror.StaticError("unknown agent-hooks command")
)

func runCLI(args []string) int {
	if len(args) == 0 {
		usage()

		return commandArgsOffset
	}

	var err error

	switch args[0] {
	case "print":
		err = printSettings(args[1:])
	case "sync":
		err = syncSettings(args[1:])
	case "doctor":
		err = doctorSettings(args[1:])
	case "verify":
		err = verifySettings(args[1:])
	case "sync-provider-matrix":
		err = syncProviderMatrix(args[1:])
	case "check-provider-matrix":
		err = checkProviderMatrix(args[1:])
	default:
		usage()

		err = fmt.Errorf("%w: %s", errUnknownCommand, args[0])
	}

	if err != nil {
		feedback.Emit(
			os.Stderr,
			feedback.Error{Message: err.Error()},
			feedback.FormatTOON,
		)

		return 1
	}

	return 0
}

func printSettings(args []string) error {
	flags := flag.NewFlagSet("print", flag.ContinueOnError)
	hookCommand := flags.String("hook-command", "", "Agent hook command")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse print flags: %w", err)
	}

	var buffer bytes.Buffer

	err = agenthooks.WriteSettings(&buffer, defaultHookCommand(*hookCommand))
	if err != nil {
		return fmt.Errorf("write agent hook settings: %w", err)
	}

	err = feedback.WriteRendered(
		os.Stdout,
		strings.TrimSuffix(buffer.String(), "\n"),
		feedback.FormatJSON,
	)
	if err != nil {
		return fmt.Errorf("emit agent hook settings: %w", err)
	}

	return nil
}

func syncSettings(args []string) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	root := flags.String("root", ".", "Repository root for agent settings")
	hookCommand := flags.String("hook-command", "", "Agent hook command")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse sync flags: %w", err)
	}

	err = agenthooks.SyncSettings(*root, defaultHookCommand(*hookCommand))
	if err != nil {
		return fmt.Errorf("sync agent hook settings: %w", err)
	}

	err = agenthooks.SyncCodexTrustState(*root, defaultHookCommand(*hookCommand), "")
	if err != nil {
		return fmt.Errorf("sync Codex hook trust: %w", err)
	}

	return nil
}

func doctorSettings(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	root := flags.String("root", ".", "Repository root for agent settings")
	hookCommand := flags.String("hook-command", "", "Agent hook command")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse doctor flags: %w", err)
	}

	err = agenthooks.DoctorSettings(*root, defaultHookCommand(*hookCommand))
	if err != nil {
		return fmt.Errorf("doctor agent hook settings: %w", err)
	}

	err = agenthooks.VerifyCodexTrustState(*root, defaultHookCommand(*hookCommand), "")
	if err != nil {
		return fmt.Errorf("doctor Codex hook trust: %w", err)
	}

	err = writeDoctorReport(os.Stdout)
	if err != nil {
		return err
	}

	return nil
}

func verifySettings(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	root := flags.String("root", ".", "Repository root for agent settings")
	hookCommand := flags.String("hook-command", "", "Agent hook command")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse verify flags: %w", err)
	}

	report, err := agenthooks.VerifySettings(*root, defaultHookCommand(*hookCommand))
	if err != nil {
		encodeErr := writeJSONReport(os.Stdout, report)
		if encodeErr != nil {
			return encodeErr
		}

		return fmt.Errorf("verify agent hook settings: %w", err)
	}

	err = agenthooks.VerifyCodexTrustState(*root, defaultHookCommand(*hookCommand), "")
	if err != nil {
		report.Status = "invalid"
		report.Checks = append(report.Checks, agenthooks.VerifyCheck{
			Provider: "codex",
			Event:    "hook-trust",
			Tool:     "config.toml",
			Status:   "fail",
			Detail:   err.Error(),
		})

		encodeErr := writeJSONReport(os.Stdout, report)
		if encodeErr != nil {
			return encodeErr
		}

		return fmt.Errorf("verify Codex hook trust: %w", err)
	}

	report.Checks = append(report.Checks, agenthooks.VerifyCheck{
		Provider: "codex",
		Event:    "hook-trust",
		Tool:     "config.toml",
		Status:   "pass",
	})

	err = writeJSONReport(os.Stdout, report)
	if err != nil {
		return err
	}

	return nil
}

func syncProviderMatrix(args []string) error {
	flags := flag.NewFlagSet("sync-provider-matrix", flag.ContinueOnError)
	root := flags.String("root", ".", "Repository root for generated docs")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse sync-provider-matrix flags: %w", err)
	}

	_, err = agenthooks.SyncProviderCapabilityMatrix(*root)
	if err != nil {
		return fmt.Errorf("sync provider capability matrix: %w", err)
	}

	return nil
}

func checkProviderMatrix(args []string) error {
	flags := flag.NewFlagSet("check-provider-matrix", flag.ContinueOnError)
	root := flags.String("root", ".", "Repository root for generated docs")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse check-provider-matrix flags: %w", err)
	}

	mismatched, err := agenthooks.CheckProviderCapabilityMatrix(*root)
	if err != nil {
		return fmt.Errorf("check provider capability matrix: %w", err)
	}

	if len(mismatched) != 0 {
		return fmt.Errorf(
			"%w: %s",
			errProviderMatrixDrift,
			strings.Join(mismatched, ", "),
		)
	}

	return nil
}

func defaultHookCommand(hookCommand string) string {
	if strings.TrimSpace(hookCommand) != "" {
		return hookCommand
	}

	runner := strings.TrimSpace(os.Getenv("CODING_ETHOS_RUN_GO_HOOK"))
	if runner == "" {
		return ""
	}

	return runner + " agent-hook"
}

func writeDoctorReport(file *os.File) error {
	payload := map[string]any{
		"status":       "valid",
		"capabilities": agenthooks.ProviderCapabilities(),
	}

	return writeJSONReport(file, payload)
}

func writeJSONReport(file *os.File, payload any) error {
	err := feedback.WriteJSON(file, payload)
	if err != nil {
		return fmt.Errorf("encode doctor report: %w", err)
	}

	return nil
}

func usage() {
	usageTo(os.Stderr)
}

func usageTo(writer io.Writer) {
	feedback.Emit(
		writer,
		feedback.Text{
			Text: "Usage: coding-ethos-agent-hooks " +
				"<print|sync|doctor|verify|sync-provider-matrix|" +
				"check-provider-matrix> [flags]",
		},
		feedback.FormatTOON,
	)
}
