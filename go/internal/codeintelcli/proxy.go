// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func recordProxyEvent(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("record-proxy-event", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	eventID := flags.String("event-id", "", "Proxy event ID")
	sessionID := flags.String("session-id", "", "Proxy session ID")
	kind := flags.String("kind", "", "Proxy event kind")
	provider := flags.String("provider", "", "Agent provider")
	tool := flags.String("tool", "", "Tool name")
	model := flags.String("model", "", "Model name")
	traceID := flags.String("trace-id", "", "Trace ID")
	trackingID := flags.String("tracking-id", "", "Tracking ID")
	direction := flags.String("direction", "", "Proxy event direction")
	payloadKind := flags.String("payload-kind", "", "Proxy payload kind")
	targetPath := flags.String("target-path", "", "Target path")
	cacheKey := flags.String("cache-key", "", "Cache key")
	inputHash := flags.String("input-hash", "", "Input payload hash")
	outputHash := flags.String("output-hash", "", "Output payload hash")
	payloadBytes := flags.Int("payload-bytes", 0, "Payload byte count")
	policyID := flags.String("policy-id", "", "Policy ID")
	decision := flags.String("decision", "", "Policy decision")
	inputTokens := flags.Int("input-tokens", 0, "Input token count")
	outputTokens := flags.Int("output-tokens", 0, "Output token count")

	err := parseCommandFlags(flags, args, "record-proxy-event")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	event := agentproxy.ProviderEvent{
		ID:            strings.TrimSpace(*eventID),
		SessionID:     strings.TrimSpace(*sessionID),
		Kind:          agentproxy.EventKind(strings.TrimSpace(*kind)),
		Provider:      strings.TrimSpace(*provider),
		Tool:          strings.TrimSpace(*tool),
		Model:         strings.TrimSpace(*model),
		TraceID:       strings.TrimSpace(*traceID),
		TrackingID:    strings.TrimSpace(*trackingID),
		Direction:     agentproxy.EventDirection(strings.TrimSpace(*direction)),
		PayloadKind:   agentproxy.PayloadKind(strings.TrimSpace(*payloadKind)),
		RecordedAtUTC: time.Now().UTC(),
		RepoRoot:      *storeFlags.root,
		TargetPath:    strings.TrimSpace(*targetPath),
		CacheKey:      strings.TrimSpace(*cacheKey),
		InputHash:     strings.TrimSpace(*inputHash),
		OutputHash:    strings.TrimSpace(*outputHash),
		PolicyID:      strings.TrimSpace(*policyID),
		Decision:      strings.TrimSpace(*decision),
		Policy: agentproxy.PolicyEvidence{
			PolicyID: strings.TrimSpace(*policyID),
			Decision: strings.TrimSpace(*decision),
		},
		TokenUsage: agentproxy.TokenUsage{
			InputTokens:  *inputTokens,
			OutputTokens: *outputTokens,
			TotalTokens:  *inputTokens + *outputTokens,
		},
		Payload: agentproxy.PayloadMeasurement{Bytes: *payloadBytes},
	}

	err = appendProxyEvent(*storeFlags.root, event)
	if err != nil {
		return err
	}

	err = store.RecordProxyEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("record proxy event: %w", err)
	}

	return encodeJSON(os.Stdout, event)
}

func appendProxyEvent(root string, event agentproxy.ProviderEvent) error {
	payload, err := rawEventPayload(event)
	if err != nil {
		return err
	}

	return appendCLIEvent(root, "proxy-event", codeintel.EventRecord{
		Kind:     "proxy_event",
		TraceID:  event.TraceID,
		Provider: event.Provider,
		Tool:     event.Tool,
		PolicyID: event.PolicyID,
		Path:     event.TargetPath,
		Payload:  payload,
	})
}

func printProxySessions(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("proxy-sessions", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	provider := flags.String("provider", "", "Filter by provider")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"proxy-sessions",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.ProxySessions(ctx, codeintel.ProxySessionQuery{
				Provider: *provider,
				Limit:    *limit,
			})
		},
	)
}

func printProxyEvents(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("proxy-events", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	sessionID := flags.String("session-id", "", "Filter by session ID")
	kind := flags.String("kind", "", "Filter by event kind")
	provider := flags.String("provider", "", "Filter by provider")
	policyID := flags.String("policy-id", "", "Filter by policy ID")
	decision := flags.String("decision", "", "Filter by decision")
	targetPath := flags.String("target-path", "", "Filter by target path")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"proxy-events",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.ProxyEvents(ctx, codeintel.ProxyEventQuery{
				SessionID:  *sessionID,
				Kind:       *kind,
				Provider:   *provider,
				PolicyID:   *policyID,
				Decision:   *decision,
				TargetPath: *targetPath,
				Limit:      *limit,
			})
		},
	)
}

func proxyFileRead(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("proxy-file-read", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	eventID := flags.String("event-id", "", "Proxy event ID")
	sessionID := flags.String("session-id", "", "Proxy session ID")
	provider := flags.String("provider", "", "Agent provider")
	tool := flags.String("tool", "", "Tool name")
	model := flags.String("model", "", "Model name")
	traceID := flags.String("trace-id", "", "Trace ID")
	trackingID := flags.String("tracking-id", "", "Tracking ID")
	targetPath := flags.String("path", "", "File path to read through the proxy cache")

	err := parseCommandFlags(flags, args, "proxy-file-read")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	result, err := store.ReadFileWithCache(ctx, codeintel.FileReadCacheRequest{
		EventID:    strings.TrimSpace(*eventID),
		SessionID:  strings.TrimSpace(*sessionID),
		Provider:   strings.TrimSpace(*provider),
		Tool:       strings.TrimSpace(*tool),
		Model:      strings.TrimSpace(*model),
		TraceID:    strings.TrimSpace(*traceID),
		TrackingID: strings.TrimSpace(*trackingID),
		RepoRoot:   *storeFlags.root,
		Cwd:        *storeFlags.root,
		TargetPath: strings.TrimSpace(*targetPath),
	})
	if err != nil {
		return fmt.Errorf("proxy file read: %w", err)
	}

	return encodeJSON(os.Stdout, result)
}
