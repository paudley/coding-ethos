<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Runtime Sandboxing

`coding-ethos` uses CEL and Go evaluators as the policy control plane: they
decide whether a command, edit, lint capture, or Git action is allowed. Runtime
sandboxing is the data plane: it constrains what an approved process can
actually see and do.

The two layers are deliberately separate. CEL remains pure and deterministic.
Go prepares facts, policy decides over those facts, and the sandbox runner
enforces the declared runtime capabilities.

## Non-Goal: LD_PRELOAD

`LD_PRELOAD` must not be used as a security boundary.

It is bypassed by statically linked binaries, Go and Rust programs that issue
direct syscalls, child processes that scrub environment variables, and setuid or
otherwise hardened executables that ignore loader injection. It is also
fragile across libc variants and architectures. It may be useful for debugging
or observability experiments, but not for `coding-ethos` enforcement.

## Capability Model

Managed tools declare their runtime capabilities in `toolcatalog`. The initial
model covers:

- read paths and write paths;
- sandbox profile;
- network access;
- Git access;
- environment access;
- process visibility;
- timeout, memory, and CPU limits.

Current managed lint and formatting tools are offline by default. The first
explicit network-capable catalog entry is `gemini-check`, which declares
`requires_network`, the `network` capability tag, and an `agent-network`
sandbox profile. Ordinary linters receive explicit `no-network` and `no-git`
capability tags so MCP responses, traces, SARIF, and CEL policies can
distinguish "capability denied by default" from "capability not documented."

Consumer repositories can add required sandbox read/write paths in
`repo_config.yaml`:

```yaml
sandbox:
  read_write_paths:
    - /opt/foundation
    - /opt/src/vllm
    - /scratch/lbox
```

These paths are additive to the managed tool's catalog capability declaration.
They are for required workspace or toolchain directories, not policy
exceptions; `.git` write binds remain blocked even if a consumer lists them
here.

## CEL Policy Surface

Tool capabilities are exposed to CEL through `tool_capabilities`. Policies can
now reason over declared runtime needs without reading host state or executing
tools.

The first principle-local policies are:

- `runtime.network_requires_approval`: if an agent attempts to run a managed
  tool that declares network access, the action is blocked unless the policy
  context is explicitly admin-approved.
- `runtime.managed_tool_capability_contract`: ordinary managed lint tools must
  declare offline/no-Git behavior, a sandbox profile, positive timeout and
  resource bounds, and seccomp profile metadata.

These policies live with `security-by-design` in `coding_ethos.yml`, not in
ad hoc Go checks or repo-local config. The Go layer supplies typed facts; CEL
owns the contract.

## Runtime Strategy

The sandbox backend is native Go-owned Linux namespace execution. The managed
capture parent starts `coding-ethos-sandbox` inside new user, PID, UTS, IPC,
and network namespaces where the requested tool does not declare network
access. The helper then applies Linux Landlock filesystem policy before it
execs the managed tool. Go owns request construction, policy facts, traces, and
normalized output; sandboxing does not depend on a host package manager,
Docker, or a third-party wrapper.

The native Go implementation lives in `go/internal/sandbox`. Managed lint
capture derives sandbox behavior from the platform and the tool catalog: on
Linux, any managed tool with a sandbox profile is sandboxed and fails closed
when native sandboxing is unavailable; on non-Linux platforms, Linux namespace
sandboxing is not advertised or selected. A missing native helper or failed
namespace setup on Linux is a normalized `runtime.sandbox_denial` failure. The
runner must not fall back to unsandboxed execution when a Linux tool declares a
sandbox profile.

The checkout build treats that dependency contract as a gate, not a runtime
surprise. `make build` invokes `coding-ethos-toolchain
validate-sandbox-runtime`, which launches a minimal native namespace probe that
proves declared writes succeed and undeclared repository writes are blocked. On
Linux, missing or unusable namespace or Landlock support fails the build with a
blocking `runtime.sandbox_dependency` diagnostic. Non-Linux platforms do not
advertise Linux namespace enforcement.

The current native profile is explicit and evidence-backed:

- `coding-ethos-sandbox` installs a Landlock ruleset that handles filesystem
  mutation operations;
- the repository and `.git` are read-only by default because no write rule is
  granted for them;
- declared write paths receive write rules only when they stay inside the
  repository and do not target `.git`;
- ordinary offline tools receive a private PID namespace and a disconnected
  network namespace; tools that declare `requires_network` keep network access
  only when policy allows that capability.

The target default profile for ordinary managed linters is:

- no network namespace access;
- read-only repository filesystem;
- read-only `.git`;
- read/write access only to declared repository paths;
- no host process visibility;
- bounded timeout, memory, and CPU;
- conservative seccomp profile metadata.

Seccomp support is explicit: a catalog entry can declare a `seccomp_profile`.
Native BPF profile loading is not implemented yet; when a profile path is
present, execution fails closed as `runtime.sandbox_denial` rather than
pretending the profile is enforced.

Resource controls are split by enforcement layer. Go wraps sandboxed managed
tool execution in a hard timeout. Memory and CPU requests are applied through
a delegated cgroup v2 hierarchy when requested by the tool capability model.
The cgroup is prepared before process start, the Linux runner starts the child
directly inside it using `clone3` cgroup file-descriptor support, and the
temporary cgroup directory is removed after the process exits. Required sandbox
mode fails closed if no delegated writable cgroup hierarchy is available or if
the limits cannot be applied.

The first default managed-linter profile is intentionally conservative:
`no-network`, `no-git`, `lint-offline`, 300 seconds, 2048 MB memory, 100% CPU,
and `deny-privilege` seccomp metadata. The seccomp metadata is auditable even
when no local BPF profile file has been installed yet.

## Trace Contract

Sandbox execution must report declared capabilities, selected profile, backend,
resource limits, and runtime denials into `.coding-ethos` traces. SARIF and MCP
should preserve that evidence so agents see a policy-linked denial rather than
raw kernel or wrapper noise.

The prototype records sandbox evidence under `result.capture.sandbox` in lint
traces and under SARIF `runs[].properties.sandbox`, including capability tags,
namespace isolation, cgroup state, timeout enforcement, and seccomp profile
state. Required-mode denials also produce a blocking `runtime.sandbox_denial`
finding grounded in `security-by-design` and
`one-path-for-critical-operations`.

Sandbox profiles fail closed when Linux namespace support is available but
unusable. There is no runtime sandbox mode switch: Linux uses sandboxing for
sandbox-profiled managed tools, and non-Linux platforms do not select Linux
namespace sandboxing.
