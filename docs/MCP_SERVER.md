<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# MCP Server

The `coding-ethos` MCP server is the agent-facing query surface for compiled
ETHOS policy, managed lint capture, generated skills, and focused remediation
guidance. It exists so agents can ask the policy system what to do before they
run broad shell commands or improvise lint workflows.

The server is exposed through the managed runtime:

```bash
bin/coding-ethos-run mcp
```

The MCP server is advisory context and managed tool access, not a bypass.
Blocking enforcement remains in the Git and agent-hook paths. MCP responses
must come from the same compiled bundle, generated configs, evidence maps, and
skill metadata used by those enforcement paths.

## Current Tool Surface

- `policy_check_command`: check a proposed shell command before running it.
- `policy_check_edit`: check a proposed file edit before applying it.
- `lint_check`: run managed lint capture for named tools, or compiled policy
  lint when no managed tool is supplied.
- `lint_advice`: map a linter diagnostic to ETHOS policy, advice, rerun
  guidance, and skill hints.
- `sarif_remediation_advice`: turn a SARIF result into ETHOS-grounded
  remediation guidance, skill context, and an MCP `lint_check` rerun request.
- `sarif_risk_summary`: summarize a SARIF run into policy, skill, tool, file,
  finding-group, severity, and next-action signals.
- `sarif_trend_analysis`: compare two SARIF runs or retained lint traces and
  identify introduced, fixed, and persisting findings.
- `sarif_policy_feedback`: report unmapped diagnostics, missing skill links,
  weak severity mappings, and noisy rules for policy authors.
- `tool_capabilities`: list managed tool sandbox capabilities, including
  `no-network`/`network`, `no-git`/`git`, sandbox profile, timeout, memory,
  CPU, seccomp profile metadata, and read/write path declarations.
- `policy_explain`: explain a compiled policy and its ETHOS grounding.
- `skill_lookup`: return the generated skill playbook for a skill ID.
- `remediation_explain`: expand an emitted `agent_remediation` payload into
  policy, principle, skill, action-context, and retry guidance.
- `code_intel_search`: query stored SARIF, remediation, and embedding evidence
  with FTS plus sqlite-vec when a query vector is supplied.
- `code_intel_index_status`: report code-intelligence store freshness,
  embedding metadata counts, and sqlite-vec row counts.
- `code_similarity_check`: compare proposed code against indexed repository
  symbols using normalized hashes and MinHash LSH before an agent writes a
  duplicate implementation.
- `code_intel_repo_map`: return the compact repository-wide AST map used for
  startup orientation.
- `code_intel_index_code`: parse selected repository paths with Tree-sitter
  and persist symbol/config chunks in the repo-local code-intelligence store.
- `code_intel_code_chunks`: return focused Tree-sitter chunks by path,
  language, symbol kind, or symbol name before agents read whole files.
- `code_intel_code_context`: expand a known chunk ID or exact path/symbol
  request into surrounding Tree-sitter context, graph edges, and linked
  findings without broad file reads.
- `code_intel_embedding_candidates`: return compact SARIF/remediation/code
  chunk records ready for an approved embedding producer.
- `skill_recommend`: rank generated skills for a task, command, path, or
  diagnostic.

The same repo map is also available as the read-only MCP resource
`coding-ethos://code-intel/repo-map`.

