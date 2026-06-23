<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# tests

`tests/` covers the supported public API and the repo’s generated-output
contract.

The suite focuses on Markdown seeding, CLI flows, generation semantics, and
Go-backed generated-artifact synchronization so behavior changes fail in a
narrow, explainable way.

When output layout, package exports, or generator behavior changes, update or
extend these tests in the same change so the repo contract remains executable.

## Test Areas

- `test_cli.py`: Markdown seeding, primary YAML validation, agent doc
  generation, and merge-preserving writes.
- `test_makefile_contract.py`: verifies generated config, prompt-pack, and
  skill-surface sync/check targets route through Go.
- `test_yaml_utils.py`: YAML formatting helpers, comment preservation, and
  folded prose rendering.
- `go/internal/e2e`: Go end-to-end scenarios that copy checked-in reference
  repositories, initialize real Git repositories, run real coding-ethos
  binaries and managed tools, and inspect real trace/SARIF output.
- `examples/reference-repos`: small checked-in repositories used as source
  material for real workflow tests. Add files that trigger real managed-tool or
  hook behavior, then extend `go/internal/e2e` to copy the fixture and assert
  user output plus trace, SARIF, or repository state.

## End-To-End Test Policy

End-to-end tests must prove real product behavior. They should use real
commands, real managed tools, real Git repositories, real filesystem state, real
MCP framing, and real trace/SARIF output. Do not add fake linters, fake Git
wrappers, fake hook executables, or fake services to end-to-end scenarios just
to make setup easier.

AI calls are the default allowed exception because live model behavior is
nondeterministic and externally controlled. Any other mock, fake executable,
fake service, or synthetic replacement in an end-to-end test requires explicit
admin approval before it is added. The test must document why no real
alternative is safe or practical, what exact behavior is being replaced, and
what risk remains uncovered. The exception must also be recorded in
`KNOWN_DEFECTS.md` with an owner, replacement plan, and removal condition.

Reference repositories are test inputs, not test doubles. They should contain
ordinary source files and project config that real tools can inspect. Do not
put fake linters, fake Git wrappers, fake hook executables, or shell-script
stand-ins in `examples/reference-repos`.

## Verification Commands

Use the Makefile target unless a narrower test is needed:

```bash
make test
```

For the full current repo gate:

```bash
make check
```

`make check` runs tests plus generated tool-config, Gemini prompt-pack, agent
skill, and provider-matrix drift checks, including real Go end-to-end workflow
tests that prepare the runtime through `make build`.

When hook runtime changes are involved, also run:

```bash
make validate
make go-test
make go-e2e-test
```

The Go E2E harness runs each external command with a timeout and, on Unix,
places the command in its own process group so timeout cleanup terminates child
processes as well as the direct command. For risky new scenarios, first run the
single scenario under the same target with an external task budget such as
`systemd-run --user --scope -p TasksMax=128`.

For Go coverage, including subprocess coverage emitted by real e2e command
execution, run:

```bash
make go-coverage
```
