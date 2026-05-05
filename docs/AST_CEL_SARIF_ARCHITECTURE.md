<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# AST, CEL, and SARIF Architecture

`coding-ethos` policy work follows one architecture:

1. Go collects facts.
2. CEL decides policy when the rule can be expressed over those facts.
3. SARIF reports exact, stable, remediation-ready findings.

This is the first path to use for new source-aware enforcement. Do not add a
new ad hoc text scanner, one-off AST traversal, or policy-specific parser unless
the shared fact path cannot represent the required input yet. In that case,
extend the fact collector first.

## Responsibilities

### Go Fact Collection

Go owns parsing and host inspection. It is responsible for:

- Tree-sitter parsing and parser lifecycle.
- File, command, shell, Git, diff, and hook context collection.
- Normalized fact records with stable field names.
- Syntax recovery and fail-fast errors where policy cannot run safely.
- Diagnostic location metadata: file, line, column, end line, node kind, symbol
  kind, symbol path, and parent symbol path.

For Python, the reusable fact surface is `python_ast`. It is populated from the
same Tree-sitter fact collector used by the compiled Python evaluators. It
includes imports, calls, functions, classes, assignments, lambdas, exception
handlers, symbol context, ancestry flags, and initial function signature facts.

### CEL Policy Decisions

CEL owns configurable policy predicates. If a decision can be expressed as a
boolean over normalized facts, put that decision beside its ETHOS principle in
`coding_ethos.yml` or the compiled policy configuration.

Examples:

```cel
python_ast.exists(fact, fact.is_dynamic_import)
```

```cel
python_ast.exists(
  fact,
  fact.node_kind == "function_definition" &&
  fact.parameter_count > 5
)
```

Do not put parsing logic, path probing, Git execution, or semantic extraction in
CEL. Add or extend Go facts instead, then keep the policy expression small and
auditable.

### SARIF Reporting

SARIF owns durable machine-readable output. AST-backed findings must preserve:

- `ruleId` from the policy ID.
- Exact artifact location and region.
- AST properties such as `ast_node_kind`, `ast_symbol_kind`,
  `ast_symbol_path`, and `ast_parent_symbol_path`.
- Partial fingerprints that remain stable across unrelated line movement.
- Principle and skill metadata so GitHub code scanning and MCP remediation
  advice point to the same explanation.

## Extension Workflow

When porting guidance from `pyqa_lint` or adding another source-aware policy:

1. Identify the fact shape needed by the rule.
2. Extend the shared Go fact collector if the fact does not exist.
3. Add CEL schema coverage for the fact when the decision should be
   configurable.
4. Express the policy in CEL when possible.
5. Emit diagnostics through the existing policy path so SARIF receives the AST
   metadata automatically.
6. Add tests at the fact, CEL, evaluator, and SARIF layers proportional to the
   new behavior.

## Current Python Fact Uses

The current Python AST policies already use this path for:

- Conditional import enforcement:
  - nested imports
  - `TYPE_CHECKING` imports
  - `except ImportError` / `ModuleNotFoundError`
  - module `__getattr__`
  - `__import__`
  - `importlib.import_module`
- Functional idiom guidance:
  - assigned lambdas
  - returned or assigned nested closure factories

Future ports from `pyqa_lint` should add facts or CEL predicates for strict
typing, signature width, docstring sections, value-type dunder inference,
interface boundaries, DI composition roots, cache wrappers, Python hygiene, and
package documentation conventions.
