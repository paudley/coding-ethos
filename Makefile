# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

.DEFAULT_GOAL := help
.SUFFIXES:
.DELETE_ON_ERROR:

LOCAL_REPO_ROOT := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
GO_TOOLS_DIR := $(LOCAL_REPO_ROOT)/go
PRECOMMIT_DIR := $(LOCAL_REPO_ROOT)/pre-commit/
HOOK_RUNNER_GO_DIR := $(GO_TOOLS_DIR)/cmd/coding-ethos-hook-runner
GO_HOOK_SOURCES := $(wildcard $(HOOK_RUNNER_GO_DIR)/*.go)
LOCAL_BIN_DIR := $(LOCAL_REPO_ROOT)/bin
GO_HOOK := $(LOCAL_BIN_DIR)/coding-ethos-run
LOCAL_BUILD_DIR := $(LOCAL_REPO_ROOT)/build
POLICY_DIR := $(LOCAL_BUILD_DIR)/policy
TOOLCHAIN_DIR := $(LOCAL_BUILD_DIR)/toolchain
MANAGED_GO_BIN_DIR := $(TOOLCHAIN_DIR)/go-bin
MANAGED_PREFIX_DIR := $(TOOLCHAIN_DIR)/prefix
MANAGED_GITHUB_BIN_DIR := $(TOOLCHAIN_DIR)/github-bin
MANAGED_TOOLCHAIN_SOURCE := $(PRECOMMIT_DIR)hooks/managed-toolchain.tsv
MANAGED_TOOLCHAIN_MANIFEST := $(TOOLCHAIN_DIR)/manifest.tsv

GIT ?= /usr/bin/git
UV ?= uv
PYTHON ?= python
GO ?= go
GOFMT ?= gofmt
GO_BUILD_FLAGS ?= -trimpath -buildvcs=false

empty :=
space := $(empty) $(empty)
path_entries := $(subst :, ,$(PATH))
build_path_entries := $(filter-out \
	$(LOCAL_BIN_DIR) \
	$(MANAGED_GO_BIN_DIR) \
	$(MANAGED_PREFIX_DIR)/bin \
	$(MANAGED_GITHUB_BIN_DIR) \
	%/.venv/bin, \
	$(path_entries))
export PATH := $(subst $(space),:,$(build_path_entries))

define resolve_hook_consumer_root
if [ -n "$${CODE_ETHOS_CONSUMER_ROOT:-}" ]; then \
	printf '%s' "$$CODE_ETHOS_CONSUMER_ROOT"; \
	exit 0; \
fi; \
super="$$("$(GIT)" -C "$(LOCAL_REPO_ROOT)" rev-parse \
	--show-superproject-working-tree 2>/dev/null)"; \
if [ -n "$$super" ]; then \
	printf '%s' "$$super"; \
else \
	top="$$("$(GIT)" -C "$(LOCAL_REPO_ROOT)" rev-parse --show-toplevel 2>/dev/null || true)"; \
	if [ -n "$$top" ]; then \
		printf '%s' "$$top"; \
	else \
		printf '%s' "$(LOCAL_REPO_ROOT)"; \
	fi; \
fi
endef

define install_git_hooks
$(call print_info,hooks: $(1)); "$(GO_TOOLS_BIN_DIR)/coding-ethos-toolchain" install-git-hooks --hooks-dir "$(1)" --runner "$(GO_HOOK)"
endef

HOOK_CONSUMER_ROOT := $(shell $(resolve_hook_consumer_root))
PARENT_REPO_CONFIG := $(shell if [ -f "$(HOOK_CONSUMER_ROOT)/repo_config.yaml" ]; then printf '%s' "$(HOOK_CONSUMER_ROOT)/repo_config.yaml"; elif [ -f "$(HOOK_CONSUMER_ROOT)/repo_config.yml" ]; then printf '%s' "$(HOOK_CONSUMER_ROOT)/repo_config.yml"; fi)
LOCAL_HOOKS_DIR := $(shell "$(GIT)" -C "$(LOCAL_REPO_ROOT)" rev-parse --path-format=absolute --git-path hooks 2>/dev/null || printf '%s/.git/hooks' "$(LOCAL_REPO_ROOT)")
GIT_COMMON_DIR := $(shell "$(GIT)" -C "$(HOOK_CONSUMER_ROOT)" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || printf '%s/.git' "$(HOOK_CONSUMER_ROOT)")
HOOKS_DIR := $(shell "$(GIT)" -C "$(HOOK_CONSUMER_ROOT)" rev-parse --path-format=absolute --git-path hooks 2>/dev/null || printf '%s/.git/hooks' "$(HOOK_CONSUMER_ROOT)")
PARENT_HOOK_RUNTIME_DIR := $(GIT_COMMON_DIR)/coding-ethos-hooks
PARENT_HOOK_BIN_DIR := $(PARENT_HOOK_RUNTIME_DIR)/bin
PARENT_POLICY_DIR := $(PARENT_HOOK_RUNTIME_DIR)/policy
GIT_HOOKS := pre-commit pre-push commit-msg
GIT_LFS_HOOKS := post-commit post-merge post-checkout
GO_TOOLS_BIN_DIR ?= $(LOCAL_BIN_DIR)
GO_TOOL_CMDS := \
	cerun \
	coding-ethos-agent-hooks \
	coding-ethos-code-intel \
	coding-ethos-policy \
	coding-ethos-lint \
	coding-ethos-hook \
	coding-ethos-hook-log \
	coding-ethos-mcp \
	coding-ethos-run \
	coding-ethos-sandbox \
	coding-ethos-toolchain \
	coding-ethos-git-hook \
	coding-ethos-git
GO_MODULE_ROOT_BINARY_OUTPUTS := \
	$(GO_TOOL_CMDS) \
	coding-ethos-hook-runner

GO_COVERAGE_MIN ?= 80.0
PYTHON_COVERAGE_MIN ?= 80
GO_TEST_TIMEOUT ?= 30s
GO_COVERAGE_DIR ?= $(LOCAL_BUILD_DIR)/coverage
REPO ?= $(LOCAL_REPO_ROOT)
PRIMARY ?= $(LOCAL_REPO_ROOT)/coding_ethos.yml
REPO_ETHOS ?=
REPO_CONFIG ?=
MERGE_STRATEGY ?= inject
MERGE_ENGINE ?= codex
MERGE_BIN ?=
MERGE_MODEL ?=
MERGE_TIMEOUT_SECONDS ?= 300
SEED_FROM ?=

APP ?= $(UV) run $(PYTHON) $(LOCAL_REPO_ROOT)/main.py

ifeq ($(abspath $(REPO)),$(LOCAL_REPO_ROOT))
DEFAULT_TOOL_CONFIG_REPO := $(LOCAL_REPO_ROOT)
else
DEFAULT_TOOL_CONFIG_REPO := $(HOOK_CONSUMER_ROOT)
endif

TOOL_CONFIG_REPO ?= $(DEFAULT_TOOL_CONFIG_REPO)

ifeq ($(strip $(REPO_ETHOS)),)
ifeq ($(abspath $(REPO)),$(LOCAL_REPO_ROOT))
REPO_ETHOS_FLAG := --repo-ethos "$(LOCAL_REPO_ROOT)/repo_ethos.yml"
else
REPO_ETHOS_FLAG :=
endif
else
REPO_ETHOS_FLAG := --repo-ethos "$(REPO_ETHOS)"
endif

ifeq ($(strip $(REPO_CONFIG)),)
REPO_CONFIG_FLAG :=
else
REPO_CONFIG_FLAG := --repo-config "$(REPO_CONFIG)"
endif

COMMON_GENERATE_FLAGS := --repo "$(REPO)" --primary "$(PRIMARY)" $(REPO_ETHOS_FLAG)
TOOL_CONFIG_FLAGS := --repo "$(TOOL_CONFIG_REPO)" $(REPO_CONFIG_FLAG)
GEMINI_PROMPT_FLAGS := --repo "$(TOOL_CONFIG_REPO)" --primary "$(PRIMARY)" $(REPO_ETHOS_FLAG) $(REPO_CONFIG_FLAG)
AGENT_SKILL_FLAGS := --repo "$(REPO)" --primary "$(PRIMARY)" $(REPO_ETHOS_FLAG)
MERGE_FLAGS = \
	--merge-existing \
	--merge-strategy "$(MERGE_STRATEGY)" \
	--merge-engine "$(MERGE_ENGINE)" \
	--merge-timeout-seconds "$(MERGE_TIMEOUT_SECONDS)"

ifneq ($(strip $(MERGE_BIN)),)
MERGE_FLAGS += --merge-bin "$(MERGE_BIN)"
endif

ifneq ($(strip $(MERGE_MODEL)),)
MERGE_FLAGS += --merge-model "$(MERGE_MODEL)"
endif

ifneq ($(strip $(TERM)),dumb)
COLOR_RESET := \033[0m
COLOR_BOLD := \033[1m
COLOR_SECTION := \033[38;5;39m
COLOR_TARGET := \033[38;5;81m
COLOR_ACCENT := \033[38;5;42m
COLOR_WARN := \033[38;5;214m
else
COLOR_RESET :=
COLOR_BOLD :=
COLOR_SECTION :=
COLOR_TARGET :=
COLOR_ACCENT :=
COLOR_WARN :=
endif

define print_step
printf '$(COLOR_SECTION)==>$(COLOR_RESET) %s\n' "$(1)"
endef

define print_info
printf '  $(COLOR_ACCENT)•$(COLOR_RESET) %s\n' "$(1)"
endef

define print_warn
printf '  $(COLOR_WARN)!$(COLOR_RESET) %s\n' "$(1)"
endef

define print_kv
printf '  $(COLOR_ACCENT)%-24s$(COLOR_RESET) %s\n' "$(1)" "$(2)"
endef

define quiet_build
tmp="$$(mktemp)"; \
trap 'rm -f "$$tmp"' EXIT; \
if ! $(MAKE) --no-print-directory build >"$$tmp" 2>&1; then \
	cat "$$tmp" >&2; \
	exit 1; \
fi
endef

.PHONY: \
	help \
	status \
	doctor \
	install \
	install-runtime \
	parent-install \
	parent-check \
	parent-lint \
	build \
	package-smoke \
	release-dry-run \
	test \
	check \
	cutover-install \
	cutover-verify \
	install-hooks \
	pre-commit \
	pre-commit-all \
	pre-push \
	commit-msg \
	hook-plan \
	validate \
	check-local-artifacts \
	go-test \
	go-e2e-test \
	go-tidy \
	lint \
	lint-fix \
	fix \
	format \
	autofix \
	fmt \
	go-tools-test \
	go-tools-build \
	go-tools-install \
	sandbox-runtime-validate \
	managed-toolchain-install \
	managed-go-tools-install \
	go-hook-runner-install \
	policy-bundle-install \
	go-tools-smoke \
	go-tools-clean \
	clean-cache \
	sync-tool-configs \
	sync-consumer-tool-configs \
	fix-configs \
	check-tool-configs \
	sync-gemini-prompts \
	check-gemini-prompts \
	check-agent-skills \
	hooks-validate \
	hooks-install \
	hooks-go-test \
	seed \
	generate \
	generate-merge \
	generate-merge-llm \
	clean \
	ensure-uv \
	ensure-go \
	ensure-gofmt \
	ensure-hook-runtime \
	guard-%

##@ Help
help: ## Show the available targets and the most useful overrides.
	@printf '\n$(COLOR_BOLD)coding-ethos$(COLOR_RESET)\n'
	@printf 'Repo-local workflow for generation, hooks, and verification.\n'
	@printf 'Run `make status` for resolved paths and `make doctor` for tool checks.\n\n'
	@awk 'BEGIN { FS = ":.*## "; section = "" } \
		/^##@/ { \
			section = substr($$0, 5); \
			printf "$(COLOR_SECTION)%s$(COLOR_RESET)\n", section; \
			next; \
		} \
		/^[a-zA-Z0-9_.%-]+:.*## / { \
			printf "  $(COLOR_TARGET)%-20s$(COLOR_RESET) %s\n", $$1, $$2; \
		}' $(MAKEFILE_LIST)
	@printf '\n$(COLOR_BOLD)Common overrides$(COLOR_RESET)\n'
	@printf '  REPO=/path/to/target-repo\n'
	@printf '  TOOL_CONFIG_REPO=/path/to/tool-config-repo\n'
	@printf '  PRIMARY=/path/to/coding_ethos.yml\n'
	@printf '  REPO_ETHOS=/path/to/repo_ethos.yml\n'
	@printf '  REPO_CONFIG=/path/to/repo_config.yml\n'
	@printf '  GO=/path/to/go GOFMT=/path/to/gofmt UV=/path/to/uv PYTHON=/path/to/python\n'
	@printf '  SEED_FROM=/path/to/ETHOS.md\n'
	@printf '  MERGE_STRATEGY=inject|llm MERGE_ENGINE=codex|gemini|claude\n'
	@printf '  MERGE_BIN=/path/to/engine MERGE_MODEL=model-name MERGE_TIMEOUT_SECONDS=300\n'
	@printf '\n$(COLOR_BOLD)Examples$(COLOR_RESET)\n'
	@printf '  make install\n'
	@printf '  make doctor\n'
	@printf '  make test\n'
	@printf '  make validate\n'
	@printf '  make parent-lint\n'
	@printf '  make install-hooks\n'
	@printf '  make cutover-install\n'
	@printf '  make cutover-verify\n'
	@printf '  make sync-tool-configs\n'
	@printf '  make sync-gemini-prompts\n'
	@printf '  make generate\n'
	@printf '  make generate REPO=/tmp/example\n'
	@printf '  make seed SEED_FROM=/tmp/ETHOS.md\n'
	@printf '  make generate-merge-llm REPO=/tmp/example MERGE_ENGINE=gemini MERGE_BIN=/usr/local/bin/gemini\n\n'

status: ## Print the resolved tool and generation configuration.
	@$(call print_step,Resolved configuration)
	@$(call print_kv,LOCAL_REPO_ROOT,$(LOCAL_REPO_ROOT))
	@$(call print_kv,HOOK_CONSUMER_ROOT,$(HOOK_CONSUMER_ROOT))
	@$(call print_kv,UV,$(UV))
	@$(call print_kv,PYTHON,$(PYTHON))
	@$(call print_kv,GO,$(GO))
	@$(call print_kv,GOFMT,$(GOFMT))
	@$(call print_kv,APP,$(APP))
	@$(call print_kv,REPO,$(REPO))
	@$(call print_kv,TOOL_CONFIG_REPO,$(TOOL_CONFIG_REPO))
	@$(call print_kv,PRIMARY,$(PRIMARY))
	@$(call print_kv,REPO_ETHOS,$(if $(strip $(REPO_ETHOS)),$(REPO_ETHOS),<auto>))
	@$(call print_kv,REPO_CONFIG,$(if $(strip $(REPO_CONFIG)),$(REPO_CONFIG),<auto>))
	@$(call print_kv,MERGE_STRATEGY,$(MERGE_STRATEGY))
	@$(call print_kv,MERGE_ENGINE,$(MERGE_ENGINE))
	@$(call print_kv,MERGE_BIN,$(if $(strip $(MERGE_BIN)),$(MERGE_BIN),<default>))
	@$(call print_kv,MERGE_MODEL,$(if $(strip $(MERGE_MODEL)),$(MERGE_MODEL),<default>))
	@$(call print_kv,MERGE_TIMEOUT_SECONDS,$(MERGE_TIMEOUT_SECONDS))

doctor: ensure-uv ensure-go ensure-gofmt ## Check local tools and important resolved paths.
	@$(call print_step,Checking local development environment)
	@$(call print_info,uv: $$(command -v "$(UV)"))
	@$(call print_info,python: $$("$(PYTHON)" --version))
	@$(call print_info,go: $$("$(GO)" version))
	@$(call print_info,gofmt: $$(command -v "$(GOFMT)"))
	@$(call print_info,hook consumer root: $(HOOK_CONSUMER_ROOT))
	@$(call print_info,tool config repo: $(TOOL_CONFIG_REPO))

##@ Setup
ensure-uv: ## Verify uv is available.
	@command -v "$(UV)" >/dev/null 2>&1 || { \
		printf '$(COLOR_WARN)uv is required but was not found on PATH.$(COLOR_RESET)\n' >&2; \
		exit 1; \
	}

ensure-go: ## Verify go is available.
	@command -v "$(GO)" >/dev/null 2>&1 || { \
		printf '$(COLOR_WARN)go is required but was not found on PATH.$(COLOR_RESET)\n' >&2; \
		exit 1; \
	}

ensure-gofmt: ## Verify gofmt is available.
	@command -v "$(GOFMT)" >/dev/null 2>&1 || { \
		printf '$(COLOR_WARN)gofmt is required but was not found on PATH.$(COLOR_RESET)\n' >&2; \
		exit 1; \
	}

ensure-hook-runtime: ## Verify managed hook runtime artifacts already exist without building them.
	@test -x "$(GO_HOOK)" || { \
		printf '$(COLOR_WARN)Managed hook runtime is missing: $(GO_HOOK).$(COLOR_RESET)\n' >&2; \
		printf '$(COLOR_WARN)Run `make build` explicitly before tests or diagnostics.$(COLOR_RESET)\n' >&2; \
		exit 2; \
	}
	@test -s "$(POLICY_DIR)/policy-bundle.json" || { \
		printf '$(COLOR_WARN)Compiled policy bundle is missing: $(POLICY_DIR)/policy-bundle.json.$(COLOR_RESET)\n' >&2; \
		printf '$(COLOR_WARN)Run `make build` explicitly before tests or diagnostics.$(COLOR_RESET)\n' >&2; \
		exit 2; \
	}
	@test -s "$(MANAGED_TOOLCHAIN_MANIFEST)" || { \
		printf '$(COLOR_WARN)Managed toolchain manifest is missing: $(MANAGED_TOOLCHAIN_MANIFEST).$(COLOR_RESET)\n' >&2; \
		printf '$(COLOR_WARN)Run `make build` explicitly before tests or diagnostics.$(COLOR_RESET)\n' >&2; \
		exit 2; \
	}

install: ensure-uv ## Sync the repo's development dependencies.
	@$(call print_step,Syncing development dependencies)
	@$(UV) sync --group dev --all-packages
	@$(MAKE) sync-tool-configs
	@$(MAKE) sync-gemini-prompts

install-runtime: ensure-uv ## Sync only the runtime dependencies.
	@$(call print_step,Syncing runtime dependencies)
	@$(UV) sync --all-packages
	@$(MAKE) sync-tool-configs
	@$(MAKE) sync-gemini-prompts

parent-install: ensure-go ## Sync parent repo coding-ethos artifacts with TOON output.
	@$(quiet_build)
	@"$(GO_HOOK)" parent-install --repo "$(HOOK_CONSUMER_ROOT)"

parent-check: ensure-go ## Verify parent repo coding-ethos artifacts with TOON output.
	@$(quiet_build)
	@"$(GO_HOOK)" parent-check --repo "$(HOOK_CONSUMER_ROOT)"

parent-lint: ensure-go ## Sync and lint the parent repo with TOON output.
	@$(quiet_build)
	@"$(GO_HOOK)" parent-lint --repo "$(HOOK_CONSUMER_ROOT)"

##@ Quality
test: ensure-uv ## Run the current automated test suite.
	@$(call print_step,Running pytest)
	@$(UV) run pytest

python-coverage: ensure-uv ## Run Python tests with coverage enforcement.
	@$(call print_step,Running Python coverage)
	@mkdir -p "$(GO_COVERAGE_DIR)"
	@$(UV) run coverage run --source=coding_ethos -m pytest tests
	@$(UV) run coverage report --fail-under="$(PYTHON_COVERAGE_MIN)"
	@$(UV) run coverage xml -o "$(GO_COVERAGE_DIR)/coverage-python.xml"

check: check-local-artifacts test check-tool-configs check-gemini-prompts check-agent-skills go-test go-e2e-test ## Run the repo's current verification gate.

check-local-artifacts: ## Fail if local build artifacts escaped managed output dirs.
	@$(call print_step,Checking for unmanaged local build artifacts)
	@set -eu; \
	found=0; \
	for name in $(GO_MODULE_ROOT_BINARY_OUTPUTS); do \
		path="$(GO_TOOLS_DIR)/$$name"; \
		if [ -e "$$path" ]; then \
			printf '$(COLOR_WARN)Unmanaged Go build artifact: %s$(COLOR_RESET)\n' "$$path" >&2; \
			found=1; \
		fi; \
	done; \
	if [ "$$found" -ne 0 ]; then \
		printf '$(COLOR_WARN)Use `make build` or `make go-tools-build`; remove module-root binaries with `make go-tools-clean`.$(COLOR_RESET)\n' >&2; \
		exit 1; \
	fi

package-smoke: ## Build, install, and smoke test the wheel outside the source checkout.
	@$(call print_step,Smoke testing built Python package)
	@set -eu; \
		smoke_env="$$(mktemp -d)"; \
		consumer="$$(mktemp -d)"; \
		trap 'rm -rf "$$smoke_env" "$$consumer" dist' EXIT; \
		rm -rf dist && \
		$(UV) build && \
		$(UV) venv "$$smoke_env" >/dev/null && \
		$(UV) pip install \
			--python "$$smoke_env/bin/python" \
			dist/coding_ethos-*.whl >/dev/null && \
		cd "$$consumer" && \
		"$$smoke_env/bin/coding-ethos" --repo generated >/dev/null && \
		test -s generated/ETHOS.md
	@$(call print_info,package smoke passed)

release-dry-run: package-smoke ## Validate release package metadata, checksums, and GitHub workflows locally.
	@$(call print_step,Running release dry run)
	@set -eu; \
		trap 'rm -rf dist dist-checksums sbom' EXIT; \
		rm -rf dist dist-checksums sbom && \
		$(UV) build && \
		uvx twine check dist/*.tar.gz dist/*.whl && \
		mkdir -p dist-checksums sbom && \
		sha256sum dist/*.tar.gz dist/*.whl > dist-checksums/SHA256SUMS && \
		sha256sum --check dist-checksums/SHA256SUMS && \
		bin/coding-ethos-run policy-tool actionlint .github/workflows/*.yml
	@$(call print_info,release dry run passed)

##@ Hooks
sync-tool-configs: ensure-go ## Generate repo-root static-analysis configs from policy.
	@$(call print_step,Syncing generated tool configs)
	@$(call print_info,repo: $(TOOL_CONFIG_REPO))
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" run ./cmd/coding-ethos-policy \
		sync-tool-configs --ethos-root "$(LOCAL_REPO_ROOT)" $(TOOL_CONFIG_FLAGS)

sync-consumer-tool-configs: ensure-go ## Generate consumer repo tool configs when installed in a parent repo.
	@if [ "$(abspath $(HOOK_CONSUMER_ROOT))" != "$(abspath $(LOCAL_REPO_ROOT))" ]; then \
		$(call print_step,Syncing generated consumer tool configs); \
		$(call print_info,repo: $(HOOK_CONSUMER_ROOT)); \
		cd "$(GO_TOOLS_DIR)" && "$(GO)" run ./cmd/coding-ethos-policy \
			sync-tool-configs --ethos-root "$(LOCAL_REPO_ROOT)" \
			--repo "$(HOOK_CONSUMER_ROOT)" $(REPO_CONFIG_FLAG); \
	fi

fix-configs: ensure-go ## Restore generated consumer repo tool configs.
	@$(call print_step,Restoring generated consumer tool configs)
	@$(call print_info,repo: $(HOOK_CONSUMER_ROOT))
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" run ./cmd/coding-ethos-policy \
		sync-tool-configs --ethos-root "$(LOCAL_REPO_ROOT)" \
		--repo "$(HOOK_CONSUMER_ROOT)" $(REPO_CONFIG_FLAG)

check-tool-configs: ensure-hook-runtime ## Fail if repo-root generated tool configs are out of sync.
	@$(call print_step,Checking generated tool configs)
	@$(call print_info,repo: $(TOOL_CONFIG_REPO))
	@"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy" \
		check-tool-configs --ethos-root "$(LOCAL_REPO_ROOT)" $(TOOL_CONFIG_FLAGS)

sync-gemini-prompts: ensure-go ## Generate the grounded Gemini prompt pack for hook runtime.
	@$(call print_step,Syncing grounded Gemini prompt pack)
	@$(call print_info,repo: $(TOOL_CONFIG_REPO))
	@$(call print_info,primary: $(PRIMARY))
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" run ./cmd/coding-ethos-policy \
		sync-gemini-prompts --ethos-root "$(LOCAL_REPO_ROOT)" $(GEMINI_PROMPT_FLAGS)

check-gemini-prompts: ensure-hook-runtime ## Fail if the grounded Gemini prompt pack is out of sync.
	@$(call print_step,Checking grounded Gemini prompt pack)
	@$(call print_info,repo: $(TOOL_CONFIG_REPO))
	@$(call print_info,primary: $(PRIMARY))
	@"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy" \
		check-gemini-prompts --ethos-root "$(LOCAL_REPO_ROOT)" $(GEMINI_PROMPT_FLAGS)

_sync-agent-skills: ensure-go
	@$(call print_step,Syncing generated agent skill surfaces)
	@$(call print_info,repo: $(REPO))
	@$(call print_info,primary: $(PRIMARY))
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" run ./cmd/coding-ethos-policy \
		sync-agent-skills --ethos-root "$(LOCAL_REPO_ROOT)" $(AGENT_SKILL_FLAGS)

_sync-consumer-agent-skills: ensure-go
	@if [ "$(abspath $(HOOK_CONSUMER_ROOT))" != "$(abspath $(LOCAL_REPO_ROOT))" ]; then \
		$(call print_step,Syncing generated consumer agent skill surfaces); \
		$(call print_info,repo: $(HOOK_CONSUMER_ROOT)); \
		cd "$(GO_TOOLS_DIR)" && "$(GO)" run ./cmd/coding-ethos-policy \
			sync-agent-skills --ethos-root "$(LOCAL_REPO_ROOT)" \
			--repo "$(HOOK_CONSUMER_ROOT)" --primary "$(PRIMARY)"; \
	fi

_sync-agent-hooks: ensure-go go-tools-install
	@$(call print_step,Syncing generated agent hook and MCP settings)
	@$(call print_info,repo: $(REPO))
	@"$(GO_TOOLS_BIN_DIR)/coding-ethos-agent-hooks" sync \
		--root "$(REPO)" \
		--hook-command "$(GO_HOOK) agent-hook"

_sync-consumer-agent-hooks: ensure-go go-tools-install
	@if [ "$(abspath $(HOOK_CONSUMER_ROOT))" != "$(abspath $(LOCAL_REPO_ROOT))" ]; then \
		$(call print_step,Syncing generated consumer agent hook and MCP settings); \
		$(call print_info,repo: $(HOOK_CONSUMER_ROOT)); \
		"$(GO_TOOLS_BIN_DIR)/coding-ethos-agent-hooks" sync \
			--root "$(HOOK_CONSUMER_ROOT)" \
			--hook-command "$(GO_HOOK) agent-hook"; \
	fi

check-agent-skills: ensure-hook-runtime ## Fail if provider skill surfaces are out of sync.
	@$(call print_step,Checking generated agent skill surfaces)
	@$(call print_info,repo: $(REPO))
	@$(call print_info,primary: $(PRIMARY))
	@"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy" \
		check-agent-skills --ethos-root "$(LOCAL_REPO_ROOT)" $(AGENT_SKILL_FLAGS)

build: sync-tool-configs sync-consumer-tool-configs sync-gemini-prompts _sync-agent-skills _sync-consumer-agent-skills go-tools-install _sync-git-hooks _sync-agent-hooks _sync-consumer-agent-hooks managed-toolchain-install go-hook-runner-install policy-bundle-install _sync-parent-hook-runtime ## Build checkout-local hook runtime artifacts.

sandbox-runtime-validate: ensure-go go-tools-install ## Validate required sandbox runtime.
	@$(call print_step,Validating native sandbox runtime)
	@"$(GO_TOOLS_BIN_DIR)/coding-ethos-toolchain" validate-sandbox-runtime
	@$(call print_info,native sandbox: required on Linux, unavailable elsewhere)

managed-toolchain-install: ensure-go go-tools-install sandbox-runtime-validate ## Install third-party hook tools into checkout-local managed toolchain dirs.
	@$(call print_step,Installing managed hook toolchain)
	@$(UV) sync --frozen --all-packages >/dev/null
	@"$(GO_TOOLS_BIN_DIR)/coding-ethos-toolchain" install-managed-toolchain \
		--manifest-source "$(MANAGED_TOOLCHAIN_SOURCE)" \
		--go-bin-dir "$(MANAGED_GO_BIN_DIR)" \
		--github-bin-dir "$(MANAGED_GITHUB_BIN_DIR)" \
		--installed-manifest "$(MANAGED_TOOLCHAIN_MANIFEST)"
	@$(call print_info,manifest: $(MANAGED_TOOLCHAIN_MANIFEST))

managed-go-tools-install: managed-toolchain-install ## Alias for installing managed hook toolchain tools.

go-hook-runner-install: ensure-go ## Build the bundled Go hook runner into the checkout-local bin directory.
	@$(call print_step,Installing bundled Go hook runner)
	@mkdir -p "$(LOCAL_BIN_DIR)"
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" build $(GO_BUILD_FLAGS) -o "$(LOCAL_BIN_DIR)/coding-ethos-hook-runner" ./cmd/coding-ethos-hook-runner
	@$(call print_info,installed: $(LOCAL_BIN_DIR)/coding-ethos-hook-runner)

_sync-git-hooks: ensure-go go-tools-install
	@$(call print_step,Syncing Git hook entrypoints)
	@$(call install_git_hooks,$(LOCAL_HOOKS_DIR))
	@if [ "$(HOOKS_DIR)" != "$(LOCAL_HOOKS_DIR)" ]; then \
		$(call install_git_hooks,$(HOOKS_DIR)); \
	fi

_sync-parent-hook-runtime: ensure-go go-tools-install policy-bundle-install
	@$(call print_step,Syncing parent hook runtime artifacts)
	@mkdir -p "$(PARENT_HOOK_BIN_DIR)" "$(PARENT_POLICY_DIR)"
	@cp "$(GO_TOOLS_BIN_DIR)"/coding-ethos-* "$(PARENT_HOOK_BIN_DIR)/"
	@cp "$(GO_TOOLS_BIN_DIR)/cerun" "$(PARENT_HOOK_BIN_DIR)/cerun"
	@"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy" compile \
		--primary "$(LOCAL_REPO_ROOT)/coding_ethos.yml" \
		--repo-ethos "$(LOCAL_REPO_ROOT)/repo_ethos.yml" \
		--config "$(LOCAL_REPO_ROOT)/config.yaml" \
		$(if $(PARENT_REPO_CONFIG),--repo-config "$(PARENT_REPO_CONFIG)",) \
		--out-dir "$(PARENT_POLICY_DIR)"
	@rm -rf "$(PARENT_HOOK_RUNTIME_DIR)/pre-commit" "$(PARENT_HOOK_RUNTIME_DIR)/build/toolchain" "$(PARENT_HOOK_RUNTIME_DIR)/build/policy"
	@mkdir -p "$(PARENT_HOOK_RUNTIME_DIR)/build" "$(PARENT_HOOK_RUNTIME_DIR)/build/policy"
	@cp -R "$(PRECOMMIT_DIR)" "$(PARENT_HOOK_RUNTIME_DIR)/pre-commit"
	@cp "$(LOCAL_REPO_ROOT)/config.yaml" "$(PARENT_HOOK_RUNTIME_DIR)/config.yaml"
	@cp "$(PARENT_POLICY_DIR)"/* "$(PARENT_HOOK_RUNTIME_DIR)/build/policy/"
	@cp -R "$(TOOLCHAIN_DIR)" "$(PARENT_HOOK_RUNTIME_DIR)/build/toolchain"
	@"$(GO_TOOLS_BIN_DIR)/coding-ethos-toolchain" install-git-shim \
		--dest-dir "$(PARENT_HOOK_BIN_DIR)" \
		--real-git "$(GIT)" \
		--runner "$(GO_HOOK)"
	@"$(GO_TOOLS_BIN_DIR)/coding-ethos-lint" \
		--install-shims \
		--tools-bin-dir "$(PARENT_HOOK_BIN_DIR)" \
		--runner "$(PARENT_HOOK_BIN_DIR)/coding-ethos-run" \
		--ethos-root "$(LOCAL_REPO_ROOT)"
	@cp "$(GO_TOOLS_BIN_DIR)/coding-ethos-git-hook" "$(PARENT_HOOK_RUNTIME_DIR)/coding-ethos-git-hook"
	@$(call print_info,runtime: $(PARENT_HOOK_RUNTIME_DIR))

policy-bundle-install: ensure-go go-tools-install managed-toolchain-install ## Compile the policy bundle into the checkout-local build directory.
	@$(call print_step,Compiling policy bundle)
	@mkdir -p "$(POLICY_DIR)"
	@args=(compile --primary "$(LOCAL_REPO_ROOT)/coding_ethos.yml" --repo-ethos "$(LOCAL_REPO_ROOT)/repo_ethos.yml" --config "$(LOCAL_REPO_ROOT)/config.yaml" --out-dir "$(POLICY_DIR)"); \
	if [ -f "$(HOOK_CONSUMER_ROOT)/repo_config.yaml" ]; then \
		args+=(--repo-config "$(HOOK_CONSUMER_ROOT)/repo_config.yaml"); \
	elif [ -f "$(HOOK_CONSUMER_ROOT)/repo_config.yml" ]; then \
		args+=(--repo-config "$(HOOK_CONSUMER_ROOT)/repo_config.yml"); \
	fi; \
	"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy" "$${args[@]}" >/dev/null
	@$(call print_info,compiled: $(POLICY_DIR)/policy-bundle.json)

install-hooks: build ## Install Git hook entrypoints.
	@$(call print_step,Git hook entrypoints refreshed by build)
	@$(call print_info,hooks: $(LOCAL_HOOKS_DIR))
	@if [ "$(HOOKS_DIR)" != "$(LOCAL_HOOKS_DIR)" ]; then \
		$(call print_info,hooks: $(HOOKS_DIR)); \
	fi
	@$(call print_info,runtime: $(PARENT_HOOK_RUNTIME_DIR))

cutover-install: build ## Install Git and agent hooks, then verify cutover readiness.
	@$(call print_step,Installing and verifying repo-local hook cutover)
	@"$(GO_HOOK)" cutover install

cutover-verify: build ## Verify Git and agent hook cutover readiness.
	@$(call print_step,Verifying repo-local hook cutover)
	@"$(GO_HOOK)" cutover verify

pre-commit: build ## Run bundled pre-commit hooks on staged files.
	@$(call print_step,Running Go pre-commit hooks on staged files)
	@"$(GO_HOOK)" git-hook pre-commit

pre-commit-all: build ## Run bundled pre-commit hooks on all files.
	@$(call print_step,Running Go pre-commit hooks on all files)
	@"$(GO_HOOK)" git-hook pre-commit --all-files

pre-push: build ## Run bundled pre-push hooks.
	@$(call print_step,Running Go pre-push hooks)
	@"$(GO_HOOK)" git-hook pre-push

commit-msg: build ## Run commit-message hooks against MSG=/path/to/file.
ifndef MSG
	@printf '$(COLOR_WARN)Usage: make commit-msg MSG=/path/to/commit-message-file$(COLOR_RESET)\n' >&2
	@exit 2
else
	@$(call print_step,Running Go commit-msg hooks)
	@"$(GO_HOOK)" git-hook commit-msg "$(MSG)"
endif

hook-plan: build ## Print the active Go hook group plan.
	@$(call print_step,Printing Go hook group plan)
	@"$(GO_HOOK)" hook-plan

validate: build ## Validate the bundled hook runtime.
	@$(call print_step,Validating bundled hook runtime)
	@"$(GO_HOOK)" git-hook validate

go-test: ensure-go ensure-hook-runtime ## Run Go tests through managed diagnostics.
	@$(call print_step,Running Go tests through managed diagnostics)
	@"$(GO_HOOK)" policy-tool go-test go

lint: ensure-hook-runtime ## Run all configured linters.
	@$(call print_step,Running configured linters)
	@"$(GO_HOOK)" policy-tool-group linters

lint-fix: format fix ## Run formatter and autofixer groups.

fix: ensure-hook-runtime ## Apply managed autofixers.
	@$(call print_step,Applying configured autofixers)
	@"$(GO_HOOK)" policy-tool-group autofixers

format: ensure-hook-runtime ## Run all configured formatters.
	@$(call print_step,Running configured formatters)
	@"$(GO_HOOK)" policy-tool-group formatters

go-e2e-test: ensure-go ## Run real workflow Go end-to-end tests.
	@$(call print_step,Running Go end-to-end workflow tests)
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" test -buildvcs=false -timeout="$(GO_TEST_TIMEOUT)" ./internal/e2e

go-coverage: go-tools-coverage go-hooks-coverage ## Run all Go tests with coverage enforcement.

go-tools-coverage: ensure-go ensure-hook-runtime ## Run shared Go tool tests with coverage enforcement.
	@$(call print_step,Running shared Go coverage)
	@mkdir -p "$(GO_COVERAGE_DIR)"
	@cd "$(GO_TOOLS_DIR)" && mapfile -t packages < <("$(GO)" list -buildvcs=false ./... | grep -v '/internal/e2e$$') && \
		"$(GO)" test -buildvcs=false -short "$${packages[@]}" -covermode=atomic -coverprofile="$(GO_COVERAGE_DIR)/go-shared.out"
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" test -buildvcs=false -timeout="$(GO_TEST_TIMEOUT)" ./internal/e2e
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" tool cover -func="$(GO_COVERAGE_DIR)/go-shared.out" > "$(GO_COVERAGE_DIR)/go-shared.txt"
	@awk -v min="$(GO_COVERAGE_MIN)" '/^total:/ { gsub("%", "", $$3); if ($$3 + 0 < min) { printf "shared Go coverage %.1f%% is below %.1f%%\n", $$3, min; exit 1 } }' "$(GO_COVERAGE_DIR)/go-shared.txt"

go-hooks-coverage: ensure-go ## Run bundled Go hook runtime tests with coverage enforcement.
	@$(call print_step,Running bundled Go hook runtime coverage)
	@mkdir -p "$(GO_COVERAGE_DIR)"
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" test -buildvcs=false -timeout="$(GO_TEST_TIMEOUT)" ./internal/hookrunnercli ./internal/hooks -covermode=atomic -coverprofile="$(GO_COVERAGE_DIR)/go-hooks.out"
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" tool cover -func="$(GO_COVERAGE_DIR)/go-hooks.out" > "$(GO_COVERAGE_DIR)/go-hooks.txt"
	@awk -v min="$(GO_COVERAGE_MIN)" '/^total:/ { gsub("%", "", $$3); if ($$3 + 0 < min) { printf "hook Go coverage %.1f%% is below %.1f%%\n", $$3, min; exit 1 } }' "$(GO_COVERAGE_DIR)/go-hooks.txt"

go-tidy: ensure-go format ## Tidy the shared Go module.
	@$(call print_step,Tidying shared Go module)
	@cd "$(GO_TOOLS_DIR)" && "$(GO)" mod tidy

autofix: lint-fix ## Apply managed formatters and autofixers.

fmt: format ## Format repo-owned source files.

go-tools-test: go-test ## Alias for managed Go diagnostics.

go-tools-build: ensure-go ## Build shared Go tools into go/bin.
	@$(call print_step,Building shared Go tools)
	@mkdir -p "$(GO_TOOLS_DIR)/bin"
	@cd "$(GO_TOOLS_DIR)" && for cmd in $(GO_TOOL_CMDS); do \
		"$(GO)" build $(GO_BUILD_FLAGS) -o "bin/$$cmd" "./cmd/$$cmd"; \
	done
	@$(call print_info,built: $(GO_TOOLS_DIR)/bin)

go-tools-install: ensure-go ## Install shared Go tools into the repo-local hook bin directory.
	@$(call print_step,Installing shared Go tools)
	@mkdir -p "$(GO_TOOLS_BIN_DIR)"
	@cd "$(GO_TOOLS_DIR)" && for cmd in $(GO_TOOL_CMDS); do \
		"$(GO)" build $(GO_BUILD_FLAGS) -o "$(GO_TOOLS_BIN_DIR)/$$cmd" "./cmd/$$cmd"; \
	done
	@$(call print_info,installed: $(GO_TOOLS_BIN_DIR))

go-tools-smoke: export CODE_ETHOS_HOOK_OUTPUT_FORMAT := toon
go-tools-smoke: go-tools-install ## Smoke test shared Go tools using only temporary runtime state.
	@$(call print_step,Smoke testing shared Go tools)
	@tmp_bin="$$(mktemp -d)"; \
		$(MAKE) --no-print-directory go-tools-install GO_TOOLS_BIN_DIR="$$tmp_bin"; \
		"$(GO_TOOLS_DIR)/scripts/smoke.sh" "$(LOCAL_REPO_ROOT)" "$$tmp_bin"

go-tools-clean: ## Remove shared Go tool build outputs under go/bin.
	@$(call print_step,Removing shared Go tool build outputs)
	@rm -rf "$(GO_TOOLS_DIR)/bin" "$(GO_TOOLS_DIR)/.cache"
	@for name in $(GO_MODULE_ROOT_BINARY_OUTPUTS); do \
		rm -f "$(GO_TOOLS_DIR)/$$name"; \
	done

clean-cache: ## Remove checkout-local hook runtime artifacts.
	@$(call print_step,Removing checkout-local hook runtime artifacts)
	@rm -rf "$(LOCAL_BIN_DIR)" "$(LOCAL_BUILD_DIR)/policy" "$(TOOLCHAIN_DIR)"
	@$(call print_warn,Removed $(LOCAL_BIN_DIR), $(LOCAL_BUILD_DIR)/policy, and $(TOOLCHAIN_DIR).)

hooks-validate: validate ## Alias for validate.
hooks-install: install-hooks ## Alias for install-hooks.
hooks-go-test: go-test ## Alias for go-test.

##@ Generation
guard-%: ## Internal guard that requires a make variable, for example guard-SEED_FROM.
	@if [ -z "$($*)" ]; then \
		printf '$(COLOR_WARN)Missing required variable: $*$(COLOR_RESET)\n' >&2; \
		exit 1; \
	fi

seed: ensure-uv guard-SEED_FROM ## Seed or refresh the primary ethos YAML from markdown.
	@$(call print_step,Seeding primary ethos from markdown)
	@$(call print_info,source: $(SEED_FROM))
	@$(call print_info,destination: $(PRIMARY))
	@$(APP) --primary "$(PRIMARY)" --seed-from-markdown "$(SEED_FROM)"

generate: ensure-uv _sync-agent-skills ## Generate agent-facing files into REPO.
	@$(call print_step,Generating agent-facing files)
	@$(call print_info,repo: $(REPO))
	@$(call print_info,primary: $(PRIMARY))
	@$(call print_info,repo ethos: $(if $(strip $(REPO_ETHOS_FLAG)),$(REPO_ETHOS_FLAG),<repo default resolution>))
	@$(APP) $(COMMON_GENERATE_FLAGS)

generate-merge: ensure-uv ## Generate files and preserve existing root files using merge settings.
	@$(call print_step,Generating with merge-existing enabled)
	@$(call print_info,repo: $(REPO))
	@$(call print_info,primary: $(PRIMARY))
	@$(call print_info,merge strategy: $(MERGE_STRATEGY))
	@$(call print_info,merge engine: $(MERGE_ENGINE))
	@$(APP) $(COMMON_GENERATE_FLAGS) $(MERGE_FLAGS)

generate-merge-llm: MERGE_STRATEGY := llm
generate-merge-llm: generate-merge ## Generate files and use the selected LLM CLI for root-file merges.

##@ Housekeeping
clean: ## Remove common Python and pytest caches from the repo.
	@$(call print_step,Removing local caches)
	@rm -rf .pytest_cache build dist .coverage htmlcov
	@find . -type d -name '__pycache__' -prune -exec rm -rf {} +
	@find . -type d -name '*.egg-info' -prune -exec rm -rf {} +
	@$(call print_warn,Removed cache directories and build artifacts.)
