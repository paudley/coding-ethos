<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# CEL Policy Example

This example shows the intended direction for custom repository policy: keep the
rule with the ETHOS principle it enforces, then let the compiler dispatch that
policy through hooks, lint, MCP, and SARIF.

## Example Principle-Owned Policy

```yaml
principles:
  - id: keep-files-focused
    title: Keep Files Focused
    directive: Split large files into focused modules instead of growing them.
    axioms:
      - id: large-files-hide-design-problems
        text: Large files hide multiple reasons to change.
    policies:
      expressions:
        - id: filesystem.large_files.no_growth
          description: Large source files must not keep growing.
          event: PreToolUse
          severity: block
          expression: |
            proposed_file_change.is_source
            && proposed_file_change.current_line_count >= 800
            && proposed_file_change.new_line_count > proposed_file_change.current_line_count
```

## Why This Shape

- The principle explains the intent.
- The axiom gives compact hook guidance.
- The CEL expression is the enforceable predicate.
- MCP can explain the policy by ID.
- SARIF can carry the same ID into code scanning and remediation workflows.

## Agent Workflow

When blocked by this policy, an agent should:

1. Stop editing the oversized file.
2. Split responsibilities into a focused module.
3. Move code first, then shrink the original file.
4. Rerun the same hook or MCP policy path.
5. Report the net file-size change as evidence.

## Related Docs

- [Policy language strategy](../../docs/POLICY_LANGUAGE_STRATEGY.md)
- [MCP server](../../docs/MCP_SERVER.md)
- [SARIF uses](../../docs/SARIF_USES.md)
