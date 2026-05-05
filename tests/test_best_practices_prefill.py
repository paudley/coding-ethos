# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT
"""Tests for the OpenSSF Best Practices prefill generator.

The tests keep the generated URLs aligned with the Best Practices automation
contract. They cover field normalization, unknown-value omission, and
section-aware URL routing because malformed proposal URLs can erase useful
operator time without changing the badge record.
"""

from tools import best_practices_prefill as module

type JSONValue = module.JSONValue


def test_prefill_url_normalizes_osps_field_names() -> None:
    criteria = [
        module.CriterionState(
            criterion="OSPS-AC-01.01",
            status="Met",
            justification="MFA enforced",
            source="prefill",
        )
    ]

    url = module.build_prefill_url(
        12737,
        "baseline-1",
        criteria,
        include_baseline=True,
    )

    assert "osps_ac_01_01_status=Met" in url
    assert "osps_ac_01_01_justification=MFA+enforced" in url


def test_report_tracks_unresolved_after_prefill_merge() -> None:
    current: dict[str, JSONValue] = {
        "build_repeatable_status": "Unmet",
        "build_repeatable_justification": "old",
        "two_person_review_status": "?",
        "two_person_review_justification": None,
    }
    manifest: dict[str, JSONValue] = {
        "criteria": {
            "build_repeatable": {
                "status": "Met",
                "justification": "documented",
            }
        }
    }

    merged = module.merge_current_and_prefill(current, manifest)
    report = module.build_report(merged)

    assert report["counts"]["Met"] == 1
    assert report["counts"]["?"] == 1
    assert report["unresolved"] == [
        {
            "criterion": "two_person_review",
            "status": "?",
            "source": "current",
            "justification": "",
        }
    ]


def test_prefill_url_omits_unknowns() -> None:
    criteria = [
        module.CriterionState(
            criterion="build_repeatable",
            status="Met",
            justification="documented",
            source="prefill",
        ),
        module.CriterionState(
            criterion="signed_releases",
            status="?",
            justification="needs confirmation",
            source="prefill",
        ),
    ]

    url = module.build_prefill_url(12737, "gold", criteria)

    assert "build_repeatable_status=Met" in url
    assert "signed_releases_status" not in url


def test_prefill_url_omits_current_values_by_default() -> None:
    criteria = [
        module.CriterionState(
            criterion="description_good",
            status="Met",
            justification="already accepted",
            source="current",
        ),
        module.CriterionState(
            criterion="build_repeatable",
            status="Met",
            justification="documented",
            source="prefill",
        ),
    ]

    url = module.build_prefill_url(12737, "gold", criteria)

    assert "build_repeatable_status=Met" in url
    assert "description_good_status" not in url


def test_prefill_urls_can_be_chunked_by_length() -> None:
    criteria = [
        module.CriterionState(
            criterion="build_repeatable",
            status="Met",
            justification="documented " * 12,
            source="prefill",
        ),
        module.CriterionState(
            criterion="signed_releases",
            status="Met",
            justification="signed " * 12,
            source="prefill",
        ),
    ]

    urls = module.build_prefill_urls(
        12737,
        "gold",
        criteria,
        max_url_length=260,
    )

    assert len(urls) == 2
    assert "build_repeatable_status=Met" in urls[0]
    assert "signed_releases_status=Met" in urls[1]


def test_gold_prefill_urls_use_owning_metal_sections() -> None:
    criteria = [
        module.CriterionState(
            criterion="build_repeatable",
            status="Met",
            justification="silver criterion",
            source="prefill",
        ),
        module.CriterionState(
            criterion="build_reproducible",
            status="Met",
            justification="gold criterion",
            source="prefill",
        ),
    ]

    urls = module.build_prefill_urls(12737, "gold", criteria)

    assert len(urls) == 2
    assert "/silver/edit?" in urls[0]
    assert "build_repeatable_status=Met" in urls[0]
    assert "/gold/edit?" in urls[1]
    assert "build_reproducible_status=Met" in urls[1]


def test_unknown_metal_criteria_are_not_emitted() -> None:
    criteria = [
        module.CriterionState(
            criterion="homepage_url",
            status="Met",
            justification="not an automation criterion",
            source="prefill",
        )
    ]

    urls = module.build_prefill_urls(12737, "gold", criteria)

    assert urls == ["https://www.bestpractices.dev/projects/12737/gold/edit?"]
