<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Trust Signals

`coding-ethos` is a security and policy-enforcement project, so public trust
signals are part of the product surface. They help contributors and downstream
repos evaluate whether the project practices the controls it asks others to
adopt.

## Current Signals

- AGPLv3-licensed source with commercial licensing available.
- Public GitHub Actions CI.
- Generated SARIF/code-scanning workflow.
- CodeQL analysis for GitHub Actions workflows, Go, and Python.
- OSV-Scanner vulnerability scanning for pull requests, merge queue, `main`,
  scheduled runs, and manual dispatch.
- Zizmor GitHub Actions security scanning with SARIF upload.
- Managed `actionlint` validation for workflow correctness in CI.
- Build distribution job with package metadata validation and artifact
  attestation.
- OpenSSF Scorecard workflow with published results for the public badge and
  SARIF upload.
- Release workflow with GitHub artifact attestations, SPDX JSON SBOMs,
  SHA-256 checksums, offline `.intoto.jsonl` attestation bundles, and PyPI
  Trusted Publishing.
- Release workflow dynamic analysis gate that fuzzes the shell parser, SARIF
  formatter, hook event decoder, and CEL input construction before release
  artifacts can be built or published.
- Dependabot configuration for GitHub Actions, Go modules, root uv
  dependencies, and hook uv dependencies, with a seven-day cooldown to avoid
  unreviewed dependency churn.
- GitHub Actions pinned to immutable commit SHAs, with release-process review
  for SHA updates.
- uv dependency resolution constrained with `[tool.uv].exclude-newer = "7 days"`
  and enforced for consumer `pyproject.toml` files. Time-sensitive security
  updates may use package-specific `exclude-newer-package` overrides instead of
  weakening the global freshness window.
- Public CI publishes JUnit XML, Python coverage reports, and Go coverage
  reports as workflow artifacts with coverage summaries in job output.
- `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `CHANGELOG.md`,
  `CODEOWNERS`, issue templates, discussion templates, and pull request
  template.
- CLA Assistant contribution certification in `CONTRIBUTING.md`; the PR
  template requires contributors to complete the CLA check when prompted, and
  CODEOWNERS names `@paudley` and `@ErinAudley` as Blackcat Informatics® Inc.
  project owners.
- Governance and continuity are documented in `CONTRIBUTING.md`; Blackcat
  Informatics® Inc. maintains the project, and the two CODEOWNERS-listed
  directors can continue issue triage, pull request review, repository
  administration, and release management if either owner becomes unavailable.
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
- Assertion-enabled dynamic analysis beyond the current release-gated fuzzing
  workflow.

## Repository Rulesets

The GitHub `Main branch protection` ruleset is the durable merge gate for
`refs/heads/main`. It requires linear signed history, blocks deletion and
non-fast-forward updates, requires the Python 3.11 test, Python 3.13 test,
distribution build, and OpenSSF Scorecard status checks, and requires code
scanning results from CodeQL, Scorecard, coding-ethos, OSV-Scanner, and Zizmor.
Pull requests to `main` require one approving review, code-owner review,
last-push approval, resolved review threads, Copilot review on push, and squash
merge.

The GitHub `Release branch approvals` ruleset applies the same review and code
scanning posture to `refs/heads/release/*`, with release branches retaining
merge commits for release integration history.

## OpenSSF Best Practices Badge

[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12737/badge)](https://www.bestpractices.dev/en/projects/12737)

The OpenSSF Best Practices Badge is a separate human-reviewed checklist. The
project tracks its public badge record at
`https://www.bestpractices.dev/en/projects/12737`.

The project currently holds the **Silver** badge. The target remains **Gold**.
Treat the badge as a project-quality checklist, not as badge decoration.
Repo-side gaps should be remediated in code, docs, CI, or governance before a
criterion is left as unmet. Criteria that depend on external GitHub
organization settings, independent contributors, or human review capacity must
remain explicit in the gap list until the underlying condition is true.

The root `.bestpractices.json` file is the durable repo-hosted proposal file
consumed by the Best Practices site. Prefer the repo-hosted reanalysis path
over query-string prefills; the prefill URL path proved too fragile and is not
part of the supported workflow.

OpenSSF Scorecard's `CII-Best-Practices` check is not controlled by repo-local
SARIF or workflow output. It calls the Best Practices badge API for the Git
repository URL and scores the public tier it receives. After the Best Practices
UI advances the project to a new tier, verify that project 12737 uses
`https://github.com/paudley/coding-ethos` as its repository URL, wait for the
public Best Practices badge/API to reflect the new tier, then rerun the
Scorecard workflow.

Preparation checklist:

- [x] Add or verify `SECURITY.md` with vulnerability reporting instructions.
- [x] Publish an initial release with clear release notes.
- [x] Document supported platforms and installation paths.
- [x] Document how dependencies are updated and audited.
- [x] Document the project governance, contribution certification, and
  maintainer contact path.
- [x] Review license, contribution, and issue-template completeness.
- [x] Add root `.bestpractices.json` for repo-hosted Best Practices
      automation proposals.
- [ ] Resolve remaining Gold gaps from the generated report.

## Public Release Checklist

- [x] Create a signed GitHub release; the current public release is `v0.3.0`.
- [ ] Attach or document generated binaries if they are part of the supported
  install path.
- [x] Generate checksums for Python distribution artifacts.
- [x] Confirm Python distributions have GitHub artifact attestations before
  upload to a certified PyPI account.
- [x] Generate and attest an SPDX JSON SBOM for release artifacts.
- [x] Attach offline `.intoto.jsonl` attestation bundles to the immutable
  GitHub release before publication.
- [x] Publish Python distributions via PyPI Trusted Publishing so PyPI publish
  attestations are generated and uploaded automatically.
- [x] Link the docs landing page from the repository homepage or README.
- [x] Upload `docs/social-preview.png` as the GitHub social preview image.
- [x] Enable GitHub Discussions with categories for policy recipes, agent
  integrations, CEL examples, MCP workflows, and showcase posts.
