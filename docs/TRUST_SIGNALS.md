<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Trust Signals

`coding-ethos` is a security and policy-enforcement project, so public trust
signals are part of the product surface. They help contributors and downstream
repos evaluate whether the project practices the controls it asks others to
adopt.

## Current Signals

- MIT licensed source.
- Public GitHub Actions CI.
- Generated SARIF/code-scanning workflow.
- CodeQL analysis for GitHub Actions workflows, Go, and Python.
- Build distribution job with package metadata validation and artifact
  attestation.
- OpenSSF Scorecard workflow with published results for the public badge and
  SARIF upload.
- Release workflow with GitHub artifact attestations, SPDX JSON SBOMs,
  SHA-256 checksums, offline `.intoto.jsonl` attestation bundles, and PyPI
  Trusted Publishing.
- Dependabot configuration.
- GitHub Actions pinned to immutable commit SHAs, with release-process review
  for SHA updates.
- uv dependency resolution constrained with `[tool.uv].exclude-newer = "7 days"`
  and enforced for consumer `pyproject.toml` files. Time-sensitive security
  updates may use package-specific `exclude-newer-package` overrides instead of
  weakening the global freshness window.
- Public CI publishes JUnit XML, Python coverage reports, and Go coverage
  reports as workflow artifacts with coverage summaries in job output.
- `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `CHANGELOG.md`,
  issue templates, and pull request template.
- Repository topics for AI-agent, MCP, CEL, static-analysis, DevSecOps, Git
  hook, and policy-as-code discovery.
- Dogfooded hook, CEL, MCP, SARIF, sandbox, and managed-toolchain enforcement.
- Red-team tests for bypass and enforcement behavior.

## OpenSSF Scorecard

Target badge:

```markdown
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/paudley/coding-ethos/badge)](https://scorecard.dev/viewer/?uri=github.com/paudley/coding-ethos)
```

The `.github/workflows/scorecard.yml` workflow publishes public Scorecard
results with `publish_results: true`, uploads Scorecard SARIF to code scanning,
and preserves the SARIF file as a workflow artifact. The public result is an
ecosystem-facing signal, so regressions are tracked as project work instead of
treated as badge cosmetics.

Current areas to monitor in the published result:

- Branch protection and required checks.
- Token permissions in GitHub Actions workflows.
- Dependency update coverage.
- Signed-release, provenance, and PyPI Trusted Publishing posture.
- Binary artifact publication and checksum policy.
- SBOM generation and attestation coverage.
- Security policy and vulnerability reporting path.
- Fuzzing coverage beyond the initial Go fuzz smoke workflow.

## OpenSSF Best Practices Badge

[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12737/badge)](https://www.bestpractices.dev/en/projects/12737)

The OpenSSF Best Practices Badge is a separate human-reviewed checklist. The
project tracks its passing badge at
`https://www.bestpractices.dev/en/projects/12737`.

Preparation checklist:

- [x] Add or verify `SECURITY.md` with vulnerability reporting instructions.
- [x] Publish an initial release with clear release notes.
- [x] Document supported platforms and installation paths.
- [x] Document how dependencies are updated and audited.
- [x] Document the project governance and maintainer contact path.
- [x] Review license, contribution, and issue-template completeness.

## Public Release Checklist

- [x] Create a `v0.1.0` GitHub release.
- [ ] Attach or document generated binaries if they are part of the supported
  install path.
- [x] Generate checksums for Python distribution artifacts.
- [x] Confirm Python distributions have GitHub artifact attestations before
  upload to a certified PyPI account.
- [x] Generate and attest an SPDX JSON SBOM for release artifacts.
- [x] Publish Python distributions via PyPI Trusted Publishing so PyPI publish
  attestations are generated and uploaded automatically.
- [x] Link the docs landing page from the repository homepage or README.
- [x] Upload `docs/social-preview.png` as the GitHub social preview image.
- [ ] Enable GitHub Discussions with categories for policy recipes, agent
  integrations, CEL examples, MCP workflows, and showcase posts.
