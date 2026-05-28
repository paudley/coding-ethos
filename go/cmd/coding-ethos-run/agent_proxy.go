// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

const (
	agentProxyCommandPassthrough = "passthrough"
)

var (
	errAgentProxyCommandRequired = apperror.StaticError(
		"agent-proxy command is required",
	)
	errAgentProxyUpstreamRequired = apperror.StaticError(
		"agent-proxy passthrough --upstream is required",
	)
	errUnknownAgentProxyCommand = apperror.StaticError(
		"unknown agent-proxy command",
	)
)

func runAgentProxyHandler(paths runtimePaths, rest []string) error {
	if len(rest) == 0 {
		return errAgentProxyCommandRequired
	}

	command := rest[0]
	args := rest[1:]

	switch command {
	case agentProxyCommandPassthrough:
		return runAgentProxyPassthrough(paths, args)
	default:
		return fmt.Errorf("%w: %q", errUnknownAgentProxyCommand, command)
	}
}

func runAgentProxyPassthrough(paths runtimePaths, args []string) error {
	flags := flag.NewFlagSet("agent-proxy passthrough", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listen := flags.String("listen", "127.0.0.1:0", "HTTP listen address")
	upstream := flags.String("upstream", "", "Upstream provider base URL")
	provider := flags.String("provider", "", "Provider label for route evidence")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse agent-proxy passthrough flags: %w", err)
	}

	if strings.TrimSpace(*upstream) == "" {
		return errAgentProxyUpstreamRequired
	}

	store, err := codeintel.Open(
		context.Background(),
		codeintel.DefaultDBPath(paths.Root),
	)
	if err != nil {
		return fmt.Errorf("open code-intel store for agent proxy evidence: %w", err)
	}
	defer store.Close()

	proxy, err := agentproxy.NewPassThroughProxy(agentproxy.PassThroughOptions{
		Recorder: store,
		Upstream: *upstream,
		Provider: *provider,
		RepoRoot: paths.Root,
	})
	if err != nil {
		return fmt.Errorf("create pass-through agent proxy: %w", err)
	}

	fmt.Fprintf(
		os.Stderr,
		"coding-ethos agent API pass-through proxy listening on %s -> %s\n",
		*listen,
		*upstream,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err = proxy.ListenAndServe(ctx, *listen)
	if errors.Is(err, context.Canceled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("run pass-through agent proxy: %w", err)
	}

	return nil
}
