---
name: "managed-toolchain"
description: "Use when captured linters, generated tool configs, managed binaries, config drift, missing tools, package-relative path resolution, or host tool usage causes lint or hook failures."
metadata:
  source: coding_ethos.yml
  generated_by: coding-ethos
  ethos_principles:
    - static-analysis-is-the-first-line-of-defense
    - validation-at-the-gate
    - one-path-for-critical-operations
    - security-by-design
---
<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Managed Toolchain And Config Integrity

Use this skill when lint behavior differs between repos, a generated config drift check fails, or a tool invocation is using consumer or host settings instead of the managed coding-ethos toolchain.

## ETHOS Grounding
- `static-analysis-is-the-first-line-of-defense`: Make ruff and mypy blocking quality gates rather than advisory tools.
- `validation-at-the-gate`: Validate configuration, schema, and extensions during bootstrap rather
than on first use.
- `one-path-for-critical-operations`: Keep one explicit, validated path for critical operations.
- `security-by-design`: Design for least privilege, validation, and safe defaults from the start.

## Short Hint
Linters and configs must resolve through coding-ethos managed paths. Restore generated configs and run captured tools through the wrapper.

## Use When
- managed toolchain
- config drift
- generated config
- real binary not found
- host linter
- package-relative path
- tool capture
- fix-configs

## Remediation Workflow
1. Treat the consumer repo as untrusted for linter binaries and config.
2. Keep target file resolution faithful to the caller, but run the managed linter and managed config from the coding-ethos checkout.
3. If generated configs drift, restore them with the documented coding-ethos fix-configs path instead of editing hashes or ignores.
4. When a managed binary is missing, repair through make build rather than installing host-global tools or editing shims.
5. Capture the normalized lint output and trace so repeated failures can become stronger ETHOS mappings.

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
### Security by Design

Security is not a feature to be added later—it is a property of the design.

Directive: Design for least privilege, validation, and safe defaults from the start.

Quick ref:
- Design for least privilege, validation, and safe defaults from the start.
- Security is not a feature to be added later—it is a property of the design.
- Secrets, credentials, and API keys must never appear in source code,
configuration files, or commit history.

#### Overview
Security is not a feature to be added later—it is a property of the
design. We build security in from the start.

#### No Secrets in Code
Secrets, credentials, and API keys must never appear in source code,
configuration files, or commit history.

* **Environment variables:** All secrets loaded via environment
  (`.env`
  files)
* **Never commit secrets:** No API keys, passwords, or tokens in any
  file
* **No default secrets:** Never provide "example" credentials that
  could be
  used in production

**The Anti-Pattern (Forbidden):**

```python
# ❌ WRONG: Hardcoded secrets
API_KEY = "sk-abc123..."  # NEVER
DATABASE_URL = "postgresql://admin:password123@prod-db/..."  # NEVER

# ❌ WRONG: "Example" secrets that become real
API_KEY = os.getenv("API_KEY", "sk-test-key-for-development")  # Still wrong
```

**The Correct Way:**

```python
# ✅ CORRECT: Secrets from environment, no defaults
API_KEY = os.environ["API_KEY"]  # Fails if not set - good

# ✅ CORRECT: Explicit configuration validation
@dataclass
class Config:
    api_key: str = field(default_factory=lambda: os.environ["API_KEY"])

    def __post_init__(self) -> None:
        if not self.api_key:
            raise ConfigurationError(
                "API_KEY environment variable is required"
            )
```

#### Parameterized Queries Only
SQL injection is a solved problem. We solve it by using parameterized
queries exclusively.

**The Anti-Pattern (Forbidden):**

```python
# ❌ CATASTROPHIC: String formatting SQL
query = f"SELECT * FROM users WHERE id = {user_id}"  # SQL injection
query = "SELECT * FROM users WHERE name = '%s'" % name  # SQL injection
```

**The Correct Way:**

```python
# ✅ CORRECT: Parameterized queries
query = "SELECT * FROM users WHERE id = $1"
result = await conn.fetch(query, user_id)

# ✅ CORRECT: Using query builder with parameterization
query = select(users).where(users.c.id == bindparam("user_id"))
result = await conn.execute(query, {"user_id": user_id})
```

#### Input Validation at Boundaries
All external input is untrusted. Validate at the boundary, then trust
internally.

* **API endpoints:** Validate all request parameters with Pydantic
* **File inputs:** Validate file types, sizes, and content
* **Configuration:** Validate at startup, not at use time (see
  [Validation at the Gate](#validation-at-the-gate))

#### Production Database Detection
Operations that could harm production must detect and refuse
production
environments.

```python
# ✅ CORRECT: Production safety guard
async def reset_database(conn: Connection) -> None:
    """Reset database to clean state. NEVER runs in production."""
    db_name = await conn.fetchval("SELECT current_database()")
    if "prod" in db_name.lower():  # Add your production DB name check here
        raise SecurityError(
            f"Refusing to reset production database: {db_name}",
            database=db_name,
    )
    await conn.execute("DROP SCHEMA public CASCADE")
```

#### Automated Security Gates
Repos should automatically detect obvious security regressions before
commit
and in CI.

Secret scanning, unsafe query detection, boundary validation, and
environment-safety guards should be machine-enforced where the repo
can
express them.

If a repo ships security hooks, they are part of the build contract
and must
not be bypassed casually.

#### Anti-Patterns (Forbidden)
* ❌ Secrets in source code, config files, or environment defaults
* ❌ String-formatted SQL queries
* ❌ Trusting user input without validation
* ❌ Running destructive operations without environment checks
* ❌ Assuming security features exist without verifying them

**The Rule:** Security is not optional. It is validated, enforced, and
designed into every component. If a security measure can be bypassed,
it's
not a security measure—it's a suggestion.

## Output Discipline
When explaining a fix, name the ETHOS principle, the concrete code change, and the verification evidence. Do not recommend weakening lint config or adding suppressions unless the ETHOS policy explicitly allows it.
