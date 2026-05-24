---
name: "managed-lint-workflow"
description: "Use when an agent needs to run lint, formatting, type checks, or policy-backed diagnostics. Prefer the MCP managed_lint tool, then the ./bin/lint wrapper, instead of invoking raw Ruff, mypy, uvx, host linters, or package-manager shortcuts."
metadata:
  source: coding_ethos.yml
  generated_by: coding-ethos
  ethos_principles:
    - static-analysis-is-the-first-line-of-defense
    - linting-as-code-quality-enforcement
    - one-path-for-critical-operations
    - validation-at-the-gate
    - radical-visibility
---
<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Managed Lint Workflow

Use this skill to choose the highest-signal lint path before invoking any checker. The goal is to keep agents on the coding-ethos managed linter path so formatting, packaged tools, trace capture, code-intel updates, and policy advice all stay connected.

## ETHOS Grounding
- `static-analysis-is-the-first-line-of-defense`: Make ruff and mypy blocking quality gates rather than advisory tools.
- `linting-as-code-quality-enforcement`: Resolve lint findings with structural fixes; suppress only with documented necessity.
- `one-path-for-critical-operations`: Keep one explicit, validated path for critical operations.
- `validation-at-the-gate`: Validate configuration, schema, and extensions during bootstrap rather
than on first use.
- `radical-visibility`: Log important decisions with context and instrument the system with metrics.

## Short Hint
Use MCP managed_lint first. It runs packaged linters, applies managed formatting where configured, records traces, updates code-intel evidence, and preserves repo-specific policy context.

## Use When
- run lint
- managed_lint
- ./bin/lint
- coding-ethos-run lint
- uvx ruff
- raw linter
- format code
- type check
- lint workflow

## Remediation Workflow
1. Prefer MCP managed_lint for agent-run checks. Pass scope, files, tool, argv, and cwd through the MCP tool instead of running shell linters.
2. If MCP is unavailable, run ./bin/lint with the narrowest useful scope, such as ./bin/lint --staged, ./bin/lint --changed, or ./bin/lint --full.
3. Do not run uvx ruff, host ruff, host mypy, package-local eslint, or other raw linters unless explicitly debugging the managed toolchain itself.
4. Use managed lint output as the source of truth because it carries normalized diagnostics, skill hints, trace IDs, policy mappings, and code-intel ingestion evidence.
5. After fixing findings, rerun the same managed_lint or ./bin/lint scope and report the exact command or MCP arguments plus any remaining diagnostics.

## Principle Details
### Static Analysis is the First Line of Defense

We rely on linters (ruff) and type checkers (mypy) to catch errors
before the code ever runs.

Directive: Make ruff and mypy blocking quality gates rather than advisory tools.

Quick ref:
- Make ruff and mypy blocking quality gates rather than advisory tools.
- We rely on linters (ruff) and type checkers (mypy) to catch errors
before the code ever runs.
- Treat repo hook and CI gates as authoritative, not optional reminders.

#### Overview
We rely on linters (ruff) and type checkers (mypy) to catch errors
before
the code ever runs.

* We do not suppress linter errors unless absolutely necessary.
* Type hints are mandatory, not optional.
* If the CI pipeline fails static analysis, the code is effectively
  broken.

#### Machine-Enforced Gates
Static analysis belongs in enforced local hooks and CI, not in
optional
tribal knowledge.

* Repos should run their canonical lint and type suites automatically
  before
  code lands.
* If a repo exposes pre-commit, pre-push, or CI static analysis gates,
  treat
  them as part of the engineering contract.
* Passing the current local gate matters more than saying a previous
  run was
  green.

#### Repo Addendum
This repo currently documents `make check` as its canonical automated
gate.

Focused commands such as `make test` or `uv run pytest` are useful
during development, but release and review readiness must use the
documented Makefile gate instead of a local preference.

This repo exists to bring consistent policy, settings, and linters to
diverse consuming repos. Treat each consuming repo as an untrusted
execution target: its binaries, `PATH`, aliases, shell state,
`pyproject.toml`, `uv` project state, and same-named tool config files
are minefields, not sources of authority.

