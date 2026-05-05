<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# OpenSSF Gold Checklist

`coding-ethos` targets the OpenSSF Best Practices **Gold** badge. The badge is
used as a project-improvement checklist: when a criterion is unmet or unknown,
the preferred response is to improve the repository, governance, CI, release
process, or documentation until the answer is true.

The root `.bestpractices.json` file is the durable machine-readable source for
repo-hosted Best Practices proposals. It mirrors the repo-backed evidence in
`docs/best_practices_prefill.json` and can be reanalyzed by the Best Practices
site when the project is saved with automation enabled.

Generate the current human-reviewed fallback prefill URLs and gap report:

```bash
make best-practices-prefill
```

The generator combines the public Best Practices project JSON for project
`12737` with checked-in repo proposals from `docs/best_practices_prefill.json`.
By default, generated URLs include only checked-in proposal entries. Use
`--include-current` if you intentionally want to include already-recorded badge
answers in the URL as well. The Best Practices edit endpoint rejects very long
URLs, so the default Makefile target chunks proposals into bounded URLs. Open
the emitted `url[1]`, review and save the changes in the Best Practices UI,
then repeat for each later chunk. The generator routes proposals to the owning
Best Practices edit section; Silver criteria must be proposed on `/silver/edit`
and Gold criteria on `/gold/edit`, otherwise the site silently ignores them.
The generator intentionally omits unknown `?` answers from generated URLs
because the Best Practices URL interface treats `?` as an explicit reset
operation.

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

These are intentionally not papered over by the prefill generator:

- `contributors_unassociated`: needs at least two unassociated significant
  contributors. This requires real project participation, not a document.
- `homepage_url` and `report_url`: present in the project JSON, but not valid
  Metal-series automation proposal criteria. Review and fill these manually in
  the Best Practices UI if they are shown as unknown.
- `two_person_review`: needs enough independent reviewer capacity for at least
  50% of modifications to receive non-author review before release.
- `test_branch_coverage80`: branch coverage is not yet measured as a release
  gate.
- `test_statement_coverage90`: the project currently enforces 80% statement
  coverage, not 90%.

The following repo-side gaps were remediated by public docs and prefill
evidence:

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

Current generated Gold count after repo-side remediation:

- `Met`: 116
- `N/A`: 9
- `Unmet`: 4
- `?`: 2

## Baseline Criteria

The public project JSON also includes OSPS Baseline criteria. Those are not
included in Gold URLs or Gold unresolved counts by default. To include them in
the report and generated URL, run:

```bash
python tools/best_practices_prefill.py --section baseline-1 --include-baseline
```
