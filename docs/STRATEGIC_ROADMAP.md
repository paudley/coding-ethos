<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Strategic Roadmap

`coding-ethos` is a defensive guardrail and quality gatekeeper between AI
coding agents and a repository. It turns an ETHOS contract into runnable
policy, managed toolchains, agent hooks, Git hooks, MCP tools, SARIF output,
runtime sandbox evidence, code-intelligence storage, and compact remediation
advice.

This roadmap describes the intended direction for the next year and the work
the project intentionally does not plan to do. It is updated as major platform
work lands so it can serve as both public roadmap and OpenSSF Best Practices
evidence.

## Current Platform Baseline

The current supported platform includes:

- generated agent surfaces for Codex, Claude Code, Gemini CLI, and generic
  agent docs;
- Git and agent hook enforcement backed by a compiled Go runtime;
- first-class CEL policy expressions grounded in `coding_ethos.yml` principles;
- AST/CEL/SARIF architecture where Go collects facts, CEL decides over facts,
  SARIF reports findings, and code-intel stores evidence;
- managed static-analysis capture for local hooks and CI;
- stdio MCP tools for policy checks, edit checks, lint advice, SARIF
  remediation, tool capabilities, skills, and code-intelligence retrieval;
- generated GitHub Actions and GitLab SARIF CI gates;
- repo-level CodeQL, OSV-Scanner, Zizmor, Scorecard, actionlint, release
  attestations, SBOMs, and package validation;
- runtime sandbox support for managed tool execution with Bubblewrap/cgroup
  evidence where available;
- repo-local SQLite code-intelligence storage for hook traces, lint traces,
  SARIF, remediation outcomes, hook analytics, Tree-sitter chunks, AST links,
  FTS5 search, and sqlite-vec derived embedding rows;
- CLA Assistant contribution certification, CODEOWNERS, governance,
  continuity, security policy, issue templates, and release documentation.

Detailed architecture is documented in:

- [AST_CEL_SARIF_ARCHITECTURE.md](AST_CEL_SARIF_ARCHITECTURE.md)
- [CODE_INTEL.md](CODE_INTEL.md)
- [MCP_SERVER.md](MCP_SERVER.md)
- [CI_CD_SARIF.md](CI_CD_SARIF.md)
- [RUNTIME_SANDBOXING.md](RUNTIME_SANDBOXING.md)
- [TRUST_SIGNALS.md](TRUST_SIGNALS.md)

## Next-Year Priorities

### 1. Agent-First Remediation Loops

Agents should repair policy failures through structured guidance instead of
rerunning broad shell commands and guessing from terminal output.

Planned work:

- add provider-backed `remediation_advice` MCP support behind a constrained,
  read-only, advice-only adapter;
- measure remediation outcomes from stored hook/lint/SARIF traces;
- improve repeated-failure analysis so noisy or unclear policies are visible;
- add more provider-native output tests for Codex, Claude Code, Gemini CLI, and
  generic MCP clients;
- use code-intel history to rank the most relevant prior fixes before an agent
  edits a file.

Out of scope:

- allowing MCP remediation tools to edit files directly;
- giving advice providers raw shell, raw Git, broad repository write access, or
  network access by default;
- treating provider-generated advice as policy truth.

### 2. AST-Backed Policy Expansion

Source-aware policy should use the shared Tree-sitter fact path before adding
new ad hoc scanners.

Planned work:

- port high-value `pyqa_lint` guidance into Go fact collectors plus
  principle-owned CEL policies;
- add richer Tree-sitter facts for function/class complexity, docstring
  structure, return/yield shape, import topology, and config-file entries;
- add CEL helper functions over AST facts while keeping CEL pure and
  host-independent;
- emit symbol-level SARIF regions, related locations, and code flows where the
  underlying AST evidence supports them;
- add regression tests proving AST facts are identical across hook, lint, CLI,
  MCP, and CI/SARIF paths.

Out of scope:

- language-specific policy paths that bypass the shared fact collector;
- parsing files from inside CEL expressions;
- probabilistic or embedding-based enforcement decisions.

### 3. Code-Intelligence Storage And Retrieval

The local code-intelligence database should become the agent memory layer for
policy, code, and remediation evidence.

Planned work:

- harden sqlite-vec hybrid search and embedding metadata workflows;
- add scheduled or explicit indexing commands suitable for CI and local
  worktrees;
- improve MCP result expansion with nearby symbols, graph edges, linked SARIF,
  related remediation history, and validation commands;
- add stale-index detection and safe rebuild workflows;
- add privacy and secret-exclusion tests for index inputs.

Out of scope:

- hosted vector databases as a required runtime dependency;
- indexing `.git`, credential directories, secrets, or protected enforcement
  internals;
- replacing exact policy checks with vector search.

### 4. Runtime Sandboxing And Capability Enforcement

CEL is the control plane. Runtime sandboxing is the data-plane boundary for
managed tools and future constrained advice providers.

Planned work:

- improve Bubblewrap profile coverage for common linters and formatters;
- document and test repo-specific read/write allow-lists for consumer
  workspaces;
- add more sandbox evidence to SARIF and code-intel analytics;
- evaluate high-isolation backends such as gVisor, eBPF observability, and
  seccomp profile generation for CI/server-side use cases.

Out of scope:

- `LD_PRELOAD` as a security boundary;
- best-effort sandbox claims without trace evidence;
- silently degrading required CI sandbox mode to advisory behavior.

### 5. Supply-Chain, Governance, And Trust Signals

The project should keep improving public trust signals while staying honest
about what is repo-local and what depends on external services.

