# Policy Compiler Plan

`coding-ethos` should compile ethos, repo policy, pre-commit settings, agent
hook settings, and git workflow rules into one versioned policy bundle.

The compiled bundle should be data, not code. Runtime components implement
evaluators, but the compiled policy bundle decides which policies are enabled,
where they apply, how severe they are, what message they emit, and which ethos
principles explain them.

## Goal

The policy compiler should provide one source of truth for:

- rendered agent guidance
- unified linting
- Go git hook execution
- agent hook behavior
- git wrapper decisions
- generated prompt packs

The architecture should look like this:

```text
coding_ethos.yml
repo_ethos.yml
config.yaml
repo_config.yaml
        |
        v
compiled coding-ethos policy bundle
        |
        +--> rendered agent guidance
        +--> unified linter
	+--> Go git hooks
        +--> agent hooks
        +--> git wrapper
        +--> generated prompt packs
```

## Compiled Artifact

The canonical compiled artifact should be JSON:

```text
.git/coding-ethos-hooks/policy/policy-bundle.json
```

JSON is the right compiled format because it is easy for Go, Python, Node, and
shell helpers to consume. It also avoids requiring tiny hook binaries to parse
YAML.

The compiler may also generate a human-readable summary:

```text
.git/coding-ethos-hooks/policy/policy-summary.md
```

RDF 1.2 export should be a later feature:

```text
.git/coding-ethos-hooks/policy/policy-bundle.trig
.git/coding-ethos-hooks/policy/policy-bundle.ttl
```

The runtime bundle should remain JSON, but the RDF export should preserve 100%
of the compiled policy data so existing RDF, RDF-star, and knowledge-graph
tooling can query policy provenance, decisions, principle relationships, and
runtime evidence.

## Bundle Shape

The bundle should have two major layers:

1. Semantic policy graph.
2. Runtime dispatch indexes.

The semantic policy graph captures what a policy means. The runtime dispatch
indexes capture how to evaluate policies quickly for a given action.

### Top-Level Schema

Example:

```json
{
  "version": 1,
  "bundle_id": "lbox-policy-2026-04-24",
  "generated_at": "2026-04-24T12:00:00Z",
  "sources": {
    "ethos": {
      "primary": "coding_ethos.yml",
      "repo": "repo_ethos.yml"
    },
    "enforcement": {
      "primary": "config.yaml",
      "repo": "repo_config.yaml"
    }
  },
  "principles": {},
  "policies": {},
  "dispatch": {}
}
```

### Principles

Principles should be normalized from the merged `coding_ethos.yml` and
`repo_ethos.yml` bundle.

Example:

```json
{
  "principles": {
    "no-conditional-imports": {
      "id": "no-conditional-imports",
      "order": 3,
      "title": "No Conditional Imports",
      "directive": "Treat required imports as hard dependencies and fail immediately if they are missing.",
      "summary": "We strictly ban the soft dependency pattern.",
      "quick_ref": [
        "Treat required imports as hard dependencies and fail immediately if they are missing.",
        "We strictly ban the soft dependency pattern."
      ],
      "tags": ["dependency", "startup", "reliability"],
      "detail_path": ".agents/ethos/no-conditional-imports.md",
      "agent_hints": {
        "codex": "Apply this rule directly in code edits and review decisions.",
        "claude": "Reference this rule explicitly when reviewing or merging repo guidance."
      }
    }
  }
}
```

The compiled principle should contain enough data for hooks and linters to
produce concise advice without loading the full rendered ethos document.

### Policies

Policies are stable semantic units. They should not be provider-specific.

Example:

```json
{
  "policies": {
    "python.conditional_imports": {
      "id": "python.conditional_imports",
      "category": "python",
      "source": {
        "file": "config.yaml",
        "path": "python.conditional_imports"
      },
      "principle_ids": ["no-conditional-imports"],
      "default_severity": "block",
      "supported_modes": ["block", "ask", "advise", "annotate", "record"],
      "message": "Required dependencies should fail immediately; ImportError fallback creates a soft dependency path.",
      "suggestion": "Remove the conditional import or configure an explicit exemption.",
      "defense_layers": {
        "persuade": true,
        "intercept": "advise",
        "mediate": "",
        "detect": "block",
        "enforce": "pre_commit",
        "verify": "",
        "record": true,
        "notify": "on_failure"
      },
      "applies_to": {
        "languages": ["python"],
        "file_patterns": ["**/*.py"]
      },
      "evaluators": [
        {
          "kind": "ast",
          "name": "python.conditional_imports"
        }
      ]
    }
  }
}
```

