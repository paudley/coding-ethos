---
name: "safe-git-workflow"
description: "Use when git commands, commit hooks, admin-approved commits, protected hook paths, raw git bypass attempts, or commit attribution failures appear in agent or developer workflows."
metadata:
  source: coding_ethos.yml
  generated_by: coding-ethos
  ethos_principles:
    - one-path-for-critical-operations
    - no-rationalized-shortcuts
    - forward-motion-only
    - evidence-based-engineering-and-decision-quality
---
<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Safe Git Workflow

Use this skill to recover from blocked git operations without bypassing hooks, rewriting protected runtime files, or hiding the state that needs review.

## ETHOS Grounding
- `one-path-for-critical-operations`: Keep one explicit, validated path for critical operations.
- `no-rationalized-shortcuts`: Do not discard work or bypass safety checks in the name of pragmatism.
- `forward-motion-only`: Fix the current state instead of blaming history or prior authors.
- `evidence-based-engineering-and-decision-quality`: Understand, plan, execute, and validate with evidence; measure before
optimizing and make trade-offs explicit.

## Short Hint
Git is a protected critical operation. Run normal git commands without bypass flags or shell indirection, keep hook failures visible, and ask an admin when policy blocks protected files.

## Use When
- raw git
- cerun_check
- cerun_run
- cerun
- admin-approved
- commit attribution
- hook bypass
- no-verify
- protected hook
- commit failed

## Remediation Workflow
1. Read the hook failure as policy feedback, not as a broken local setup.
2. Use MCP cerun_check before risky Git or shell work, then execute through MCP cerun_run or the repo-local cerun wrapper when the preflight allows it.
3. Do not try alternate shells, absolute git paths, subprocesses, aliases, PATH edits, no-verify flags, or direct hook edits.
4. If protected coding-ethos files must change, prepare the work and ask the admin to review and finalize the commit using the documented admin-approved flow.
5. Report the files changed, checks run, hook output, and unresolved risks before asking for commit or push approval.

## Principle Details
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
### No Rationalized Shortcuts

We strictly ban any action that discards, bypasses, or destroys work
under the rationalization of "pragmatism," "efficiency," or
"complexity."

Directive: Do not discard work or bypass safety checks in the name of pragmatism.

Quick ref:
- Do not discard work or bypass safety checks in the name of pragmatism.
- We strictly ban any action that discards, bypasses, or destroys work
under the rationalization of "pragmatism," "efficiency," or
"complexity."
- The following thought patterns are explicitly banned.

#### Overview
**We strictly ban any action that discards, bypasses, or destroys work
under
the rationalization of "pragmatism," "efficiency," or "complexity."**

This is the most insidious failure mode in automated systems: the
agent that
convinces itself that destroying weeks of work is acceptable because
"it's
taking too long" or "this is too complex." This behavior is not
pragmatic—it
is catastrophic negligence disguised as reasonable decision-making.

#### The Forbidden Rationalizations
The following thought patterns are explicitly banned. If you catch
yourself
thinking any of these, STOP IMMEDIATELY:

* ❌ "This is taking too long, so I'll take a pragmatic approach..."
* ❌ "This is too complex, so let me simplify by..."
* ❌ "I don't have enough time/context, so I'll just..."
* ❌ "To move forward efficiently, I'll skip..."
* ❌ "Rather than evaluate each item, I'll batch..."
* ❌ "The safest approach is to take all changes from one side..."
* ❌ "I'll mark this as done and document what was skipped..."

**These rationalizations ALWAYS precede destructive actions.** They
are the
warning signs of impending catastrophe.

#### The Merge Catastrophe Pattern
The canonical example: During a complex merge with 50 commits
requiring
careful evaluation, the agent decides:

> "This is becoming too complex. Let me take a pragmatic approach and
accept
all changes from the feature branch."

This single sentence destroys weeks of work. It:

1. Discards all careful analysis that was the purpose of the task
2. Potentially overwrites critical fixes from main
3. Creates a "completed" task that is actually a time bomb
4. Requires days of forensic work to even discover the damage

**The Anti-Pattern (Forbidden):**

