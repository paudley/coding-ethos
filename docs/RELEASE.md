<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Release Process

This document describes the public release path for `coding-ethos`.

## Versioning

`coding-ethos` follows semantic versioning:

- Patch releases fix bugs, docs, CI, or packaging without changing supported
  behavior.
- Minor releases add features or new policy surfaces in a backward-compatible
  way.
- Major releases may change generated config contracts, hook behavior, or
  policy bundle compatibility.

The Python package version lives in `pyproject.toml`.

## Pre-Release Checklist

- [ ] Confirm the branch is not `main`.
- [ ] Update `CHANGELOG.md`.
- [ ] Update `pyproject.toml` version when cutting a release.
- [ ] Run `make check`.
- [ ] Run generated config verification for any changed config surfaces.
- [ ] Verify GitHub Actions and SARIF gates are green on the release PR.
- [ ] Verify OpenSSF Scorecard has a recent successful run on `main`.
- [ ] Refresh pinned GitHub Action SHAs after reviewing upstream release notes
  for each action used by `.github/workflows/*.yml`.
- [ ] Verify `[tool.uv].exclude-newer = "7 days"` remains present in every
  project `pyproject.toml` before refreshing lock files.
- [ ] Review any `[tool.uv].exclude-newer-package` override and keep only
  package-specific exceptions required for security updates inside the global
  dependency cooldown window.
- [ ] Verify `SECURITY.md`, `README.md`, and `docs/index.md` still describe
  the supported install and reporting paths.
- [ ] Confirm no local paths, secrets, or generated runtime outputs are staged.

## Build Artifacts

The supported local build path is:

```bash
make build
```

The supported validation path is:

```bash
make check
```

If publishing Python distributions:

```bash
make release-dry-run
```

Before publishing, run the GitHub `Release` workflow manually with `dry_run`
enabled. That exercises the hosted release job, OIDC permissions, GitHub
artifact attestations, checksum generation, and SBOM generation while skipping
GitHub release creation and PyPI publication.

Do not upload to PyPI until release artifacts have provenance. The GitHub
Actions build distribution job validates package metadata with `uvx twine
check dist/*.tar.gz dist/*.whl`, generates SHA-256 checksums, and uses GitHub
artifact attestations for both `dist/*.tar.gz`/`dist/*.whl` and
`dist-checksums/SHA256SUMS`. It also generates an SPDX JSON SBOM at
`sbom/coding-ethos.spdx.json` and creates an SBOM attestation bound to the
distribution artifact checksums. Treat those attestations as the prerequisite
for any certified PyPI upload.

The supported release path is the `.github/workflows/release.yml` workflow
triggered by pushing a signed `v*` tag. The workflow builds and attests
artifacts, exports offline `.intoto.jsonl` attestation bundles, creates a draft
GitHub release with the distributions, checksums, SBOM, and attestation bundles
already attached, publishes the release, then publishes to PyPI through the
`pypi` GitHub environment. This ordering is required for GitHub immutable
releases: assets must be attached before publication rather than uploaded after
the release is published.

PyPI upload uses OIDC Trusted Publishing through
`pypa/gh-action-pypi-publish`, and enables PyPI digital attestations. Configure
the corresponding Trusted Publisher in PyPI before cutting a release:

- owner: `paudley`
- repository: `coding-ethos`
- workflow: `release.yml`
- environment: `pypi`

Create the GitHub environment before the first release:

```bash
gh api repos/paudley/coding-ethos/environments/pypi \
  --method PUT \
  --field wait_timer=0
```

Then configure required reviewers in the GitHub UI or with the GitHub API if
the project has more than one maintainer. PyPI must also be configured with the
Trusted Publisher tuple above before the workflow can publish without an API
token.

Consumers can verify GitHub artifact attestations with:

```bash
gh attestation verify dist/coding_ethos-*.tar.gz \
  --repo paudley/coding-ethos
gh attestation verify dist/coding_ethos-*.whl \
  --repo paudley/coding-ethos
gh attestation verify dist-checksums/SHA256SUMS \
  --repo paudley/coding-ethos
gh attestation verify dist/coding_ethos-*.whl \
  --repo paudley/coding-ethos \
  --predicate-type https://spdx.dev/Document
```

After a PyPI release, verify PyPI publish attestations with the current PyPI
attestation tooling and a concrete distribution file URL from PyPI:

```bash
uvx pypi-attestations verify pypi \
  --repository https://github.com/paudley/coding-ethos \
  https://files.pythonhosted.org/.../coding_ethos-0.1.0-py3-none-any.whl
```

If publishing compiled Go helper binaries, attach checksums and document:

- target OS and architecture
- build command
- source commit
- checksum algorithm
- whether the artifact is required or optional

## Release Notes

Release notes should include:

- summary of user-visible changes
- supported upgrade path
- new or changed hooks, MCP tools, CEL inputs, SARIF behavior, or generated
  config
- migration notes for consumer repos
- verification evidence
- known limitations

Template:

```markdown
## Summary

-

## Added

-

## Changed

-

## Migration Notes

-

## Verification

- `make check`
- GitHub Actions CI
- Coding Ethos SARIF Gate
- Build distribution and artifact attestation
- SPDX SBOM artifact and SBOM attestation
- OpenSSF Scorecard
- PyPI Trusted Publishing attestations

## Known Limitations

-
```

## Post-Release Checklist

- [ ] Push the signed release tag.
- [ ] Verify the release workflow created the GitHub release.
- [ ] Verify GitHub release distributions, checksums, SBOM, and `.intoto.jsonl`
  attestation bundles were attached before publication by the release workflow.
- [ ] Verify PyPI shows digital attestations for the uploaded release files.
- [ ] Verify release links from `README.md` and package metadata.
- [ ] Update OpenSSF Scorecard and Best Practices tracking in
  `docs/TRUST_SIGNALS.md`.
- [ ] Announce the release using copy maintained outside the repo.