Captured lint commands must run coding-ethos-managed tool versions from
the controlled hook project, with explicit coding-ethos-generated config
flags. Resolve the caller's target paths and globs first, then execute
the managed linter with those resolved targets. Do not execute the
parent repo's linter binary or inherit its config discovery.

Generated linter configs are part of the enforcement boundary. If a
generated config or its hash manifest drifts, fail before invoking any
linter and tell the caller to restore the generated files before
continuing.
### Linting as Code Quality Enforcement

Linters are not suggestions; they are automated code reviewers enforcing our standards.

Directive: Resolve lint findings with structural fixes; suppress only with documented necessity.

Quick ref:
- Resolve lint findings with structural fixes; suppress only with documented necessity.
- Linters are not suggestions; they are automated code reviewers enforcing our standards.
- Do not weaken hooks or broaden suppressions just to get green faster.

#### Overview
Linters are not suggestions; they are automated code reviewers
enforcing our standards. We work with them, not around them.

#### SOLID-First Resolution
When a linter flags an issue, the correct response is to apply SOLID
principles to resolve it properly:

* **Long functions?** Apply SRP—split into focused,
  single-responsibility
  units.
* **Too many parameters?** Apply ISP—consider configuration objects or
  dependency injection.
* **Complex conditionals?** Apply OCP—use polymorphism or strategy
  patterns.
* **Tight coupling?** Apply DIP—introduce abstractions and inject
  dependencies.

We do not silence linters because refactoring feels inconvenient.

#### Suppression Policy
Lint suppressions are a last resort, permitted only when:

1. **Technical necessity:** The suppression is genuinely required
   (e.g., a
   false positive from the linter).
2. **Inferior alternatives:** All workarounds would make the code
   demonstrably worse.
3. **Full documentation:** The suppression includes a clear
   explanation of
   why it exists.

**Required Documentation for Any Suppression:**

* Why the suppression is necessary
* Why alternatives are technically inferior
* Reference to any relevant issues or discussions

**Example of Acceptable Suppression:**

```python
# noqa: E501 - SQL query string must remain on single line for readability
# and to match the query plan documentation. Breaking across lines would
# obscure the join structure. See: docs/query_patterns.md#complex-joins
query = "SELECT a.id, b.name, c.value FROM table_a a JOIN table_b b ON ..."
```

#### Active Maintenance
When working on code that contains suppression comments, you must
evaluate
whether they are still necessary:

* Can the underlying issue now be fixed properly?
* Has the codebase evolved to make a better solution possible?
* Is the suppression documented adequately?

If a suppression is no longer justified, remove it and fix the issue.

#### Enforcement Machinery Is Part of the Contract
Pre-commit, pre-push, and CI lint gates are part of normal
development, not
optional wrappers.

* Do not weaken hook configuration to get a green result faster.
* Do not introduce broad file-level ignores, pyproject ignore blocks,
  or
  comment suppressions as convenience escape hatches.
* Do not use environment-variable bypasses such as `SKIP=` to drop
  selected
  gates out of the commit flow.
* Fix the code, or document a principled and reviewable exception when
  the
  rule itself is wrong.

#### Anti-Patterns (Forbidden)
* ❌ Adding `# type: ignore` without explanation
* ❌ Blanket file-level suppressions (`# noqa` at the top of a file)
* ❌ Disabling rules in `pyproject.toml` because "there are too many
  warnings"
* ❌ Using suppressions to avoid refactoring
* ❌ Copying existing suppression patterns without understanding why
  they
  exist

#### The Audit Trail
Every suppression tells a story. Future maintainers must be able to
understand:

1. What triggered the suppression
2. Why it couldn't be fixed properly
3. Under what conditions the suppression could be removed

Undocumented suppressions are technical debt masquerading as
solutions.
### One Path for Critical Operations

When there are multiple ways to accomplish a critical operation, bugs
hide in the less-traveled path.

Directive: Keep one explicit, validated path for critical operations.

