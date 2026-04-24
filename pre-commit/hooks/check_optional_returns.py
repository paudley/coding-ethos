#!/usr/bin/env python3
"""Pre-commit hook to block ``| None`` type annotations in Python files.

Scans all type annotation positions for ``| None`` usage:

- Return types: ``def foo() -> X | None:``
- Parameters: ``def foo(x: X | None):``
- Variable annotations: ``x: X | None``
- Class variables: ``class Foo: x: X | None``

Per ETHOS §5, required dependencies and internal state must not use optional
types. Functions must return their declared type or raise an exception.

Auto-exemptions:
    ``__exit__`` and ``__aexit__`` parameters are exempt (Python protocol
    requires ``| None`` for exc_type, exc_val, exc_tb).

Usage:
    Pre-commit: python pre-commit/hooks/check_optional_returns.py [files...]

Exit codes:
    0: All checks passed
    1: One or more files contain ``| None`` annotations
"""

import ast
import sys
from pathlib import Path
from typing import Final, NamedTuple

MIN_REQUIRED_ARGS: Final[int] = 2


class Violation(NamedTuple):
    """A ``| None`` type annotation violation.

    Args:
        line: Source line number (1-indexed).
        context: Human-readable description of the violation.

    """

    line: int
    context: str


def _contains_none_union(node: ast.expr) -> bool:
    """Check if an AST annotation node contains ``X | None``.

    Detects ``X | None``, ``None | X``, and nested unions like
    ``X | Y | None`` in type annotations.

    Args:
        node: AST expression node representing a type annotation.

    Returns:
        True if the annotation contains a union with None.

    """
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.BitOr):
        if isinstance(node.right, ast.Constant) and node.right.value is None:
            return True
        if isinstance(node.left, ast.Constant) and node.left.value is None:
            return True
        return _contains_none_union(node.left) or _contains_none_union(node.right)
    return False


def _target_name(node: ast.expr) -> str:
    """Extract a readable name from an assignment target.

    Args:
        node: AST expression node (Name, Attribute, etc.).

    Returns:
        Human-readable name string.

    """
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        return f"{_target_name(node.value)}.{node.attr}"
    return "<expr>"


_EXIT_METHODS: Final[frozenset[str]] = frozenset({"__exit__", "__aexit__"})


class _NoneUnionVisitor(ast.NodeVisitor):
    """AST visitor that collects ``| None`` violations with context.

    Tracks class nesting to distinguish class variables from local variables.
    Resets class context inside function bodies so method-local annotations
    are reported as variables, not class variables.

    Auto-exempts ``__exit__``/``__aexit__`` methods.
    """

    def __init__(self) -> None:
        self.violations: list[Violation] = []
        self._in_class: bool = False

    def visit_ClassDef(self, node: ast.ClassDef) -> None:
        """Enter class scope — annotations here are class variables."""
        old = self._in_class
        self._in_class = True
        self.generic_visit(node)
        self._in_class = old

    def visit_FunctionDef(self, node: ast.FunctionDef) -> None:
        """Check function return type and parameters, then visit body."""
        self._check_function(node)
        old = self._in_class
        self._in_class = False  # Method body annotations are local variables
        self.generic_visit(node)
        self._in_class = old

    def visit_AsyncFunctionDef(self, node: ast.AsyncFunctionDef) -> None:
        """Check async function return type and parameters, then visit body."""
        self._check_function(node)
        old = self._in_class
        self._in_class = False
        self.generic_visit(node)
        self._in_class = old

    def visit_AnnAssign(self, node: ast.AnnAssign) -> None:
        """Check variable and class variable annotations."""
        if _contains_none_union(node.annotation):
            target = _target_name(node.target)
            context = "class variable" if self._in_class else "variable"
            self.violations.append(
                Violation(node.lineno, f"| None {context}: {target}")
            )
        self.generic_visit(node)

    def _check_arg(self, arg: ast.arg, prefix: str = "") -> None:
        """Check a single function argument annotation for ``| None``.

        Args:
            arg: AST argument node.
            prefix: Display prefix (e.g., ``"*"`` or ``"**"``).

        """
        ann = arg.annotation
        if ann is not None and _contains_none_union(ann):
            self.violations.append(
                Violation(ann.lineno, f"| None parameter: {prefix}{arg.arg}")
            )

    def _check_function(self, node: ast.FunctionDef | ast.AsyncFunctionDef) -> None:
        """Check return annotation and all parameter annotations.

        Auto-exempts ``__exit__`` and ``__aexit__`` methods (Python protocol
        requires ``| None`` for exception parameters).

        Args:
            node: Function or async function definition node.

        """
        # Auto-exempt __exit__/__aexit__ — Python protocol requires | None
        if node.name in _EXIT_METHODS:
            return

        # Return type
        returns = node.returns
        if returns is not None and _contains_none_union(returns):
            self.violations.append(
                Violation(returns.lineno, f"| None return: {node.name}()")
            )

        # Positional, positional-only, and keyword-only parameters
        for arg in [*node.args.args, *node.args.posonlyargs, *node.args.kwonlyargs]:
            self._check_arg(arg)

        # *args
        if node.args.vararg:
            self._check_arg(node.args.vararg, prefix="*")

        # **kwargs
        if node.args.kwarg:
            self._check_arg(node.args.kwarg, prefix="**")


def scan_file(path: Path) -> list[tuple[int, str]]:
    """Scan a Python file for ``| None`` type annotations.

    Checks all annotation positions: return types, parameters,
    variable annotations, and class variable annotations.

    Args:
        path: Path to the Python file.

    Returns:
        List of (line_number, description) tuples for violations found.

    Raises:
        OSError: If the file cannot be read.
        SyntaxError: If the file contains invalid Python syntax.

    """
    text = path.read_text(encoding="utf-8")
    tree = ast.parse(text, filename=str(path))

    visitor = _NoneUnionVisitor()
    visitor.visit(tree)
    return [(v.line, v.context) for v in sorted(visitor.violations)]


def main() -> int:
    """Scan files for banned | None annotations."""
    if len(sys.argv) < MIN_REQUIRED_ARGS:
        sys.stderr.write("No files to check\n")
        return 0

    files = [
        Path(f) for f in sys.argv[1:] if Path(f).suffix == ".py" and Path(f).exists()
    ]
    if not files:
        return 0

    has_errors = False

    for path in files:
        try:
            violations = scan_file(path)
        except SyntaxError as exc:
            print(f"ERROR: {path}: {exc}")
            has_errors = True
            continue
        except OSError as exc:
            print(f"ERROR: {path}: Could not read file: {exc}")
            has_errors = True
            continue

        for lineno, context in violations:
            has_errors = True
            print(f"ERROR: {path}:{lineno}: {context}")

    if has_errors:
        print("\n" + "=" * 60)
        print("Optional type annotation check FAILED")
        print("All types must be non-optional. Use exceptions, not | None.")
        print("=" * 60)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
