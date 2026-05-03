<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.1] - 2026-05-03

### Fixed

- Fixed CodeQL workflow analysis so Go build arguments are not interpreted as
  missing Go packages.
- Addressed CodeQL findings for writable file close handling, bounded
  allocation sizes, Gemini cache-key hashing, Python implicit string
  concatenation, and mixed return paths.
- Raised the managed hook toolchain `pip` floor to the Dependabot-safe release
  while preserving the global uv dependency freshness window with a
  package-specific security-update override.
- Preserved derived skill evidence precedence when enriching lint decisions and
  added a regression test for stale `skill_id` evidence.

## [0.2.0] - 2026-05-03

### Added

- Initial public release packaging with a real `pyproject.toml` build
  configuration, project metadata, and a `pytest` dev dependency group.
- MIT `LICENSE` plus SPDX copyright and license headers across first-party source and project files.
- Community and project policy documents: `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, and `SECURITY.md`.
- GitHub Actions CI workflow that runs tests on Python 3.11 and 3.13, builds
  the distribution, and uploads build artifacts.
- Dependabot configuration for `uv` dependencies and GitHub Actions updates.
- GitHub issue and pull request templates for bugs, feature requests, and contribution workflow guidance.
- Project discoverability docs, including a GitHub Pages landing page,
  comparison, integrations, threat model, trust signals, release process,
  discussions plan, examples, social preview, and recorded demo assets.
- OpenSSF Scorecard publishing workflow, GitHub artifact attestations for
  distributions, checksums, and SBOMs, plus a PyPI Trusted Publishing release
  workflow that keeps package upload credentials out of repository secrets.

### Changed

- Improved the Makefile with strict Bash execution, `make doctor`, clearer
  help/status output, configurable `GOFMT`, and formatting over every Go hook
  source file.
- Improved the README into a GitHub-style project page with quick start, workflow, configuration, and policy links.
- Improved package metadata with AI-agent, MCP, CEL, SARIF, DevSecOps,
  Git-hook, policy-as-code, static-analysis, documentation, and security
  discovery terms.
- Expanded repository documentation with current architecture analysis,
  generated artifact boundaries, Makefile workflows, hook runtime guidance, and
  verification contracts.
- Changed seeded `source_markdown` provenance to point at the generated
  repo-local `ETHOS.md` instead of an absolute local path.
- Expanded `.gitignore` with Python, cache, editor, and local tool state ignores suitable for public release.
- Added live GitHub project URLs to the package metadata.
- Replaced the older distribution provenance action with the current
  `actions/attest` workflow and documented attestation and SBOM verification.
