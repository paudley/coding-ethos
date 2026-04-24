<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Contributing to coding-ethos

Thanks for your interest in contributing. `coding-ethos` is an open source project maintained by Blackcat Informatics® Inc. We welcome improvements to code, tests, documentation, examples, and release tooling.

## Code of Conduct

Please read the [Code of Conduct](CODE_OF_CONDUCT.md) before participating. We expect respectful, professional collaboration. To report unacceptable behaviour, email <conduct@blackcat.ca>.

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
6. Run the verification steps below before requesting review.

## Development setup

### Prerequisites

- Git
- Python 3.11+
- `uv`

### Local setup

```bash
git clone https://github.com/<your-username>/<repo-name>.git
cd <repo-name>
uv sync --group dev --all-packages
make help
```

## Project-specific guidance

- Keep `coding_ethos.yml` and generated documentation examples aligned.
- If CLI behavior changes, update [README.md](README.md).
- If repo-overlay behavior changes, update [repo_ethos.example.yml](repo_ethos.example.yml).
- If output structure changes, update tests to cover the new contract.

## Verification

Before requesting review, make sure you:

- [ ] ran `uv run pytest`
- [ ] ran `make check-tool-configs` after changing `config.yaml`, `repo_config.example.yaml`, or tool-config generation logic
- [ ] ran `make check-gemini-prompts` after changing Gemini prompts, `coding_ethos.yml`, `repo_ethos.yml`, `config.yaml`, or `repo_config.example.yaml`
- [ ] ran `make validate` after changing files under `pre-commit/`
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
