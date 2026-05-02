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

### Policy Coverage Maps

SARIF can represent findings, but the same rule metadata can also support a
coverage view: which ETHOS principles, policies, skills, and tool families ran
for a commit or PR. A no-findings run is still useful evidence when it proves
that the expected defenses actually executed.

### Trend And Regression Tracking

Stable rule IDs and fingerprints make it possible to compare SARIF runs across
commits. `coding-ethos` can identify newly introduced violations, reopened
findings, fixed findings, and policy areas that are getting worse over time.
That lets CI and agents prioritize regressions instead of treating every
historic finding as equally urgent.

### PR Risk Summaries

SARIF can feed a compact PR risk summary for agents and reviewers: blocking
findings, security findings, changed-file hotspots, policy families affected,
and recommended MCP calls for remediation. The summary should be derived from
the SARIF and trace data rather than being a separate analysis path.

### Security Audit Evidence

CI should retain SARIF and `.coding-ethos` traces as an audit bundle. This
provides evidence that protected controls ran, which policies were evaluated,
which violations were blocked, and what remediation guidance was available at
the time of review.

### IDE And Editor Diagnostics

The same SARIF output can back editor diagnostics for VS Code, Cursor, and
other SARIF-aware clients. This gives developers and agents earlier feedback
without creating a second diagnostic model separate from hooks and CI.

### Policy Authoring Feedback

Unmapped diagnostics, weak severity mappings, missing skill IDs, noisy rules,
and repeated suppressions should be visible through SARIF analysis. Policy
authors can use that feedback to improve evidence maps, CEL expressions,
skill linkage, and generated tool configuration.

## MCP Integration

The MCP server should expose SARIF-aware tools that let an agent:

- submit a SARIF run or trace ID for focused remediation;
- explain a SARIF rule in ETHOS and policy terms;
- group related findings by fingerprint, policy, skill, and file;
- identify newly introduced findings compared with a prior SARIF run;
- produce a concise, agent-native repair plan with exact validation commands.

These tools should call into the same compiled policy bundle and normalized
diagnostic model used by hooks and CI. MCP must not become a policy bypass or a
separate advisory-only implementation.

## GitHub And GitLab

GitHub code scanning is the first-class upload target for SARIF annotations,
but the format remains useful outside GitHub. GitLab and other CI systems can
retain SARIF as artifacts, feed merge-request annotation layers, or pass SARIF
to downstream security dashboards. The repository contract should stay
provider-neutral: stable identifiers, repository-relative paths, deterministic
fingerprints, compact result volume, and retained trace artifacts.
