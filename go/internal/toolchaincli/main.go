// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolchaincli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

const (
	commandArgIndex   = 1
	commandArgsOffset = 2
	githubAPIVersion  = "2022-11-28"

	privateFileMode     = 0o600
	executableFileMode  = 0o755
	directoryMode       = 0o755
	githubErrorBodySize = 4096
	gitHookCommand      = "git-hook"
	lfsHookCommand      = "lfs-hook"
	agentHookFixHintCap = 2
	toolManifestFields  = 8

	maxExtractedArchiveMemberBytes = 512 * 1024 * 1024
)

var (
	errAssetNotFound   = apperror.StaticError("release asset not found")
	errActionRequired  = apperror.StaticError("cutover-report requires --action")
	errFileRequired    = apperror.StaticError("sha256 requires --file")
	errArchiveTooLarge = apperror.StaticError(
		"archive member exceeds extraction size limit",
	)
	errNegativeTarMember = apperror.StaticError("tar member has negative size")
	errFixItemsOpen      = apperror.StaticError(
		"cutover-report fix item file must be readable",
	)
	errRepoRequired  = apperror.StaticError("github-asset-url requires --repo")
	errTagRequired   = apperror.StaticError("github-asset-url requires --tag")
	errAssetRequired = apperror.StaticError(
		"github-asset-url requires --asset-substring",
	)
	errBinaryRequired = apperror.StaticError("install-github-binary requires --binary")
	errDestRequired   = apperror.StaticError("install-git-shim requires --dest-dir")
	errGitRequired    = apperror.StaticError("install-git-shim requires --real-git")
	errHashRequired   = apperror.StaticError("install-github-binary requires --sha256")
	errHookRequired   = apperror.StaticError("install-git-shim requires --runner")
	errHooksRequired  = apperror.StaticError(
		"git hook shim command requires --hooks-dir",
	)
	errInputRequired    = apperror.StaticError("fix item command requires --input")
	errManifestRequired = apperror.StaticError(
		"install-managed-toolchain requires --manifest-source",
	)
	errRealGitRequired = apperror.StaticError(
		"repo-ignore-fix-items requires --real-git",
	)
	errRepoRootRequired = apperror.StaticError(
		"repo-ignore-fix-items requires --repo-root",
	)
)

func gitHookNames() []string {
	return []string{"pre-commit", "pre-push", "commit-msg"}
}

func obsoleteManagedGitHookNames() []string {
	return []string{"prepare-commit-msg"}
}

func lfsHookNames() []string {
	return []string{
		"post-commit",
		"post-merge",
		"post-checkout",
	}
}

type release struct {
	Assets []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func runCLI(args []string) int {
	if len(args) == 0 {
		usage()

		return commandArgsOffset
	}

	handler, ok := toolchainCommandHandlers()[args[0]]
	if !ok {
		usage()

		return commandArgsOffset
	}

	err := handler(args[1:])
	if err != nil {
		var diagnosticErr interface {
			Diagnostics() []diagnostics.Diagnostic
		}
		if errors.As(err, &diagnosticErr) {
			printToolchainDiagnostics(diagnosticErr.Diagnostics())

			return 1
		}

		fmt.Fprintln(os.Stderr, err)

		return 1
	}

	return 0
}

func printToolchainDiagnostics(items []diagnostics.Diagnostic) {
	if len(items) == 0 {
		return
	}

	output, err := hookoutput.FormatLintResult(lint.Result{
		Scope:       "toolchain",
		Status:      "blocked",
		Diagnostics: items,
	}, hookoutput.FormatTOON)
	if err != nil {
		for _, item := range items {
			fmt.Fprintf(os.Stderr, "%s: %s\n", item.Tool, item.Message)
		}

		return
	}

	fmt.Fprintln(os.Stderr, output)
}

type toolchainCommandHandler func([]string) error

func toolchainCommandHandlers() map[string]toolchainCommandHandler {
	return map[string]toolchainCommandHandler{
		"agent-hook-fix-items":      agentHookFixItems,
		"cutover-report":            cutoverReport,
		"cutover-verify":            cutoverVerify,
		"github-asset-url":          githubAssetURL,
		"install-github-binary":     installGitHubBinaryCommand,
		"install-managed-toolchain": installManagedToolchainCommand,
		"install-git-hooks":         installGitHooks,
		"install-git-shim":          installGitShimCommand,
		"git-hook-fix-items":        gitHookFixItems,
		"repo-ignore-fix-items":     repoIgnoreFixItems,
		"runtime-fix-items":         runtimeFixItems,
		"sha256":                    printSHA256,
		"validate-sandbox-runtime":  validateSandboxRuntimeCommand,
		"verify-git-hooks":          verifyGitHooks,
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, strings.Join(usageLines(), "\n"))
}

func usageLines() []string {
	return []string{
		"usage:",
		"  coding-ethos-toolchain github-asset-url",
		"    --repo owner/name --tag v1.2.3 --asset-substring linux-amd64",
		"  coding-ethos-toolchain agent-hook-fix-items",
		"    --input agent-verify-output.txt",
		"  coding-ethos-toolchain cutover-report",
		"    --action verify --status ready --repo .",
		"    --git-hooks PASS --agent-hooks PASS",
		"    --repo-ignores PASS --runtime PASS",
		"  coding-ethos-toolchain cutover-verify",
		"    --root . --runner bin/coding-ethos-run",
		"    --hooks-dir .git/hooks --real-git /usr/bin/git",
		"    --bundle-root pre-commit",
		"  coding-ethos-toolchain install-github-binary",
		"    --repo owner/name --tag v1.2.3",
		"    --asset-substring linux-amd64 --binary tool",
		"    --dest-dir bin --sha256 abc123",
		"  coding-ethos-toolchain install-git-hooks",
		"    --hooks-dir .git/hooks --runner bin/coding-ethos-run",
		"  coding-ethos-toolchain install-git-shim",
		"    --dest-dir bin --real-git /usr/bin/git",
		"    --runner bin/coding-ethos-run",
		"  coding-ethos-toolchain install-managed-toolchain",
		"    --manifest-source managed-toolchain.tsv",
		"    --go-bin-dir build/toolchain/go-bin",
		"    --github-bin-dir build/toolchain/github-bin",
		"    --installed-manifest build/toolchain/manifest.tsv",
		"  coding-ethos-toolchain git-hook-fix-items",
		"    --hooks-dir .git/hooks --runner bin/coding-ethos-run",
		"  coding-ethos-toolchain repo-ignore-fix-items",
		"    --repo-root . --real-git /usr/bin/git",
		"  coding-ethos-toolchain runtime-fix-items",
		"    --input runtime-output.txt",
		"  coding-ethos-toolchain sha256 --file path",
		"  coding-ethos-toolchain validate-sandbox-runtime",
		"    # validates Linux sandbox support; no-op on other platforms",
		"  coding-ethos-toolchain verify-git-hooks",
		"    --hooks-dir .git/hooks --runner bin/coding-ethos-run",
	}
}
