<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Codex prompt addon

You are working in `coding-ethos`.
Treat `AGENTS.md` as the source of truth when it is present.
If a task touches architecture, validation, error handling, or delegation, consult the matching detail doc under `.agents/ethos/` before acting.

Core principles:
- SOLID is Law: Enforce SOLID and simplicity; remove speculative abstractions.
- Fail Fast, Fail Hard (Overview): Crash early on ambiguous startup and configuration states instead of degrading silently.
- No Conditional Imports: Treat required imports as hard dependencies and fail immediately if they are missing.
- Static Analysis is the First Line of Defense: Make ruff and mypy blocking quality gates rather than advisory tools.
- No Optional Types for Required Dependencies: Model required dependencies as non-optional and default to full-strength behavior.
- SOLID is Law quick ref: Enforce SOLID and simplicity; remove speculative abstractions. | We do not view the SOLID principles as academic suggestions.
- Fail Fast, Fail Hard (Overview) quick ref: Crash early on ambiguous startup and configuration states instead of degrading silently. | Ambiguity is the enemy of reliability.
- No Conditional Imports quick ref: Treat required imports as hard dependencies and fail immediately if they are missing. | We strictly ban the "soft dependency" pattern.
- Static Analysis is the First Line of Defense quick ref: Make ruff and mypy blocking quality gates rather than advisory tools. | We rely on linters (ruff) and type checkers (mypy) to catch errors
before the code ever runs.
- No Optional Types for Required Dependencies quick ref: Model required dependencies as non-optional and default to full-strength behavior. | We strictly ban | None (or Optional) for dependencies that are
required for correct operation.
