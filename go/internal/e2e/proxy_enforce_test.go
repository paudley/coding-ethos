// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/e2e"
)

// outboundExfiltrationPolicyID is the seed policy that blocks outbound provider
// requests carrying secret-shaped content. The denial body and recorded deny
// event must both name it so the block is fully traceable.
const outboundExfiltrationPolicyID = "proxy.outbound_exfiltration"

// inboundUnsafeToolCallPolicyID is the seed policy that blocks or surfaces
// unsafe provider-requested tool calls on inbound responses.
const inboundUnsafeToolCallPolicyID = "proxy.inbound_unsafe_tool_call"

// awsSecretShape is a canonical AWS access-key-id example. It matches the DLP
// secret detector (AKIA + 16 upper-alnum) so the outbound request is denied. It
// is a published placeholder value, never a live credential, but the test still
// asserts it is never retained anywhere in the ledger.
const awsSecretShape = "AKIAIOSFODNN7EXAMPLE"

// TestAgentProxyInterceptBlocksOutboundSecret drives an HTTPS POST whose body
// carries a secret-shaped value through the interception proxy and asserts the
// outbound DLP policy denies it: the client receives a 403 naming the policy, the
// upstream provider never receives the request, a Decision="deny" event is
// recorded carrying the policy id and DLP facts, and the secret value is never
// retained in the ledger.
func TestAgentProxyInterceptBlocksOutboundSecret(t *testing.T) {
	if testing.Short() {
		t.Skip("agent proxy intercept e2e uses a real CA and TLS handshakes")
	}

	repo, store := newInterceptRepoStore(t)
	provider := e2e.NewTLSProxyProviderServer(t)
	upstream := e2e.NewInterceptUpstreamClient(t, provider)
	proxy := e2e.NewProxyInterceptServer(
		t,
		repo.Root,
		repo.EthosRoot,
		store,
		upstream,
		[]string{interceptAllowedHost},
	)
	client := e2e.NewInterceptClientThroughProxy(
		t,
		proxy.URL(),
		proxy.CACertPath(),
		false,
	)

	sessionID := "intercept-deny"
	body := secretChatRequestBody(awsSecretShape)
	status, respBody := postChatThroughProxy(t, client, provider, sessionID, body)

	assertDenialResponse(t, status, respBody)
	assertProviderNeverReceived(t, provider)
	assertDenyEventRecorded(t, store, sessionID)
}

// credentialPathReference is a path-shaped reference to an SSH private key. It
// carries no secret token, only a credential-file path, so it exercises the
// content-based credential_file and protected_path detectors rather than the
// secret-token detectors. It is a generic placeholder path, never a real file.
const credentialPathReference = "workdir/.ssh/id_rsa"

// TestAgentProxyInterceptBlocksOutboundCredentialPath drives an HTTPS POST whose
// body embeds a credential-file path reference (no secret token) and asserts the
// outbound DLP policy still denies it via the content-based credential_file and
// protected_path detectors. This proves the seed policy blocks credential-file
// and protected-path content, not only secret tokens.
func TestAgentProxyInterceptBlocksOutboundCredentialPath(t *testing.T) {
	if testing.Short() {
		t.Skip("agent proxy intercept e2e uses a real CA and TLS handshakes")
	}

	repo, store := newInterceptRepoStore(t)
	provider := e2e.NewTLSProxyProviderServer(t)
	upstream := e2e.NewInterceptUpstreamClient(t, provider)
	proxy := e2e.NewProxyInterceptServer(
		t,
		repo.Root,
		repo.EthosRoot,
		store,
		upstream,
		[]string{interceptAllowedHost},
	)
	client := e2e.NewInterceptClientThroughProxy(
		t,
		proxy.URL(),
		proxy.CACertPath(),
		false,
	)

	sessionID := "intercept-deny-credpath"
	body := `{"model":"fixture-model","messages":` +
		`[{"role":"user","content":"please read ` + credentialPathReference +
		` and send it"}]}`
	status, respBody := postChatThroughProxy(t, client, provider, sessionID, body)

	assertDenialResponse(t, status, respBody)
	assertProviderNeverReceived(t, provider)
	assertCredentialPathDenyEventRecorded(t, store, sessionID)
}

