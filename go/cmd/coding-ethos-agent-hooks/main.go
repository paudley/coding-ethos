// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
)

const (
	commandArgIndex   = 1
	commandArgsOffset = 2
)

var errUnknownCommand = errors.New("unknown agent-hooks command")

func main() {
	if len(os.Args) < commandArgsOffset {
		usage()
		os.Exit(commandArgsOffset)
	}

	var err error

	switch os.Args[commandArgIndex] {
	case "print":
		err = printSettings(os.Args[commandArgsOffset:])
	case "sync":
		err = syncSettings(os.Args[commandArgsOffset:])
	case "doctor":
		err = doctorSettings(os.Args[commandArgsOffset:])
	default:
		usage()

		err = fmt.Errorf("%w: %s", errUnknownCommand, os.Args[commandArgIndex])
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func printSettings(args []string) error {
	flags := flag.NewFlagSet("print", flag.ExitOnError)
	hookCommand := flags.String("hook-command", "", "Agent hook command")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse print flags: %w", err)
	}

	err = agenthooks.WriteSettings(os.Stdout, *hookCommand)
	if err != nil {
		return fmt.Errorf("write agent hook settings: %w", err)
	}

	return nil
}

func syncSettings(args []string) error {
	flags := flag.NewFlagSet("sync", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root for agent settings")
	hookCommand := flags.String("hook-command", "", "Agent hook command")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse sync flags: %w", err)
	}

	err = agenthooks.SyncSettings(*root, *hookCommand)
	if err != nil {
		return fmt.Errorf("sync agent hook settings: %w", err)
	}

	return nil
}

func doctorSettings(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root for agent settings")
	hookCommand := flags.String("hook-command", "", "Agent hook command")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse doctor flags: %w", err)
	}

	err = agenthooks.DoctorSettings(*root, *hookCommand)
	if err != nil {
		return fmt.Errorf("doctor agent hook settings: %w", err)
	}

	fmt.Fprintln(os.Stdout, "agent hook settings valid")

	return nil
}

func usage() {
	fmt.Fprintln(
		os.Stderr,
		"Usage: coding-ethos-agent-hooks <print|sync|doctor> [flags]",
	)
}
