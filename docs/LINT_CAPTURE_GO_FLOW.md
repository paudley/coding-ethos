<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Managed Diagnostics And Lint Capture Flow

Managed capture is the single evidence path for linters, formatters,
type-checkers, and test runners that coding-ethos controls. A tool may be
invoked from a hook group, `make lint`, `make fix`, CI, or an explicit
`coding-ethos-run policy-tool ...` command, but the expected runtime contract is
the same: catalog-backed execution, separate stdout/stderr capture, parser
normalization, policy enrichment, trace retention, and SARIF-compatible
diagnostics.

Hook runner code must not implement parser-specific logic. It owns hook group
execution and report assembly. Parsing, stream handling, managed executable
resolution, formatter change detection, type-checker normalization, and test
output normalization belong in the managed capture and diagnostics packages.

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
7. The managed tool runs with the best available machine-readable output mode.
8. stdout and stderr are drained concurrently and retained separately, so
   verbose stderr cannot block a run that still has stdout open.
9. Diagnostics are parsed from both streams, enriched with compiled policy
   evidence maps, logged under `.coding-ethos/lint-runs`, and rendered as TOON
   or human output.

Parser aliases are catalog data. Wrapper tools such as autofixers can preserve
their own tool identity while parsing as the underlying tool. For example, a
Ruff autofix wrapper can report the managed wrapper name in trace metadata while
using the Ruff parser for diagnostics.

If a tool exits nonzero without parseable diagnostics, coding-ethos reports a
structured tool failure instead of dumping raw terminal output. The raw streams
remain in retained traces and SARIF properties for audit/debugging, but
user-facing output must stay bounded, actionable, and policy-addressable. SARIF
is the superset evidence channel; CEL sees only parsed and normalized facts that
are understood well enough to drive deterministic policy.

Go test capture uses the same path through the normal Go workflow. `make
go-test` runs managed `go test` capture, and `make go-e2e-test` runs the e2e
package with `go test`. That keeps Go test output in the normal parser, CEL
promotion, trace logging, and SARIF formatting path without creating a separate
compile-and-run test path. Test targets are not build targets.

Shell now only does stable process handoff. It does not own lint tool lists,
target resolution, config integrity, managed executable selection,
output-format coercion, or policy result rendering.

## Runtime Package Boundaries

The command binaries under `go/cmd/` are process entrypoints. They should stay
thin and call internal packages directly rather than shelling out to other
coding-ethos Go binaries.

Relevant owners:

| Concern | Package |
| --- | --- |
| Managed process capture | `go/internal/managedcapture` |
| Parser normalization | `go/diagnostics` |
| Hook group execution | `go/internal/hookrunnercli` |
| Lint CLI orchestration | `go/internal/lintcli` |
| Policy-tool dispatch | `go/cmd/coding-ethos-run` plus internal runtime packages |
| Tool definitions and parser aliases | `go/toolcatalog` |

Future tool integrations should add or improve catalog metadata and diagnostics
parsers before adding bespoke hookrunner code.
