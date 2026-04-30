# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

.DEFAULT_GOAL := help
.SUFFIXES:
.DELETE_ON_ERROR:

LOCAL_REPO_ROOT := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
PRECOMMIT_DIR := $(LOCAL_REPO_ROOT)/pre-commit/
HOOKS_GO_DIR := $(PRECOMMIT_DIR)hooks/go-hooks
GO_HOOK_SOURCES := $(wildcard $(HOOKS_GO_DIR)/*.go)
GO_TOOLS_DIR := $(LOCAL_REPO_ROOT)/go

define resolve_hook_consumer_root
super="$$(git -C "$(LOCAL_REPO_ROOT)" rev-parse \
	--show-superproject-working-tree 2>/dev/null)"; \
if [ -n "$$super" ]; then \
	printf '%s' "$$super"; \
else \
	git -C "$(LOCAL_REPO_ROOT)" rev-parse --show-toplevel; \
fi
endef

HOOK_CONSUMER_ROOT := $(shell $(resolve_hook_consumer_root))
GIT_COMMON_DIR := $(shell git -C "$(HOOK_CONSUMER_ROOT)" rev-parse --path-format=absolute --git-common-dir)
HOOKS_DIR := $(shell git -C "$(HOOK_CONSUMER_ROOT)" rev-parse --path-format=absolute --git-path hooks)
GO_HOOK := $(PRECOMMIT_DIR)hooks/run-go-hook.sh
LOCAL_BIN_DIR := $(LOCAL_REPO_ROOT)/bin
LOCAL_BUILD_DIR := $(LOCAL_REPO_ROOT)/build
POLICY_DIR := $(LOCAL_BUILD_DIR)/policy
TOOLCHAIN_DIR := $(LOCAL_BUILD_DIR)/toolchain
MANAGED_GO_BIN_DIR := $(TOOLCHAIN_DIR)/go-bin
MANAGED_PREFIX_DIR := $(TOOLCHAIN_DIR)/prefix
MANAGED_GITHUB_BIN_DIR := $(TOOLCHAIN_DIR)/github-bin
GIT_HOOKS := pre-commit pre-push commit-msg
GIT_LFS_HOOKS := post-commit post-merge post-checkout
GO_TOOLS_BIN_DIR ?= $(LOCAL_BIN_DIR)
SHFMT_VERSION ?= v3.13.1
GO_TOOL_CMDS := \
	coding-ethos-agent-hooks \
	coding-ethos-policy \
	coding-ethos-lint \
	coding-ethos-hook \
	coding-ethos-git-hook \
	coding-ethos-git

UV ?= uv
PYTHON ?= python
GO ?= go
GOFMT ?= gofmt
GO_TOOLS_CACHE_DIR ?= $(GO_TOOLS_DIR)/.cache/go-build
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

.PHONY: \
	help \
	status \
	doctor \
	install \
	install-runtime \
	build \
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
	go-test \
	go-tidy \
	go-fmt \
	fmt \
	go-tools-test \
	go-tools-build \
	go-tools-install \
	managed-toolchain-install \
	managed-go-tools-install \
	go-hook-runner-install \
	policy-bundle-install \
	go-tools-smoke \
	go-tools-clean \
	clean-cache \
	sync-tool-configs \
	fix-configs \
	check-tool-configs \
	sync-gemini-prompts \
	check-gemini-prompts \
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

##@ Quality
test: ensure-uv ## Run the current automated test suite.
	@$(call print_step,Running pytest)
	@$(UV) run pytest

check: test check-tool-configs check-gemini-prompts go-test go-tools-test go-tools-smoke ## Run the repo's current verification gate.

##@ Hooks
sync-tool-configs: ensure-uv ## Generate repo-root pyright, mypy, Ruff, Pylint, yamllint, and golangci-lint configs.
	@$(call print_step,Syncing generated tool configs)
	@$(call print_info,repo: $(TOOL_CONFIG_REPO))
	@$(APP) $(TOOL_CONFIG_FLAGS) --sync-tool-configs

fix-configs: ensure-uv ## Restore generated consumer repo tool configs.
	@$(call print_step,Restoring generated consumer tool configs)
	@$(call print_info,repo: $(HOOK_CONSUMER_ROOT))
	@$(APP) --repo "$(HOOK_CONSUMER_ROOT)" $(REPO_CONFIG_FLAG) --sync-tool-configs

check-tool-configs: ensure-uv ## Fail if repo-root generated tool configs are out of sync.
	@$(call print_step,Checking generated tool configs)
	@$(call print_info,repo: $(TOOL_CONFIG_REPO))
	@$(APP) $(TOOL_CONFIG_FLAGS) --check-tool-configs

sync-gemini-prompts: ensure-uv ## Generate the grounded Gemini prompt pack for hook runtime.
	@$(call print_step,Syncing grounded Gemini prompt pack)
	@$(call print_info,repo: $(TOOL_CONFIG_REPO))
	@$(call print_info,primary: $(PRIMARY))
	@$(APP) $(GEMINI_PROMPT_FLAGS) --sync-gemini-prompts

check-gemini-prompts: ensure-uv ## Fail if the grounded Gemini prompt pack is out of sync.
	@$(call print_step,Checking grounded Gemini prompt pack)
	@$(call print_info,repo: $(TOOL_CONFIG_REPO))
	@$(call print_info,primary: $(PRIMARY))
	@$(APP) $(GEMINI_PROMPT_FLAGS) --check-gemini-prompts

build: sync-tool-configs sync-gemini-prompts go-tools-install go-hook-runner-install policy-bundle-install ## Build checkout-local hook runtime artifacts.

managed-toolchain-install: managed-go-tools-install ## Install third-party hook tools into checkout-local managed toolchain dirs.

managed-go-tools-install: ensure-go ## Install Go-distributed hook tools into the managed Go bin dir.
	@$(call print_step,Installing managed Go toolchain tools)
	@mkdir -p "$(MANAGED_GO_BIN_DIR)"
	@GOBIN="$(MANAGED_GO_BIN_DIR)" "$(PRECOMMIT_DIR)hooks/install-source-tool.sh" \
		go mvdan.cc/sh/v3/cmd/shfmt@$(SHFMT_VERSION)
	@$(call print_info,installed: $(MANAGED_GO_BIN_DIR)/shfmt)

go-hook-runner-install: ensure-go ## Build the bundled Go hook runner into the checkout-local bin directory.
	@$(call print_step,Installing bundled Go hook runner)
	@mkdir -p "$(LOCAL_BIN_DIR)"
	@cd "$(HOOKS_GO_DIR)" && "$(GO)" build -buildvcs=false -o "$(LOCAL_BIN_DIR)/coding-ethos-hook-runner" .
	@$(call print_info,installed: $(LOCAL_BIN_DIR)/coding-ethos-hook-runner)

policy-bundle-install: ensure-go go-tools-install managed-toolchain-install ## Compile the policy bundle into the checkout-local build directory.
	@$(call print_step,Compiling policy bundle)
	@mkdir -p "$(POLICY_DIR)"
	@args=(compile --primary "$(LOCAL_REPO_ROOT)/coding_ethos.yml" --config "$(LOCAL_REPO_ROOT)/config.yaml" --out-dir "$(POLICY_DIR)"); \
	if [ -f "$(HOOK_CONSUMER_ROOT)/repo_config.yaml" ]; then \
		args+=(--repo-config "$(HOOK_CONSUMER_ROOT)/repo_config.yaml"); \
	elif [ -f "$(HOOK_CONSUMER_ROOT)/repo_config.yml" ]; then \
		args+=(--repo-config "$(HOOK_CONSUMER_ROOT)/repo_config.yml"); \
	fi; \
	"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy" "$${args[@]}" >/dev/null
	@$(call print_info,compiled: $(POLICY_DIR)/policy-bundle.json)

install-hooks: build ## Install Git hook shims.
	@$(call print_step,Installing Git hook shims)
	@mkdir -p "$(HOOKS_DIR)"
	@for hook in $(GIT_HOOKS); do \
		cp "$(PRECOMMIT_DIR)hooks/run-git-hook.sh" "$(HOOKS_DIR)/$$hook"; \
		chmod +x "$(HOOKS_DIR)/$$hook"; \
	done
	@for hook in $(GIT_LFS_HOOKS); do \
		cp "$(PRECOMMIT_DIR)hooks/run-lfs-hook.sh" "$(HOOKS_DIR)/$$hook"; \
		chmod +x "$(HOOKS_DIR)/$$hook"; \
	done
	@$(call print_info,installed: Go hook runner)
	@$(call print_info,installed: Git LFS delegation hooks)

cutover-install: build ## Install Git and agent hooks, then verify cutover readiness.
	@$(call print_step,Installing and verifying repo-local hook cutover)
	@"$(GO_HOOK)" cutover install

cutover-verify: ensure-go ## Verify Git and agent hook cutover readiness.
	@$(call print_step,Verifying repo-local hook cutover)
	@"$(GO_HOOK)" cutover verify

pre-commit: ensure-go ## Run bundled pre-commit hooks on staged files.
	@$(call print_step,Running Go pre-commit hooks on staged files)
	@"$(GO_HOOK)" git-hook pre-commit

pre-commit-all: ensure-go ## Run bundled pre-commit hooks on all files.
	@$(call print_step,Running Go pre-commit hooks on all files)
	@"$(GO_HOOK)" git-hook pre-commit --all-files

pre-push: ensure-go ## Run bundled pre-push hooks.
	@$(call print_step,Running Go pre-push hooks)
	@"$(GO_HOOK)" git-hook pre-push

commit-msg: ensure-go ## Run commit-message hooks against MSG=/path/to/file.
ifndef MSG
	@printf '$(COLOR_WARN)Usage: make commit-msg MSG=/path/to/commit-message-file$(COLOR_RESET)\n' >&2
	@exit 2
else
	@$(call print_step,Running Go commit-msg hooks)
	@"$(GO_HOOK)" git-hook commit-msg "$(MSG)"
endif

hook-plan: ensure-go ## Print the active Go hook group plan.
	@$(call print_step,Printing Go hook group plan)
	@"$(GO_HOOK)" hook-plan

validate: ensure-go ## Validate the bundled hook runtime.
	@$(call print_step,Validating bundled hook runtime)
	@"$(GO_HOOK)" git-hook validate

go-test: ensure-go ## Run the bundled Go helper tests.
	@$(call print_step,Running bundled Go hook tests)
	@cd "$(HOOKS_GO_DIR)" && "$(GO)" test ./...

go-tidy: ensure-go go-fmt ## Tidy and format the bundled Go hook helper.
	@$(call print_step,Tidying bundled Go hook helper)
	@cd "$(HOOKS_GO_DIR)" && "$(GO)" mod tidy

go-fmt: ensure-gofmt ## Format the bundled Go hook helper.
	@$(call print_step,Formatting bundled Go hook helper)
	@"$(GOFMT)" -w $(GO_HOOK_SOURCES)

fmt: go-fmt ## Format repo-owned generated helper source files.

go-tools-test: ensure-go ## Run the shared Go tool tests.
	@$(call print_step,Running shared Go tool tests)
	@mkdir -p "$(GO_TOOLS_CACHE_DIR)"
	@cd "$(GO_TOOLS_DIR)" && GOCACHE="$(GO_TOOLS_CACHE_DIR)" "$(GO)" test ./...

go-tools-build: ensure-go ## Build shared Go tools into go/bin.
	@$(call print_step,Building shared Go tools)
	@mkdir -p "$(GO_TOOLS_DIR)/bin"
	@mkdir -p "$(GO_TOOLS_CACHE_DIR)"
	@cd "$(GO_TOOLS_DIR)" && for cmd in $(GO_TOOL_CMDS); do \
		GOCACHE="$(GO_TOOLS_CACHE_DIR)" "$(GO)" build -buildvcs=false -o "bin/$$cmd" "./cmd/$$cmd"; \
	done
	@$(call print_info,built: $(GO_TOOLS_DIR)/bin)

go-tools-install: ensure-go ## Install shared Go tools into the repo-local hook bin directory.
	@$(call print_step,Installing shared Go tools)
	@mkdir -p "$(GO_TOOLS_BIN_DIR)"
	@mkdir -p "$(GO_TOOLS_CACHE_DIR)"
	@cd "$(GO_TOOLS_DIR)" && for cmd in $(GO_TOOL_CMDS); do \
		GOCACHE="$(GO_TOOLS_CACHE_DIR)" "$(GO)" build -buildvcs=false -o "$(GO_TOOLS_BIN_DIR)/$$cmd" "./cmd/$$cmd"; \
	done
	@$(call print_info,installed: $(GO_TOOLS_BIN_DIR))

go-tools-smoke: ## Smoke test shared Go tools using only temporary runtime state.
	@$(call print_step,Smoke testing shared Go tools)
	@tmp_bin="$$(mktemp -d)"; \
		$(MAKE) --no-print-directory go-tools-install GO_TOOLS_BIN_DIR="$$tmp_bin"; \
		"$(GO_TOOLS_DIR)/scripts/smoke.sh" "$(LOCAL_REPO_ROOT)" "$$tmp_bin"

go-tools-clean: ## Remove shared Go tool build outputs under go/bin.
	@$(call print_step,Removing shared Go tool build outputs)
	@rm -rf "$(GO_TOOLS_DIR)/bin" "$(GO_TOOLS_DIR)/.cache"

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

generate: ensure-uv ## Generate agent-facing files into REPO.
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
