// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package webguidancecli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/webguidance"
)

const outputFormatTOON = "toon"

var (
	errCommandRequired     = apperror.StaticError("web-guidance command is required")
	errUnknownCommand      = apperror.StaticError("unknown web-guidance command")
	errUnsupportedFormat   = apperror.StaticError("unsupported web-guidance output format")
	errSearchQueryRequired = apperror.StaticError("web-guidance search query is required")
	errRetrieveIDsRequired = apperror.StaticError("web-guidance retrieve id is required")
)

type command func(context.Context, []string) error

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printUsage()

		return errCommandRequired
	}

	handler, ok := commandHandlers()[args[0]]
	if !ok {
		return fmt.Errorf("%w: %s", errUnknownCommand, args[0])
	}

	return handler(ctx, args[1:])
}

func commandHandlers() map[string]command {
	return map[string]command{
		"list":     list,
		"retrieve": retrieve,
		"search":   search,
	}
}

func list(ctx context.Context, args []string) error {
	options, err := parseOptions("list", args)
	if err != nil {
		return err
	}

	response, err := webguidance.Adapter{Root: options.root}.List(
		ctx,
		webguidance.ListInput{Refresh: options.refresh},
	)
	if err != nil {
		return fmt.Errorf("list modern web guidance: %w", err)
	}

	return writeResponse(options.format, response)
}

func search(ctx context.Context, args []string) error {
	options, err := parseOptions("search", args)
	if err != nil {
		return err
	}

	query := strings.TrimSpace(strings.Join(options.rest, " "))
	if query == "" {
		return errSearchQueryRequired
	}

	response, err := webguidance.Adapter{Root: options.root}.Search(
		ctx,
		webguidance.SearchInput{
			Query:         query,
			Limit:         options.limit,
			BrowserPolicy: options.browserPolicy,
			Refresh:       options.refresh,
		},
	)
	if err != nil {
		return fmt.Errorf("search modern web guidance: %w", err)
	}

	return writeResponse(options.format, response)
}

func retrieve(ctx context.Context, args []string) error {
	options, err := parseOptions("retrieve", args)
	if err != nil {
		return err
	}

	if len(options.rest) == 0 {
		return errRetrieveIDsRequired
	}

	response, err := webguidance.Adapter{Root: options.root}.Retrieve(
		ctx,
		webguidance.RetrieveInput{
			IDs:           options.rest,
			BrowserPolicy: options.browserPolicy,
			Refresh:       options.refresh,
		},
	)
	if err != nil {
		return fmt.Errorf("retrieve modern web guidance: %w", err)
	}

	return writeResponse(options.format, response)
}

type options struct {
	root          string
	format        string
	browserPolicy string
	rest          []string
	limit         int
	refresh       bool
}

func parseOptions(name string, args []string) (options, error) {
	flags := flag.NewFlagSet(name, flag.ExitOnError)
	root := flags.String(
		"root",
		".",
		"Repository root containing coding-ethos Modern Web Guidance cache and config",
	)
	format := flags.String(
		"format",
		outputFormatTOON,
		"Output format, one of toon, human, or json",
	)
	browserPolicy := flags.String(
		"browser-policy",
		"",
		"Browser-support policy context to include in guidance provenance",
	)
	limit := flags.Int("limit", 0, "Maximum search results to return")
	refresh := flags.Bool(
		"refresh",
		false,
		"Force a fresh upstream lookup when network is allowed",
	)

	err := flags.Parse(normalizeFlagOrder(args))
	if err != nil {
		return options{}, fmt.Errorf("parse web-guidance %s flags: %w", name, err)
	}

	return options{
		root:          *root,
		format:        strings.TrimSpace(*format),
		browserPolicy: strings.TrimSpace(*browserPolicy),
		limit:         *limit,
		refresh:       *refresh,
		rest:          flags.Args(),
	}, nil
}

func normalizeFlagOrder(args []string) []string {
	flagArgs := []string{}
	positionals := []string{}

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--":
			positionals = append(positionals, args[index:]...)

			return append(flagArgs, positionals...)
		case arg == "--refresh":
			flagArgs = append(flagArgs, arg)
		case strings.HasPrefix(arg, "--refresh="):
			flagArgs = append(flagArgs, arg)
		case valueFlag(arg):
			flagArgs = append(flagArgs, arg)
			if index+1 < len(args) {
				flagArgs = append(flagArgs, args[index+1])
				index++
			}
		case valueFlagWithEquals(arg):
			flagArgs = append(flagArgs, arg)
		default:
			positionals = append(positionals, arg)
		}
	}

	return append(flagArgs, positionals...)
}

func valueFlag(arg string) bool {
	switch arg {
	case "--root", "--format", "--browser-policy", "--limit":
		return true
	default:
		return false
	}
}

func valueFlagWithEquals(arg string) bool {
	for _, prefix := range []string{
		"--root=",
		"--format=",
		"--browser-policy=",
		"--limit=",
	} {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}

	return false
}

func writeResponse(format string, response webguidance.Response) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", outputFormatTOON:
		err := feedback.WriteRendered(
			os.Stdout,
			webguidance.FormatTOON(response),
			feedback.FormatTOON,
		)
		if err != nil {
			return fmt.Errorf("write modern web guidance TOON response: %w", err)
		}

		return nil
	case feedback.FormatHuman:
		err := feedback.WriteRendered(
			os.Stdout,
			webguidance.FormatHuman(response),
			feedback.FormatHuman,
		)
		if err != nil {
			return fmt.Errorf("write modern web guidance human response: %w", err)
		}

		return nil
	case feedback.FormatJSON:
		err := feedback.WriteJSON(os.Stdout, response)
		if err != nil {
			return fmt.Errorf("write modern web guidance JSON response: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %q", errUnsupportedFormat, format)
	}
}

func printUsage() {
	feedback.Emit(
		os.Stderr,
		feedback.Text{Text: strings.Join([]string{
			"Usage: coding-ethos-run web-guidance <command> [options]",
			"Commands:",
			"  list                 show available Modern Web Guidance guides",
			"  search <query>       search Modern Web Guidance",
			"  retrieve <id>        retrieve one or more guides",
		}, "\n")},
		feedback.FormatTOON,
	)
}
