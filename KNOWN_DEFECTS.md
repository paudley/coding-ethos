<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Known Defects

This file tracks accepted defects that are intentionally present in the
repository. It is not a suppression list or a place to normalize broken
behavior. Each entry must include the scope, owner, rationale, replacement plan,
and removal condition.

## End-To-End Test Fakes And Mocks

End-to-end tests must exercise real workflows: real commands, real managed
tools, real Git repositories, real filesystem state, real MCP framing, and real
trace/SARIF output. AI calls are the default allowed exception because live LLM
behavior is nondeterministic and externally controlled.

No mock, fake executable, fake service, or synthetic replacement may be added to
the end-to-end suite unless all of the following are true:

- An admin explicitly approves the exception before it is added.
- The test documents the exception next to the scenario with an extensive
  rationale explaining why no real alternative is safe or practical.
- The documentation states exactly what real behavior is being replaced.
- The documentation states what risk remains uncovered by the fake.
- This file receives a matching entry with an owner, replacement plan, and
  removal condition.

### Current Entries

#### Agent Proxy Fixture Provider

- Scope: `go/internal/e2e/proxy_harness.go`
- Owner: Blackcat Informatics maintainers
- Rationale: Live AI provider calls are nondeterministic, externally billed, and
  cannot be made reliable enough for a blocking end-to-end test. The fixture
  provider replaces only the remote model endpoint; the scenario still uses a
  real temporary Git repository, real local files, real HTTP framing, real
  provider-envelope structs, and the real code-intel DuckDB ledger.
- Real behavior replaced: remote provider request/response behavior for a
  single deterministic assistant response.
- Remaining risk: provider-specific authentication, streaming semantics, rate
  limits, and vendor error bodies are not exercised by this fixture.
- Replacement plan: add provider-specific contract tests when the Agent Proxy
  supports opt-in live-provider validation with explicit credentials and
  operator approval.
- Removal condition: remove the fixture once live-provider contract tests can
  run deterministically without external cost or credential exposure.

#### Agent Proxy TLS Fixture Provider

- Scope: `go/internal/e2e/proxy_harness.go` (the TLS fake provider used by the
  CONNECT TLS-MITM interception end-to-end test).
- Owner: Blackcat Informatics maintainers
- Rationale: Live AI provider TLS endpoints are nondeterministic, externally
  billed, and cannot back a blocking TLS-MITM end-to-end test that asserts
  byte-identical forwarding and body-free recording. This fixture is
  operator-approved. It replaces only the remote provider TLS endpoint; the
  scenario still uses a real temporary Git repository, a real local CA, real
  per-host leaf minting, real TLS handshakes, real HTTP framing, and the real
  code-intel DuckDB ledger.
- Real behavior replaced: the remote provider's TLS endpoint serving a
  deterministic OpenAI-shaped chat completion and a deterministic Server-Sent
  Events stream.
- Remaining risk: real vendor authentication, streaming reconstruction, rate
  limits, HTTP/2-specific provider behaviors, and vendor error bodies are not
  exercised by this fixture.
- Replacement plan: add provider-specific contract tests when the Agent Proxy
  supports opt-in live-provider validation with explicit credentials and
  operator approval.
- Removal condition: remove the fixture once live-provider contract tests can
  run deterministically without external cost or credential exposure.