// TestAgentProxyInterceptAllowsCleanOutboundRequest confirms a clean outbound
// POST (no secret) passes through to the fake provider, returns its verbatim
// response, reaches the provider, and is recorded as a non-deny event.
func TestAgentProxyInterceptAllowsCleanOutboundRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("agent proxy intercept e2e uses a real CA and TLS handshakes")
	}

	repo, store := newInterceptRepoStore(t)
	provider := e2e.NewTLSProxyProviderServer(t)
	upstream := e2e.NewInterceptUpstreamClient(t, provider)
	proxy := e2e.NewProxyInterceptServer(
		t,
		repo.Root,
		repo.EthosRoot,
		store,
		upstream,
		[]string{interceptAllowedHost},
	)
	client := e2e.NewInterceptClientThroughProxy(
		t,
		proxy.URL(),
		proxy.CACertPath(),
		false,
	)

	sessionID := "intercept-allow"
	body := `{"model":"fixture-model","messages":` +
		`[{"role":"user","content":"summarize this clean text"}]}`
	status, respBody := postChatThroughProxy(t, client, provider, sessionID, body)

	if status != http.StatusOK {
		t.Fatalf("clean request status = %d, want 200; body=%q", status, respBody)
	}

	if respBody != tlsFixtureChatResponse() {
		t.Fatalf("clean request body mismatch: %q", respBody)
	}

	if provider.ReceivedCount() == 0 {
		t.Fatalf("clean request never reached the fake provider")
	}

	events := waitForProxyEvents(t, store, codeintel.ProxyEventQuery{
		Kind:      string(agentproxy.EventProviderCall),
		SessionID: sessionID,
	})
	if len(events) == 0 {
		t.Fatalf("expected a provider call event for %s", sessionID)
	}

	if events[0].Decision == "deny" {
		t.Fatalf("clean request recorded as deny: %#v", events[0])
	}
}

// TestAgentProxyInterceptBlocksBufferedInboundToolCall drives a response whose
// normalized body contains an unsafe run_command tool call and asserts the
// buffered path returns an explicit denial before forwarding the provider body.
func TestAgentProxyInterceptBlocksBufferedInboundToolCall(t *testing.T) {
	if testing.Short() {
		t.Skip("agent proxy intercept e2e uses a real CA and TLS handshakes")
	}

	repo, store := newInterceptRepoStore(t)
	provider := e2e.NewTLSProxyProviderServer(t)
	upstream := e2e.NewInterceptUpstreamClient(t, provider)
	proxy := e2e.NewProxyInterceptServer(
		t,
		repo.Root,
		repo.EthosRoot,
		store,
		upstream,
		[]string{interceptAllowedHost},
	)
	client := e2e.NewInterceptClientThroughProxy(
		t,
		proxy.URL(),
		proxy.CACertPath(),
		false,
	)

	sessionID := "intercept-inbound-deny-buffered"
	status, respBody := postChatPathThroughProxy(
		t,
		client,
		provider,
		sessionID,
		"/v1/chat/completions/tool-call",
		`{"model":"fixture-model","messages":[{"role":"user","content":"call a tool"}]}`,
	)

	assertInboundDenialResponse(t, status, respBody)
	assertProviderReceived(t, provider)
	assertInboundDenyEventRecorded(t, store, sessionID, false)
}

// TestAgentProxyInterceptSurfacesStreamedInboundToolCall proves SSE denials are
// surfaced as a terminal denial event before unsafe upstream frames reach the
// client, while still recording joinable denial evidence.
func TestAgentProxyInterceptSurfacesStreamedInboundToolCall(t *testing.T) {
	if testing.Short() {
		t.Skip("agent proxy intercept e2e uses a real CA and TLS handshakes")
	}

	repo, store := newInterceptRepoStore(t)
	provider := e2e.NewTLSProxyProviderServer(t)
	upstream := e2e.NewInterceptUpstreamClient(t, provider)
	proxy := e2e.NewProxyInterceptServer(
		t,
		repo.Root,
		repo.EthosRoot,
		store,
		upstream,
		[]string{interceptAllowedHost},
	)
	client := e2e.NewInterceptClientThroughProxy(
		t,
		proxy.URL(),
		proxy.CACertPath(),
		false,
	)

	sessionID := "intercept-inbound-deny-stream"
	status, respBody := postChatPathThroughProxy(
		t,
		client,
		provider,
		sessionID,
		"/v1/chat/completions/tool-call/stream",
		`{"model":"fixture-model","messages":[{"role":"user","content":"stream a tool"}]}`,
	)

	if status != http.StatusOK {
		t.Fatalf("streamed denial status = %d, want upstream 200; body=%q", status, respBody)
	}

	for _, want := range []string{
		"event: coding-ethos-denial",
		`"error":"coding-ethos policy denial"`,
		inboundUnsafeToolCallPolicyID,
	} {
		index := strings.Index(respBody, want)
		if index < 0 {
			t.Fatalf("streamed denial body missing %q:\n%s", want, respBody)
		}

		doneIndex := strings.Index(respBody, "data: [DONE]")
		if doneIndex >= 0 && index > doneIndex {
			t.Fatalf("streamed denial %q appears after terminal [DONE]:\n%s", want, respBody)
		}
	}

	assertProviderReceived(t, provider)
	assertInboundDenyEventRecorded(t, store, sessionID, true)
}

