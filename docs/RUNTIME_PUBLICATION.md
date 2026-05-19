# Runtime Publication Model

The PyPI package is the Python generator distribution. It includes the CLI,
default `coding_ethos.yml`, base `config.yaml`, example overlays, and prompt
templates needed to render repo docs and managed config files.

The compiled Go enforcement runtime is not bundled into the wheel. Hook and
agent enforcement currently use a source checkout or submodule and build the
runtime with `make build` before installation. That keeps platform-specific
binaries, managed tool assets, checksums, and attestations out of a Python-only
wheel until the project has a complete platform packaging strategy.

Future compiled runtime publication should use GitHub release assets first:

- one archive per OS/architecture;
- SHA-256 checksums for every archive;
- GitHub artifact attestations for every archive;
- SBOM coverage for the release asset set;
- documented upgrade behavior from an installed runtime to a newer release;
- no downgrade or arbitrary version pin path for protected hook runtimes.

Companion platform wheels are acceptable only after the project has a verified
upgrade and checksum model for every supported platform. A universal wheel must
not silently carry host-specific binaries.

## Installation Implications

Use PyPI when a repo only needs root agent-document generation:

```bash
uvx coding-ethos --repo .
```

Use a source checkout or submodule when a repo needs enforcement:

```bash
make build
make cutover-install
```

Generated CI config follows the enforcement path: it builds the checkout-local
runtime, then runs the policy/SARIF gate with platform-derived sandboxing.
