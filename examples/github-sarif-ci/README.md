<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# GitHub SARIF CI Example

This example shows the intended consumer-repo flow for generated SARIF gates.

## Generated Config

By default, generated tool config sync writes the GitHub SARIF workflow when
GitHub Actions generation is enabled in the merged enforcement config.

```yaml
generated_config:
  ci:
    github_actions:
      enabled: true
```

The generated workflow should be treated like other generated repo config:
change source config first, regenerate, and review the diff.

## Expected CI Behavior

The SARIF gate should:

- run the compiled coding-ethos policy lint path
- write SARIF even when policy violations fail the gate
- upload SARIF to GitHub code scanning
- upload artifacts for debugging when configured
- fail the job after uploads if blocking findings exist

## Agent Workflow

When CI reports coding-ethos SARIF findings, agents should:

1. Inspect the SARIF/code-scanning finding.
2. Use MCP `sarif_remediation_advice` for ETHOS-grounded repair context.
3. Fix the structural issue.
4. Rerun local managed checks.
5. Push only after local evidence is clean.

## Related Docs

- [CI/CD SARIF](../../docs/CI_CD_SARIF.md)
- [SARIF uses](../../docs/SARIF_USES.md)
- [SARIF editor integration](../../docs/SARIF_EDITOR_INTEGRATION.md)
