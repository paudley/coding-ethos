---
name: "agent-operating-discipline"
description: "Use when implementing, reviewing, refactoring, simplifying, debugging, or planning code changes that need explicit assumptions, minimal scope, surgical edits, and verifiable success criteria."
metadata:
  source: coding_ethos.yml
  generated_by: coding-ethos
  ethos_principles:
    - evidence-based-engineering-and-decision-quality
    - solid-is-law
    - universal-responsibility
    - testing-as-specification
    - documentation-as-contract
---
<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Agent Operating Discipline

Use this skill before broad coding work so agents avoid hidden assumptions, speculative abstractions, drive-by refactors, and vague completion claims.

## ETHOS Grounding
- `evidence-based-engineering-and-decision-quality`: Understand, plan, execute, and validate with evidence; measure before
optimizing and make trade-offs explicit.
- `solid-is-law`: Enforce SOLID and simplicity; remove speculative abstractions.
- `universal-responsibility`: Own every error or warning you touch and verify claims with evidence.
- `testing-as-specification`: Treat tests as executable behavioral contracts and update them with code changes.
- `documentation-as-contract`: Keep public behavior documented as part of the interface contract.

## Short Hint
State assumptions, choose the smallest sufficient design, keep edits traceable to the request, and define the verification that proves success.

## Use When
- implement
- refactor
- simplify
- redesign
- clean up
- review
- fix bug
- add feature
- make faster
- success criteria
- assumptions
- tradeoffs
- surgical change
- overcomplicated

## Remediation Workflow
1. State the task interpretation, assumptions, ambiguity, and relevant trade-offs before making broad changes.
2. Pick the smallest implementation that satisfies the current requirement; defer abstractions, configurability, and extension points until there is a concrete second use.
3. Keep every changed line traceable to the user request or to cleanup directly caused by the change.
4. Match local style and ownership boundaries; mention unrelated dead code or improvement ideas instead of editing them opportunistically.
5. Convert the task into explicit success criteria and verify with focused tests, lint, type checks, or documented evidence before claiming completion.

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
### SOLID is Law

We do not view the SOLID principles as academic suggestions.

Directive: Enforce SOLID and simplicity; remove speculative abstractions.

Quick ref:
- Enforce SOLID and simplicity; remove speculative abstractions.
- We do not view the SOLID principles as academic suggestions.
- SOLID governs structure; these principles govern complexity:

#### Overview
We do not view the SOLID principles as academic suggestions. They are
the
strict rules of engagement for our codebase.

* **Single Responsibility (SRP):** Modules must have one reason to
  change.
  If a class handles storage I/O *and* metadata parsing, it is broken.
  Split
  it.
* **Open/Closed (OCP):** The system grows by adding new classes
  (extensions), not by editing existing ones. We use registries and
  dependency injection to introduce new behaviors.
* **Liskov Substitution (LSP):** A ServiceProvider must behave like a
  ServiceProvider. If your implementation raises unexpected exceptions
  or
  violates the protocol contract, it is a bug.
* **Interface Segregation (ISP):** Clients should not depend on
  methods they
  do not use. We prefer small, specific Protocols in interfaces/ over
  monolithic base classes.
* **Dependency Inversion (DIP):** We depend on abstractions
  (Protocols), not
  concretions. The Dependency Injection container (the DI container)
  is the
  heart of the application; manual instantiation of service chains in
  business logic is forbidden. *(See Protocol-First Design for how we implement this.)*

#### Beyond SOLID: Simplicity Precepts
SOLID governs structure; these principles govern complexity:

