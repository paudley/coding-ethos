<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Known Defects

This file tracks accepted defects that are intentionally present in the
repository. It is not a suppression list or a place to normalize broken
behavior. Each entry must include the scope, owner, rationale, replacement plan,
and removal condition.

## End-To-End Test Fakes And Mocks

End-to-end tests must exercise real workflows: real commands, real managed
tools, real Git repositories, real filesystem state, real MCP framing, and real
trace/SARIF output. AI calls are the default allowed exception because live LLM
behavior is nondeterministic and externally controlled.

No mock, fake executable, fake service, or synthetic replacement may be added to
the end-to-end suite unless all of the following are true:

- An admin explicitly approves the exception before it is added.
- The test documents the exception next to the scenario with an extensive
  rationale explaining why no real alternative is safe or practical.
- The documentation states exactly what real behavior is being replaced.
- The documentation states what risk remains uncovered by the fake.
- This file receives a matching entry with an owner, replacement plan, and
  removal condition.

### Current Entries

No approved end-to-end fakes or mocks.