```bash
# ❌ CATASTROPHIC: "Pragmatic" merge that discards work
git checkout feature-branch
git merge main -X theirs  # "Accept all from feature to resolve conflicts"
git commit -m "Merged main into feature"
# WEEKS OF WORK DESTROYED IN ONE COMMAND
```

**The Correct Way:**

```bash
# ✅ CORRECT: If merge is complex, acknowledge complexity honestly
# Option 1: Continue methodically, one conflict at a time
git checkout feature-branch
git merge main
# For EACH conflict: understand both sides, make informed decision

# Option 2: If genuinely blocked, STOP and communicate
echo "Merge requires human review: 15 conflicts with semantic complexity"
# DO NOT "resolve" by discarding one side
```

#### Complexity Is Not an Excuse
When a task is complex, the correct response is:

1. **Break it down:** Decompose into smaller, manageable steps
2. **Document progress:** Record what has been done and what remains
3. **Communicate blockers:** If genuinely stuck, say so explicitly
4. **Request guidance:** Ask for clarification or prioritization
5. **Preserve state:** Never destroy existing work to "simplify"

The INCORRECT response is to take a "shortcut" that invalidates the
work.

#### Time Pressure Is Fictional
AI agents do not have meetings to attend, deadlines to meet, or trains
to
catch. Any sense of "time pressure" is a hallucination that precedes
destructive behavior.

**The Anti-Pattern (Forbidden Thinking):**

* ❌ "I need to wrap this up..."
* ❌ "Running out of time to evaluate each..."
* ❌ "To finish quickly, I'll..."

**The Correct Way:**

* ✅ "This requires 50 evaluations. I will perform 50 evaluations."
* ✅ "Each commit needs review. I will review each commit."
* ✅ "The task is not complete until all items are addressed."

#### "Done" Means Actually Done
A task is complete when ALL of the following are true:

1. Every item in scope has been addressed individually
2. No work has been discarded, overwritten, or "simplified away"
3. The result achieves the original objective
4. Quality gates (tests, types, lint) pass
5. The work can withstand scrutiny

A task is NOT complete when:

* You've marked it done but skipped items
* You've "resolved" conflicts by discarding one side
* You've "cleaned up" by deleting things you didn't understand
* You've "simplified" by removing functionality

#### The Detection Heuristic
Before any destructive or simplifying action, ask:

> "If I showed this action to the person who requested the task, would
they
be horrified?"

If yes—or even maybe—STOP. Do not proceed. Communicate instead.

#### Specific Forbidden Actions
The following actions are NEVER acceptable as "shortcuts":

* ❌ `git merge -X theirs` or `git merge -X ours` to "resolve"
  conflicts
* ❌ `git checkout --theirs .` or `git checkout --ours .` to "simplify"
* ❌ Deleting files because they're "causing problems"
* ❌ Skipping commits during a rebase because they're "complex"
* ❌ Reverting changes because "it's easier to start fresh"
* ❌ Closing issues/PRs as "complete" when work was skipped
* ❌ "Batch processing" that means "ignoring individual items"
* ❌ Inventing file paths that don't exist in the codebase
* ❌ Referencing function names that haven't been verified
* ❌ Claiming code "should work" without actually testing it
* ❌ Using `--no-verify` to bypass pre-commit hooks
* ❌ Using `SKIP=` to disable selected hooks while pretending the
  commit
  passed the real gate

#### The Accountability Test
After completing any task, you must be able to honestly answer:

1. "Did I address every item individually?" — YES
2. "Did I preserve all existing work?" — YES
3. "Did I take any shortcuts that bypassed the task's purpose?" — NO
4. "Would the requester approve of my method if they watched?" — YES

If any answer is wrong, the task is not complete—it is sabotaged.

#### When Genuinely Stuck
If a task is genuinely beyond your capability:

1. **STOP** — Do not attempt a "pragmatic" workaround
2. **PRESERVE** — Ensure no work is lost or corrupted
3. **DOCUMENT** — Explain specifically what is blocking progress
4. **COMMUNICATE** — Request human intervention explicitly
5. **WAIT** — Do not "resolve" the situation by destroying things

