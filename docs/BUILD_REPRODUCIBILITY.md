<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Build Reproducibility

`coding-ethos` treats reproducibility as a supply-chain control. The project
does not claim that every third-party tool invocation is hermetic, but the
repo-owned build and release path is designed so generated project artifacts
can be rebuilt from source with stable inputs.

## Current Controls

- Python dependencies are locked in `uv.lock`.
- Go dependencies are locked in `go/go.sum`.
- The release workflow builds from a Git tag and records provenance with
  GitHub artifact attestations.
- Release artifacts include SHA-256 checksums, SPDX JSON SBOM output, and
  offline `.intoto.jsonl` attestation bundles.
- `make build` regenerates managed config, agent settings, prompt packs, policy
  bundles, hook runtimes, and parent runtime artifacts from checked-in sources.
- Generated config drift is checked through managed hash manifests.
- Repo-owned Go command builds use `-trimpath` and `-buildvcs=false` so local
  checkout paths and volatile VCS metadata do not enter the compiled binaries.

## Verification

The supported local verification path is:

```bash
make build
make check-tool-configs
make check
```

Release verification additionally uses:

```bash
make release-dry-run
```

The release workflow then repeats the build in GitHub Actions, attaches
checksums and SBOMs, and creates artifact attestations for the published
distribution files.

## Known Limits

The managed hook toolchain intentionally installs external lint and analysis
tools so consuming repositories can use consistent versions. Those tools are
locked and audited, but they are not represented as a single hermetic binary
image. Future work may add a fully hermetic release build profile once the
runtime sandboxing roadmap has stabilized.
