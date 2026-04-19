<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial public release packaging with a real `pyproject.toml` build configuration, project metadata, and a `pytest` dev dependency group.
- MIT `LICENSE` plus SPDX copyright and license headers across first-party source and project files.
- Community and project policy documents: `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, and `SECURITY.md`.
- GitHub Actions CI workflow that runs tests on Python 3.11 and 3.13, builds the distribution, and uploads build artifacts.
- Dependabot configuration for `uv` dependencies and GitHub Actions updates.
- GitHub issue and pull request templates for bugs, feature requests, and contribution workflow guidance.

### Changed

- Improved the README into a GitHub-style project page with quick start, workflow, configuration, and policy links.
- Changed seeded `source_markdown` provenance to point at the generated repo-local `ETHOS.md` instead of an absolute local path.
- Expanded `.gitignore` with Python, cache, editor, and local tool state ignores suitable for public release.
- Added live GitHub project URLs to the package metadata.
