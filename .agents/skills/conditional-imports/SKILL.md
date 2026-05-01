---
name: "conditional-imports"
description: "Use when Python code uses conditional imports, TYPE_CHECKING import branches, local imports that hide dependency problems, Ruff local-import diagnostics, or cyclic import diagnostics that need an ETHOS-grounded repair."
metadata:
  source: coding_ethos.yml
  generated_by: coding-ethos
  ethos_principles:
    - no-conditional-imports
    - protocol-first-design
    - solid-is-law
---
<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Conditional Imports Remediation

Use this skill to turn import-related lint and type-check findings into structural Python refactors instead of hiding imports behind runtime branches.

## ETHOS Grounding
- `no-conditional-imports`: Treat required imports as hard dependencies and fail immediately if they are missing.
- `protocol-first-design`: Define and verify interfaces before writing or referencing implementations.
- `solid-is-law`: Enforce SOLID and simplicity; remove speculative abstractions.

## Short Hint
Conditional imports are banned. Treat required imports as hard dependencies and use protocols, dependency inversion, or cleaner module boundaries to break cycles.

## Use When
- conditional import
- TYPE_CHECKING
- local import
- ruff local import
- cyclic import
- import cycle
- soft dependency

## Remediation Workflow
1. Identify whether the import is required for correct operation. If it is required, make it a normal module-level import.
2. If the import was conditional to avoid a cycle, find the interface the caller actually needs and extract or reuse a Protocol-oriented boundary.
3. Move shared interfaces, lightweight data contracts, or registration seams to a lower-level module instead of importing concrete implementations both ways.
4. Remove TYPE_CHECKING-only imports when runtime behavior depends on the imported symbol; annotations must not mask missing runtime dependencies.
5. Re-run the relevant lint/type checks and explain the structural boundary that replaced the conditional import.

## Principle Details
### No Conditional Imports

We strictly ban the "soft dependency" pattern.

Directive: Treat required imports as hard dependencies and fail immediately if they are missing.

Quick ref:
- Treat required imports as hard dependencies and fail immediately if they are missing.
- We strictly ban the "soft dependency" pattern.

#### Overview
**We strictly ban the "soft dependency" pattern.**

If a module requires a library, that library must be present. We do
not wrap
imports in try/except blocks to hide missing dependencies. If the
environment is missing a requirement, the application must crash at
the
import stage.

**The Anti-Pattern (Forbidden):**

```python
# ❌ WRONG: Hiding environmental rot
try:
    import redis
    HAS_REDIS = True
except ImportError:
    HAS_REDIS = False  # Silent degradation
```

**The Correct Way:**

```python
# ✅ CORRECT: Deterministic failure
import redis  # If this is missing, we crash. Good.
```
### Protocol-First Design

Implementations are ephemeral; Protocols are forever.

Directive: Define and verify interfaces before writing or referencing implementations.

Quick ref:
- Define and verify interfaces before writing or referencing implementations.
- Implementations are ephemeral; Protocols are forever.
- Before referencing any Protocol, type, or interface, verify it exists in the codebase.

#### Overview
Implementations are ephemeral; Protocols are forever.

* We define behaviors in src/interfaces/ before we write a single line
  of
  implementation code.
* We code against the interface, ensuring that our tests and our logic
  remain valid even if we swap Postgres for DuckDB or Local Storage
  for S3.

#### Verify Before You Reference
Before referencing any Protocol, type, or interface, verify it exists
in the
codebase.

**Anti-Patterns (Forbidden):**

* ❌ Inventing Protocol methods that don't exist in interfaces/
* ❌ Assuming a type signature without reading the actual Protocol
  definition
* ❌ Referencing classes or functions that haven't been verified to
  exist
* ❌ Describing Protocol capabilities based on assumptions, not
  inspection

**The Correct Way:**

* ✅ Read the Protocol file before implementing against it
* ✅ Verify method signatures match the actual Protocol definition
* ✅ State "I could not find this Protocol" rather than guessing its
  shape
* ✅ Check imports exist in the codebase before adding them
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
  business logic is forbidden. *(See [Protocol-First Design](#protocol-first-design) for how we implement this.)*

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
