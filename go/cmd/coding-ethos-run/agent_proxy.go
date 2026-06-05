// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/adapter"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/ca"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/proxypolicy"
	"blackcat.ca/coding-ethos/go/lintcapture"
)

const (
	agentProxyCommandPassthrough = "passthrough"
	agentProxyCommandCAStatus    = "ca-status"
	agentProxyCommandIntercept   = "intercept"
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
	errAgentProxyOnErrorInvalid = apperror.StaticError(
		"agent-proxy intercept --on-error must be fail_closed or passthrough",
	)
	errAgentProxyInterceptDisabled = apperror.StaticError(
		"agent-proxy intercept refused to start while interception is disabled",
	)
)

const (
	agentProxyOnErrorFailClosed  = "fail_closed"
	agentProxyOnErrorPassthrough = "passthrough"
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
	case agentProxyCommandCAStatus:
		return runAgentProxyCAStatus(paths, args)
	case agentProxyCommandIntercept:
		return runAgentProxyIntercept(paths, args)
	default:
		return fmt.Errorf("%w: %q", errUnknownAgentProxyCommand, command)
	}
}

func runAgentProxyCAStatus(paths runtimePaths, args []string) error {
	flags := flag.NewFlagSet("agent-proxy ca-status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse agent-proxy ca-status flags: %w", err)
	}

	mode, approval := resolveProxyInterceptionConfig(paths)

	evidence, err := ca.Evaluate(ca.GateInput{
		Now:        time.Now().UTC(),
		Mode:       mode,
		CAApproval: approval,
		RepoRoot:   paths.Root,
		EnvOptIn:   agentAPIProxyInterceptOptIn(),
	})
	if err != nil {
		return fmt.Errorf("evaluate agent-proxy interception gate: %w", err)
	}

	return writeAgentProxyCAStatus(evidence)
}

func writeAgentProxyCAStatus(evidence agentproxy.InterceptionEvidence) error {
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agent-proxy interception evidence: %w", err)
	}

	_, err = fmt.Fprintln(os.Stdout, string(encoded))
	if err != nil {
		return fmt.Errorf("write agent-proxy interception evidence: %w", err)
	}

	return nil
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

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(
		context.Background(),
		"tcp",
		strings.TrimSpace(*listen),
	)
	if err != nil {
		return fmt.Errorf("listen for pass-through agent proxy: %w", err)
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(
		os.Stderr,
		"coding-ethos agent API pass-through proxy listening on %s -> %s\n",
		listener.Addr().String(),
		*upstream,
	)

	err = proxy.ListenAndServeOnListener(ctx, listener)
	if errors.Is(err, context.Canceled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("run pass-through agent proxy: %w", err)
	}

	return nil
}

// interceptFlags holds the parsed agent-proxy intercept command-line flags. The
// allow list is merged with the repo config allow list before the proxy is
// built, and onError is empty unless the operator overrode the config policy.
type interceptFlags struct {
	listen     string
	provider   string
	onError    string
	allowHosts []string
}

// runAgentProxyIntercept starts the transparent TLS-MITM interception proxy. It
// resolves the interception gate from repo config plus the env opt-in, refuses
// to start under a disabled fail-closed policy, and otherwise serves CONNECT
// traffic, blind-tunneling everything when interception is disabled but the
// operator chose passthrough.
func runAgentProxyIntercept(paths runtimePaths, args []string) error {
	parsed, err := parseInterceptFlags(args)
	if err != nil {
		return err
	}

	config, err := lintcapture.LoadRuntimeConfig(paths.EthosRoot, paths.Root)
	if err != nil {
		return fmt.Errorf("load runtime config for agent-proxy intercept: %w", err)
	}

	onError := resolveInterceptOnError(parsed.onError, config)

	mode, approval := resolveProxyInterceptionConfig(paths)

	evidence, err := ca.Evaluate(ca.GateInput{
		Now:        time.Now().UTC(),
		Mode:       mode,
		CAApproval: approval,
		RepoRoot:   paths.Root,
		EnvOptIn:   agentAPIProxyInterceptOptIn(),
	})
	if err != nil {
		return fmt.Errorf("evaluate agent-proxy interception gate: %w", err)
	}

	runtime, err := resolveInterceptRuntime(paths.Root, evidence, onError)
	if err != nil {
		return err
	}

	allowHosts := mergeInterceptAllowHosts(
		config.ProxyInterceptionAllowHosts(),
		parsed.allowHosts,
	)

	return serveInterceptProxy(paths, serveInterceptRequest{
		flags:        parsed,
		runtime:      runtime,
		onError:      onError,
		allowHosts:   allowHosts,
		maxNormalize: config.ProxyInterceptionMaxNormalizeBytes(),
	})
}

