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