Quick ref:
- Keep one explicit, validated path for critical operations.
- When there are multiple ways to accomplish a critical operation, bugs
hide in the less-traveled path.
- Use the repo's canonical validation entrypoint; do not invent partial substitutes.

#### Overview
When there are multiple ways to accomplish a critical operation, bugs
hide in the less-traveled path. We enforce a single, canonical path
for operations that matter.

#### No Modal Behavior Switches
**We strictly ban boolean parameters that fundamentally change what a
function does.**

A parameter like `persist: bool = True` creates two different
functions
masquerading as one. Callers must understand not just *what* the
function
does, but *which version* they're invoking. This is a recipe for
confusion
and bugs.

**The Anti-Pattern (Forbidden):**

```python
# ❌ WRONG: Boolean switch creates two code paths
async def onboard_dataframe(
    self,
    entity_id: str,
    dataframe: DataFrame,
    *,
    persist: bool = True,  # This creates a fork in behavior
) -> ProcessingResult:
    result = self._compute_schema_and_stats(dataframe)

    if persist:  # One code path: compute AND store
        await self._store_metadata(entity_id, result)
        await self._store_statistics(entity_id, result)

    return result  # Other code path: compute only
```

**Why This Is Dangerous:**

1. **Hidden Complexity:** The function signature lies about what it
   does.
   Two behaviors, one name.
2. **Testing Burden:** Both paths must be tested, but only one is
   typically
   exercised in production.
3. **Bug Magnet:** Callers pick the wrong path. The bug we hit:
   `persist=True` bypassed ULID generation because it wasn't the
   canonical
   persistence path.
4. **Violation of SRP:** The function has two reasons to
   change—computation
   logic OR persistence logic.
5. **Documentation Rot:** Docstrings must explain both modes,
   inevitably
   becoming stale for one.

#### The Correct Way: Separate Concerns, Single Path
If an operation can be decomposed, decompose it. If persistence must
happen,
it happens through ONE designated service.

```python
# ✅ CORRECT: Function does ONE thing
async def onboard_dataframe(
    self,
    entity_id: str,
    dataframe: DataFrame,
) -> ProcessingResult:
    """Compute schema and statistics for a dataframe.

    This function ONLY computes. It does NOT persist.
    Persistence is handled by EntityMetadataService.ingest_dataset().
    """
    return ProcessingResult(
        schema=self._infer_schema(dataframe),
        profile=self._compute_statistics(dataframe),
    )

# ✅ CORRECT: Orchestration layer calls the ONE persistence path
async def ingest(self, request: IngestionRequest) -> IngestionResponse:
    onboarding_result = await onboarding_service.onboard_dataframe(...)

    # THE canonical path for persistence—generates ULID, stores everything
    await dataset_service.ingest_dataset(entity_id, metadata_payload)
```

#### Identifying Modal Functions
Watch for these warning signs:

* Parameters named `persist`, `save`, `commit`, `execute`, `dry_run`,
  `skip_*`
* Boolean parameters that guard large `if/else` blocks
* Functions where "it depends" on a flag whether side effects occur
* Docstrings that say "If X is True, then... otherwise..."

#### Canonical Validation Entry Points
Repos must define one documented way to run quality gates locally.

If a repo documents `make check`, `pre-commit run --all-files`, or
another
validation entrypoint, use that path.

Do not substitute a hand-picked subset and then claim the work is
validated.

Alternate commands may exist for debugging, but only the canonical
gate
proves readiness.

#### The Exception: Explicit Dry-Run at API Boundaries
A `dry_run` parameter is acceptable ONLY at the outermost API boundary
(CLI
commands, REST endpoints) where the user explicitly requests a
preview. It
must:

1. Be clearly named and documented as a preview mode
2. Execute the SAME code path but skip the final commit/write
3. Never be the mechanism for internal code to "optionally" persist

```python
# ✅ ACCEPTABLE: CLI dry-run for user preview
@cli.command()
def ingest(path: Path, dry_run: bool = False):
    """Ingest a dataset. Use --dry-run to preview without persisting."""
    result = service.ingest(path, dry_run=dry_run)
    # dry_run affects the FINAL write, not internal behavior
```