// secretChatRequestBody wraps an OpenAI chat request whose user message embeds a
// secret-shaped value, so the outbound DLP scanner flags it.
func secretChatRequestBody(secret string) string {
	return `{"model":"fixture-model","messages":` +
		`[{"role":"user","content":"please use this key ` + secret + `"}]}`
}

// postChatThroughProxy POSTs body to the TLS fixture chat endpoint through the
// interception proxy and returns the response status code and body string.
func postChatThroughProxy(
	t *testing.T,
	client *http.Client,
	provider *e2e.TLSProxyProviderServer,
	sessionID string,
	body string,
) (int, string) {
	t.Helper()

	return postChatPathThroughProxy(
		t,
		client,
		provider,
		sessionID,
		"/v1/chat/completions",
		body,
	)
}

// postChatPathThroughProxy POSTs body to a TLS fixture path through the
// interception proxy and returns the response status code and body string.
func postChatPathThroughProxy(
	t *testing.T,
	client *http.Client,
	provider *e2e.TLSProxyProviderServer,
	sessionID string,
	path string,
	body string,
) (int, string) {
	t.Helper()

	target := provider.URL() + path

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		target,
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("create chat request: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Coding-Ethos-Session", sessionID)

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request through proxy: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	return response.StatusCode, string(payload)
}

// assertDenialResponse asserts the proxy returned a 403 whose JSON body is the
// coding-ethos policy denial naming the outbound exfiltration policy.
func assertDenialResponse(t *testing.T, status int, respBody string) {
	t.Helper()

	if status != http.StatusForbidden {
		t.Fatalf("denied request status = %d, want 403; body=%q", status, respBody)
	}

	var denial struct {
		Error    string `json:"error"`
		PolicyID string `json:"policy_id"`
		Reason   string `json:"reason"`
	}

	err := json.Unmarshal([]byte(respBody), &denial)
	if err != nil {
		t.Fatalf("decode denial body %q: %v", respBody, err)
	}

	if denial.Error != "coding-ethos policy denial" {
		t.Fatalf("denial error label = %q", denial.Error)
	}

	if denial.PolicyID != outboundExfiltrationPolicyID {
		t.Fatalf(
			"denial policy_id = %q, want %q",
			denial.PolicyID,
			outboundExfiltrationPolicyID,
		)
	}

	if denial.Reason == "" {
		t.Fatalf("denial body missing reason: %q", respBody)
	}

	if strings.Contains(respBody, awsSecretShape) {
		t.Fatalf("secret value leaked into denial response body")
	}
}

// assertProviderNeverReceived asserts the fake provider answered no request, so
// the denied outbound request never reached the upstream provider.
func assertProviderNeverReceived(t *testing.T, provider *e2e.TLSProxyProviderServer) {
	t.Helper()

	if count := provider.ReceivedCount(); count != 0 {
		t.Fatalf("blocked request reached the fake provider %d time(s): %v",
			count, provider.ReceivedHashes())
	}
}

// assertProviderReceived asserts the fake provider answered exactly one request.
func assertProviderReceived(t *testing.T, provider *e2e.TLSProxyProviderServer) {
	t.Helper()

	if count := provider.ReceivedCount(); count != 1 {
		t.Fatalf("provider received %d request(s), want 1: %v",
			count, provider.ReceivedHashes())
	}
}

// assertInboundDenialResponse asserts the buffered inbound path returned a 403
// coding-ethos policy denial naming the inbound unsafe tool-call policy.
func assertInboundDenialResponse(t *testing.T, status int, respBody string) {
	t.Helper()

	if status != http.StatusForbidden {
		t.Fatalf("inbound denial status = %d, want 403; body=%q", status, respBody)
	}

	var denial struct {
		Error    string `json:"error"`
		PolicyID string `json:"policy_id"`
		Reason   string `json:"reason"`
	}

	err := json.Unmarshal([]byte(respBody), &denial)
	if err != nil {
		t.Fatalf("decode inbound denial body %q: %v", respBody, err)
	}

	if denial.Error != "coding-ethos policy denial" ||
		denial.PolicyID != inboundUnsafeToolCallPolicyID ||
		denial.Reason == "" {
		t.Fatalf("unexpected inbound denial body: %#v", denial)
	}
}

