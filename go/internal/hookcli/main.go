// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookcli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const blockedExitCode = 2

var (
	errBundleRequired = apperror.StaticError("--bundle is required")
	errInvalidBundle  = apperror.StaticError("invalid policy bundle")
)

func runWithIO(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("coding-ethos-hook", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	jsonOutput := flags.Bool("json", false, "Emit JSON result to stdout")

	err := flags.Parse(args)
	if err != nil {
		printErr(stderr, err)

		return 1
	}

	if *bundlePath == "" {
		printErr(stderr, errBundleRequired)

		return 1
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		printErr(stderr, err)

		return 1
	}

	err = bundle.Validate()
	if err != nil {
		printErr(
			stderr,
			fmt.Errorf("%w:\n%s", errInvalidBundle, policy.FormatValidationError(err)),
		)

		return 1
	}

	event, err := hooks.DecodeEvent(stdin)
	if err != nil {
		printErr(stderr, err)

		return 1
	}

	startedAt := time.Now()

	result, err := hooks.Run(bundle, hooks.Options{Event: event})
	if err != nil {
		printErr(stderr, err)

		return 1
	}

	result.RuntimeMS = time.Since(startedAt).Milliseconds()

	err = persistHookResult(event, result)
	if err != nil {
		printErr(stderr, err)

		return 1
	}

	if *jsonOutput {
		err = hooks.EncodeResult(stdout, result)
		if err != nil {
			printErr(stderr, err)

			return 1
		}
	}

	if result.Blocked() {
		printBlocked(stderr, result)

		return blockedExitCode
	}

	return 0
}

func persistHookResult(event hooks.Event, result hooks.Result) error {
	err := hooks.WriteAgentHookTraceFromEnv(event, result)
	if err != nil {
		return fmt.Errorf("write agent hook trace: %w", err)
	}

	return writeProxyEvents(result)
}

func writeProxyEvents(result hooks.Result) error {
	eventsByRoot := map[string][]agentproxy.ProviderEvent{}

	for _, event := range result.ProxyEvents {
		if event.RepoRoot == "" {
			continue
		}

		eventsByRoot[event.RepoRoot] = append(eventsByRoot[event.RepoRoot], event)
	}

	for root, events := range eventsByRoot {
		store, err := codeintel.Open(
			context.Background(),
			codeintel.DefaultDBPath(root),
		)
		if err != nil {
			return fmt.Errorf("open proxy output ledger: %w", err)
		}

		err = recordProxyEvents(store, events)
		closeErr := store.Close()

		if err != nil {
			return err
		}

		if closeErr != nil {
			return fmt.Errorf("close proxy output ledger: %w", closeErr)
		}
	}

	return nil
}

func recordProxyEvents(
	store *codeintel.Store,
	events []agentproxy.ProviderEvent,
) error {
	for _, event := range events {
		err := store.RecordProxyEvent(context.Background(), event)
		if err != nil {
			return fmt.Errorf("record proxy output event: %w", err)
		}
	}

	return nil
}

func readBundle(path string) (policy.Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("decode bundle: %w", err)
	}

	return bundle, nil
}

func printBlocked(writer io.Writer, result hooks.Result) {
	advice := hooks.BlockedAdvice(result)
	if result.Provider != "" {
		advice = hooks.ProviderBlockMessage(result)
	}

	if advice == "" {
		return
	}

	fmt.Fprintln(writer, advice)
}

func printErr(writer io.Writer, err error) {
	fmt.Fprintf(writer, "%s\n", err)
}
