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
uv build
uvx twine check dist/*
```

Do not upload to PyPI until release artifacts have provenance. The GitHub
Actions build distribution job validates package metadata with `uvx twine
check dist/*` and uses GitHub artifact attestations for `dist/*`. Treat those
attestations as the prerequisite for any certified PyPI upload.

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

## Known Limitations

-
```

## Post-Release Checklist

- [ ] Create the GitHub release.
- [ ] Attach artifacts and checksums if applicable.
- [ ] Verify release links from `README.md` and package metadata.
- [ ] Update OpenSSF Scorecard and Best Practices tracking in
  `docs/TRUST_SIGNALS.md`.
- [ ] Announce the release using copy maintained outside the repo.
