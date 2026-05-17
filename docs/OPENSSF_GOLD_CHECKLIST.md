<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# OpenSSF Gold Checklist

`coding-ethos` targets the OpenSSF Best Practices **Gold** badge. The badge is
used as a project-improvement checklist: when a criterion is unmet or unknown,
the preferred response is to improve the repository, governance, CI, release
process, or documentation until the answer is true.

The root `.bestpractices.json` file is the durable machine-readable source for
repo-hosted Best Practices proposals. Keep it aligned with the public project
record and ask the Best Practices site to reanalyze the repository when the
project is saved with automation enabled. Query-string prefill URLs are not a
supported workflow for this repo; they proved too fragile and can silently fail
to apply the intended evidence.

## Current Repo-Side Remediations

- Reproducible-build evidence is documented in
  `docs/BUILD_REPRODUCIBILITY.md`.
- Go build commands use `-trimpath` and `-buildvcs=false` through
  `GO_BUILD_FLAGS`.
- Security assurance evidence is documented in
  `docs/SECURITY_ASSURANCE_CASE.md`.
- Contribution, governance, CLA, code review, testing, and release expectations
  are linked from `CONTRIBUTING.md`, `SECURITY.md`, `docs/RELEASE.md`, and
  `docs/TRUST_SIGNALS.md`.

## Remaining Gold Gaps

These are intentionally not papered over by repo-local evidence:

- `contributors_unassociated`: needs at least two unassociated significant
  contributors. This requires real project participation, not a document.
- `homepage_url` and `report_url`: present in the project JSON, but not valid
  Metal-series automation proposal criteria. Review and fill these manually in
  the Best Practices UI if they are shown as unknown.
- `two_person_review`: needs enough independent reviewer capacity for at least
  50% of modifications to receive non-author review before release.
- `dynamic_analysis_enable_assertions`: release-gated fuzzing is now in place,
  but the project does not yet maintain a broad assertion-enabled dynamic
  analysis configuration.
- `test_branch_coverage80`: branch coverage is not yet measured as a release
  gate.
- `test_statement_coverage90`: the project currently enforces 80% statement
  coverage, not 90%.

The following repo-side gaps were remediated by public docs and
`.bestpractices.json` evidence:

- `copyright_per_file` and `license_per_file`
- `crypto_algorithm_agility`
- `crypto_certificate_verification`
- `crypto_credential_agility`
- `crypto_tls12`
- `crypto_used_network`
- `crypto_verification_private`
- `hardened_site`
- `hardening`
- `require_2FA`
- `secure_2FA`
- `small_tasks`
- `signed_releases`
- `version_tags_signed`

Current Gold count after repo-side remediation:

- `Met`: 115
- `N/A`: 9
- `Unmet`: 5
- `?`: 2

## Baseline Criteria

The public project JSON also includes OSPS Baseline criteria. Track those in
`.bestpractices.json` if they become part of the project target.
