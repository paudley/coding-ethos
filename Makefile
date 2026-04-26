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
ROOT_LEFTHOOK := $(HOOK_CONSUMER_ROOT)/lefthook.yml
GO_HOOK := $(PRECOMMIT_DIR)hooks/run-go-hook.sh
LEFTHOOK_RUNNER := $(PRECOMMIT_DIR)hooks/run-lefthook.sh
LOCAL_BIN_DIR := $(GIT_COMMON_DIR)/coding-ethos-hooks
LOCAL_LEFTHOOK := $(LOCAL_BIN_DIR)/lefthook
LOCAL_LEFTHOOK_VERSION_FILE := $(LOCAL_BIN_DIR)/lefthook.version
GIT_HOOKS := pre-commit pre-push commit-msg
LEFTHOOK_VERSION_FILE := $(PRECOMMIT_DIR)lefthook.version
LEFTHOOK_VERSION := $(strip $(shell cat "$(LEFTHOOK_VERSION_FILE)"))

UV ?= uv
PYTHON ?= python
GO ?= go
GOFMT ?= gofmt
REPO ?= $(LOCAL_REPO_ROOT)
TOOL_CONFIG_REPO ?= $(HOOK_CONSUMER_ROOT)
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

LEFTHOOK := $(LOCAL_LEFTHOOK)

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

define run_lefthook
cd "$(HOOK_CONSUMER_ROOT)" && { \
	$(LEFTHOOK) run --no-auto-install $(1) 2>&1 \
		| "$(GO_HOOK)" quiet-filter; \
	exit "$${PIPESTATUS[0]}"; \
}
endef

.PHONY: \
	help \
	status \
	doctor \
	install \
	install-runtime \
	test \
	check \
	install-hooks \
	pre-commit \
	pre-commit-all \
	pre-push \
	commit-msg \
	validate \
	lefthook-validate \
	go-test \
	go-tidy \
	go-fmt \
	fmt \
	clean-cache \
	sync-tool-configs \
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
	ensure-lefthook \
	check-root-config \
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
		/^[a-zA-Z0-9_.-]+:.*## / { \
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
	@$(call print_kv,LOCAL_LEFTHOOK,$(LOCAL_LEFTHOOK))
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

doctor: ensure-uv ensure-go ensure-gofmt check-root-config ## Check local tools and important resolved paths.
	@$(call print_step,Checking local development environment)
	@$(call print_info,uv: $$(command -v "$(UV)"))
	@$(call print_info,python: $$("$(PYTHON)" --version))
	@$(call print_info,go: $$("$(GO)" version))
	@$(call print_info,gofmt: $$(command -v "$(GOFMT)"))
	@$(call print_info,lefthook config: $(ROOT_LEFTHOOK))
	@$(call print_info,hook consumer root: $(HOOK_CONSUMER_ROOT))

##@ Setup
ensure-uv:
	@command -v "$(UV)" >/dev/null 2>&1 || { \
		printf '$(COLOR_WARN)uv is required but was not found on PATH.$(COLOR_RESET)\n' >&2; \
		exit 1; \
	}

ensure-go:
	@command -v "$(GO)" >/dev/null 2>&1 || { \
		printf '$(COLOR_WARN)go is required but was not found on PATH.$(COLOR_RESET)\n' >&2; \
		exit 1; \
	}

ensure-gofmt:
	@command -v "$(GOFMT)" >/dev/null 2>&1 || { \
		printf '$(COLOR_WARN)gofmt is required but was not found on PATH.$(COLOR_RESET)\n' >&2; \
		exit 1; \
	}