#### Anti-Patterns (Forbidden)
* ❌ `def process(data, save: bool = True)` — modal persistence
* ❌ `def validate(schema, strict: bool = False)` — if strictness
  matters,
  make two functions
* ❌ `def fetch(url, cache: bool = True)` — caching is infrastructure,
  not a
  per-call decision
* ❌ Internal functions with `persist`, `commit`, or `store` boolean
  parameters

#### The Rule
For any critical operation, ask: "Is there exactly ONE way to do this
correctly?" If the answer involves "it depends on a boolean
parameter," the design is wrong. Split the function, or designate a
single orchestration point.

#### Repo Addendum
Keep generation routed through `coding_ethos.cli.main()` and the
shared
rendering pipeline instead of adding ad hoc writers for individual
root
files.

If output behavior changes, update the source YAML, renderer logic,
and
tests together rather than patching only one layer.
### Validation at the Gate

Configuration, schema, and extension availability are validated
immediately upon container initialization.

Directive: Validate configuration, schema, and extensions during bootstrap rather
than on first use.

Quick ref:
- Validate configuration, schema, and extensions during bootstrap rather
than on first use.
- Configuration, schema, and extension availability are validated
immediately upon container initialization.

#### Overview
Configuration, schema, and extension availability are validated
immediately
upon container initialization.

* If a Postgres extension is missing, we raise ExtensionMissingError
  immediately. We do not wait for a query to fail 5 hours later.
* If a cache adapter is misconfigured, we prevent the application from
  starting.
### Radical Visibility

We believe that if an event wasn't logged, it didn't happen.

Directive: Log important decisions with context and instrument the system with metrics.

Quick ref:
- Log important decisions with context and instrument the system with metrics.
- We believe that if an event wasn't logged, it didn't happen.
- Everything is Logged: Ingestion steps, query rewrites, cache
hits/misses, and decision branches must emit logs.

#### Overview
We believe that if an event wasn't logged, it didn't happen. Logging
is not a debugging tool to be added later; it is a primary feature of
the system.

#### Ubiquitous Logging
* **Everything is Logged:** Ingestion steps, query rewrites, cache
  hits/misses, and decision branches must emit logs.
* **Structured Data:** We use structlog to treat logs as data, not
  text.
  Logs must be machine-parsable (JSON in production).
* **Context is King:** Every log line must carry context. Who
  initiated
  this? What is the correlation_id? What dataset is being processed?
  * *Anti-Pattern:* logger.error("File not found")
  * *Required:* logger.error("ingestion_failed",
    reason="file_not_found",
    path=path, entity_id=id, correlation_id=cid)

#### Traceability
Because we rely on dependency injection and protocols, control flow
can be complex. Detailed logging allows us to reconstruct the
execution path of any request post-mortem.

#### Metrics Instrumentation
Logs tell us *what happened*. Metrics tell us *how often* and *how
fast*.
Both are mandatory.

* **OTel is Standard:** We use OpenTelemetry for all metrics. No
  custom
  metrics frameworks.
* **Every Operation is Measured:** Counters for occurrences,
  histograms for
  durations, gauges for current state.
* **Labels Add Dimension:** Metrics without labels are nearly useless.
  Include `status`, `operation`, `component`.

**The Correct Way:**

```python
from app.observability.tracing import (
    increment_counter,
    record_histogram,
    traced,
)

@traced("ingestion.process_file", {"component": "ingestion"})
async def process_file(self, path: Path) -> ProcessResult:
    start = time.perf_counter()
    try:
        result = await self._do_process(path)
        increment_counter("app.files_processed_total", {"status": "success"})
        return result
    finally:
        duration_ms = (time.perf_counter() - start) * 1000
        record_histogram("app.process_duration_ms", duration_ms)
```

If you cannot answer "How many times did X happen last hour?" or "What
is
the p99 latency of Y?"—your code is incomplete.

## Output Discipline
When explaining a fix, name the ETHOS principle, the concrete code change, and the verification evidence. Do not recommend weakening lint config or adding suppressions unless the ETHOS policy explicitly allows it.
