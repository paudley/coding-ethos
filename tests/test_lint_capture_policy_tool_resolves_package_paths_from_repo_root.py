# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed lint capture policy tool tests.

Responsibility is narrow.
Public imports stay aligned."""

import os
from pathlib import Path

from tests.lint_capture_support import (
    REPO_ROOT,
    _prepare_consumer_repo,
    _run,
    _sync_consumer_tool_configs,
)


def test_policy_tool_resolves_package_paths_from_repo_root(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    (consumer / "repo_config.yml").write_text(
        """
version: 1
python:
  source_paths:
    - lbox-platform/lib/python/lbox
  extra_paths:
    - lbox-platform/lib/python
""".lstrip(),
        encoding="utf-8",
    )
    _sync_consumer_tool_configs(consumer)
    nested = consumer / "lbox-platform" / "lib" / "python"
    package = nested / "lbox" / "corpus"
    package.mkdir(parents=True)
    target = package / "inline_migration.py"
    target.write_text("import os\n\nVALUE = 1\n", encoding="utf-8")

    env = os.environ.copy()
    env["CODE_ETHOS_HOOK_OUTPUT_FORMAT"] = "toon"
    result = _run(
        [
            str(REPO_ROOT / "bin" / "coding-ethos-run"),
            "policy-tool",
            "ruff",
            "check",
            "lbox/corpus/inline_migration.py",
        ],
        cwd=consumer,
        env=env,
        check=False,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 1, output
    assert "F401" in output
    assert "lbox-platform/lib/python/lbox/corpus/inline_migration.py" in output
    assert "coding-ethos/lbox/corpus/inline_migration.py" not in output
    assert str(consumer) not in output
