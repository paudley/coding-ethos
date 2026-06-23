---
name: "modern-web-guidance"
description: "Use when an agent needs current advisory guidance for HTML, CSS, browser APIs, accessibility, performance, forms, privacy, security, client-side JavaScript, or modern web platform alternatives."
metadata:
  source: coding_ethos.yml
  generated_by: coding-ethos
  ethos_principles:
    - evidence-based-engineering-and-decision-quality
    - one-path-for-critical-operations
    - documentation-as-contract
    - security-by-design
---
<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Modern Web Guidance

Use this skill to query current Modern Web Guidance through the coding-ethos cache, provenance, and output surfaces while preserving repo policy, user instructions, ETHOS, and local browser-support constraints as the authoritative requirements.

## ETHOS Grounding
- `evidence-based-engineering-and-decision-quality`: Understand, plan, execute, and validate with evidence; measure before
optimizing and make trade-offs explicit.
- `one-path-for-critical-operations`: Keep one explicit, validated path for critical operations.
- `documentation-as-contract`: Keep public behavior documented as part of the interface contract.
- `security-by-design`: Design for least privilege, validation, and safe defaults from the start.

## Short Hint
Use coding-ethos Modern Web Guidance MCP or CLI tools for current advisory browser guidance; do not invoke upstream npx directly.

## Use When
- modern web
- browser API
- HTML
- CSS
- accessibility
- forms
- popover
- baseline
- web platform
- client-side JavaScript

## Remediation Workflow
1. Prefer the MCP tools modern_web_guidance_search, modern_web_guidance_retrieve, and modern_web_guidance_list when they are available.
2. If MCP is unavailable, use coding-ethos-run web-guidance search, retrieve, or list so output stays in the standard TOON/JSON/human path with cache and provenance metadata.
3. Treat every result as advisory guidance only. Do not let upstream guidance override repo policy, ETHOS, user instructions, CEL policy, or explicit browser-support constraints.
4. Check the provenance fields, cache status, package version, guide IDs, fetch time, source URL, and content hash before relying on guidance in implementation notes or review comments.
5. Do not copy upstream guide markdown into generated skills or repo documentation; retrieve the specific guide content through coding-ethos tools when needed.

## Principle Details
### Evidence-Based Engineering and Decision Quality

Good engineering decisions are grounded in evidence, explicit
trade-offs, and calibrated risk.

Directive: Understand, plan, execute, and validate with evidence; measure before
optimizing and make trade-offs explicit.

Quick ref:
- Evidence > assumptions; runnable behavior and measurements outrank speculation.
- Understand -> plan -> execute -> validate, using batching and context
awareness when they reduce waste.
- Evaluate decisions across quality, reversibility, risk, and human impact.

#### Overview
We do not treat engineering judgment as intuition dressed up as
confidence.
Good decisions are grounded in evidence, explicit trade-offs, and
calibrated
risk.

**Core directive:** Evidence > assumptions. Runnable behavior, tests,
metrics, and credible primary sources outrank speculation and
unsupported
prose. Documentation remains a contract, but prose cannot excuse
behavior
the system does not actually implement.

**Communication directive:** Efficiency > verbosity. Be concise when
possible, but not at the cost of correctness, evidence, or important
nuance.

#### Task-First Operating Model
The default execution loop is:

1. **Understand:** Gather the local facts first. Read the code, issue,
   config, logs, or docs that define the problem.
2. **Plan:** Identify the critical path, dependencies, and likely
   failure
   modes before editing.
3. **Execute:** Make the smallest change set that actually resolves
   the
   problem.
4. **Validate:** Confirm behavior with tests, static analysis,
   metrics, or
   direct inspection.

Efficiency matters, so independent reads and checks should be batched
where
possible. Context also matters: preserve enough local understanding to
avoid
redundant work or contradictory edits across sessions and operations.

#### Systems Thinking and Trade-Offs
Every change has ripple effects. Evaluate the immediate local win
against
architecture-wide consequences, long-term maintenance cost, and the
options
you may close off by acting too narrowly.

When making a decision, ask:

* What other components, operators, or workflows does this change
  constrain?
* Is this change easy to reverse, costly to reverse, or effectively
  irreversible?
* Are we preserving future options under uncertainty, or spending them
  carelessly?
