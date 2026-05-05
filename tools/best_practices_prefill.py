# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT
"""Generate OpenSSF Best Practices Badge prefill review URLs.

The helper compares the public Best Practices project JSON with the
repo-maintained proposal manifest. It emits a gap report and section-correct
edit URLs so project maintainers can review proposed badge updates without
resetting unknown fields.
"""

import argparse
import http
import http.client
import json
import sys
import urllib.parse
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import TypedDict, cast

type JSONValue = (
    None | bool | int | float | str | list["JSONValue"] | dict[str, "JSONValue"]
)
type JSONObject = dict[str, JSONValue]
type ReportItem = dict[str, str]


class Report(TypedDict):
    """Structured Best Practices prefill report."""

    counts: dict[str, int]
    proposed_count: int
    proposed_criteria: list[str]
    unresolved: list[ReportItem]


PROJECT_ID = 12737
REPO_URL = "https://github.com/paudley/coding-ethos"
PROJECT_JSON_URL = f"https://www.bestpractices.dev/projects/{PROJECT_ID}.json"
DEFAULT_PREFILL = Path("docs/best_practices_prefill.json")

METAL_CRITERION_SECTIONS: dict[str, str] = {
    "assurance_case": "silver",
    "build_repeatable": "silver",
    "crypto_algorithm_agility": "silver",
    "crypto_certificate_verification": "silver",
    "crypto_credential_agility": "silver",
    "crypto_tls12": "silver",
    "crypto_used_network": "silver",
    "crypto_verification_private": "silver",
    "hardening": "silver",
    "implement_secure_design": "silver",
    "input_validation": "silver",
    "signed_releases": "silver",
    "test_policy_mandated": "silver",
    "version_tags_signed": "silver",
    "achieve_silver": "gold",
    "build_reproducible": "gold",
    "code_review_standards": "gold",
    "contributors_unassociated": "gold",
    "copyright_per_file": "gold",
    "hardened_site": "gold",
    "license_per_file": "gold",
    "require_2FA": "gold",
    "secure_2FA": "gold",
    "security_review": "gold",
    "small_tasks": "gold",
    "test_branch_coverage80": "gold",
    "test_statement_coverage90": "gold",
    "two_person_review": "gold",
}


class BestPracticesPrefillError(ValueError):
    """Raised when Best Practices prefill inputs are malformed."""


@dataclass(frozen=True)
class CriterionState:
    """Current or proposed state for one Best Practices criterion."""

    criterion: str
    status: str
    justification: str
    source: str


