# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from pathlib import Path

from coding_ethos import format_yaml_file, render_yaml


def test_render_yaml_folds_long_single_line_scalars() -> None:
    rendered = render_yaml(
        {
            "summary": (
                "This summary line is intentionally long enough to require folded "
                "YAML output so prose stays within the repo line-length policy."
            )
        }
    )

    assert "summary: >-" in rendered
    assert (
        "This summary line is intentionally long enough to require folded" in rendered
    )
    assert "repo line-length policy." in rendered


def test_format_yaml_file_preserves_comments_and_folds_prose(tmp_path: Path) -> None:
    target = tmp_path / "sample.yml"
    target.write_text(
        (
            "# top comment\n"
            "item:\n"
            '  summary: "This summary line is intentionally long enough to require '
            'folded YAML output so prose stays within the repo line-length policy."\n'
        ),
        encoding="utf-8",
    )

    format_yaml_file(target)
    reformatted = target.read_text(encoding="utf-8")

    assert "# top comment" in reformatted
    assert "summary: >-" in reformatted
