<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Supply-Chain Attestations

`coding-ethos` publishes trust evidence in layers. The goal is not to claim
that an attested artifact is automatically safe; the goal is to make the source,
workflow, identity, digest, and release path verifiable.

## Current Policy

- OpenSSF Scorecard runs on `main`, weekly, branch-protection changes, and
  manual dispatch. Results are published to the public Scorecard API and
  uploaded as SARIF.
- GitHub Actions workflows pin external actions to immutable commit SHAs. The
  release checklist requires reviewing and refreshing those SHAs deliberately
  instead of floating on version tags.
- Distribution builds validate package metadata before upload.
- Python source and wheel distributions receive GitHub artifact attestations
  with SLSA provenance through `actions/attest`.
- Distribution checksums are generated separately and attested separately.
- An SPDX JSON SBOM is generated with Syft through Anchore's SBOM action,
  uploaded as a durable artifact, attached to GitHub releases before
  publication, and attested against the distribution checksum subjects.
- Offline `.intoto.jsonl` attestation bundles are exported from GitHub
  artifact attestations and attached to releases for consumers and ecosystem
  scanners that inspect release assets directly.
- PyPI upload uses Trusted Publishing through a dedicated `pypi` GitHub
  environment. No PyPI API token belongs in repository secrets.
- PyPI publish attestations are produced by `pypa/gh-action-pypi-publish` and
  uploaded with the release files.

## Why These Controls

GitHub artifact attestations bind a subject path or checksum to an in-toto
statement signed through Sigstore using the workflow's OIDC identity. That gives
consumers a verifiable link from an artifact digest back to the repository,
workflow, commit, and event that produced it.

PyPI Trusted Publishing removes long-lived upload credentials from GitHub
secrets. PyPI digital attestations then bind the uploaded release files to the
Trusted Publisher identity that performed the upload.

OpenSSF Scorecard is a public, repeatable health signal. The project publishes
Scorecard results so the badge and API reflect a current repository-owned run
instead of waiting for external ecosystem scans.

SBOMs complement provenance. Provenance says where and how an artifact was
built; the SBOM records the component inventory used for dependency review,
license review, vulnerability matching, and downstream audit.

## Verification Commands

Verify GitHub artifact attestations for release artifacts:

```bash
gh attestation verify dist/coding_ethos-*.tar.gz \
  --repo paudley/coding-ethos
gh attestation verify dist/coding_ethos-*.whl \
  --repo paudley/coding-ethos
gh attestation verify dist-checksums/SHA256SUMS \
  --repo paudley/coding-ethos
```

Verify the SBOM attestation bound to the release artifact checksums:

```bash
gh attestation verify dist/coding_ethos-*.whl \
  --repo paudley/coding-ethos \
  --predicate-type https://spdx.dev/Document
```

Download offline attestation bundles from a release and verify them with the
artifact they describe:

```bash
gh release download v0.1.1 \
  --repo paudley/coding-ethos \
  --pattern '*.intoto.jsonl'
gh attestation verify dist/coding_ethos-*.whl \
  --repo paudley/coding-ethos \
  --bundle coding_ethos-*.whl.intoto.jsonl
```

Verify checksums:

```bash
sha256sum --check dist-checksums/SHA256SUMS
```

Verify PyPI publish attestations after a release with a concrete distribution
file URL from PyPI:

```bash
uvx pypi-attestations verify pypi \
  --repository https://github.com/paudley/coding-ethos \
  https://files.pythonhosted.org/.../coding_ethos-0.1.0-py3-none-any.whl
```

Inspect the public Scorecard result:

```bash
curl -fsS https://api.scorecard.dev/projects/github.com/paudley/coding-ethos
```

## Release Workflow Contract

The release workflow is intentionally split:

- `build` creates the distributions, validates metadata, generates checksums,
  generates the SPDX JSON SBOM, creates GitHub provenance and SBOM
  attestations, and uploads workflow artifacts.
- `publish-github-release` downloads the distributions, checksum file, and
  SBOM; exports offline `.intoto.jsonl` attestation bundles; creates a draft
  GitHub release; attaches every release asset; and only then publishes the
  release.
- `publish-pypi` downloads only the distributions and publishes through PyPI
  Trusted Publishing from the protected `pypi` environment.

Keep these responsibilities separate. The PyPI publishing job should stay
small, run on GitHub-hosted Ubuntu, use OIDC, and avoid unrelated build or
mutation steps. The GitHub release job must attach assets before publication so
immutable releases contain the distributions, checksums, SBOM, and offline
attestation bundles from the start.

The same workflow also supports manual `workflow_dispatch` dry runs. A manual
dry run executes the build job, package metadata validation, checksum
generation, SBOM generation, GitHub provenance attestations, SBOM attestation,
and artifact uploads. It intentionally skips GitHub release creation and PyPI
publication so the release identity can be tested before creating or publishing
a real release.

## First-Release Setup

Before the first PyPI release, run the local release dry run:

```bash
make release-dry-run
```

Then run the GitHub release workflow manually with `dry_run` enabled. Download
the `dist`, `dist-checksums`, and `dist-sbom` artifacts from that run and verify
the generated attestations before publishing the first release.

Then configure a PyPI Trusted Publisher:

- PyPI project: `coding-ethos`
- GitHub owner: `paudley`
- GitHub repository: `coding-ethos`
- Workflow filename: `release.yml`
- Environment: `pypi`

Configure the GitHub `pypi` environment before the first public release. If the
project has multiple maintainers, use environment reviewers to keep release
publication as a deliberate approval step even though the credential exchange is
automated.

The environment can be created from the CLI:

```bash
gh api repos/paudley/coding-ethos/environments/pypi \
  --method PUT \
  --field wait_timer=0
```

Add required reviewers through the GitHub environment settings or with the
GitHub API after resolving the maintainer or team reviewer IDs.
