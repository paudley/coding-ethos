# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Makefile parent upgrade contract tests.

Responsibility is narrow.
Public imports stay aligned."""

from tests.makefile_contract_support import _makefile_lines, _target_block


def test_upgrade_runs_full_parent_submodule_refresh_workflow() -> None:
    makefile, _ = _makefile_lines()
    upgrade_block = _target_block(makefile, "upgrade")

    assert (
        "upgrade: upgrade-parent-submodule build parent-install parent-check "
        "parent-lint cutover-verify ##"
    ) in upgrade_block


def test_upgrade_parent_submodule_aliases_parent_update_submodule() -> None:
    makefile, _ = _makefile_lines()
    alias_block = _target_block(makefile, "upgrade-parent-submodule")

    assert "upgrade-parent-submodule: parent-update-submodule ##" in alias_block
