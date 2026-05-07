# Policy Lint Basic Reference Repo

This fixture is a small, real repository layout used by Go end-to-end tests and
by documentation examples. Tests copy it into a temporary directory, initialize
Git there, and run coding-ethos commands against the copied files.

Add new scenarios by creating focused files under a language directory. End-to-end
tests should run the real managed tools against those files; do not add fake
linters or shell-script test doubles here.

When adding a new reference repo or scenario:

- Keep the fixture small and readable.
- Commit source files that trigger real tool behavior, not generated outputs.
- Add a Go e2e test that copies the fixture with `e2e.FromReference`.
- Assert user-visible output plus retained trace, SARIF, or repository state.
- Record any non-AI mock or fake exception in `KNOWN_DEFECTS.md` before adding it.
