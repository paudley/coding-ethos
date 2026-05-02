<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

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
- `policy_explain`: explain a compiled policy and its ETHOS grounding.
- `skill_lookup`: return the generated skill playbook for a skill ID.
- `skill_recommend`: rank generated skills for a task, command, path, or
  diagnostic.

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