Required fields:

- `id`
- `category`
- `source`
- `principle_ids`
- `default_severity`
- `supported_modes`
- `message`
- `suggestion`
- `defense_layers`
- `applies_to`
- `evaluators`

Policy IDs should be stable and namespaced.

Examples:

- `python.conditional_imports`
- `python.optional_returns`
- `python.catch_and_silence`
- `python.structured_logging`
- `python.direct_imports`
- `pytest.gate`
- `generated_config.freshness`
- `git.hook_bypass`
- `git.staged_admin_files`
- `git.commit_head_advanced`
- `agent_hooks.protected_paths`

## Runtime Dispatch

Runtime dispatch indexes should be optimized for fast lookup by the current
runtime surface.

The dispatch layer should reference policies by ID rather than duplicating
their semantics.

Example:

```json
{
  "dispatch": {
    "hooks": {
      "PreToolUse": {
        "Write": [
          {
            "policy_id": "python.conditional_imports",
            "mode": "advise",
            "path_patterns": ["**/*.py"]
          }
        ],
        "Bash": [
          {
            "policy_id": "git.hook_bypass",
            "mode": "block",
            "command_patterns": ["--no-verify", "SKIP=", "git commit -n"]
          }
        ]
      },
      "PostToolUse": {
        "Bash": [
          {
            "policy_id": "pytest.gate",
            "mode": "annotate",
		    "command_patterns": ["pytest", "make check", "make pre-commit"]
          }
        ]
      }
    },
    "linter": {
      "files": [
        "python.conditional_imports",
        "python.catch_and_silence",
        "python.optional_returns"
      ],
      "staged": [
        "git.staged_admin_files",
        "generated_config.freshness"
      ],
      "smoke": [
        "pytest.gate"
      ],
      "full": [
        "pytest.gate",
        "typecheck.mypy",
        "typecheck.pyright"
      ]
    },
    "git": {
      "commit": {
        "pre": [
          "git.hook_bypass",
          "git.staged_admin_files",
          "lint.staged_required"
        ],
        "post": [
          "git.commit_head_advanced"
        ]
      }
    }
  }
}
```

## Semantic Graph Representation

The compiled JSON bundle should be graph-shaped, even though JSON is the
runtime serialization.

The semantic graph should model:

- principles
- policies
- evaluators
- dispatch entries
- source files
- generated artifacts
- runtime surfaces
- linter modes
- git operations
- decisions
- evidence
- suggestions

The JSON representation may use maps for fast lookup, but the logical model is
a graph of nodes and typed relationships.

Example:

```json
{
  "nodes": {
    "policy:python.conditional_imports": {
      "type": "Policy",
      "severity": "block"
    },
    "principle:no-conditional-imports": {
      "type": "Principle",
      "title": "No Conditional Imports"
    },
    "evaluator:python.conditional_imports": {
      "type": "Evaluator",
      "kind": "ast"
    }
  },
  "edges": [
    {
      "subject": "policy:python.conditional_imports",
      "predicate": "explained_by",
      "object": "principle:no-conditional-imports"
    },
    {
      "subject": "policy:python.conditional_imports",
      "predicate": "evaluated_by",
      "object": "evaluator:python.conditional_imports"
    }
  ]
}
```

Runtime code should not need graph traversal for hot-path decisions. Dispatch
tables provide the optimized runtime view. The semantic graph exists so every
decision can be explained, exported, and audited.

## RDF 1.2 Export

RDF 1.2 export should be a full-fidelity representation of the compiled policy
bundle, not a lossy summary.

This is a later feature, but the JSON model should be designed so RDF export is
straightforward.

### Goals

The RDF export should support:

- querying which ethos principles justify a policy
- querying which policies apply to a file, command, hook event, or linter mode
- tracing a runtime decision back to config source files
- comparing policy bundles over time
- joining policy data with external knowledge graphs
- storing hook and linter decision evidence in existing RDF tooling
- analyzing repo-specific overrides across many repos

### Scope

The RDF export should preserve:

- bundle metadata
- source files and source hashes
- principles
- principle tags and relationships
- policies
- policy severities and supported modes
- policy source locations
- evaluator definitions
- dispatch indexes
- path and command matchers
- linter modes
- git operation rules
- generated artifact relationships
- suggestions and messages
- runtime decisions when exported from state
- evidence attached to runtime decisions

