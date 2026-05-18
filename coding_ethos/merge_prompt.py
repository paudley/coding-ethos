# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Prompt construction for external root-file merge engines.

Responsibility is narrow.
Public imports stay aligned.
"""


def build_merge_prompt(target_name: str, merge_topics: list[str]) -> str:
    """Build the external LLM merge prompt for one root document."""
    topic_lines = ""
    if merge_topics:
        topic_lines = (
            "\nPreserve repo-specific content related to these topics when it "
            "is still relevant:\n"
        )
        topic_lines += "\n".join(f"- {topic}" for topic in merge_topics)
        topic_lines += "\n"

    return f"""You are merging two versions of `{target_name}`.

Inputs in the current directory:
- `existing.md`: the current file from the repo
- `generated.md`: the newly generated ethos-aware candidate

Your task:
1. Read both files completely.
2. Produce a merged result at `merged.md`.

Merge requirements:
- Preserve important repo-specific operational content from `existing.md`.
- Integrate all important ethos and agent-guidance content from `generated.md`.
- Prefer preserving concrete repo instructions, commands, paths, caveats,
  imports, and process notes from `existing.md` when they still apply.
- Prefer preserving structure that makes the file usable by the target agent.
{topic_lines}
- Keep imports, references, commands, paths, workflow notes, and local
  conventions if they are still relevant.
- Remove obvious duplication and resolve contradictions in favor of the newer
  generated ethos guidance where the old file is generic or redundant.
- Do not collapse the file into a tiny summary if `existing.md` contains
  important concrete repo instructions.
- Keep the result as valid Markdown.
- Only write `merged.md`. Do not create any other files.

Output contract:
- `merged.md` must contain the final merged file content for `{target_name}`.
"""
