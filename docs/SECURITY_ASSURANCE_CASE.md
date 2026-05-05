<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Security Assurance Case

This document states the security argument for `coding-ethos` as a set of
claims, evidence, and known limits. It is intentionally concrete: the project
does not claim that agents become safe because a hook exists. The claim is that
the repository applies layered controls that make risky actions visible,
policy-addressable, and harder to land unnoticed.

## Top-Level Claim

`coding-ethos` reduces the risk of unsafe AI-agent development workflows by
combining policy-as-code, source-aware analysis, managed lint execution, SARIF
reporting, MCP guidance, and repository trust controls.

## Evidence

- ETHOS principles are stored in `coding_ethos.yml` and compiled into a policy
  bundle instead of being maintained only as prose.
- CEL-backed policy expressions make many decisions inspectable and testable.
- Go Tree-sitter facts feed source-aware Python policy so import tricks and
  structural hazards can be detected before commit.
- The hook runtime emits TOON, JSON, and SARIF findings with policy, skill, and
  remediation metadata.
- Managed lint capture keeps passing-tool warnings visible and policy
  addressable instead of relying only on process exit codes.
- CodeQL, OSV-Scanner, Zizmor, OpenSSF Scorecard, actionlint, golangci-lint,
  Ruff, mypy, Pyright, Pylint, Bandit, SQLFluff, Tombi, ShellCheck, hadolint,
  and project policy gates run through local or CI enforcement paths.
- Release workflows generate checksums, SPDX SBOMs, GitHub artifact
  attestations, offline `.intoto.jsonl` bundles, and PyPI Trusted Publishing
  attestations.
- `SECURITY.md`, `docs/THREAT_MODEL.md`, `docs/RUNTIME_SANDBOXING.md`, and
  `docs/SUPPLY_CHAIN_ATTESTATIONS.md` document reporting, threat boundaries,
  sandbox intent, and provenance limits.

## Input Validation Claim

Inputs from agent tools, shell commands, JSON hook payloads, SARIF files, YAML
configuration, CEL expressions, and code-intelligence traces are parsed through
structured parsers or typed loaders where practical. Malformed shell text is
denied rather than treated as a compatibility case. Policy evaluation receives
normalized facts instead of ad hoc string fragments where source structure or
shell structure matters.

## Secure Design Claim

The project uses defense in depth:

- policy decisions are centralized before allow/deny output;
- protected enforcement points are write-protected;
- managed tool execution avoids parent-repo tool and config shadowing;
- SARIF and trace outputs preserve evidence for later review;
- release artifacts carry provenance and component inventory;
- documentation states what the controls do and what they do not do.

## Known Limits

- Local hooks are not a complete sandbox. Runtime sandboxing is a roadmap item
  and currently complements, rather than replaces, policy evaluation.
- The project cannot prove that an external agent provider will honor every
  hook rewrite or message format forever; provider behavior is tested and
  version drift is treated as a bug.
- GitHub organization settings such as mandatory 2FA and release-review rules
  must be confirmed outside the repository.
- Gold-level social criteria such as unassociated significant contributors and
  two-person review require real project governance capacity, not just files.
