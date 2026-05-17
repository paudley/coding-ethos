<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# GitHub Discussions Plan

GitHub Discussions should be used for open-ended community work that is not yet
an actionable issue or pull request.

## Recommended Categories

| Category | Purpose |
| --- | --- |
| Announcements | Releases, roadmap updates, and public project status. |
| Policy recipes | Reusable ETHOS, CEL, and repo-policy patterns. |
| Agent integrations | Codex, Claude Code, Gemini CLI, and other MCP client setup. |
| MCP workflows | Tool ideas, client behavior, lint-advice flows, and server ergonomics. |
| CEL examples | Typed input examples, helper functions, and migration candidates. |
| SARIF workflows | Code scanning, editor integration, remediation, and trend analysis. |
| Showcase | Dogfooding reports, consumer repo examples, and demos. |
| Q&A | Usage questions that do not need a bug report. |

## Issue vs Discussion

Use an issue for:

- reproducible bugs
- accepted implementation tasks
- failing checks
- security-independent defect reports
- narrow feature requests with clear acceptance criteria

Use a discussion for:

- policy design questions
- integration recipes
- demos and showcases
- support questions
- brainstorming around future MCP, CEL, SARIF, or sandbox features

## Moderation Notes

- Redirect vulnerability reports to `SECURITY.md`.
- Keep conversations technical and aligned with `CODE_OF_CONDUCT.md`.
- Convert discussions into issues once the required change is concrete.
- Close or archive threads that become stale and unactionable.
