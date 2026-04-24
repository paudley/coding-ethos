# coding-ethos-hooks

Lefthook hooks for coding-ethos bundles.

## Included Hooks

- **go-hooks/** - Fast generic file checks, shell checks, commitlint, commit attribution, direct-import enforcement, utility and SQL centralization, file and module doc checks, type-check orchestration, pytest gating, pyproject ignore enforcement, repo-root Python version consistency checks, shared hook policy, and the active Gemini AI review runner
- **check_complexity.py** - Cyclomatic complexity checks via Radon
- **check_maintainability.py** - Maintainability index checks via Radon
- **check_vulture.py** - Dead-code detection via Vulture

## Installation

Install Lefthook from the repository root that exposes `lefthook.yml`:

```bash
cd /path/to/repo
make -C code-ethos install-hooks
```

## Dependencies

- pyyaml >= 6.0
- go >= 1.26

## Development

Bundle policy now comes from the repo-root `config.yaml` plus an optional
consumer-root `repo_config.yaml`. Generated tool configs live at the consumer
repo root, and the Go hook runner reads that merged policy directly.