Planned work:

- complete OpenSSF Best Practices Silver and Gold criteria where applicable;
- keep Scorecard, CodeQL, OSV-Scanner, Zizmor, Dependabot, fuzz smoke,
  attestations, SBOMs, and pinned Actions current;
- require CLA Assistant once its PR status check is visible and stable;
- keep governance, continuity, security, release, and contribution docs current;
- expand fuzz coverage beyond smoke tests for shell parsing, SARIF, CEL inputs,
  hook payloads, and code-intel ingestion.

Out of scope:

- claiming OpenSSF Best Practices tiers before the public API reports them;
- treating badges as substitutes for tests, review, and release evidence;
- requiring contributors to accept unnecessary employment-style obligations.

### 6. Centralized ETHOS Registry And Inheritance

Organizations need baseline guardrails with local refinement.

Planned work:

- support inherited ETHOS and policy presets from local files, pinned GitHub
  sources, or enterprise registries;
- record inherited source hashes and provenance in generated artifacts and
  traces;
- allow local repos to override context without copying the full baseline;
- reject unpinned remote policy sources unless explicitly allowed.

Out of scope:

- mutable remote policy inputs without hashes or provenance;
- hidden organization policy that cannot be audited from generated outputs;
- repo-local overrides that weaken critical policy without explicit review.

### 7. IDE And Editor Integration

Git hooks catch bad work at the gate. Editor integration should catch it
earlier.

Planned work:

- prototype a VS Code/Cursor extension that consumes `coding-ethos` policy and
  SARIF output;
- surface ETHOS, skill, and MCP remediation links next to diagnostics;
- support advisory edit-time checks before considering blocking editor flows.

Out of scope:

- editor-only enforcement that disagrees with hook/CI policy;
- duplicating policy logic in TypeScript when the compiled Go runtime can
  provide the answer.

### 8. Localization Readiness

The project is currently English-first because its target audience, ETHOS
contract, generated agent instructions, remediation advice, contribution
process, and security process are maintained in English. Localization is not a
current release commitment, but it should be approached deliberately if the
project starts serving non-English contributor communities.

Planned work:

- keep runtime UI strings distinguishable from authored ETHOS policy content;
- add message IDs for stable CLI, hook, MCP, and validation UI strings before
  adding any second language;
- evaluate `gettext`/`.po` for Python and a Go message catalog such as
  `go-i18n` for compiled runtime text;
- add tests that enumerate message IDs and verify catalog completeness for
  supported locales.

Out of scope:

- ad hoc translation of ETHOS principles, generated skills, policy advice, or
  remediation prose without treating those translations as reviewed policy
  content;
- locale-sensitive behavior that changes enforcement decisions;
- committing to a translated UI before there is demonstrated user demand.

### 9. Agent Proxy And Context-Economy Controls

Open issues #52 through #62 define an Agent Proxy direction: move selected
agent/provider/tool traffic through a policy-aware mediation layer so
`coding-ethos` can reduce token waste, prevent data leakage, and intervene
before unsafe tool instructions reach local execution.

This is a major platform extension, not a small hook feature. The proxy must
reuse the same evidence architecture as the rest of the project:

- Go normalizes facts and provider/tool events;
- CEL evaluates deterministic policy over those facts;
- SARIF and traces record decisions, locations, and remediation metadata;
- code-intel stores session history, AST anatomy, search indexes, and
  remediation outcomes;
- MCP explains policies and offers focused follow-up context.

The foundation contract and operator threat model are documented in
[AGENT_PROXY.md](AGENT_PROXY.md). Future proxy issues should extend that
contract instead of adding feature-local event models or ledgers.

Planned foundation work:

- define a provider-agnostic proxy event envelope for prompts, responses, tool
  calls, file reads, directory listings, edits, search requests, and tool
  outputs (initial contract in place);
- add a repo-local session ledger for payload hashes, token estimates, file
  read cache state, policy injections, output transforms, and edit outcomes;
- add protocol adapters for OpenAI, Anthropic, and Gemini payload schemas
  behind narrow interfaces;
- add tokenizer and content-transform interfaces for token budgets, stack
  trace preservation, semantic pagination, and output compression;
- add compact code-intel retrieval APIs for AST anatomy maps, repo maps,
  semantic chunks, symbol summaries, hybrid search, and index freshness;
- add an exact SEARCH/REPLACE patch engine with content-hash preconditions,
  rollback, and AST affected-symbol evidence;
- add transactional lint-shielding workflows that can apply safe autofixes
  without hiding semantic changes from the user;
- add proxy-specific DLP facts and CEL scopes for outbound prompts, inbound
  tool calls, and local tool output (initial CEL object in place);
- add proxy traces, SARIF properties, and code-intel tables for cache hits,
  truncation, semantic search results, policy injections, patch outcomes, and
  network/API denials (initial schema in place);
- build an Agent Proxy E2E harness with fake provider endpoints and real local
  files/tools.

Out of scope:

- transparent TLS interception as an invisible default;
- proxy edits, truncation, policy injection, or output suppression without
  traceable evidence;
- provider-specific policy paths that bypass the normalized event envelope;
- vector/RAG-based enforcement decisions;
- hiding hook or CI failures behind proxy auto-remediation.

## Maintenance Rules

- Every roadmap item that lands should update this file or the linked design
  document in the same branch.
- New policy work should prefer the AST/CEL/SARIF architecture before adding a
  bespoke evaluator.
- New agent-facing features should expose MCP and trace evidence when useful.
- New trust claims should link to public evidence and avoid overstating
  external badge state.
