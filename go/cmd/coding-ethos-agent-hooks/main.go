// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	commandArgsOffset = 2
)

var errUnknownCommand = apperror.StaticError("unknown agent-hooks command")

func main() {
	os.Exit(runCLI(os.Args[1:]))
}

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
	default:
		usage()

		err = fmt.Errorf("%w: %s", errUnknownCommand, args[0])
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)

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

	err = agenthooks.WriteSettings(os.Stdout, defaultHookCommand(*hookCommand))
	if err != nil {
		return fmt.Errorf("write agent hook settings: %w", err)
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

	err = writeJSONReport(os.Stdout, report)
	if err != nil {
		return err
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
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(payload)
	if err != nil {
		return fmt.Errorf("encode doctor report: %w", err)
	}

	return nil
}

func usage() {
	usageTo(os.Stderr)
}

func usageTo(writer io.Writer) {
	fmt.Fprintln(
		writer,
		"Usage: coding-ethos-agent-hooks <print|sync|doctor|verify> [flags]",
	)
}