`skill_recommend` is also the runtime bridge for general operating discipline.
For broad implementation, review, refactoring, simplification, or debugging
requests, it should surface the `agent-operating-discipline` skill before the
agent starts editing. That skill adapts the useful behavioral pattern from
[`forrestchang/andrej-karpathy-skills`](https://github.com/forrestchang/andrej-karpathy-skills)
into ETHOS-derived guidance: explicit assumptions, smallest sufficient design,
surgical diffs, and verifiable success criteria.

Tool definitions include `coding_ethos` metadata so clients can distinguish
advisory tools from managed execution tools and know whether a tool reads
files, runs managed binaries, or persists traces.

## SARIF Remediation

`sarif_remediation_advice` is the bridge between CI/code-scanning evidence and
agent repair loops. It accepts either a SARIF payload or a retained lint
`trace_id`, plus an optional `result_index`, normalizes the selected result,
and returns:

- the rule, policy, skill, ETHOS principle, source tool, source location, and
  stable SARIF fingerprint;
- the coding-ethos finding group ID/key when SARIF includes cross-tool
  grouping metadata;
- CEL provenance when the SARIF rule or result records it;
- the relevant generated skill summary when available;
- guardrails that preserve policy and generated configuration;
- an MCP `lint_check` request the agent should use after applying a fix.

This tool does not read files or rerun lint by itself. It translates the same
SARIF evidence emitted by hooks and CI into a compact repair packet so agents
can avoid guessing from raw code-scanning output.

`sarif_risk_summary` accepts the same SARIF payload or retained lint `trace_id`
and gives agents a compact triage view before they pick a repair target:
result counts, blocking/security counts, top policies, skills, tools, files,
finding groups, and the next MCP call to make for result-level remediation.

Trace lookup is intentionally narrow. MCP accepts only a trace file name from
the configured consumer root's `.coding-ethos/lint-runs/` directory. It rejects
path-like trace IDs and replays the trace through the existing normalized lint
trace reader before formatting SARIF. This keeps trace remediation on the same
policy interpretation path as hooks, lint, and CI.

`sarif_trend_analysis` accepts baseline/current SARIF payloads or retained lint
trace IDs. It can also accept prior `history_sarif` payloads or
`history_trace_ids` to classify reopened findings. It compares coding-ethos
group keys first, then stable fingerprints, and finally a deterministic
policy/file/message fallback. Agents should use introduced, reopened, and
worsening findings as the first repair queue and persisting findings as
supporting context.

`sarif_policy_feedback` is for policy maintainers, not application repair. It
flags diagnostics that lack a policy ID, lack a skill ID, appear security-like
but are only notes or warnings, or repeat often enough to suggest noisy rule
mapping. The response points authors back to `policy_explain` and
`skill_recommend` rather than creating a separate policy interpretation path.

## Agent Remediation Advice Expansion

A high-value next expansion is a focused `remediation_advice` MCP tool. The
tool should broker advice from an available agent provider, such as
`claude -p`, Codex, or Gemini, while keeping the provider inside a constrained
advice-only environment.

The intent is:

1. `lint_check` reports normalized managed-tool findings.
2. `lint_advice` enriches findings with policy IDs, ETHOS principle IDs,
   evidence-map advice, rerun guidance, and skill hints.
3. `skill_recommend` selects the relevant generated playbooks.
4. `remediation_advice` asks a tightly scoped agent provider for a repair plan
   grounded in that context.

The tool must return advice only. It must not edit files, execute arbitrary
commands, bypass hooks, weaken policy, broaden suppressions, or hide failures.

## Constrained Provider Environment

When `coding-ethos` is started by an agent and called by an agent, an agent
provider is already available in the environment. That does not make raw
provider execution acceptable. Provider access must be routed through a managed
adapter with a deliberately small capability set.

The recommended provider sandbox is a highly restricted hook environment with:

- read-only access to explicitly requested source files and generated ETHOS
  context;
- access to the `coding-ethos` MCP stack for policy, lint, skill, and advice
  queries;
- no write access to the repository;
- no direct Git access;
- no shell escape path;
- no network access unless the configured provider requires it and the operator
  has explicitly allowed that provider;
- fixed argv construction, not arbitrary command strings;
- timeout and output-size limits;
- trace logging for the request, selected context, provider, and response.

The provider prompt should be generated from structured data, not a broad repo
dump. Inputs should include only:

- normalized findings and hook decisions;
- relevant file paths and compact snippets when needed;
- policy IDs and policy explanations;
- ETHOS principle summaries;
- generated skill hints and remediation steps;
- the exact rerun command or MCP lint request the agent should use after
  applying a fix.

The prompt must explicitly require structural fixes, policy preservation, and
hook-compatible remediation. It must prohibit bypass advice, broad suppressions,
generated-config edits, hash-manifest edits, raw Git workarounds, and direct
tool invocations when an MCP managed path exists.

## Provider Adapter Contract

Provider adapters should be first-class Go implementations, not shell snippets.
Each adapter should declare:

- provider name;
- executable path or managed command reference;
- fixed argument shape;
- startup validation requirements;
- maximum input bytes;
- maximum output bytes;
- timeout;
- whether network access is required;
- supported output format.

The adapter should accept a structured advice request and return a structured
advice response:

```json
{
  "summary": "Short diagnosis",
  "policy_ids": ["python.conditional_imports"],
  "principle_ids": ["no-conditional-imports"],
  "skill_ids": ["conditional-imports"],
  "steps": ["Move the import to module scope."],
  "rerun": {
    "tool": "lint_check",
    "arguments": {
      "tool": "ruff",
      "files": ["src/app.py"]
    }
  },
  "risks": ["Verify import cycle boundaries before moving the import."]
}
```

Free-form provider output may be stored for traceability, but the MCP response
should expose normalized fields first so callers can act on it reliably.

## Failure Behavior

The remediation advice service must fail fast when provider configuration is
ambiguous or unsafe:

- missing provider binary;
- provider binary outside an approved managed path;
- unsupported provider;
- prompt too large;
- output not parseable as the required schema;
- request includes write, Git, shell, or bypass capabilities;
- no policy or evidence grounding for the requested advice.

If no provider is configured, the MCP server may fall back to deterministic
local advice from evidence maps and generated skills, but it must mark the
response as `provider: "local"` and avoid pretending that an external agent
review occurred.

## Traceability

Every provider-backed advice request should persist a `.coding-ethos` trace
with:

- request ID;
- provider;
- selected policy IDs;
- selected principle IDs;
- selected skill IDs;
- file paths and snippet ranges, not whole-file dumps by default;
- model/provider exit status;
- normalized advice response;
- parse or validation failures.

Those traces let the project measure whether advice helps agents resolve
failures faster and whether specific policies need better evidence maps,
skills, or deterministic remediation text.
