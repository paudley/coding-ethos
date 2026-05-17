<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# SARIF Editor Integration

`coding-ethos` editor diagnostics should consume the same SARIF emitted by
hooks, managed lint capture, CI, and MCP. Editors must not implement a second
policy engine or reinterpret policy configuration.

## Contract

The editor-facing flow is:

1. Run `bin/coding-ethos-run policy-lint --sarif` with the same file scope the
   editor wants to display.
2. Parse SARIF `runs[].results[]` for diagnostics.
3. Use `ruleId`, `locations[]`, `level`, and `message.text` for editor
   markers.
4. Use `properties.policy_id`, `properties.skill_id`,
   `properties.ethos_ids`, and `properties.advice` for hover text and quick
   links.
5. Use `properties.coding_ethos_group_id` to group duplicate findings from
   multiple tools.
6. Use MCP `sarif_remediation_advice` for focused repair guidance when the
   user or agent opens a finding.

The editor integration should prefer changed-file or open-file scopes. Whole
repository scans belong in CI or explicit audit commands because editor
feedback should be fast and local.

## Required Behavior

- Never read or edit `coding_ethos.yml`, generated tool configs, hook runtime
  files, or `.code-ethos/tool-config-hashes.json` to manufacture editor
  diagnostics.
- Never map pathless policy context to a fake editor location.
- Preserve repository-relative paths from SARIF artifact URIs.
- Keep SARIF fingerprints and `coding_ethos_group_id` intact so editor,
  CI, and MCP views refer to the same finding.
- Use MCP `policy_explain`, `skill_lookup`, and
  `sarif_remediation_advice` for explanation and remediation instead of
  embedding separate guidance rules in editor code.

## Minimal Adapter Shape

An editor adapter only needs three operations:

- `lintOpenFiles(files)`: run managed SARIF lint for the supplied files.
- `diagnosticsFromSarif(sarif)`: convert SARIF results into editor markers.
- `remediate(resultIndex, sarif)`: call MCP `sarif_remediation_advice` with
  the same SARIF payload and selected result index.

All richer behavior, including risk summaries, trend analysis, policy
authoring feedback, and skill lookup, should be routed through MCP.