* **YAGNI (You Aren't Gonna Need It):** Do not build for hypothetical
  future
  requirements. If the feature isn't needed today, don't add it.
  Abstractions "for future flexibility" are premature complexity.
  Build the
  simplest thing that works.

* **KISS (Keep It Simple, Stupid):** When choosing between approaches,
  prefer the simpler one. A 10-line function with duplication beats a
  50-line abstraction. Clever code is a liability. Code should be
  boring.

* **Principle of Least Astonishment (POLA):** APIs, functions, and
  behaviors
  should do what their names suggest. If a function named `get_user()`
  modifies the database, it is broken regardless of whether it
  "works." No
  surprises.

* **Law of Demeter (Don't Talk to Strangers):** A method should only
  call
  methods on: (1) itself, (2) its parameters, (3) objects it creates,
  (4)
  its direct dependencies. Chains like `user.address.city.name`
  violate
  this—each dot is a coupling point that makes code brittle.

* **Composition over Inheritance:** Prefer composing objects from
  smaller
  components over deep inheritance hierarchies. In Python, use
  Protocols for
  interfaces and inject dependencies rather than inheriting behavior.
  Inheritance creates tight coupling; composition creates flexibility.

**Anti-Patterns to Watch For:**

* ❌ Adding a config system for values that could be constants
* ❌ Creating an abstract base class for a single implementation
* ❌ Adding optional parameters "in case someone needs them later"
* ❌ Building plugin architectures when a simple import would suffice
* ❌ Chaining through multiple objects to get data (violates Demeter)
* ❌ Creating deep inheritance hierarchies when composition would work
* ❌ Null Objects for "deployment flexibility" when the dependency is
  always
  required

**The Rule:** The best code is the code you didn't write. Every
abstraction
must earn its place by solving a problem that exists TODAY, not one
that
might exist tomorrow.
### Universal Responsibility

Every bug and every lint warning is everyone's responsibility.

Directive: Own every error or warning you touch and verify claims with evidence.

Quick ref:
- Own every error or warning you touch and verify claims with evidence.
- Every bug and every lint warning is everyone's responsibility.
- We do not accept excuses based on the origin of a problem.

#### Overview
Every bug and every lint warning is everyone's responsibility. There
is no such thing as "pre-existing" or "not my code."

#### No Exemptions
We do not accept excuses based on the origin of a problem. If you can
see
it, you own it.

* **All errors are in scope:** Type errors, lint warnings, test
  failures—regardless of when they were introduced.
* **All warnings need addressing:** A warning is a latent bug waiting
  to
  manifest.
* **No "pre-existing" carve-outs:** The commit history is irrelevant
  to your
  responsibility.

#### Anti-Patterns (Forbidden Thinking)
The following rationalizations are explicitly banned:

* ❌ "This is a pre-existing error, not related to my work."
* ❌ "These warnings were present on main, so I can ignore them."
* ❌ "I didn't introduce this bug, so it's not my problem."
* ❌ "The CI was already failing before my changes."

#### The Correct Way
Correct thinking sounds like this:

* ✅ "There are 240 lint warnings in this branch. I need a systematic
  strategy to fix them before I commit."
* ✅ "This type error predates my changes, but I'll fix it now while I
  understand the context."
* ✅ "The test suite has 3 flaky tests. I'll stabilize them before
  adding my
  new tests."

#### Verification Is Your Responsibility
Claims require evidence. Assertions require verification.

* ❌ Claiming "I verified this works" without actually running tests
* ❌ Asserting a file exists without using Glob or ls to check
* ❌ Stating "this function does X" without reading its implementation
* ❌ Assuming code behavior without tracing the execution path

* ✅ "I ran the test suite and all tests pass"
* ✅ "I verified the file exists at src/app/storage.py"
* ✅ "Based on reading lines 45-60, this function returns..."
* ✅ "I have not verified this assumption—let me check"

**The Rule:** If you touch a file, you leave it cleaner than you found
it.
If you see a problem, you fix it or you file an issue—but you do not
pretend
it doesn't exist.

*(See also: Forward Motion Only
for
the forward-looking mindset that prevents blame culture.)*
### Testing as Specification

Tests are not afterthoughts—they are the executable specification of system behavior.

Directive: Treat tests as executable behavioral contracts and update them with code changes.

Quick ref:
- Treat tests as executable behavioral contracts and update them with code changes.
- Tests are not afterthoughts—they are the executable specification of system behavior.
- There is no such thing as an "acceptable" test failure or a "known flaky" test.

#### Overview
Tests are not afterthoughts—they are the executable specification of
system behavior. If something isn't tested, it doesn't work.

#### 100% Pass Rate Is Non-Negotiable
There is no such thing as an "acceptable" test failure or a "known
flaky"
test.

* **All tests must pass:** Every test run must complete with zero
  failures,
  zero errors.
* **No "known failures":** A test that sometimes fails is a bug, not a
  feature.
* **No skipped tests without reason:** `@pytest.mark.skip` requires a
  documented justification and a path to re-enablement.

**The Anti-Pattern (Forbidden):**

```python
# ❌ WRONG: Skipping tests to make CI green
@pytest.mark.skip(reason="flaky, fix later")  # "Later" never comes
def test_concurrent_writes():
    ...

# ❌ WRONG: Tests that always pass by testing nothing
def test_important_feature():
    pass  # TODO: implement

# ❌ WRONG: Mocking away the thing you're testing
def test_database_write(mock_db):
    mock_db.write.return_value = True  # You're testing the mock, not the code
    assert service.save(data)  # This proves nothing
```

**The Correct Way:**

```python
# ✅ CORRECT: Real tests with real isolation
@pytest.fixture
def db_session(test_database):
    """Provide isolated database session with automatic rollback."""
    async with test_database.transaction() as session:
        yield session
        # Transaction automatically rolls back - true isolation

# ✅ CORRECT: Tests that verify actual behavior
async def test_concurrent_writes(db_session):
    """Verify concurrent writes don't corrupt data."""
    results = await asyncio.gather(
        service.write(db_session, data1),
        service.write(db_session, data2),
    )
    assert all(r.success for r in results)
    assert await service.count(db_session) == 2
```

#### Test Isolation via Transaction Rollback
Tests must be isolated without compromising realism. We use
transaction
rollback, not mocks:

* **Real database operations:** Tests hit the actual database, not
  mocks.
* **Transaction boundaries:** Each test runs in a transaction that
  rolls
  back.
* **No cross-test pollution:** Test order must not affect results.
* **Production parity:** Test database matches production schema
  exactly.

#### Test Type Hierarchy
* **Unit tests:** Fast, focused, test one component in isolation.
* **Integration tests:** Test component interactions with real
  dependencies.
* **Gold standard tests:** Verify behavior against known-correct
  datasets.

Each layer must pass completely before the next layer runs.

#### Anti-Patterns (Forbidden)
* ❌ Modifying tests to make failures pass
* ❌ Adding `# type: ignore` to test files to hide type errors
* ❌ Creating tests that can never fail (tautological tests)
* ❌ Mocking the component under test
* ❌ Testing implementation details instead of behavior
* ❌ Claiming "tests pass" when tests were skipped or disabled

**The Rule:** Tests are the contract. If a test fails, either the code
is
wrong or the test is wrong—but something IS wrong. Fix it; don't hide
it.

#### Repo Addendum
`tests/test_cli.py` is the executable contract for rendering coverage,
inject-mode merges, symlink replacement, and LLM merge dispatch.

Extend the tests whenever output structure, merge semantics, or CLI
flow
changes.
### Documentation as Contract

An undocumented public function is a bug.

Directive: Keep public behavior documented as part of the interface contract.

Quick ref:
- Keep public behavior documented as part of the interface contract.
- An undocumented public function is a bug.
- Every public function must have a Google-style docstring with:

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

## Output Discipline
When explaining a fix, name the ETHOS principle, the concrete code change, and the verification evidence. Do not recommend weakening lint config or adding suppressions unless the ETHOS policy explicitly allows it.