### Formats

Preferred formats:

- TriG for named graphs and provenance.
- Turtle for compact single-graph export.
- N-Quads when line-oriented processing is useful.

RDF export should not replace JSON for hook runtime use.

### Named Graphs

TriG export should separate stable policy data from runtime evidence.

Example graph names:

- `urn:coding-ethos:policy-bundle:<bundle-hash>:principles`
- `urn:coding-ethos:policy-bundle:<bundle-hash>:policies`
- `urn:coding-ethos:policy-bundle:<bundle-hash>:dispatch`
- `urn:coding-ethos:policy-bundle:<bundle-hash>:sources`
- `urn:coding-ethos:runtime:<repo-id>:decisions`

### Example RDF

Example policy representation:

```turtle
@prefix ethos: <https://blackcat.ca/ns/coding-ethos#> .
@prefix policy: <urn:coding-ethos:policy:> .
@prefix principle: <urn:coding-ethos:principle:> .
@prefix evaluator: <urn:coding-ethos:evaluator:> .

policy:python.conditional_imports
  a ethos:Policy ;
  ethos:category "python" ;
  ethos:defaultSeverity ethos:Block ;
  ethos:explainedBy principle:no-conditional-imports ;
  ethos:evaluatedBy evaluator:python.conditional_imports ;
  ethos:message "Required dependencies should fail immediately; ImportError fallback creates a soft dependency path." ;
  ethos:suggestion "Remove the conditional import or configure an explicit exemption." .

principle:no-conditional-imports
  a ethos:Principle ;
  ethos:order 3 ;
  ethos:title "No Conditional Imports" ;
  ethos:directive "Treat required imports as hard dependencies and fail immediately if they are missing." ;
  ethos:tag "dependency", "startup", "reliability" .

evaluator:python.conditional_imports
  a ethos:Evaluator ;
  ethos:evaluatorKind ethos:AstEvaluator .
```

Example runtime decision representation:

```turtle
@prefix decision: <urn:coding-ethos:decision:> .

decision:123
  a ethos:PolicyDecision ;
  ethos:appliedPolicy policy:python.conditional_imports ;
  ethos:decision ethos:Block ;
  ethos:affectedFile "src/app/plugins.py" ;
  ethos:message "Required dependencies should fail immediately." ;
  ethos:evidenceHash "sha256:..." .
```

RDF 1.2 features can be used to annotate statements and decisions where useful,
but the export should remain consumable by standard RDF tooling. If a target
toolchain has uneven RDF 1.2 support, the exporter should also be able to emit
classic RDF reification or named-graph provenance.

### Export Timing

RDF export should be optional at first.

Suggested commands:

```bash
coding-ethos policy export-rdf --repo .
coding-ethos policy export-rdf --repo . --format trig
coding-ethos policy export-rdf --repo . --include-runtime-decisions
```

The first implementation of the policy compiler should not block on RDF export,
but the JSON schema should avoid choices that would make full-fidelity RDF
export difficult later.

## Evaluator Types

The compiler should select evaluator types rather than compiling all checks
into regex patterns.

Supported evaluator kinds should include:

- `argv`: structured command arguments, best for git.
- `shell`: shell command scan, useful for unsafe Bash patterns.
- `path`: path ownership and protected path checks.
- `ast`: Python AST checks.
- `text`: simple content checks.
- `config`: generated config freshness and schema checks.
- `git_state`: staged files, `HEAD`, branch, and worktree checks.
- `external`: Ruff, mypy, pyright, pytest, Gemini, and other tools.

Runtime components should implement evaluator logic. The compiled bundle should
configure evaluator use.

## Severity and Modes

Policies should support these modes where practical:

- `off`: do not evaluate.
- `record`: log only.
- `advise`: add context to the agent or linter output.
- `annotate`: explain completed tool output in policy terms.
- `ask`: require confirmation or permission before proceeding.
- `block`: deny the action or fail the linter.

The same policy may use different modes in different surfaces.

Example:

- `python.conditional_imports` may advise during `PreToolUse` edits.
- It may block in `coding-ethos lint --staged`.
- It may block in pre-commit.

## Decision Shape

Every runtime should return a common decision shape.

Example:

```json
{
  "decision": "block",
  "policy_id": "git.hook_bypass",
  "severity": "block",
  "principle_ids": ["one-path-for-critical-operations"],
  "message": "Blocked --no-verify. This repo treats hooks as mandatory quality gates.",
  "suggestion": "Run the hook, inspect the failure, and fix the underlying issue.",
  "evidence": {
    "command": ["git", "commit", "--no-verify"]
  }
}
```

This same object should be renderable as:

- Claude hook output.
- git-wrapper stderr.
- unified linter output.
- pre-commit summary output.
- session-end advice.

## Compiler Pipeline

The compiler should run this pipeline:

```text
load sources
  -> validate source schemas
  -> merge repo overlays
  -> normalize principles
  -> normalize enforcement config
  -> attach principles to policies
  -> compile evaluators
  -> build dispatch indexes
  -> write policy-bundle.json
  -> write hash and metadata
```

### Source Loading

Inputs:

- shared ethos: `coding_ethos.yml`
- repo ethos overlay: `repo_ethos.yml`
- shared enforcement config: `config.yaml`
- repo enforcement overlay: `repo_config.yaml`

The compiler should use existing `coding_ethos` loaders where possible.

### Validation

Validation should fail fast for:

- unknown policy IDs
- unknown principle IDs
- invalid severities
- invalid event names
- invalid tool matchers
- invalid path matcher syntax
- missing evaluator implementations
- repo overrides that reference disabled or unavailable policies

### Normalization

Normalization should:

- resolve aliases
- merge repo overlays
- apply defaults
- convert relative paths to repo-relative paths
- attach generated artifact paths
- attach source locations for diagnostics
- attach ethos principle references

### Dispatch Compilation

Dispatch compilation should build compact indexes for:

- hook event and tool dispatch
- linter mode dispatch
- git operation dispatch
- path pattern dispatch
- command pattern dispatch
- generated artifact checks

Dispatch tables should be small enough for hook runtimes to load quickly.

### Metadata and Hashing

The compiler should write metadata alongside the bundle.

Example:

```json
{
  "bundle_hash": "sha256:...",
  "source_hashes": {
    "coding_ethos.yml": "sha256:...",
    "repo_ethos.yml": "sha256:...",
    "config.yaml": "sha256:...",
    "repo_config.yaml": "sha256:..."
  },
  "generated_at": "2026-04-24T12:00:00Z"
}
```

Runtime components can use this to detect stale compiled policy.

## Storage Layout

Recommended generated layout:

```text
.git/coding-ethos-hooks/
  policy/
    policy-bundle.json
    policy-metadata.json
    policy-summary.md
    policy-bundle.trig
    policy-bundle.ttl
  lint-state/
  agent-state/
  git-state/
  bin/
```

The policy bundle is generated state. It should not normally be committed.

Repo-owned policy source files remain committed:

- `coding_ethos.yml`
- `repo_ethos.yml`
- `config.yaml`
- `repo_config.yaml`

## Runtime Consumers

### Agent Hooks

Agent hooks consume:

- `dispatch.hooks`
- policy messages
- principle quick refs
- severity modes
- path and command matchers

Agent hooks should use the bundle to decide when to block, ask, advise,
annotate, record, or route.

### Unified Linter

The unified linter consumes:

- `dispatch.linter`
- evaluator definitions
- source locations
- principle IDs
- severity
- suggestions

The linter should emit the shared decision shape.

### Git Wrapper

The git wrapper consumes:

- `dispatch.git`
- protected branch policy
- admin-only file policy
- bypass policy
- worktree policy
- post-action verification policy

The wrapper should use structured argv and git-state evaluators rather than
duplicating hook regex logic.

### Pre-Commit

Pre-commit consumes:

- the same policy definitions
- generated tool configs
- linter mode mappings

Pre-commit remains the hard commit-time gate. If pre-commit and the unified
linter disagree, that is a `coding-ethos` bug.

### Prompt Packs

Prompt packs consume:

- principles
- policy descriptions
- source paths
- repo context
- relevant check metadata

Prompt packs should stay derived from the same compiled model.

## Example End-to-End Flow

1. A repo updates `repo_config.yaml` to require smoke tests before commit.
2. `coding-ethos policy compile` regenerates `policy-bundle.json`.
3. Agent hooks now know `git commit` requires current staged content to be
   covered by `coding-ethos lint --staged` and the configured smoke gate.
