# coding-ethos-hooks

Lefthook utilities for coding-ethos bundles.

## Overview

This package provides shared utilities for coding-ethos Lefthook hooks.

## Available Hooks

The parent `pre-commit/hooks/` directory contains the actual hook implementations:

- `go-hooks/` - Generic file checks, shell checks, commitlint, commit attribution, shared hook policy, and the active Gemini AI review runner
- `check_init_docs.py` - Enforce module documentation
- `check_complexity.py` - Cyclomatic complexity checks
- `check_maintainability.py` - Maintainability index checks
- `parallel_type_check.py` - Parallel pyright/mypy execution
- `validate_manifest.py` - Manifest YAML validation

## Installation

Hooks are installed via Lefthook. See `lefthook.yml` for configuration.
