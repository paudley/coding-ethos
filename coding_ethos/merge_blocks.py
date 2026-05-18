# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed Markdown block insertion and removal helpers.

Responsibility is narrow.
Public imports stay aligned.
"""

from coding_ethos.merge_strip import strip_managed_blocks

__all__ = [
    "inject_addendum_block",
    "inject_import_block",
    "strip_managed_blocks",
]


def block_markers(target_name: str, block_name: str) -> tuple[str, str]:
    """Provide focused helper behavior for the split module."""
    token = f"{block_name} {target_name}"
    return (
        f"<!-- coding-ethos:begin {token} -->",
        f"<!-- coding-ethos:end {token} -->",
    )


def remove_managed_block(
    content: str, begin_marker: str, end_marker: str
) -> tuple[str, bool]:
    """Provide focused helper behavior for the split module."""
    start = content.find(begin_marker)
    if start == -1:
        return content, False

    end = content.find(end_marker, start)
    if end == -1:
        return content, False

    end += len(end_marker)
    before = content[:start].rstrip("\n")
    after = content[end:].lstrip("\n")

    merged = f"{before}\n\n{after}" if before and after else before + after

    if merged and not merged.endswith("\n"):
        merged += "\n"
    return merged, True


def build_managed_block(begin_marker: str, end_marker: str, body: str) -> str:
    """Provide focused helper behavior for the split module."""
    return f"{begin_marker}\n{body.rstrip()}\n{end_marker}"


def append_managed_block(
    content: str, *, target_name: str, block_name: str, body: str
) -> str:
    """Provide focused helper behavior for the split module."""
    begin_marker, end_marker = block_markers(target_name, block_name)
    base_content, _ = remove_managed_block(content, begin_marker, end_marker)
    block = build_managed_block(begin_marker, end_marker, body)

    if not base_content.strip():
        return block + "\n"
    return base_content.rstrip() + "\n\n" + block + "\n"


def prepend_managed_block(
    content: str, *, target_name: str, block_name: str, body: str
) -> str:
    """Provide focused helper behavior for the split module."""
    begin_marker, end_marker = block_markers(target_name, block_name)
    base_content, _ = remove_managed_block(content, begin_marker, end_marker)
    block = build_managed_block(begin_marker, end_marker, body)

    if not base_content.strip():
        return block + "\n"
    return block + "\n\n" + base_content.lstrip("\n")


def inject_import_block(
    *,
    target_name: str,
    existing_content: str,
    import_lines: list[str],
) -> str:
    """Inject required managed import lines into an existing root file."""
    if not import_lines:
        return existing_content

    begin_marker, end_marker = block_markers(target_name, "imports")
    content_without_block, _ = remove_managed_block(
        existing_content, begin_marker, end_marker
    )
    present_lines = {line.strip() for line in content_without_block.splitlines()}
    missing_imports = [line for line in import_lines if line not in present_lines]
    if not missing_imports:
        return content_without_block
    return prepend_managed_block(
        content_without_block,
        target_name=target_name,
        block_name="imports",
        body="\n".join(missing_imports),
    )


def inject_addendum_block(
    *,
    target_name: str,
    existing_content: str,
    addendum_content: str,
) -> str:
    """Append the managed additive ethos block to an existing root file."""
    return append_managed_block(
        existing_content,
        target_name=target_name,
        block_name="managed",
        body=addendum_content,
    )
