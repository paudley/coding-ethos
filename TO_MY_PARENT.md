<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# TO_MY_PARENT.md

This checkout exports a parent-repo workflow for consumers that install
`coding-ethos/` as a submodule.

## Interface

Prefer the Go runner as the stable interface:

```sh
coding-ethos/bin/coding-ethos-run parent-install
coding-ethos/bin/coding-ethos-run parent-check
coding-ethos/bin/coding-ethos-run parent-lint
```

The Make targets are thin aliases:

```sh
make -C coding-ethos parent-install
make -C coding-ethos parent-check
make -C coding-ethos parent-lint
```

The runner auto-detects the parent repository when Git reports this checkout as
a submodule. Use `--repo /path/to/repo` only when that detection is wrong.

## Parent Inputs

The parent repository may provide:

- `repo_ethos.yml` or `repo_ethos.yaml`
- `repo_config.yaml` or `repo_config.yml`

Parent repos can declare conservative defaults with `repo.kind` or `profiles`:

```yaml
repo:
  kind: go-static-site
profiles:
  - generated-site-output
```

Explicit `repo_config.yaml` settings override these profile defaults.

`parent-install` syncs generated parent artifacts and atomically refreshes the
compiled executables in the parent repository's common
`.git/coding-ethos-hooks/bin/` runtime. `parent-check` verifies those artifacts
without rewriting them, including byte identity and executable independence
from any retiring worktree. `parent-lint` syncs the parent artifacts, then runs
the full parent lint scope through the compiled policy bundle.

## Output Contract

The parent commands emit TOON. Successful install/check output is intentionally
small: status, repo, and the artifact classes touched. Lint output is the normal
coding-ethos TOON lint report.

## Generated Parent Artifacts

The parent repo should make an explicit policy choice for generated artifacts:

| Artifact | Default expectation |
| --- | --- |
| `.agents/` | commit when agent skill surfaces are shared by the team |
| `.claude/` | commit when Claude settings are part of the repo contract |
| `.codex/` | commit when Codex settings are part of the repo contract |
| `.gemini/` | commit when Gemini settings are part of the repo contract |
| `.mcp.json` | commit when MCP wiring is part of the repo contract |
| `.coding-ethos/gemini/prompt-pack.json` | commit |
| `.coding-ethos/tool-config-hashes.json` | commit |
| generated tool configs | commit unless the parent repo documents them as local-only |

If a parent repo chooses local-only generated workspace files, it should record
that policy in `repo_config.yaml` and `.gitignore` together.
