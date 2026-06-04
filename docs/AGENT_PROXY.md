<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Agent Proxy Foundation

The Agent Proxy is planned as an opt-in runtime boundary between AI coding
agents, provider APIs, local tools, and repository files. It is not a separate
policy engine. Proxy work must use the existing coding-ethos architecture:
Go collects facts, CEL evaluates principle-owned policy, SARIF reports
actionable evidence, MCP explains decisions, and code-intel stores the ledger.

## Trust Boundary

The proxy boundary includes:

- outbound provider requests, including prompts, attachments, tool definitions,
  and model-selection metadata;
- inbound provider responses, including tool-call requests, streaming chunks,
  and assistant text;
- local tool calls and tool outputs routed through agent workflows;
- file reads, directory listings, search requests, edit proposals, patch
  outcomes, cache hits, truncation, policy injection, and remediation actions.

The proxy must treat all provider payloads, tool outputs, and agent-supplied
edit requests as untrusted data. It may inspect and transform those payloads
only through explicit, traceable policy decisions. It must not silently edit,
truncate, inject, cache, suppress, or expand data without ledger evidence.

## Operator Model

The proxy is not an invisible default. Operators must explicitly enable it and
understand the privacy and compatibility implications.

Required operator decisions:

- whether outbound provider traffic may be inspected;
- whether TLS/API interception is enabled, including local CA lifecycle and
  trust-store changes;
- which providers and local tools are routed through the proxy;
- which sandbox profile applies to local tool execution;
- which repository paths may be read, written, indexed, cached, or excluded.

TLS interception is high risk. It can expose prompt and response content to a
local process and can fail when providers change protocol behavior. It must
remain an explicit, documented operator choice and must never be introduced as
a hidden fallback.

## Baseline Pass-Through Routing

The first live Agent API proxy mode is mechanical pass-through routing. It is
owned by `coding-ethos-run agent-proxy passthrough`, forwards HTTP provider
traffic to an explicit upstream, and preserves upstream response status,
headers, and body. It does not inspect, mutate, block, cache, or retain payload
bodies.

Routing remains disabled unless both environment variables are set:

```text
CODE_ETHOS_AGENT_API_PROXY=1
CODE_ETHOS_AGENT_API_PROXY_URL=http://127.0.0.1:<port>
```

When those variables are present, `coding-ethos-run` exports `HTTP_PROXY`,
`HTTPS_PROXY`, `http_proxy`, and `https_proxy` for child agent processes. The
status command reports `agent_api_proxy` so operators can tell whether routing
is disabled, correctly enabled, or misconfigured. This baseline intentionally
does not install a CA, modify trust stores, or force HTTPS interception; those
belong to the later HTTPS adapter layer.

Pass-through routing records body-free `proxy.pass_through` evidence in the
code-intel proxy ledger: method, upstream host/scheme, status code, payload byte
count when known, and `payload_body_retained=false`. That proves routing
occurred without storing sensitive prompt or response bodies.

## Event Envelope

All proxy features must emit the same provider-neutral event envelope. The Go
contract lives in `go/internal/agentproxy/events.go` as `ProviderEvent`.

Every event should carry as much of this as the source can provide:

- event ID, session ID, trace ID, and tracking ID;
- provider, model, tool name, repository root, cwd, and target path;
- event kind, direction, payload kind, and cache key;
- input/output payload hashes and payload byte measurements;
- token counts or conservative token estimates;
- policy ID, decision, skill ID, principle IDs, MCP explanation tool, and
  policy evidence ID;
- DLP facts such as credential-like content, credential filenames, protected
  paths, ignored directories, large payloads, and binary payloads;
- ordered transform records for DLP inspection, diagnostic extraction,
  stack-trace preservation, token budgeting, pagination, compression,
  injection, truncation, and patch/remediation outcomes.

Provider adapters for OpenAI, Anthropic, Gemini, and other APIs must translate
provider-specific JSON into this envelope before policy code sees it. Policy
code must not depend on raw provider JSON.

## Provider Adapters

Provider adapters live in `go/internal/agentproxy/adapter` and implement the
`agentproxy.Adapter` interface. The interface and its normalization structs are
declared in `agentproxy`, but the concrete OpenAI, Anthropic, and Gemini
adapters live in the child package so the transport core never imports
provider-specific JSON handling. The intercept layer depends only on the
injected `agentproxy.AdapterRegistry`.

Adapters are pure and IO-free. They receive already-buffered plaintext bytes
plus a sanitized `RequestContext`/`ResponseContext` (method, host, path, content
type, status) and never see request headers, so auth tokens cannot leak into
normalization output. Detection is by host suffix plus path prefix only; bodies
are never sniffed to choose an adapter, and the registry resolves the most
specific match.