ensure-lefthook: check-root-config ensure-go
	@mkdir -p "$(LOCAL_BIN_DIR)"
	@if [ ! -x "$(LOCAL_LEFTHOOK)" ] \
		|| [ ! -f "$(LOCAL_LEFTHOOK_VERSION_FILE)" ] \
		|| [ "$$(cat "$(LOCAL_LEFTHOOK_VERSION_FILE)" 2>/dev/null)" != "$(LEFTHOOK_VERSION)" ]; then \
		$(call print_step,Installing repo-local Lefthook $(LEFTHOOK_VERSION)); \
		GOBIN="$(LOCAL_BIN_DIR)" "$(GO)" install github.com/evilmartians/lefthook@$(LEFTHOOK_VERSION); \
		printf '%s\n' "$(LEFTHOOK_VERSION)" > "$(LOCAL_LEFTHOOK_VERSION_FILE)"; \
	fi

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

check: test check-tool-configs check-gemini-prompts ## Run the repo's current verification gate.

##@ Hooks
check-root-config:
	@if [ ! -e "$(ROOT_LEFTHOOK)" ]; then \
		printf '$(COLOR_WARN)Missing %s.$(COLOR_RESET)\n' "$(ROOT_LEFTHOOK)" >&2; \
		printf 'Restore the repo-root lefthook.yml symlink before running hook targets.\n' >&2; \
		exit 1; \
	fi

sync-tool-configs: ensure-uv ## Generate repo-root pyright, mypy, Ruff, yamllint, and golangci-lint configs.
	@$(call print_step,Syncing generated tool configs)
	@$(call print_info,repo: $(TOOL_CONFIG_REPO))
	@$(APP) $(TOOL_CONFIG_FLAGS) --sync-tool-configs

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

install-hooks: sync-tool-configs sync-gemini-prompts ensure-lefthook ## Install Git hook shims.
	@$(call print_step,Installing Git hook shims)
	@mkdir -p "$(HOOKS_DIR)"
	@for hook in $(GIT_HOOKS); do \
		cp "$(LEFTHOOK_RUNNER)" "$(HOOKS_DIR)/$$hook"; \
		chmod +x "$(HOOKS_DIR)/$$hook"; \
	done
	@if [ -f "$(HOOKS_DIR)/prepare-commit-msg" ] \
		&& grep -q 'call_lefthook run "prepare-commit-msg"' \
			"$(HOOKS_DIR)/prepare-commit-msg"; then \
		rm -f "$(HOOKS_DIR)/prepare-commit-msg"; \
	fi
	@$(call print_info,installed: $(LOCAL_LEFTHOOK))

pre-commit: ensure-lefthook ## Run bundled pre-commit hooks on staged files.
	@$(call print_step,Running Lefthook pre-commit on staged files)
	@$(call run_lefthook,--no-stage-fixed pre-commit)

pre-commit-all: ensure-lefthook ## Run bundled pre-commit hooks on all files.
	@$(call print_step,Running Lefthook pre-commit on all files)
	@$(call run_lefthook,--no-stage-fixed pre-commit --all-files)

pre-push: ensure-lefthook ## Run bundled pre-push hooks.
	@$(call print_step,Running Lefthook pre-push)
	@$(call run_lefthook,pre-push)

commit-msg: ensure-lefthook ## Run commit-message hooks against MSG=/path/to/file.
ifndef MSG
	@printf '$(COLOR_WARN)Usage: make commit-msg MSG=/path/to/commit-message-file$(COLOR_RESET)\n' >&2
	@exit 2
else
	@$(call print_step,Running Lefthook commit-msg)
	@$(call run_lefthook,commit-msg "$(MSG)")
endif

validate: ensure-lefthook ## Validate the bundled Lefthook configuration.
	@$(call print_step,Validating bundled pre-commit hooks)
	@cd "$(HOOK_CONSUMER_ROOT)" && $(LEFTHOOK) validate

lefthook-validate: validate

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

clean-cache: ## Remove cached bundled hook binaries from .git.
	@$(call print_step,Removing cached bundled hook binaries)
	@rm -rf "$(LOCAL_BIN_DIR)"
	@$(call print_warn,Removed $(LOCAL_BIN_DIR).)

hooks-validate: validate
hooks-install: install-hooks
hooks-go-test: go-test

##@ Generation
guard-%:
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
