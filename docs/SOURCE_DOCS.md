<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Source Docs Index

## Documents

- `docs/REPOSITORY_ANALYSIS.md`: architecture map, source-of-truth
  boundaries, generated artifacts, and verification contract.
- `docs/index.md`: public docs landing page for policy-as-code, AI-agent
  enforcement, MCP, CEL, SARIF, sandboxing, and roadmap entry points.
- `docs/TRUST_SIGNALS.md`: OpenSSF, release, security, social-preview, and
  publication checklist for project credibility and organic discovery.
- `docs/OPENSSF_GOLD_CHECKLIST.md`: Best Practices Gold target, prefill
  generator workflow, remaining gaps, and remediation plan.
- `docs/SECURITY_ASSURANCE_CASE.md`: security claims, evidence, validation
  posture, secure-design posture, and explicit limits.
- `docs/GOLD_SECURITY_POSTURE.md`: OpenSSF Gold evidence for cryptography
  applicability, TLS verification, hosted-site hardening, and signed releases.
- `docs/BUILD_REPRODUCIBILITY.md`: repeatable build controls, deterministic Go
  build flags, release provenance, and known reproducibility limits.
- `tools/MODULE.md`: repository-local helper tool contract and test
  expectations for maintenance automation.
- `docs/SUPPLY_CHAIN_ATTESTATIONS.md`: Scorecard publishing, GitHub artifact
  attestations, SBOM generation, PyPI Trusted Publishing, checksum policy, and
  verification commands.
- `docs/THREAT_MODEL.md`: protected assets, actors, trust boundaries, primary
  risks, out-of-scope claims, and defense-in-depth summary.
- `docs/RELEASE.md`: versioning, release checklist, build artifacts, release
  notes, and post-release actions.
- `docs/DISCUSSIONS.md`: recommended GitHub Discussions categories and
  issue-versus-discussion routing.
- `docs/DEMO.md`: verified MCP, command-block, lint-check, and SARIF demo
  excerpts plus recording instructions.
- `docs/assets/README.md`: asset inventory and regeneration instructions for
  demo recordings, rendered GIFs, and social preview images.
- `docs/COMPARISON.md`: positioning against pre-commit, CodeQL, Semgrep, OPA,
  branch protection, and plain agent instructions.
- `docs/INTEGRATIONS.md`: integration guide for Codex, Claude Code, Gemini
  CLI, MCP clients, GitHub Actions, GitLab CI, SARIF consumers, and managed
  static-analysis tools.
- `docs/MCP_SERVER.md`: stdio MCP server contract, current tools, and
  expansion plan for agent-facing policy, lint, SARIF, and remediation
  services.
- `docs/AGENT_REMEDIATION.md`: normalized `agent_remediation` payload shape,
  MCP remediation flow, provider examples, and trace-summary contract.
- `docs/CODE_INTEL.md`: Tree-sitter AST code intelligence plan and current
  implementation, SQLite canonical storage, sqlite-vec vector search, hybrid
  retrieval, and MCP search tools.
- `docs/AST_CEL_SARIF_ARCHITECTURE.md`: preferred architecture for collecting
  parsed source facts, evaluating principle-owned CEL policies, emitting
  stable SARIF, and storing code-intelligence evidence.
- `docs/POLICY_LANGUAGE_STRATEGY.md`: CEL policy-language strategy, typed
  inputs, helper functions, and the path from Go evaluators into
  principle-owned policy-as-code.
- `docs/CI_CD_SARIF.md`: generated GitHub Actions and GitLab CI SARIF gates,
  upload behavior, artifacts, and consumer repo integration.
- `docs/SARIF_USES.md`: practical SARIF uses beyond code scanning, including
  remediation loops, trend analysis, risk summaries, and editor integration.
- `docs/SARIF_EDITOR_INTEGRATION.md`: editor and local developer workflows
  built around SARIF output.
- `docs/RUNTIME_SANDBOXING.md`: Bubblewrap, cgroup, seccomp, capability, and
  CEL-backed runtime sandboxing strategy.
- `docs/RUNTIME_PUBLICATION.md`: PyPI generator package boundaries and the
  release-asset model required before compiled Go runtimes are distributed.
- `docs/RED_TEAM_SUITE.md`: adversarial tests for policy, hook, MCP, shell,
  sandbox, and SARIF behavior.
- `docs/STRATEGIC_ROADMAP.md`: major platform roadmap across MCP, CEL, SARIF,
  sandboxing, centralized ETHOS registry, and agent remediation loops.
- `docs/HOOK_RUNTIME_BOOTSTRAP.md`: target model for consumer hook entrypoints,
  checkout-local runtime artifacts, and bootstrap repair behavior.
- `docs/LINT_CAPTURE_GO_FLOW.md`: target model for replacing shell-owned lint
  capture with compiled Go request, config, target-resolution, tool execution,
  logging, and rendering stages.
- `TODO.md`: active implementation tasks that are not yet part of the
  supported runtime contract.
- `coding_ethos/CODING_ETHOS.md`: package overview, module boundaries, and
  supported public imports.
- `tests/TESTS.md`: test-suite scope and expectations for behavior changes.
- `pre-commit/PRE-COMMIT.md`: bundled Go hook installation, generated config,
  hook inventory, and update workflow.
- `pre-commit/hooks/HOOKS.md`: hook implementation overview and development
  commands.
- `pre-commit/hooks/coding_ethos_hooks/CODING_ETHOS_HOOKS.md`: hook package
  overview, installation flow, and runtime boundaries.
- `examples/README.md`: small user-facing examples, starting with the MCP lint
  advice workflow agents should prefer over raw linter invocation.