```python
# ✅ CORRECT: Honest acknowledgment of limitation
raise TaskComplexityError(
    "Merge contains 15 semantic conflicts requiring domain knowledge. "
    "Conflicts in: auth.py, payment.py, user_service.py. "
    "Human review required to determine correct resolution."
)

# ❌ WRONG: "Pragmatic" resolution that destroys work
logger.info("Taking pragmatic approach to complex merge")
subprocess.run(["git", "checkout", "--theirs", "."])  # CATASTROPHE
```

#### The Rule
**No shortcut is acceptable if it discards, overwrites, or invalidates
work.** Complexity is a reason to be MORE careful, not less. Time
pressure is fictional. "Pragmatic" is not a synonym for "destructive."
If you cannot complete a task correctly, say so—do not pretend
completion by destroying the work.
### Forward Motion Only

We look forward, not backward.

Directive: Fix the current state instead of blaming history or prior authors.

Quick ref:
- Fix the current state instead of blaming history or prior authors.
- We look forward, not backward.
- We strictly ban switching to main or other branches to verify whether
errors "existed before."

#### Overview
We look forward, not backward. The past is for learning, not for blame
assignment or responsibility avoidance.

#### No Branch Comparison for Blame
**We strictly ban switching to `main` or other branches to verify
whether
errors "existed before."**

When you encounter an error, warning, or failing test during
development,
the correct response is to fix it. The incorrect response is to check
out
another branch to determine whether you can blame someone else or
claim "it
was already broken."

**The Anti-Pattern (Forbidden):**

```bash
# ❌ WRONG: Switching branches to assign blame
git stash
git checkout main
mypy src/  # "See? It failed on main too, not my problem"
git checkout feature-branch
git stash pop
# Now dealing with merge conflicts and wasted time
```

**Why This Is Dangerous:**

1. **Time Sink:** Context switching between branches wastes time and
   creates
   merge conflicts.
2. **Blame Culture:** Looking backward for blame poisons team
   dynamics.
3. **Responsibility Avoidance:** "Pre-existing" is not a valid excuse
   (see
   Section 13: Universal Responsibility).
4. **State Corruption:** Stashing and branch switching risks losing
   work or
   corrupting state.
5. **Forward Progress Halts:** Energy spent proving fault is energy
   not
   spent fixing problems.

**The Correct Way:**

```bash
# ✅ CORRECT: See error, fix error, move forward
mypy src/
# Error found? Fix it. Now.
# Don't ask "whose fault?" Ask "how do I fix it?"
```

#### The Forward-Looking Mindset
When encountering issues during development:

* ✅ "This error exists. I will fix it."
* ✅ "This warning appeared. I will address it."
* ✅ "This test fails. I will make it pass."
* ❌ "Let me check if this was already broken..."
* ❌ "I didn't introduce this, so it's not my responsibility..."
* ❌ "The CI was already failing before my changes..."

#### Historical Analysis Has Its Place
`git blame` and `git log` are valuable tools—for understanding *why*
code
exists, not for deflecting responsibility. Use history to:

* Understand the intent behind a design decision
* Find the author to ask clarifying questions
* Trace the evolution of a feature for documentation

Never use history to:

* Prove an error is "someone else's problem"
* Justify ignoring warnings or failures
* Avoid fixing issues you encounter

#### No False Claims of Prior Work
Claims about previous verification or analysis must be accurate.

* ❌ "I already checked this file" when you haven't read it in the
  current
  session
* ❌ "I verified this works" when you haven't run the actual tests
* ❌ "This path exists" when you haven't used Glob or ls to confirm
* ❌ "I reviewed this earlier" when referring to work from a previous
  context

* ✅ "Let me verify this again to be sure"
* ✅ "I'll re-run the tests to confirm the current state"
* ✅ "Checking now..." followed by actual verification

**The Rule:** The moment you see a problem, you own it. The commit
history
is irrelevant to your responsibility. Fix it and move forward.
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

## Output Discipline
When explaining a fix, name the ETHOS principle, the concrete code change, and the verification evidence. Do not recommend weakening lint config or adding suppressions unless the ETHOS policy explicitly allows it.
