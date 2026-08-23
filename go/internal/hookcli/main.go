// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookcli

import (
	"context"
	"encoding/json"
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

const blockedExitCode = hooks.AgentHookBlockedExitCode

var (
	errBundleRequired  = apperror.StaticError("--bundle is required")
	errInvalidBundle   = apperror.StaticError("invalid policy bundle")
	errInvalidContract = apperror.StaticError(
		"--contract must be neutral-v1",
	)
)

type codeIntelStoreOpener func(
	context.Context,
	string,
) (*codeintel.Store, error)

func runWithIO(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("coding-ethos-hook", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	jsonOutput := flags.Bool("json", false, "Emit JSON result to stdout")
	contract := flags.String(
		"contract",
		"",
		"Provider-neutral hook contract (neutral-v1)",
	)
	provider := flags.String(
		"provider",
		"",
		"Provider override for native hook adapters",
	)

	err := flags.Parse(args)
	if err != nil {
		printErr(stderr, err)

		return 1
	}

	if *bundlePath == "" {
		printErr(stderr, errBundleRequired)

		return 1
	}

	selectedContract, err := resolveHookContract(*contract)
	if err != nil {
		printErr(stderr, err)

		return 1
	}

	bundle, err := loadValidatedBundle(*bundlePath)
	if err != nil {
		printErr(stderr, err)

		return 1
	}

	event, err := hooks.DecodeEvent(stdin)
	if err != nil {
		printErr(stderr, err)

		return 1
	}

	if *provider != "" {
		event.ProviderHint = *provider
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

	return emitHookResult(
		stdout,
		stderr,
		result,
		*jsonOutput,
		selectedContract,
	)
}

func resolveHookContract(contract string) (string, error) {
	selected := contract
	if selected == "" {
		selected = os.Getenv("CODE_ETHOS_HOOK_CONTRACT")
	}

	if selected != "" && selected != hooks.HookContractV1Selector {
		return "", errInvalidContract
	}

	return selected, nil
}

func emitHookResult(
	stdout io.Writer,
	stderr io.Writer,
	result hooks.Result,
	jsonOutput bool,
	selectedContract string,
) int {
	if jsonOutput || selectedContract != "" {
		err := encodeHookResult(stdout, result, selectedContract)
		if err != nil {
			printErr(stderr, err)

			return 1
		}
	}

	if !result.Blocked() {
		return 0
	}

	if selectedContract == hooks.HookContractV1Selector {
		return hooks.AgentHookBlockedExitCode
	}

	if !jsonOutput || result.Provider == "kimi" {
		printBlocked(stderr, result)
	}

	return hooks.AgentHookBlockedExitCodeForProvider(result.Provider)
}

func encodeHookResult(
	writer io.Writer,
	result hooks.Result,
	selectedContract string,
) error {
	if selectedContract == hooks.HookContractV1Selector {
		err := hooks.EncodeNeutralHookResultV1(writer, result)
		if err != nil {
			return fmt.Errorf("encode neutral hook result: %w", err)
		}

		return nil
	}

	err := hooks.EncodeResult(writer, result)
	if err != nil {
		return fmt.Errorf("encode provider hook result: %w", err)
	}

	return nil
}

func loadValidatedBundle(path string) (policy.Bundle, error) {
	bundle, err := readBundle(path)
	if err != nil {
		return policy.Bundle{}, err
	}

	err = bundle.Validate()
	if err != nil {
		return policy.Bundle{}, fmt.Errorf(
			"%w:\n%s",
			errInvalidBundle,
			policy.FormatValidationError(err),
		)
	}

	return bundle, nil
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
	err := appendProxyEventLog(root, events)
	if err != nil {
		return err
	}

	err = tryWriteProxyEventsForRoot(root, events, openStore)
	if err == nil {
		autoPruneCodeIntelDB(root)

		return nil
	}

	if codeintel.IsStoreLockError(err) {
		return nil
	}

	return err
}

func appendProxyEventLog(root string, events []agentproxy.ProviderEvent) error {
	records := make([]codeintel.EventRecord, 0, len(events))
	for _, event := range events {
		payload, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("encode proxy output event: %w", err)
		}

		records = append(records, codeintel.EventRecord{
			Kind:        "proxy_event",
			SourceRunID: event.SessionID,
			TraceID:     event.TraceID,
			Provider:    event.Provider,
			Tool:        event.Tool,
			PolicyID:    event.PolicyID,
			Path:        event.TargetPath,
			Payload:     payload,
		})
	}

	err := codeintel.NewEventLog(
		codeintel.DefaultEventLogDir(codeintel.ResolveStateRoot(root)),
	).AppendStream(proxyEventLogStreamID(events), records)
	if err != nil {
		return fmt.Errorf("append proxy output event log: %w", err)
	}

	return nil
}

func proxyEventLogStreamID(events []agentproxy.ProviderEvent) string {
	for _, event := range events {
		if event.SessionID != "" {
			return event.SessionID
		}

		if event.ID != "" {
			return event.ID
		}
	}

	return "proxy-event"
}

func tryWriteProxyEventsForRoot(
	root string,
	events []agentproxy.ProviderEvent,
	openStore codeIntelStoreOpener,
) error {
	store, err := openStore(
		context.Background(),
		codeintel.DefaultDBPath(codeintel.ResolveStateRoot(root)),
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
