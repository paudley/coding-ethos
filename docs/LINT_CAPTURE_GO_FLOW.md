<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Go Lint Capture Flow

The runtime flow is:

1. A small installed shim dispatches into the compiled coding-ethos runtime.
2. The Go dispatcher builds a `lintcapture.Request` from the tool name,
   original argv, invocation cwd, consumer repo root, ethos checkout root,
   managed tool path, output format, and trace root.
3. Go loads the merged consumer config once and derives package-relative lint
   source roots from policy data.
4. Go validates generated tool config integrity before the managed linter runs.
5. Go resolves lint targets against invocation cwd, consumer root, configured
   package roots, and globs while rejecting repo-escaping config.
6. Go resolves the managed executable or wrapper from `toolcatalog`
   capabilities, not host `PATH`.
7. The managed linter runs with forced machine-readable output.
8. Diagnostics are parsed, enriched with compiled policy evidence maps, logged
   under `.coding-ethos/lint-runs`, and rendered as TOON or human output.

Shell now only does stable process handoff. It does not own lint tool lists,
target resolution, config integrity, managed executable selection,
output-format coercion, or policy result rendering.
