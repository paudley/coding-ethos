<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# tests

`tests/` covers the supported public API and the repo’s generated-output
contract.

The suite focuses on Markdown seeding, CLI flows, generation semantics, and
Gemini prompt-pack synchronization so behavior changes fail in a narrow,
explainable way.

When output layout, package exports, or generator behavior changes, update or
extend these tests in the same change so the repo contract remains executable.

## Test Areas

- `test_cli.py`: Markdown seeding, primary YAML validation, agent doc
  generation, merge-preserving writes, tool-config sync, and drift detection.
- `test_gemini_prompt_pack.py`: prompt-pack rendering, repo context grounding,
  enforcement notes, sync behavior, and drift detection.
- `test_yaml_utils.py`: YAML formatting helpers, comment preservation, and
  folded prose rendering.

## Verification Commands

Use the Makefile target unless a narrower test is needed:

```bash
make test
```

For the full current repo gate:

```bash
make check
```

`make check` runs tests plus generated tool-config and Gemini prompt-pack drift
checks. When hook runtime changes are involved, also run:

```bash
make validate
make go-test
```