// parseInterceptFlags parses the intercept subcommand flags, validating the
// on-error override against the supported policies.
func parseInterceptFlags(args []string) (interceptFlags, error) {
	flags := flag.NewFlagSet("agent-proxy intercept", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listen := flags.String("listen", "127.0.0.1:0", "HTTP listen address")
	allowHosts := flags.String("allow-hosts", "", "Comma-separated allow list")
	onError := flags.String("on-error", "", "Override on-error policy")
	provider := flags.String("provider", "", "Provider label for route evidence")

	err := flags.Parse(args)
	if err != nil {
		return interceptFlags{}, fmt.Errorf("parse agent-proxy intercept flags: %w", err)
	}

	resolvedOnError := strings.TrimSpace(*onError)
	if !validInterceptOnError(resolvedOnError) {
		return interceptFlags{}, fmt.Errorf("%w: %q", errAgentProxyOnErrorInvalid, *onError)
	}

	return interceptFlags{
		listen:     strings.TrimSpace(*listen),
		provider:   *provider,
		onError:    resolvedOnError,
		allowHosts: splitInterceptAllowHosts(*allowHosts),
	}, nil
}

// validInterceptOnError reports whether an operator-supplied on-error override
// is empty (use config) or one of the supported policies.
func validInterceptOnError(value string) bool {
	switch value {
	case "", agentProxyOnErrorFailClosed, agentProxyOnErrorPassthrough:
		return true
	default:
		return false
	}
}

// resolveInterceptOnError prefers an operator override, falling back to the
// repo config on-error policy when no flag was supplied.
func resolveInterceptOnError(
	flagValue string,
	config lintcapture.RuntimeConfig,
) string {
	if flagValue != "" {
		return flagValue
	}

	return config.ProxyInterceptionOnError()
}

// interceptRuntime captures the resolved interception capability for a run. A
// disabled runtime carries no issuer and blind-tunnels every host; an enabled
// runtime always carries the leaf issuer the proxy needs to terminate TLS.
type interceptRuntime struct {
	issuer  agentproxy.LeafIssuer
	enabled bool
}

// resolveInterceptRuntime decides whether this run intercepts traffic. A gate
// that is enabled mints a leaf issuer; a disabled gate (or a missing CA when
// enabled) blind-tunnels under passthrough but refuses to start under
// fail_closed.
func resolveInterceptRuntime(
	repoRoot string,
	evidence agentproxy.InterceptionEvidence,
	onError string,
) (interceptRuntime, error) {
	if !evidence.Enabled {
		if onError == agentProxyOnErrorFailClosed {
			return interceptRuntime{}, fmt.Errorf(
				"%w: %s",
				errAgentProxyInterceptDisabled,
				evidence.Reason,
			)
		}

		return interceptRuntime{enabled: false}, nil
	}

	issuer, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{})
	if err != nil {
		if onError == agentProxyOnErrorFailClosed {
			return interceptRuntime{}, fmt.Errorf("create agent-proxy leaf issuer: %w", err)
		}

		return interceptRuntime{enabled: false}, nil
	}

	return interceptRuntime{issuer: issuer, enabled: true}, nil
}

// serveInterceptRequest bundles the resolved inputs needed to build and serve
// the interception proxy.
type serveInterceptRequest struct {
	flags        interceptFlags
	runtime      interceptRuntime
	onError      string
	allowHosts   []string
	maxNormalize int64
}

