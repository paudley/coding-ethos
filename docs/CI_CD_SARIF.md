<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# CI/CD And SARIF

Local hooks are the first gate. CI is the independent gate that still runs when
a developer or agent bypasses local enforcement.

`coding-ethos` emits SARIF from the same normalized diagnostics used by hook
and lint-capture output. SARIF results include stable policy IDs, source tool
codes, ETHOS principle IDs, skill IDs, file and line locations, remediation
advice, and audit properties.

## Output Contract

Use `--sarif` on lint result paths:

```bash
pre-commit/hooks/run-go-hook.sh policy-lint --sarif --scope files --files path/to/file.py
pre-commit/hooks/run-go-hook.sh policy-lint --sarif --replay .coding-ethos/lint-runs/<trace>.json
```

Captured managed tools can also emit SARIF:

```bash
pre-commit/hooks/run-go-hook.sh policy-lint --managed-capture-tool ruff --sarif -- check path/to/file.py
```

SARIF is intentionally not supported for explanatory or aggregate analysis
commands such as `--explain` and `--analyze-log`; those are operator summaries,
not per-result diagnostic reports.

## Reusable GitHub Workflow

The reusable workflow at `.github/workflows/coding-ethos-sarif.yml` builds the
managed runtime, runs the configured project gate, emits SARIF, uploads it to
GitHub code scanning, and preserves SARIF plus `.coding-ethos` traces as
workflow artifacts. Keeping the reusable component under `.github/workflows/`
means the repo dogfoods it directly and validates it with managed `actionlint`.

```yaml
name: coding-ethos

on:
  pull_request:
  push:
    branches: [main]

jobs:
  policy:
    uses: ./coding-ethos/.github/workflows/coding-ethos-sarif.yml
    permissions:
      contents: read
      security-events: write
    with:
      coding-ethos-path: coding-ethos
      repo-root: .
      gate-command: make -C coding-ethos check
```

If the repository does not check out `coding-ethos` as a submodule, replace the
`make -C coding-ethos` paths with the repository-local install path used by the
project.

## Reusable GitLab CI Template

The reusable GitLab template at `ci/gitlab/coding-ethos-sarif.yml` exposes a
hidden `.coding_ethos_sarif` job. Consumers include it, extend it, and override
variables only when their checkout layout differs.

```yaml
include:
  - local: coding-ethos/ci/gitlab/coding-ethos-sarif.yml

coding_ethos:
  extends: .coding_ethos_sarif
  image: golang:1.24
```

GitLab runners should use the same compiled bundle and managed toolchain as
local hooks, then keep SARIF as a job artifact. Projects can wire the artifact
into their own merge-request annotation or security-reporting layer as needed.

## Operator Rules

- CI must run `make build` before policy commands so the compiled bundle,
  generated tool configs, managed binaries, MCP settings, skills, and hook
  runtime are in sync.
- CI should run `make check` or the project-specific equivalent as the blocking
  gate. SARIF upload is an audit and review surface, not the only enforcement
  mechanism.
- SARIF artifacts should be retained with `.coding-ethos` traces so reviewers
  can replay failures without rerunning the underlying tools.