`NormalizeRequest` extracts the model, messages, and tool definitions;
`NormalizeResponse` extracts assistant messages, tool calls, and token usage.
Tool definitions and tool-call arguments are reduced to schema/argument hashes,
never raw schemas or argument JSON. `agentproxy.OutboundEvent` and
`agentproxy.InboundEvent` then build body-free `ProviderEvent`s: message content
is never copied into the event, only counts, hashes, measurements, structural
tool-call names, and token usage. This keeps the pass-through retention contract
(`payload_body_retained=false`) for intercepted traffic.

Streaming responses (`text/event-stream`) are intentionally not reconstructed at
this stage: they are marked `streaming_not_normalized` so the limitation is
explicit rather than silently misclassified. A matched path whose body fails to
parse is reported as an explicit normalization error, never reclassified as a
different provider.

## Opt-In HTTPS Interception Gate

HTTPS interception is disabled by default and fails closed. The opt-in gate
lives in `go/internal/agentproxy/ca` and mirrors the sandbox opt-in model:
explicit modes only (`off`/`required`, no implicit default), with an
`agentproxy.InterceptionEvidence` record emitted for every outcome so a disabled
or denied state is visible rather than silent.

Interception is enabled only when all of the following hold:

- `proxy.interception.mode: required` in `config.yaml`/`repo_config.yaml`;
- the `CODE_ETHOS_AGENT_PROXY_INTERCEPT=1` environment opt-in is set, so a stale
  checked-in config cannot enable interception on its own;
- when a local CA already exists and `proxy.interception.ca_approval` is set, the
  approval token matches the provisioned CA fingerprint (otherwise the gate
  fails closed with `Denied` evidence).

When enabled, the gate provisions a local ECDSA P-256 root CA under
`<repoRoot>/.coding-ethos/cache/agent-proxy-ca/` (`ca-cert.pem` mode `0644`,
`ca-key.pem` mode `0600`, plus `metadata.json` carrying the fingerprint and
validity). The CA cert path is exposed for later sandbox trust-store binding.
The host trust store is never modified. Operators inspect the gate decision with
`coding-ethos-run agent-proxy ca-status`, and the `status` command reports
`agent_api_proxy_interception`.

Leaf-certificate minting, the CONNECT TLS-MITM interception proxy, and the
sandbox trust-store binding that consume this CA are tracked separately and ship
behind this default-off gate.

## Code-Intel Ledger

The repo-local code-intel database stores proxy sessions, events, transforms,
policy evidence, DLP facts, cache keys, payload hashes, token counts, and trace
correlation. The ledger answers questions such as:

- which sessions repeatedly read the same files;
- which events exceeded token budgets or triggered truncation;
- which provider payloads carried DLP facts;
- which transforms were applied and why;
- which policy decision authorized a cache hit, policy injection, patch, or
  suppression;
- which SARIF result, trace, or MCP explanation corresponds to a proxy event.

The ledger is local-first and repository scoped. It must not index `.git`,
credential directories, protected enforcement internals, or configured secret
exclusion paths.

## Tool Output Compression

Proxy-side tool output compression lives in `go/internal/agentproxy`. The
default transform preserves the beginning and ending of long tool output,
inserts an explicit omission marker, writes the full original output to a
session-local `coding-ethos-tool-output-*.log` evidence file in the system temp
directory, and records
token/hash/path evidence through the normal transform record path. This keeps
command identity, early setup failures, and terminal stack-trace exceptions
visible while removing repetitive progress output and dependency-frame noise.
The runtime prunes stale matching evidence files from the OS temp directory
before writing a new one. The default retention is 24 hours, with an optional
byte budget, and is controlled by `outputs.prune.surfaces.proxy_temp_evidence`
in `config.toml` with repo-specific `repo_config.toml` overrides.

Agent hooks now route Bash `PostToolUse` output through this transform path
before any output is returned to the provider. The live path first parses known
compiler, linter, and test output into a compact diagnostic table, then applies
line compression and a hard token-budget transform. It stores the proxy
`tool_output` event and transform ledger in the repo-local code-intel database
when the provider payload includes a session id. Repositories can tune
`proxy.output_compression.max_lines`, `head_lines`, `tail_lines`, `max_tokens`,
`head_tokens`, `tail_tokens`, and `max_diagnostics` in `repo_config.yaml`. The
temp-evidence lifecycle is configured under `outputs.prune` in TOML. The
`CODE_ETHOS_PROXY_OUTPUT_MAX_TOKENS`,
`CODE_ETHOS_PROXY_OUTPUT_HEAD_TOKENS`, and
`CODE_ETHOS_PROXY_OUTPUT_TAIL_TOKENS` environment variables remain available
for local runtime token tuning.

Compression must remain traceable. A compressed payload should carry metadata
that records the omitted line count and temporary full-output path, and the
corresponding proxy event should store the transform record in code-intel.
Silent truncation is not allowed. The temp evidence file is debug evidence,
not durable archival storage.

## File Read Deduplication