def main(argv: list[str] | None = None) -> int:
    """Run the Best Practices prefill generator."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--current",
        default=PROJECT_JSON_URL,
        help="Current project JSON URL or local JSON path.",
    )
    parser.add_argument(
        "--prefill",
        default=str(DEFAULT_PREFILL),
        help="Checked-in proposal manifest.",
    )
    parser.add_argument(
        "--section",
        default="choose",
        help="Best Practices section path: choose, passing, silver, or gold.",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Emit machine-readable merged state and report.",
    )
    parser.add_argument(
        "--include-baseline",
        action="store_true",
        help="Include OSPS Baseline criteria in URLs and unresolved reports.",
    )
    parser.add_argument(
        "--include-current",
        action="store_true",
        help="Include current non-prefill answers in the generated URL.",
    )
    parser.add_argument(
        "--max-url-length",
        type=int,
        default=0,
        help=(
            "Split prefill output into multiple URLs no longer than this "
            "length where practical. Use 0 to emit one URL."
        ),
    )
    args = parser.parse_args(argv)

    current = load_current_project(args.current)
    manifest = load_prefill_manifest(Path(args.prefill))
    merged = merge_current_and_prefill(current, manifest)
    report = build_report(merged, include_baseline=args.include_baseline)
    urls = build_prefill_urls(
        PROJECT_ID,
        args.section,
        merged,
        include_baseline=args.include_baseline,
        include_current=args.include_current,
        max_url_length=args.max_url_length,
    )
    url = urls[0] if len(urls) == 1 else ""

    if args.json:
        print(
            json.dumps(
                {
                    "project_id": PROJECT_ID,
                    "section": args.section,
                    "url": url,
                    "urls": urls,
                    "report": report,
                    "criteria": [state.__dict__ for state in merged],
                },
                indent=2,
                sort_keys=True,
            )
        )

        return 0

    print(f"project: {PROJECT_ID}")
    print(f"section: {args.section}")
    if len(urls) == 1:
        print(f"url: {urls[0]}")
    else:
        print(f"urls: {len(urls)}")
        for index, chunked_url in enumerate(urls, start=1):
            print(f"url[{index}]: {chunked_url}")
    print()
    print_report(report)

    return 0


def load_current_project(source: str) -> JSONObject:
    """Load current project data from Best Practices or a local file."""
    if source.startswith(("http://", "https://")):
        payload = fetch_best_practices_json(source)
    else:
        payload = Path(source).read_bytes()

    data: object = json.loads(payload)
    if not isinstance(data, dict):
        msg = "current project JSON must be an object"
        raise BestPracticesPrefillError(msg)

    return typed_json_object(cast(object, data), "current project JSON")


def fetch_best_practices_json(source: str) -> bytes:
    """Fetch JSON from the Best Practices project host."""
    parsed = urllib.parse.urlparse(source)
    if parsed.scheme != "https" or parsed.netloc != "www.bestpractices.dev":
        msg = "remote project JSON must use https://www.bestpractices.dev"
        raise BestPracticesPrefillError(msg)

    connection = http.client.HTTPSConnection(parsed.netloc, timeout=30)
    try:
        path = parsed.path
        if parsed.query:
            path = f"{path}?{parsed.query}"
        connection.request("GET", path, headers={"Accept": "application/json"})
        response = connection.getresponse()
        if response.status != http.HTTPStatus.OK:
            msg = f"Best Practices request failed with HTTP {response.status}"
            raise BestPracticesPrefillError(msg)

        return response.read()
    finally:
        connection.close()


def load_prefill_manifest(path: Path) -> JSONObject:
    """Load checked-in proposal data."""
    data: object = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        msg = "prefill manifest must be an object"
        raise BestPracticesPrefillError(msg)

    return typed_json_object(cast(object, data), "prefill manifest")


def typed_json_object(value: object, label: str) -> JSONObject:
    """Return a JSON object after validating the dynamic decoder boundary."""
    if not isinstance(value, Mapping):
        msg = f"{label} must be an object"
        raise BestPracticesPrefillError(msg)

    raw_mapping = cast(Mapping[object, object], value)

    return {
        str(key): typed_json_value(item, label) for key, item in raw_mapping.items()
    }


def typed_json_value(value: object, label: str) -> JSONValue:
    """Return a JSON value after validating nested decoder output."""
    if value is None or isinstance(value, bool | int | float | str):
        return value
    if isinstance(value, Sequence) and not isinstance(value, str | bytes):
        raw_sequence = cast(Sequence[object], value)

        return [typed_json_value(item, label) for item in raw_sequence]
    if isinstance(value, Mapping):
        raw_mapping = cast(Mapping[object, object], value)

        return {
            str(key): typed_json_value(item, label) for key, item in raw_mapping.items()
        }

    msg = f"{label} contains unsupported JSON value {type(value).__name__}"
    raise BestPracticesPrefillError(msg)


def merge_current_and_prefill(
    current: JSONObject,
    manifest: JSONObject,
) -> list[CriterionState]:
    """Merge current badge answers with checked-in proposal overrides."""
    raw_proposals = manifest.get("criteria", {})
    if not isinstance(raw_proposals, dict):
        msg = "prefill criteria must be an object"
        raise BestPracticesPrefillError(msg)
    proposals: JSONObject = raw_proposals

    criteria = sorted(
        key[: -len("_status")] for key in current if key.endswith("_status")
    )
    merged: list[CriterionState] = []
    for criterion in criteria:
        raw_proposed = proposals.get(criterion, {})
        if raw_proposed and not isinstance(raw_proposed, dict):
            msg = f"prefill criterion {criterion!r} must be an object"
            raise BestPracticesPrefillError(msg)
        proposed = raw_proposed if isinstance(raw_proposed, dict) else {}

        status = str(proposed.get("status", current.get(f"{criterion}_status", "?")))
        justification = str(
            proposed.get(
                "justification",
                current.get(f"{criterion}_justification") or "",
            )
        )
        source = "prefill" if proposed else "current"
        merged.append(CriterionState(criterion, status, justification, source))

    for criterion, raw_proposed in sorted(proposals.items()):
        if criterion in criteria:
            continue
        if not isinstance(raw_proposed, dict):
            msg = f"prefill criterion {criterion!r} must be an object"
            raise BestPracticesPrefillError(msg)
        proposed = raw_proposed
        merged.append(
            CriterionState(
                criterion=criterion,
                status=str(proposed["status"]),
                justification=str(proposed.get("justification", "")),
                source="prefill",
            )
        )

    return merged


def build_prefill_url(
    project_id: int,
    section: str,
    criteria: list[CriterionState],
    *,
    include_baseline: bool = False,
    include_current: bool = False,
) -> str:
    """Build a human-reviewed Best Practices edit URL."""
    urls = build_prefill_urls(
        project_id,
        section,
        criteria,
        include_baseline=include_baseline,
        include_current=include_current,
    )

    return urls[0]


def build_prefill_urls(
    project_id: int,
    section: str,
    criteria: list[CriterionState],
    *,
    include_baseline: bool = False,
    include_current: bool = False,
    max_url_length: int = 0,
) -> list[str]:
    """Build one or more human-reviewed Best Practices edit URLs."""
    groups = build_prefill_param_groups(
        criteria,
        section=section,
        include_baseline=include_baseline,
        include_current=include_current,
    )
    urls: list[str] = []
    chunk: list[tuple[str, str]] = []
    chunk_section = ""
    for group in groups:
        group_section, group_params = group
        proposed = [*chunk, *group_params]
        proposed_url = build_url_from_params(project_id, group_section, proposed)
        if chunk and group_section != chunk_section:
            urls.append(build_url_from_params(project_id, chunk_section, chunk))
            chunk = [*group_params]
            chunk_section = group_section

            continue

        if max_url_length <= 0 or len(proposed_url) <= max_url_length or not chunk:
            chunk = proposed
            chunk_section = group_section

            continue

        urls.append(build_url_from_params(project_id, chunk_section, chunk))
        chunk = [*group_params]
        chunk_section = group_section

    if chunk:
        urls.append(build_url_from_params(project_id, chunk_section, chunk))

    return urls or [build_url_from_params(project_id, section, [])]


def build_prefill_params(
    criteria: list[CriterionState],
    *,
    section: str,
    include_baseline: bool = False,
    include_current: bool = False,
) -> list[tuple[str, str]]:
    """Build query parameters for all included prefill criteria."""
    return [
        item
        for group in build_prefill_param_groups(
            criteria,
            section=section,
            include_baseline=include_baseline,
            include_current=include_current,
        )
        for item in group[1]
    ]


def build_prefill_param_groups(
    criteria: list[CriterionState],
    *,
    section: str,
    include_baseline: bool = False,
    include_current: bool = False,
) -> list[tuple[str, list[tuple[str, str]]]]:
    """Build grouped query parameters, keeping each criterion atomic."""
    groups: list[tuple[str, list[tuple[str, str]]]] = []
    for state in criteria:
        if state.source == "current" and not include_current:
            continue
        if state.status == "?":
            continue
        if is_baseline_criterion(state.criterion) and not include_baseline:
            continue

        target_section = proposal_section(state.criterion, section)
        if not target_section:
            continue

        field = proposal_field_name(state.criterion)
        group = [(f"{field}_status", state.status)]
        if state.justification:
            group.append((f"{field}_justification", state.justification))
        groups.append((target_section, group))

    groups.sort(key=lambda item: section_sort_key(item[0]))

    return groups


def section_sort_key(section: str) -> tuple[int, str]:
    """Return stable ordering for generated edit-section URLs."""
    order = {
        "passing": 0,
        "silver": 1,
        "gold": 2,
        "baseline-1": 3,
        "baseline-2": 4,
        "baseline-3": 5,
        "choose": 6,
    }

    return (order.get(section, 99), section)


def proposal_section(criterion: str, requested_section: str) -> str:
    """Return the edit section that can accept a proposed criterion."""
    if is_baseline_criterion(criterion):
        return requested_section
    if requested_section in {"passing", "silver", "gold"}:
        return METAL_CRITERION_SECTIONS.get(criterion, "")

    return requested_section


def build_url_from_params(
    project_id: int,
    section: str,
    params: list[tuple[str, str]],
) -> str:
    """Build a Best Practices edit URL from query parameters."""
    query = urllib.parse.urlencode(params)
    return f"https://www.bestpractices.dev/projects/{project_id}/{section}/edit?{query}"


def proposal_field_name(criterion: str) -> str:
    """Return the query-string field prefix for a criterion."""
    return criterion.lower().replace("-", "_").replace(".", "_")


def is_baseline_criterion(criterion: str) -> bool:
    """Return whether a criterion belongs to the OSPS Baseline series."""
    return criterion.upper().startswith("OSPS-")


def build_report(
    criteria: list[CriterionState],
    *,
    include_baseline: bool = False,
) -> Report:
    """Summarize met, unmet, unknown, and proposed criteria."""
    counts: dict[str, int] = {}
    unresolved: list[ReportItem] = []
    proposals: list[str] = []
    for state in criteria:
        if is_baseline_criterion(state.criterion) and not include_baseline:
            continue

        counts[state.status] = counts.get(state.status, 0) + 1
        if state.source == "prefill":
            proposals.append(state.criterion)
        if state.status in {"?", "Unmet"}:
            unresolved.append(
                {
                    "criterion": state.criterion,
                    "status": state.status,
                    "source": state.source,
                    "justification": state.justification,
                }
            )

    return {
        "counts": counts,
        "proposed_count": len(proposals),
        "proposed_criteria": proposals,
        "unresolved": unresolved,
    }


def print_report(report: Report) -> None:
    """Print a compact human report."""
    counts = report["counts"]
    print("counts: " + ", ".join(f"{key}={counts[key]}" for key in sorted(counts)))
    print(f"proposals: {report['proposed_count']}")

    unresolved = report["unresolved"]
    if not unresolved:
        print("unresolved: none")

        return

    print(f"unresolved: {len(unresolved)}")
    for item in unresolved:
        print(f"  - {item['criterion']}: {item['status']} ({item['source']})")


if __name__ == "__main__":
    sys.exit(main())
