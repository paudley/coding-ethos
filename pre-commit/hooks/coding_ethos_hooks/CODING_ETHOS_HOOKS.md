# coding-ethos-hooks

Lefthook utilities for coding-ethos bundles.

## Overview

This package provides shared utilities for coding-ethos Lefthook hooks.

## Available Hooks

The parent `pre-commit/hooks/` directory contains the actual hook implementations:

- `go-hooks/` - Generic file checks, shell checks, commitlint, commit attribution, direct-import enforcement, utility and SQL centralization, file and module doc checks, type-check orchestration, pytest gating, comment suppression enforcement, manifest and plan validation, pyproject ignore enforcement, repo-root Python version consistency checks, shared hook policy, and the active Gemini AI review runner
- `check_complexity.py` - Cyclomatic complexity checks
- `check_maintainability.py` - Maintainability index checks
- `check_vulture.py` - Dead-code checks

## Installation

Hooks are installed from the repository root with `make install-hooks` or, in a
consuming repo, `make -C code-ethos install-hooks`. The bundle keeps its pinned
Lefthook binary repo-local and reads merged policy from the repo-root
`config.yaml` plus optional consumer override YAML.
