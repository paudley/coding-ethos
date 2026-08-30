# Measuring Coding Ethos Token Economy

Coding Ethos uses two deliberately separate evidence tiers:

1. A historical report measures gross context removed by recorded proxy
   transforms. It is observational and never claims provider-billed savings.
2. A randomized three-arm benchmark compares provider-native token ledgers for
   `full`, `static`, and `off` runs that share the same source, prompt, model,
   reasoning effort, and validators.

`full` receives the frozen global context plus the manifest's dynamic Coding
Ethos configuration. `static` receives exactly the same global context without
the dynamic configuration or state environment. `off` receives neither, while
retaining governance files that belong to the frozen task repository itself.

## Report observational history

Historical reporting reads one or more existing code-intel stores. Every
source is named explicitly with a repeatable `--db`; no ambient database is
selected. The time interval is also explicit and half-open: `--from` is
inclusive and `--to` is exclusive.

```bash
bin/coding-ethos-run code-intel token-economy report \
  --historical \
  --db /private/lane-1/code-intel.duckdb \
  --db /private/lane-2/code-intel.duckdb \
  --from 2026-08-01T00:00:00Z \
  --to 2026-09-01T00:00:00Z \
  --output-prefix /private/reports/token-economy-history
```

The report canonicalizes and sorts source paths before aggregation, counts an
event ID at most once, and fails if the same event ID appears in distinct
sources. Each source is hashed before it is opened and after it is closed; a
changed source aborts report creation. The JSON and Markdown artifacts record
the normalized UTC window, ordered source identities, before/after hashes, and
the deterministic aggregation contract. Historical results always remain
observational and set `causal=false`.

## Freeze and validate a protocol

Start with an absolute-path YAML draft. Hash and version fields may be blank in
the draft because `freeze` recomputes them from the named files, Git archives,
executables, arguments, and provider `--version` output. The output is
create-new and mode `0600`.

```yaml
schema_version: 1
experiment_id: ethos-economy-2026-08
created_at_utc: "2026-08-28T18:00:00Z"
randomization_seed: "publish-this-before-running"
replicates: 2
analysis_block_checkpoints: [10, 20]
static_context:
  path: /private/protocol/coding-ethos-AGENTS.md
  sha256: ""
provider:
  kind: codex
  executable: /absolute/path/to/codex
  executable_sha256: ""
  runtime_version: ""
  model: your-approved-model-id
  reasoning_effort: high
  auth_file: /private/codex-home/auth.json
full_config_overrides:
  - 'features.code_intel=true'
tasks:
  - task_id: real-fix-01
    kind: real
    repository_path: /absolute/path/to/frozen-source-repository
    commit: 0123456789abcdef0123456789abcdef01234567
    source_archive_sha256: ""
    prompt_path: /private/protocol/prompts/real-fix-01.txt
    prompt_sha256: ""
    validator_sha256: ""
    agent_timeout: 45m
    validation_timeout: 20m
    allowed_paths:
      - go/internal/example
    validators:
      - [/absolute/path/to/go, test, ./internal/example, -count=1]
  - task_id: diagnostic-01
    kind: diagnostic
    repository_path: /absolute/path/to/frozen-diagnostic-repository
    commit: fedcba9876543210fedcba9876543210fedcba98
    source_archive_sha256: ""
    prompt_path: /private/protocol/prompts/diagnostic-01.txt
    prompt_sha256: ""
    validator_sha256: ""
    agent_timeout: 30m
    validation_timeout: 10m
    allowed_paths:
      - pkg/diagnostic
    validators:
      - [/absolute/path/to/pytest, -q, pkg/diagnostic]
```

The task list above is abbreviated; a causal protocol needs at least ten
distinct tasks. The corpus must include both real and diagnostic tasks. A manifest can contain
at most 20 tasks, two replicates, and 120 provider runs. Validators are argv
arrays executed directly; Coding Ethos does not interpolate them through a
shell. Analysis checkpoints count complete task-replicate three-arm blocks,
must increase, and must end at `tasks * replicates`. Replicate-one blocks are
scheduled before replicate-two blocks so the first ten blocks cover ten
distinct tasks. Their intervals use Bonferroni-adjusted alpha so family-wise
confidence remains 95% across every declared look. The auth file must be a
private regular file and is copied only into an ephemeral provider home for
each run.

```bash
bin/coding-ethos-run code-intel token-economy benchmark freeze \
  --draft /private/protocol/draft.yaml \
  --output /private/protocol/frozen.yaml

bin/coding-ethos-run code-intel token-economy benchmark validate \
  --manifest /private/protocol/frozen.yaml
```

Both commands inspect identities and invoke only the provider's `--version`
mode. They do not start a model run.

## Run controlled blocks

`run` is the only model-backed command. It requires an explicit absolute state
root and an operator-approved cap that is a multiple of three. Scheduling is
deterministic and randomized in complete `full`/`static`/`off` blocks. Each run
uses a new no-remote Git workspace and an isolated provider home. A terminal
failure, timeout, missing ledger, validator failure, or out-of-scope edit is
recorded as evidence; it is not silently replaced. Model-generated commands
run in the workspace-write sandbox with outbound network access disabled.

```bash
bin/coding-ethos-run code-intel token-economy benchmark run \
  --manifest /private/protocol/frozen.yaml \
  --state-root /private/evidence \
  --approved-max-runs 30
```

The command is resumable and immutable. Re-running it skips already recorded
run IDs. For adaptive collection, report only when a predeclared checkpoint has
that many complete task-replicate three-arm blocks, and authorize another multiple of three
only when the result remains imprecise. At least ten complete tasks are required
for a causal claim. Stop at a supported conclusion or the manifest maximum; the
runner never launches additional blocks on its own.

```bash
bin/coding-ethos-run code-intel token-economy report \
  --state-root /private/evidence \
  --experiment-id ethos-economy-2026-08 \
  --output-prefix /private/reports/ethos-economy-2026-08-look-01
```

The primary comparison is `full` versus `off`; `full` versus `static` and
`static` versus `off` separate dynamic and static mechanisms. A causal savings
conclusion requires complete randomized arm evidence, at least ten distinct
tasks, a savings interval excluding zero, no added severe governance
violations, and acceptance-rate noninferiority within five percentage points.
Otherwise the report explicitly returns inconclusive, no detectable
difference, regression, or quality tradeoff.

Codex execution and Claude ledger ingestion are supported. The controlled
runner is Codex-only in schema version 1; a future Claude runner can implement
the same frozen protocol without changing the provider-neutral evidence store.