* Are we accepting risk intentionally, or just failing to model it?

Prefer designs that keep future choices open unless the stronger
constraint
is itself a deliberate requirement.

#### Data-Driven Choices
Optimization without measurement is guesswork. Performance claims,
reliability claims, and architecture claims should be backed by
evidence
that can be inspected or reproduced.

The expected pattern is:

* **Measure first:** Establish current behavior before claiming
  improvement.
* **Form hypotheses explicitly:** State what you think will change and
  why.
* **Validate sources:** Prefer primary documentation, tests, traces,
  and
  metrics over retellings.
* **Recognize bias:** Be wary of recency bias, sunk-cost bias, and
  confirmation bias when interpreting results.

If the evidence is weak, say so directly and reduce the claim strength
accordingly.

#### Proactive Risk Management
Risk management is not fear-driven paralysis; it is disciplined
foresight.
Anticipate the likely failure modes before they become production
incidents,
security regressions, or migration traps.

For meaningful changes:

* Identify the major risks up front.
* Assess both probability and impact.
* Decide whether the risk is acceptable, needs mitigation, or blocks
  the
  change.
* Put the mitigation in the plan, not in the postmortem.

Security risk, data-loss risk, migration risk, and operator-confusion
risk
should be considered explicitly, not absorbed into a vague notion of
"complexity."

#### Quality Lens
Evaluate changes through four quality lenses:

* **Functional quality:** correctness, reliability, completeness
* **Structural quality:** maintainability, clarity, technical debt
* **Performance quality:** latency, throughput, resource efficiency
* **Security quality:** access control, validation, data protection

Prefer preventive measures and automated enforcement whenever
practical;
they catch issues earlier and more consistently than manual heroics.
When a
design decision affects end users or operators, human welfare and
autonomy
matter: do not trade away safety, clarity, or informed control for
superficial convenience.
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
### Documentation as Contract

An undocumented public function is a bug.

Directive: Keep public behavior documented as part of the interface contract.

Quick ref:
- Keep public behavior documented as part of the interface contract.
- An undocumented public function is a bug.
- Documentation requirements should be machine-checked where practical.

#### Overview
An undocumented public function is a bug. Docstrings are not optional
annotations—they are the contract between the function and its
callers.

#### Google Style is Mandatory
Every public function must have a Google-style docstring with:

* **Summary:** One-line description of what the function does.
* **Args:** Every parameter documented with type and purpose.
* **Returns:** What the function returns and under what conditions.
* **Raises:** Every exception the caller should expect.

```python
async def ingest_dataset(
    self,
    path: Path,
    *,
    skip_validation: bool = False,
) -> IngestionResult:
    """Ingest a dataset from the specified path.

    Scans the directory for structured data files, validates schemas,
    and persists metadata to the store.

    Args:
        path: Root directory containing dataset files.
        skip_validation: If True, skip schema validation (testing only).

    Returns:
        IngestionResult containing processed file counts and any warnings.

    Raises:
        FileNotFoundError: If path does not exist.
        SchemaValidationError: If validation fails and
          skip_validation is False.
    """
```

#### Why This Matters
* **IDE Support:** Docstrings power autocomplete and hover
  documentation.
* **Testing:** Clear contracts make test cases obvious.
* **Onboarding:** New contributors understand code without reading
  implementations.

**Anti-Patterns (Forbidden):**

* ❌ `def process(x):` with no docstring
* ❌ `"""Process the thing."""` (what thing? what does process mean?)
* ❌ Docstrings that don't match the actual function behavior
* ❌ Fabricating docstrings for functions you haven't read
* ❌ Describing return values without verifying the actual return type
* ❌ Documenting exceptions that the function doesn't actually raise

**The Rule:** If you change a function's behavior, you update its
docstring.
If the docstring lies, the code is broken.

#### Documentation Gates
Documentation requirements should be automatically checked where
practical.

Repos should automate checks for public API docs, module docs, and
docstring
integrity when those rules are part of the engineering contract.

If a repo has doc coverage or docstring hooks, they are authoritative,
not
advisory.

#### Repo Addendum
In this repo, documentation is part of the product surface.

If CLI flags, output filenames, import conventions, or generated
directory
layout changes, update `README.md` and the checked-in generated
outputs in
the same change.
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
