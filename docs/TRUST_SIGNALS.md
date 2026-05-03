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
- Build distribution job with package metadata validation and artifact
  attestation.
- OpenSSF Scorecard workflow with published results for the public badge and
  SARIF upload.
- Release workflow with GitHub artifact attestations, SPDX JSON SBOMs,
  SHA-256 checksums, and PyPI Trusted Publishing.
- Dependabot configuration.
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
and preserves the SARIF file as a workflow artifact. The badge may report
`invalid repo path` until the first successful run on `main` has populated the
public Scorecard API.

Expected areas to monitor after the first published result:

- Branch protection and required checks.
- Token permissions in GitHub Actions workflows.
- Dependency update coverage.
- Signed-release, provenance, and PyPI Trusted Publishing posture.
- Binary artifact publication and checksum policy.
- SBOM generation and attestation coverage.
- Security policy and vulnerability reporting path.

## OpenSSF Best Practices Badge

The OpenSSF Best Practices Badge is a separate human-reviewed checklist. The
project should apply once the public-facing security docs are complete.

Preparation checklist:

- [x] Add or verify `SECURITY.md` with vulnerability reporting instructions.
- [ ] Publish an initial release with clear release notes.
- [x] Document supported platforms and installation paths.
- [x] Document how dependencies are updated and audited.
- [x] Document the project governance and maintainer contact path.
- [x] Review license, contribution, and issue-template completeness.

## Public Release Checklist

- [ ] Create a `v0.1.0` GitHub release.
- [ ] Attach or document generated binaries if they are part of the supported
  install path.
- [ ] Add checksums for published binary artifacts.
- [x] Confirm Python distributions have GitHub artifact attestations before
  upload to a certified PyPI account.
- [x] Generate and attest an SPDX JSON SBOM for release artifacts.
- [x] Publish Python distributions via PyPI Trusted Publishing so PyPI publish
  attestations are generated and uploaded automatically.
- [ ] Link the docs landing page from the repository homepage or README.
- [ ] Upload `docs/social-preview.png` as the GitHub social preview image.
- [ ] Enable GitHub Discussions with categories for policy recipes, agent
  integrations, CEL examples, MCP workflows, and showcase posts.
