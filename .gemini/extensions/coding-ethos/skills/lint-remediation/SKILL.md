<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

---
name: lint-remediation
description: Use when Ruff, mypy, pyright, pylint, Bandit, SQLFluff, Tombi, or another managed checker reports findings that need structural code-quality remediation rather than suppressions or config weakening.
metadata:
  source: coding_ethos.yml
  generated_by: coding-ethos
  ethos_principles:
    - static-analysis-is-the-first-line-of-defense
    - linting-as-code-quality-enforcement
    - universal-responsibility
    - solid-is-law
---

# ETHOS-Grounded Lint Remediation

Use this skill to convert linter output into concrete refactoring work grounded in ETHOS, especially when an agent is tempted to silence the finding or weaken the rules.

## ETHOS Grounding
- `static-analysis-is-the-first-line-of-defense`: Make ruff and mypy blocking quality gates rather than advisory tools.
- `linting-as-code-quality-enforcement`: Resolve lint findings with structural fixes; suppress only with documented necessity.
- `universal-responsibility`: Own every error or warning you touch and verify claims with evidence.
- `solid-is-law`: Enforce SOLID and simplicity; remove speculative abstractions.

## Short Hint
Linters are enforced code reviewers. Fix findings structurally, keep managed config intact, and suppress only when technically necessary and fully documented.

## Use When
- ruff
- mypy
- pyright
- pylint
- bandit
- sqlfluff
- lint failure
- type error
- noqa
- suppression

## Remediation Workflow
1. Classify each finding by the code-quality concern it represents, not by the tool name alone.
2. Prefer structural fixes: split responsibilities, tighten types, remove dead paths, parameterize unsafe operations, or clarify contracts.
3. Do not edit generated lint config, broaden ignores, or add suppressions to get green faster.
4. If a suppression is genuinely necessary, document why the alternatives are worse and keep the scope as small as possible.
5. Verify with the managed coding-ethos tool path and report the exact checks that now pass or still fail.

## Principle Details
### Static Analysis is the First Line of Defense

We rely on linters (ruff) and type checkers (mypy) to catch errors
before the code ever runs.

Directive: Make ruff and mypy blocking quality gates rather than advisory tools.

Quick ref:
- Make ruff and mypy blocking quality gates rather than advisory tools.
- We rely on linters (ruff) and type checkers (mypy) to catch errors
before the code ever runs.
- Static analysis belongs in enforced local hooks and CI, not in
optional tribal knowledge.

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
This repo currently documents `uv run pytest` as its canonical
automated
gate.

If lint or type-check tooling is introduced, wire it into the
documented
workflow, update the repo commands, and treat it as part of the
contract
instead of a local preference.

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

## Output Discipline
When explaining a fix, name the ETHOS principle, the concrete code change, and the verification evidence. Do not recommend weakening lint config or adding suppressions unless the ETHOS policy explicitly allows it.