4. The unified linter knows which pytest command implements the smoke gate.
5. The git wrapper blocks unmanaged commits that are not covered by the
   required linter state.
6. Pre-commit still runs the authoritative gate at commit time.

## CLI

Suggested commands:

```bash
coding-ethos policy compile --repo .
coding-ethos policy validate --repo .
coding-ethos policy explain python.conditional_imports
coding-ethos policy dump --json
coding-ethos policy doctor --repo .
```

The linter, hooks, and git wrapper should compile automatically when needed,
but explicit commands are useful for debugging.

Gemini-backed corpus review remains a pre-commit/pre-push concern.
The agent hook runtime is intentionally local policy evaluation only: it reads
agent hook JSON from stdin, applies the compiled dispatch table, and exits with
code 2 for blocking decisions.

## Initial Go Implementation

The first Go implementation lives under `go/`.

Current package layout:

```text
go/
  cmd/
    coding-ethos-git/
      main.go
    coding-ethos-hook/
      main.go
    coding-ethos-lint/
      main.go
    coding-ethos-policy/
      main.go
  internal/
    evaluators/
    gitwrap/
    hooks/
    lint/
    policy/
      bundle.go
      compiler.go
      decision.go
      explain.go
      json.go
      metadata.go
      summary.go
      validate.go
```

Current command surface:

```bash
make go-tools-test
make go-tools-build
make go-tools-install
```

`GO_TOOLS_BIN_DIR` is overrideable. Use that override when testing install
behavior without touching a parent repo's `.git/coding-ethos-hooks/bin`:

```bash
make go-tools-install GO_TOOLS_BIN_DIR=/tmp/coding-ethos-tools-bin
```

The installed tools can then be exercised against a temporary policy bundle:

```bash
/tmp/coding-ethos-tools-bin/coding-ethos-policy compile \
  --primary coding_ethos.yml \
  --config config.yaml \
  --out-dir /tmp/coding-ethos-policy

/tmp/coding-ethos-tools-bin/coding-ethos-lint \
  --bundle /tmp/coding-ethos-policy/policy-bundle.json \
  --staged \
  --argv "git commit --no-verify -m test" \
  --json

/tmp/coding-ethos-tools-bin/coding-ethos-hook \
  --bundle /tmp/coding-ethos-policy/policy-bundle.json \
  --json

/tmp/coding-ethos-tools-bin/coding-ethos-git \
  --bundle /tmp/coding-ethos-policy/policy-bundle.json \
  --check-only \
  --json \
  commit --no-verify -m test
```

The initial compiler reads:

- `coding_ethos.yml`
- `config.yaml`
- optional `repo_config.yaml`

It currently emits:

- `policy-bundle.json`
- `policy-metadata.json`
- `policy-summary.md`

The first compiled policy subset covers:

- `python.conditional_imports`
- `python.optional_returns`
- `python.catch_and_silence`
- `python.structured_logging`
- `python.direct_imports`
- `pytest.gate`
- `generated_config.freshness`
- `git.hook_bypass`
- `git.staged_admin_files`
- `git.commit_head_advanced`

This is intentionally not the final compiler surface. It establishes the shared
Go package, the bundle schema, validation, deterministic output, source hashing,
and the first real YAML-to-policy compilation path.

## Guardrails

- Do not make runtime hooks parse full YAML policy on every invocation.
- Do not duplicate policy semantics in hook code, git wrapper code, and
  pre-commit code.
- Do not make the compiled bundle provider-specific.
- Do not make regex the default evaluator for structured commands.
- Do not let generated policy state become hand-edited source.
- Do not allow stale compiled policy to masquerade as current.
- Do not emit generic advice when a policy can provide a specific suggestion.
- Do not treat advisory linter output as proof of readiness.

## First Implementation Scope

The first implementation should compile enough policy for:

- hook bypass blocking
- dangerous git command blocking
- protected path checks
- staged admin-file checks
- commit `HEAD` verification
- generated config freshness
- pytest smoke gate metadata
- a small set of Python policy checks already present in pre-commit

It should produce:

- `policy-bundle.json`
- `policy-metadata.json`
- `policy-summary.md`
- a validation command
- at least one hook consumer test
- at least one linter consumer test
- at least one git-wrapper consumer test

The goal of the first pass is not to cover every ethos principle. The goal is
to establish the common bundle format and prove that hooks, linter, pre-commit,
and git wrapper can consume the same policy source.
