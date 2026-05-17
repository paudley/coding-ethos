// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package geminiprompts

const (
	defaultCodeBatchSize      = 3
	defaultShellBatchSize     = 5
	largePromptFileLimitKB    = 50
	shellEthosFileLimitKB     = 30
	suppressionCheckBatchSize = 8
	placeholderBatchSize      = 10
)

func checkSpecs() map[string]checkSpec {
	shellSelector := selectorSpec{
		IncludeExtensions:           []string{".sh", ".bash"},
		ExcludeSubstrings:           []string{},
		ExcludePrefixes:             []string{},
		AllowExtensionlessInScripts: true,
		ShebangMarkers:              []string{"bash", "sh"},
	}

	return map[string]checkSpec{
		"code_ethos": {
			FileScope:     "code",
			BatchSize:     defaultCodeBatchSize,
			MaxFileSizeKB: largePromptFileLimitKB,
			Selector: selectorSpec{
				IncludeExtensions: []string{
					".py",
					".pyi",
					".sh",
					".bash",
					".go",
					".rs",
					".ts",
					".js",
				},
				ExcludeSubstrings: []string{
					"test_",
					"_test.",
					".test.",
					"/tests/",
					"/test/",
					"/__pycache__/",
					"/node_modules/",
					"/vendor/",
					"/.venv/",
					"/venv/",
					"/migrations/",
				},
				ExcludePrefixes: []string{
					".venv/",
					"venv/",
					"__pycache__/",
					"node_modules/",
				},
				AllowExtensionlessInScripts: false,
				ShebangMarkers:              []string{"python", "bash", "sh"},
			},
		},
		"shell_review": {
			FileScope:     "shell",
			BatchSize:     defaultShellBatchSize,
			MaxFileSizeKB: largePromptFileLimitKB,
			Selector:      shellSelector,
		},
		"shell_ethos": {
			FileScope:     "shell",
			BatchSize:     defaultShellBatchSize,
			MaxFileSizeKB: shellEthosFileLimitKB,
			Selector:      shellSelector,
		},
		"shell_documentation": {
			FileScope:     "shell",
			BatchSize:     defaultShellBatchSize,
			MaxFileSizeKB: largePromptFileLimitKB,
			Selector:      shellSelector,
		},
		"shellcheck_suppression": {
			FileScope:     "shell",
			BatchSize:     suppressionCheckBatchSize,
			MaxFileSizeKB: largePromptFileLimitKB,
			Selector:      shellSelector,
		},
		"shell_placeholder": {
			FileScope:     "shell",
			BatchSize:     placeholderBatchSize,
			MaxFileSizeKB: largePromptFileLimitKB,
			Selector:      shellSelector,
		},
	}
}

func checkSpecPayloads(specs map[string]checkSpec) map[string]any {
	payloads := make(map[string]any, len(specs))
	for name, spec := range specs {
		payloads[name] = map[string]any{
			"fileScope":     spec.FileScope,
			"selector":      selectorPayload(spec.Selector),
			"batchSize":     spec.BatchSize,
			"maxFileSizeKb": spec.MaxFileSizeKB,
		}
	}

	return payloads
}

func selectorPayload(spec selectorSpec) map[string]any {
	return map[string]any{
		"excludePrefixes":             spec.ExcludePrefixes,
		"excludeSubstrings":           spec.ExcludeSubstrings,
		"includeExtensions":           spec.IncludeExtensions,
		"shebangMarkers":              spec.ShebangMarkers,
		"allowExtensionlessInScripts": spec.AllowExtensionlessInScripts,
	}
}
