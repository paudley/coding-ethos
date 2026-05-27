// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookcli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	blockedExitCode          = hooks.AgentHookBlockedExitCode
	proxyLedgerLockWait      = 20 * time.Second
	proxyLedgerRetryInterval = 100 * time.Millisecond
)

var (
	errBundleRequired = apperror.StaticError("--bundle is required")
	errInvalidBundle  = apperror.StaticError("invalid policy bundle")
)

type codeIntelStoreOpener func(
	context.Context,
	string,
) (*codeintel.Store, error)

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
		if *jsonOutput && result.Provider != "" {
			return 0
		}

		if !*jsonOutput {
			printBlocked(stderr, result)
		}

		return blockedExitCode
	}

	return 0
}

func persistHookResult(event hooks.Event, result hooks.Result) error {
	err := hooks.WriteAgentHookTraceFromEnv(event, result)
	if err != nil {
		return fmt.Errorf("write agent hook trace: %w", err)
	}

	return writeProxyEvents(result, codeintel.Open)
}

func writeProxyEvents(result hooks.Result, openStore codeIntelStoreOpener) error {
	eventsByRoot := map[string][]agentproxy.ProviderEvent{}

	for _, event := range result.ProxyEvents {
		if event.RepoRoot == "" {
			continue
		}

		eventsByRoot[event.RepoRoot] = append(eventsByRoot[event.RepoRoot], event)
	}

	for root, events := range eventsByRoot {
		err := writeProxyEventsForRoot(root, events, openStore)
		if err != nil {
			return err
		}
	}

	return nil
}

func writeProxyEventsForRoot(
	root string,
	events []agentproxy.ProviderEvent,
	openStore codeIntelStoreOpener,
) error {
	deadline := time.Now().Add(proxyLedgerLockWait)

	for {
		err := tryWriteProxyEventsForRoot(root, events, openStore)
		if err == nil {
			autoPruneCodeIntelDB(root)

			return nil
		}

		if !codeintel.IsStoreLockError(err) || time.Now().After(deadline) {
			return err
		}

		time.Sleep(proxyLedgerRetryInterval)
	}
}

func tryWriteProxyEventsForRoot(
	root string,
	events []agentproxy.ProviderEvent,
	openStore codeIntelStoreOpener,
) error {
	store, err := openStore(
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

	return nil
}

func autoPruneCodeIntelDB(root string) {
	err := outputsurface.AutoPruneCodeIntelDB(context.Background(), root)
	if err == nil {
		return
	}

	debuglog.Debug(
		"hookcli.code_intel.auto_prune.warn",
		zap.String("root", root),
		zap.Error(err),
	)
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

	feedback.Emit(writer, feedback.Text{Text: advice}, feedback.FormatTOON)
}

func printErr(writer io.Writer, err error) {
	feedback.Emit(writer, feedback.Error{Message: err.Error()}, feedback.FormatTOON)
}