Proxy-side file read caching is session scoped and hash validated. The
`proxy-file-read` bridge reads a repo-relative file, stores the resulting
`file_read` event in code-intel, and uses the recorded output hash as the cache
validator. When the same session asks for the same path again and the current
file hash still matches, the bridge records a `cache_hit` event with a
`file-read-cache` transform and returns a short cached-read stub instead of the
full file body.

The cache must miss whenever the file changes, the path changes, or the session
changes. A transparent proxy should reuse this path before returning read tool
output to an agent so repeated reads save tokens without hiding changed source.

## File Read Boundary

Provider-native file read tools are the supported live path for source reads.
Claude-style Bash file-tool emulation such as `cat <path>`,
`sed -n '1,20p' <path>`, `awk ... <path>`, `tee <path>`, and `echo`/`printf`
write-redirection forms are blocked before execution so policy receives
structured file targets instead of opaque shell output. That fail-closed behavior takes
precedence over older live `cat` pagination experiments.

The `code-intel proxy-file-read` bridge remains the explicit path for
session-scoped read-cache evidence. A future transparent proxy can still add
pagination or cached-read transforms at the provider file-read boundary, but it
must record `file_read` and `cache_hit` events in the provider-neutral proxy
ledger rather than inferring file reads from shell output.

## Startup Repo Map

On `SessionStart`, the hook runtime refreshes the repo-local Tree-sitter index
and injects a compact `coding_ethos_repo_map` when indexed source symbols are
available. The map ranks files by symbol/chunk signals and includes concise
symbol signatures so agents can choose focused reads before broad exploration.
The same map is available through MCP as `code_intel_repo_map` and the
`coding-ethos://code-intel/repo-map` resource.

## Directory Listing Anatomy

Directory listing enrichment uses the same transform contract. The
code-intel store builds a directory-local anatomy map from its AST index and
`EnrichDirectoryListing` appends a compact TOON block to the raw listing text.
The original listing remains intact, and the proxy pipeline returns the
transform name, hashes, token counts, and injected file count to the caller. A
live hook proxy persists that returned transform record on its proxy event with
the `proxy.directory_anatomy` policy ID. The implementation is inspired by
Aider's repo map, but it uses
coding-ethos' repo-local AST ledger instead of reparsing source during prompt
construction.

The interception-adjacent command classifier lives in `agentproxy` and
recognizes conservative single-target `ls` and `tree` invocations. The
PostToolUse Bash proxy path uses that classifier after parsing the command with
the shared shell parser, refreshes source files for the listed directory, and
emits the enriched output as AdditionalContext for successful listings. `ls`
uses direct child anatomy. `tree` uses recursive anatomy, with `tree -L N`
limiting nested files to the displayed depth. The
`code-intel enrich-listing` command is the runnable bridge for this behavior:
it accepts raw listing output, infers the target directory from `--command` when
`--path` is not supplied, refreshes source files for that listing shape, and
emits the original listing plus the anatomy block. The command does not create a
proxy event by itself.

## CEL And SARIF Contract

Proxy facts exposed to CEL use the `proxy` input object. CEL may inspect the
event kind, provider, direction, payload kind, target path, token counts,
payload size, policy decision, trace IDs, cache key, and DLP facts. CEL must
remain pure: it cannot read files, execute commands, call providers, or inspect
host state.

SARIF output must carry proxy properties when a finding originated from proxy
policy or transformation:

- `proxy_event_id`
- `proxy_session_id`
- `proxy_event_kind`
- `proxy_direction`
- `proxy_payload_kind`
- `proxy_trace_id`
- `proxy_tracking_id`
- `proxy_transform`

These properties let code scanning, MCP remediation, and code-intel queries
join a SARIF result back to the originating proxy event.

## Search/Replace Edit Enforcement

The first #62 enforcement slice protects local edit tools before a full
provider/API proxy exists:

- `Write` may create a new file, but rewriting an existing regular text file is
  blocked by `proxy.search_replace_edit`;
- `Edit` and `MultiEdit` proposals against a readable regular text file must
  provide non-empty search blocks;
- each search block is evaluated sequentially against the current proposed file
  content and must match exactly once;
- missing and non-unique search blocks are blocked before the provider tool can
  mutate the file;
- policy evidence records the target file, block index, match count, reason, and
  current content hash where available.

This slice intentionally does not claim the full proxy patch roadmap. Remaining
work includes AST affected-symbol evidence, durable proxy trace/code-intel
storage for patch outcomes, and transactional rollback around future proxy-owned
edit application.

## Feature Work Rules

Before implementing an Agent Proxy issue, confirm that the feature uses:

- the shared `ProviderEvent` envelope;
- the repo-local proxy session/event ledger;
- CEL for configurable decisions;
- SARIF and trace evidence for user-visible findings;
- MCP policy explanations for remediation guidance;
- the existing code-intel retrieval APIs instead of reparsing source in the
  proxy path.

Feature-specific event models, private ledgers, hidden truncation, ad hoc DLP
string scanners in policy code, or provider-specific policy branches are not
acceptable.
