<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Gold Security Posture

This document records OpenSSF Best Practices Gold security posture decisions
that depend on repository policy, cryptography usage, hosted-site hardening, and
release signing. It exists so badge answers are backed by public project
evidence instead of one-off form text.

## Cryptography

`coding-ethos` does not implement custom cryptographic protocols. It relies on
publicly reviewed mechanisms provided by supported platforms and libraries:

- HTTPS/TLS for GitHub, PyPI, Best Practices, and other remote service access.
- GitHub OIDC and Sigstore-backed artifact attestations for provenance.
- PyPI Trusted Publishing and PyPI digital attestations for package upload.
- SHA-256 checksums for release artifact integrity.
- OpenPGP only as an optional encrypted vulnerability-reporting channel listed
  in `SECURITY.md`.

The project must not introduce private cryptographic algorithms or home-grown
protocols. If a future feature needs cryptography, the design must identify the
published protocol or library, algorithm agility path, credential rotation path,
certificate/TLS verification behavior, and tests that prove verification is not
disabled.

## Certificate And TLS Verification

Repository-owned code that accesses remote HTTPS services must use standard
TLS certificate verification. It must not disable certificate verification,
trust arbitrary custom roots, or accept plaintext downgrade paths.

## Credential Agility

Long-lived release credentials are avoided where supported:

- PyPI publishing uses OIDC Trusted Publishing instead of a PyPI API token.
- GitHub artifact attestations use GitHub Actions OIDC identity.
- Security reports can use the published OpenPGP key, and the security policy
  remains the public location for rotating that key if needed.

## Hosted-Site Hardening

The public project site is GitHub Pages generated from the same Markdown docs
stored in the repository. The site is static: it has no login, no cookies, no
server-side application state, and no project-managed password database.

Security expectations:

- serve the site only over HTTPS;
- keep active content minimal;
- avoid third-party script injection for project-critical docs;
- keep source docs usable directly from the repository if the hosted site is
  unavailable or if a user wants to avoid a browser.

GitHub Pages controls most HTTP response headers for the hosted site. Because
the project does not operate a custom web server for the docs site, unsupported
headers such as a custom Content-Security-Policy are tracked as platform limits
rather than repo-owned runtime code.

## Repository Account Security

GitHub two-factor authentication is enabled for the account that administers
the repository. As the project grows into a multi-maintainer or organization
model, repository administration should require GitHub 2FA for every account
with write, maintain, or admin access.

## Release Signing And Tags

The release process requires signed `v*` tags. Release artifacts are then built
by GitHub Actions from that tag and published with checksums, SBOM output,
GitHub artifact attestations, offline `.intoto.jsonl` bundles, and PyPI
Trusted Publishing attestations.

Before claiming a release-signing criterion as met, verify the public release
tag with:

```bash
git tag -v vX.Y.Z
```

If a release tag is not signed and verified, do not mark the signed-release or
signed-version-tag criteria as met for that release.
