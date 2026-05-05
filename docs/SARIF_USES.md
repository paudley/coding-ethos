<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# SARIF Uses

SARIF is more than a CI upload format for `coding-ethos`. It should become the
shared evidence ledger that connects hooks, managed lint capture, CI, MCP,
agent remediation, and audit review. The durable contract is a normalized
finding with stable policy identity, source location, ETHOS grounding, skill
guidance, and replayable trace context.

## Design Rules

- Emit SARIF only from normalized, evidence-backed diagnostics.
- Keep artifact paths repository-relative and avoid workstation-specific data.
- Preserve stable `ruleId`, rule metadata, `partialFingerprints`, ETHOS
  principle IDs, policy IDs, and skill IDs so findings can be tracked across
  local hooks, CI, and MCP.
- Retain `.coding-ethos` traces beside SARIF artifacts so every result can be
  replayed or explained without rerunning the original tool.
- Treat SARIF as a review and remediation surface, not as the only enforcement
  path. Hooks, CI gates, and managed tool execution remain blocking controls.

## High-Value Uses

### Code-Intelligence Storage

SARIF results are ingested into the repo-local code-intelligence store so
agents can search prior violations, remediation advice, outcome records, and
Tree-sitter-linked source chunks without reparsing every trace. SQLite remains
the canonical ledger, FTS5 provides exact search, and sqlite-vec provides
derived vector retrieval for records that have approved embeddings. SARIF
therefore becomes a durable input to MCP retrieval, not just a transient CI
upload.

### Agent Remediation Input

Agents should be able to hand a SARIF result or run to the MCP server and ask
for focused repair advice. The advice should be grounded in the result's
policy ID, ETHOS principle IDs, skill ID, source tool, source location, and
trace evidence. This gives agents a better path than rerunning a linter and
guessing from raw terminal output.

### Cross-Tool Finding Deduplication

The same defect can surface through Ruff, Bandit, mypy, pyright, a custom CEL
policy, and a hook evaluator. SARIF fingerprints give us a common grouping
surface for "one defect, many witnesses." This should reduce duplicate agent
work while preserving every contributing tool as supporting evidence.
`coding-ethos` records this as `coding_ethos_group_id` and
`coding_ethos_group_key` on each result, plus a compact
`runs[].properties.finding_groups` ledger for downstream agents and reviewers.

### Policy Coverage Maps

SARIF can represent findings, but the same rule metadata can also support a
coverage view: which ETHOS principles, policies, skills, and tool families ran
for a commit or PR. A no-findings run is still useful evidence when it proves
that the expected defenses actually executed.

`coding-ethos` records this as `runs[].properties.policy_coverage`. The
coverage block is intentionally summary-shaped so GitHub, GitLab, MCP, and
agent prompts can consume it without replaying traces. It should remain
derived from normalized decisions and diagnostics, not from a separate CI-only
policy inventory.

### Trend And Regression Tracking

Stable rule IDs and fingerprints make it possible to compare SARIF runs across
commits. `coding-ethos` can identify newly introduced violations, reopened
findings, fixed findings, and policy areas that are getting worse over time.
That lets CI and agents prioritize regressions instead of treating every
historic finding as equally urgent.
`sarif_trend_analysis` compares baseline/current SARIF payloads or retained
lint trace IDs, and can use older history payloads to identify reopened
findings. Worsening findings are detected when a persisting finding's current
SARIF level is more severe than its baseline level.

### PR Risk Summaries

SARIF can feed a compact PR risk summary for agents and reviewers: blocking
findings, security findings, changed-file hotspots, policy families affected,
and recommended MCP calls for remediation. The summary should be derived from
the SARIF and trace data rather than being a separate analysis path.
`sarif_risk_summary` provides the first SARIF-only summary path for agents.

### Security Audit Evidence

CI should retain SARIF and `.coding-ethos` traces as an audit bundle. This
provides evidence that protected controls ran, which policies were evaluated,
which sandbox profile and declared capabilities were requested for managed
tool execution, and whether any runtime sandbox denial occurred. Sandbox
evidence belongs in SARIF run properties and trace records, not as invented
file-level alerts when the denial has no source location. The same bundle
should show which violations were blocked and what remediation guidance was
available at the time of review.

### IDE And Editor Diagnostics

The same SARIF output can back editor diagnostics for VS Code, Cursor, and
other SARIF-aware clients. This gives developers and agents earlier feedback
without creating a second diagnostic model separate from hooks and CI.
The adapter contract is documented in
[`SARIF_EDITOR_INTEGRATION.md`](SARIF_EDITOR_INTEGRATION.md).

### Policy Authoring Feedback

Unmapped diagnostics, weak severity mappings, missing skill IDs, noisy rules,
and repeated suppressions should be visible through SARIF analysis. Policy
authors can use that feedback to improve evidence maps, CEL expressions,
skill linkage, and generated tool configuration.
`sarif_policy_feedback` exposes this as an MCP tool for SARIF payloads or
retained lint trace IDs.

## MCP Integration

The MCP server should expose SARIF-aware tools that let an agent:

- submit a SARIF run or trace ID for focused remediation;
- explain a SARIF rule in ETHOS and policy terms;
- group related findings by fingerprint, policy, skill, and file;
- identify newly introduced findings compared with a prior SARIF run;
- compare baseline/current SARIF or retained lint traces for introduced,
  fixed, and persisting findings;
- produce a concise, agent-native repair plan with exact validation commands.
- summarize a SARIF run into policy, skill, file, tool, finding-group, and
  next-action risk signals.
- report policy-authoring feedback for unmapped diagnostics, missing skills,
  weak severity mappings, and noisy rules.

These tools should call into the same compiled policy bundle and normalized
diagnostic model used by hooks and CI. MCP must not become a policy bypass or a
separate advisory-only implementation.

`sarif_remediation_advice` is the first implemented SARIF-aware MCP tool. It
accepts SARIF JSON or a retained lint trace ID, selects a result, preserves CEL
and policy provenance, attaches generated skill context, carries the SARIF
grouping key, and returns the MCP `lint_check` request an agent should use to
verify the repair. `sarif_risk_summary` follows the same input contract for
run-level triage.

## GitHub And GitLab

GitHub code scanning is the first-class upload target for SARIF annotations,
but the format remains useful outside GitHub. GitLab and other CI systems can
retain SARIF as artifacts, feed merge-request annotation layers, or pass SARIF
to downstream security dashboards. The repository contract should stay
provider-neutral: stable identifiers, repository-relative paths, deterministic
fingerprints, compact result volume, and retained trace artifacts.
