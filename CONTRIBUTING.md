<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Contributing to coding-ethos

Thanks for your interest in contributing. `coding-ethos` is an open source
project maintained by Blackcat Informatics® Inc. We welcome improvements to
code, tests, documentation, examples, and release tooling.

## Code of Conduct

Please read the [Code of Conduct](CODE_OF_CONDUCT.md) before participating. We
expect respectful, professional collaboration. To report unacceptable behaviour,
email <conduct@blackcat.ca>.

## Ways to contribute

### Report bugs

Before opening an issue:

- Search existing issues first
- Verify the problem on the latest code in `main` when possible
- Capture a minimal reproduction

When filing a bug, include:

- a clear title
- exact reproduction steps
- expected behavior
- actual behavior
- relevant command output or stack traces
- environment details such as OS, Python version, and `uv` version

### Suggest enhancements

Feature requests are welcome. Good enhancement reports usually include:

- the problem you are trying to solve
- the current limitation or workflow pain
- the proposed behavior
- concrete examples of the expected result

### Submit pull requests

1. Fork the repository and create a branch from `main`.
2. Install dependencies with `uv sync --group dev`.
3. Make the smallest coherent change that solves the problem.
4. Add or update tests when behavior changes.
5. Update docs and examples when outputs, flags, or workflow change.
6. Complete the CLA Assistant check if prompted on your pull request.
7. Run the verification steps below before requesting review.

## Contributor License Agreement

External contributions are accepted under the project Contributor License
Agreement, enforced by CLA Assistant:

https://gist.github.com/paudley/38386a03dd8b25d7c26cd9ce146219c1

When you open a pull request, CLA Assistant may comment with a signing link and
publish a status check. Follow that link and sign in with GitHub to accept the
agreement. After you accept it, CLA Assistant updates the pull request status.

The CLA confirms that you have the right to submit the contribution, grants the
project the rights needed to use and redistribute it, and does not require you
to provide support, updates, or future contributions.

The project also accepts Developer Certificate of Origin style sign-off trailers
as additional contribution evidence, but DCO sign-off does not replace the CLA
Assistant check when that check is required:

```text
Signed-off-by: Your Name <you@example.com>
```

For Blackcat Informatics® Inc. maintainers, `@paudley` and `@ErinAudley` are
directors of Blackcat Informatics® Inc. and are authorized to submit project
contributions on behalf of the company. The repository `CODEOWNERS` file lists
both maintainers as project owners.

## Governance and Continuity

`coding-ethos` is maintained by Blackcat Informatics® Inc. The project owners
make decisions through GitHub issues, pull requests, discussions, and release
reviews, using the repository ETHOS, documented quality bar, security policy,
and release process as the decision framework.

The current project owners are listed in `.github/CODEOWNERS`:

- `@paudley`
- `@ErinAudley`

Both project owners are directors of Blackcat Informatics® Inc. and have
authority to administer the repository for the company. This shared ownership
is the project's continuity mechanism: if one owner becomes unavailable, the
other can continue issue triage, pull request review, repository
administration, and release management.

## Development setup

### Prerequisites

- Git
- Python 3.13+
- `uv`

### Local setup

```bash
git clone https://github.com/<your-username>/<repo-name>.git
cd <repo-name>
make install
make doctor
make help
```

## Project-specific guidance

- Keep `coding_ethos.yml` and generated documentation examples aligned.
- If CLI behavior changes, update [README.md](README.md).
- If repo-overlay behavior changes, update [repo_ethos.example.yml](repo_ethos.example.yml).
- If enforcement config behavior changes, update [repo_config.example.yaml](repo_config.example.yaml).
- If hook behavior changes, update
  [pre-commit/PRE-COMMIT.md](pre-commit/PRE-COMMIT.md) or
  [pre-commit/hooks/HOOKS.md](pre-commit/hooks/HOOKS.md).
- If output structure changes, update tests to cover the new contract.
- If `repo_ethos.yml` or renderer behavior changes, regenerate the checked-in agent docs.

## Coding Style

Contributions should follow the standard style guides for the primary
languages used by the project and must pass the managed style and lint tools
configured in this repository.

- Python follows [PEP 8](https://peps.python.org/pep-0008/) and the stricter
  project rules enforced by Ruff, mypy, Pyright, Pylint, and the generated
  Python tool configs.
- Go follows `gofmt`, `go vet`, [Effective Go](https://go.dev/doc/effective_go),
  and [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments), with
  additional checks through the project Go test suites and generated
  `golangci-lint` config.
- Shell follows ShellCheck and `shfmt` guidance. Prefer moving durable shell
  behavior into Go when the logic is part of hook, policy, or managed-toolchain
  runtime.
- YAML, TOML, SQL, GitHub Actions, and container/config files follow the
  generated configs for yamllint, Tombi, SQLFluff, actionlint, and hadolint.

The canonical verification command is `make check`. For generated config or
hook-runtime changes, also run the narrower checks listed below.

## Verification

Before requesting review, make sure you:

- [ ] ran `uv run pytest`
- [ ] ran `make doctor` after changing Makefile tool resolution or hook path logic
- [ ] ran `make check` for the current repository gate
- [ ] ran `make check-tool-configs` after changing `config.yaml`,
  `repo_config.example.yaml`, or tool-config generation logic
- [ ] ran `make check-gemini-prompts` after changing Gemini prompts,
  `coding_ethos.yml`, `repo_ethos.yml`, `config.yaml`, or
  `repo_config.example.yaml`
- [ ] ran `make validate` after changing files under `pre-commit/`
- [ ] ran `make generate` after changing `coding_ethos.yml`, `repo_ethos.yml`, or generated-doc rendering behavior
- [ ] updated tests for any behavioral change
- [ ] updated `README.md` if usage, flags, or outputs changed
- [ ] updated `repo_ethos.example.yml` if repo overlay behavior changed

## Commit messages

We prefer [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` new functionality
- `fix:` bug fixes
- `docs:` documentation-only changes
- `refactor:` internal restructuring without behavior change
- `test:` test additions or updates
- `chore:` maintenance work

Examples:

```text
feat: add repo overlay path aliases
fix: preserve existing claude imports during inject merge
docs: clarify llm merge workflow
test: cover symlink replacement for ETHOS.md
```

## Questions

For public questions, open an issue or discussion in the repository. For private matters, email <oss@blackcat.ca>.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