// serveInterceptProxy opens the evidence store, builds the interception proxy,
// binds the listener, and serves CONNECT traffic until interrupted. When
// interception is enabled it loads the compiled policy bundle and builds the
// outbound evaluator, refusing to start if either step fails so enforcement is
// never silently absent.
func serveInterceptProxy(paths runtimePaths, request serveInterceptRequest) error {
	store, err := codeintel.Open(
		context.Background(),
		codeintel.DefaultDBPath(paths.Root),
	)
	if err != nil {
		return fmt.Errorf("open code-intel store for agent proxy evidence: %w", err)
	}
	defer store.Close()

	options := agentproxy.InterceptOptions{
		Recorder:     store,
		Registry:     adapter.DefaultRegistry(),
		Issuer:       request.runtime.issuer,
		Provider:     request.flags.provider,
		RepoRoot:     paths.Root,
		OnError:      request.onError,
		AllowHosts:   request.allowHosts,
		MaxNormalize: request.maxNormalize,
		Enabled:      request.runtime.enabled,
	}

	// A blind-tunnel-only run decrypts nothing, so it needs no evaluator. An
	// enabled run must build one or refuse to start so enforcement is never
	// silently absent.
	options, err = attachInterceptEvaluator(options, paths, request.runtime.enabled)
	if err != nil {
		return err
	}

	proxy, err := agentproxy.NewInterceptProxy(options)
	if err != nil {
		return fmt.Errorf("create interception agent proxy: %w", err)
	}

	return runInterceptListener(proxy, request.flags.listen)
}

// attachInterceptEvaluator builds and attaches the outbound policy evaluator to
// options when interception is enabled, leaving a blind-tunnel-only run's
// options untouched. A bundle-load or compile failure aborts the run.
func attachInterceptEvaluator(
	options agentproxy.InterceptOptions,
	paths runtimePaths,
	enabled bool,
) (agentproxy.InterceptOptions, error) {
	if !enabled {
		return options, nil
	}

	evaluator, err := interceptEvaluator(paths)
	if err != nil {
		return agentproxy.InterceptOptions{}, err
	}

	options.Evaluator = evaluator

	return options, nil
}

// interceptEvaluator loads the compiled policy bundle and builds the outbound
// proxy policy evaluator. A bundle-load or compile failure aborts the run so
// enforcement never starts in a degraded state.
func interceptEvaluator(paths runtimePaths) (*proxypolicy.Evaluator, error) {
	bundle, err := loadInterceptPolicyBundle(paths)
	if err != nil {
		return nil, err
	}

	evaluator, err := proxypolicy.New(bundle)
	if err != nil {
		return nil, fmt.Errorf("build proxy policy evaluator: %w", err)
	}

	return evaluator, nil
}

// loadInterceptPolicyBundle opens and decodes the compiled policy bundle the
// interception proxy enforces, mirroring the hook runtime's bundle resolution.
func loadInterceptPolicyBundle(paths runtimePaths) (policy.Bundle, error) {
	bundleFile, err := os.Open(hookPolicyBundlePath(paths))
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open policy bundle for agent proxy: %w", err)
	}
	defer bundleFile.Close()

	bundle, err := policy.DecodeBundle(bundleFile)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("decode policy bundle for agent proxy: %w", err)
	}

	return bundle, nil
}

// runInterceptListener binds the listener, prints the bound address, and serves
// CONNECT traffic until the interrupt-aware context is canceled.
func runInterceptListener(proxy *agentproxy.InterceptProxy, listen string) error {
	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(context.Background(), "tcp", listen)
	if err != nil {
		return fmt.Errorf("listen for interception agent proxy: %w", err)
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(
		os.Stderr,
		"coding-ethos agent API interception proxy listening on %s\n",
		listener.Addr().String(),
	)

	err = proxy.ListenAndServeOnListener(ctx, listener)
	if errors.Is(err, context.Canceled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("run interception agent proxy: %w", err)
	}

	return nil
}

// splitInterceptAllowHosts splits a comma-separated allow list into trimmed,
// lowercased host entries, discarding blanks.
func splitInterceptAllowHosts(value string) []string {
	hosts := []string{}

	for raw := range strings.SplitSeq(value, ",") {
		host := strings.ToLower(strings.TrimSpace(raw))
		if host != "" {
			hosts = append(hosts, host)
		}
	}

	return hosts
}

// mergeInterceptAllowHosts unions the config allow list with the flag allow
// list, preserving order and discarding duplicates.
func mergeInterceptAllowHosts(configHosts, flagHosts []string) []string {
	merged := make([]string, 0, len(configHosts)+len(flagHosts))
	seen := map[string]struct{}{}

	for _, host := range append(append([]string(nil), configHosts...), flagHosts...) {
		if _, ok := seen[host]; ok {
			continue
		}

		seen[host] = struct{}{}
		merged = append(merged, host)
	}

	if len(merged) == 0 {
		return nil
	}

	return merged
}
