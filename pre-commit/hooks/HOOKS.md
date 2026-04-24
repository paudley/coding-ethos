# coding-ethos-hooks

Lefthook hooks for coding-ethos bundles.

## Included Hooks

- **go-hooks/** - Fast generic file checks, shell checks, commitlint, commit attribution, shared hook policy, and the active Gemini AI review runner
- **check_pyproject_ignores.py** - Blocks adding linter file-ignore config in pyproject.toml
- **parallel_type_check.py** - Parallel type checking
- **validate_manifest.py** - Manifest validation

## Installation

Install Lefthook from the repository root that exposes `lefthook.yml`:

```bash
cd /path/to/repo
make -C code-ethos/pre-commit install-hooks
```

## Dependencies

- pyyaml >= 6.0
- go >= 1.26

## Development

Tool configurations are inherited from the repository root unless a consuming
repo overrides them.