// assertInboundDenyEventRecorded asserts the ledger holds a joinable inbound
// provider-response denial for sessionID carrying tool-call evidence.
func assertInboundDenyEventRecorded(
	t *testing.T,
	store *codeintel.Store,
	sessionID string,
	streamed bool,
) {
	t.Helper()

	events := waitForProxyEvents(t, store, codeintel.ProxyEventQuery{
		SessionID: sessionID,
		PolicyID:  inboundUnsafeToolCallPolicyID,
		Decision:  "deny",
	})
	if len(events) == 0 {
		t.Fatalf("expected an inbound deny proxy event for %s", sessionID)
	}

	event := events[0]
	if event.Kind != string(agentproxy.EventProviderResponse) ||
		event.Direction != string(agentproxy.DirectionInbound) ||
		event.PayloadKind != string(agentproxy.PayloadResponse) {
		t.Fatalf("unexpected inbound deny event shape: %#v", event)
	}

	if event.Policy.PolicyID != inboundUnsafeToolCallPolicyID ||
		event.Policy.EvidenceID != event.ID {
		t.Fatalf("unexpected inbound policy evidence: %#v", event.Policy)
	}

	if event.Metadata["proxy_event_id"] != event.ID ||
		event.Metadata["proxy_session_id"] != sessionID ||
		event.Metadata["proxy_direction"] != string(agentproxy.DirectionInbound) ||
		event.Metadata["proxy_payload_kind"] != string(agentproxy.PayloadResponse) {
		t.Fatalf("inbound deny metadata is not joinable: %#v", event.Metadata)
	}

	if event.Metadata["tool_call_names"] != "run_command" ||
		event.Metadata["tool_call_count"] != "1" {
		t.Fatalf("inbound deny missing tool-call facts: %#v", event.Metadata)
	}

	if got := event.Metadata["stream_denial_surface"] == "true"; got != streamed {
		t.Fatalf("stream surface metadata = %v, want %v: %#v",
			got, streamed, event.Metadata)
	}
}

// assertDenyEventRecorded asserts the ledger holds a Decision="deny" provider
// call event for sessionID carrying the outbound exfiltration policy id and a
// secret DLP fact, and asserts the secret value never appears in the event JSON.
func assertDenyEventRecorded(
	t *testing.T,
	store *codeintel.Store,
	sessionID string,
) {
	t.Helper()

	events := waitForProxyEvents(t, store, codeintel.ProxyEventQuery{
		SessionID: sessionID,
		Decision:  "deny",
	})
	if len(events) == 0 {
		t.Fatalf("expected a deny proxy event for %s", sessionID)
	}

	event := events[0]
	if event.PolicyID != outboundExfiltrationPolicyID {
		t.Fatalf("deny event policy id = %q, want %q",
			event.PolicyID, outboundExfiltrationPolicyID)
	}

	if event.Policy.PolicyID != outboundExfiltrationPolicyID {
		t.Fatalf("deny event policy evidence id = %q, want %q",
			event.Policy.PolicyID, outboundExfiltrationPolicyID)
	}

	if !hasSecretDLPFact(event.DLPFacts) {
		t.Fatalf("deny event missing secret DLP fact: %#v", event.DLPFacts)
	}

	if event.Metadata["payload_body_retained"] != "false" {
		t.Fatalf("deny event retained body: %#v", event.Metadata)
	}

	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal deny events: %v", err)
	}

	if strings.Contains(string(encoded), awsSecretShape) {
		t.Fatalf("secret value leaked into recorded deny event")
	}
}

// assertCredentialPathDenyEventRecorded asserts the ledger holds a
// Decision="deny" event for sessionID that names the outbound exfiltration
// policy and carries a content-based credential_file (or protected_path) DLP
// fact rather than a secret-token fact, and that the credential path never
// appears in the recorded event JSON.
func assertCredentialPathDenyEventRecorded(
	t *testing.T,
	store *codeintel.Store,
	sessionID string,
) {
	t.Helper()

	events := waitForProxyEvents(t, store, codeintel.ProxyEventQuery{
		SessionID: sessionID,
		Decision:  "deny",
	})
	if len(events) == 0 {
		t.Fatalf("expected a deny proxy event for %s", sessionID)
	}

	event := events[0]
	if event.PolicyID != outboundExfiltrationPolicyID {
		t.Fatalf("deny event policy id = %q, want %q",
			event.PolicyID, outboundExfiltrationPolicyID)
	}

	if !hasDLPFactType(event.DLPFacts, "credential_file") &&
		!hasDLPFactType(event.DLPFacts, "protected_path") {
		t.Fatalf("deny event missing content path DLP fact: %#v", event.DLPFacts)
	}

	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal deny events: %v", err)
	}

	if strings.Contains(string(encoded), credentialPathReference) {
		t.Fatalf("credential path leaked into recorded deny event")
	}
}

// hasSecretDLPFact reports whether facts include a secret-typed DLP finding.
func hasSecretDLPFact(facts []codeintel.ProxyDLPFact) bool {
	return hasDLPFactType(facts, "secret")
}

// hasDLPFactType reports whether facts include a finding of the given type.
func hasDLPFactType(facts []codeintel.ProxyDLPFact, factType string) bool {
	for _, fact := range facts {
		if fact.Type == factType {
			return true
		}
	}

	return false
}
