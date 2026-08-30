// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package astfacts

import (
	"path/filepath"
	"slices"
	"strings"
	"unsafe"

	tree_sitter_toml "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go"
	tree_sitter_yaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_json "github.com/tree-sitter/tree-sitter-json/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

const (
	LanguageGo         = "go"
	LanguagePython     = "python"
	LanguageJavaScript = "javascript"
	LanguageJSON       = "json"
	LanguageMarkdown   = "markdown"
	LanguageNQuads     = "nquads"
	LanguageNTriples   = "ntriples"
	LanguageRust       = "rust"
	LanguageShell      = "shell"
	LanguageSPARQL     = "sparql"
	LanguageTOML       = "toml"
	LanguageTriG       = "trig"
	LanguageTurtle     = "turtle"
	LanguageTypeScript = "typescript"
	LanguageYAML       = "yaml"

	ExtractorKindExternal   = "external"
	ExtractorKindGoldmark   = "goldmark"
	ExtractorKindTreeSitter = "tree-sitter"

	PurrdfExtractorName     = "purrdf"
	PurrdfExtractorProtocol = "coding-ethos.external-extractor.v1"
	PurrdfExtractorRevision = "3aa4ba514ee9cdbfb6966bd0f1fedb87e0d8a0b6"
)

// ExtractorDescriptor identifies the exact implementation that produces facts.
type ExtractorDescriptor struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
	Protocol    string `json:"protocol,omitempty"`
}

// LanguageDescriptor is the centralized source-language and extractor contract.
type LanguageDescriptor struct {
	Extractor    ExtractorDescriptor `json:"extractor"`
	Language     string              `json:"language"`
	Variant      string              `json:"variant"`
	Extensions   []string            `json:"extensions"`
	Capabilities []string            `json:"capabilities"`
	BuiltIn      bool                `json:"built_in"`
}

type languageBinding struct {
	parserFactory func() unsafe.Pointer
	descriptor    LanguageDescriptor
}

// LanguageDescriptors returns a stable copy of every recognized source language.
func LanguageDescriptors() []LanguageDescriptor {
	bindings := languageBindings()

	descriptors := make([]LanguageDescriptor, 0, len(bindings))
	for _, binding := range bindings {
		descriptor := binding.descriptor
		descriptor.Extensions = slices.Clone(descriptor.Extensions)
		descriptor.Capabilities = slices.Clone(descriptor.Capabilities)
		descriptors = append(descriptors, descriptor)
	}

	return descriptors
}

// SourceLanguageForPath reports every recognized language, including external ones.
func SourceLanguageForPath(path string) (LanguageDescriptor, bool) {
	binding, ok := bindingForPath(path)
	if !ok {
		return LanguageDescriptor{}, false
	}

	return binding.descriptor, true
}

// LanguageForPath reports languages handled by the in-process AST resolver.
func LanguageForPath(path string) (string, bool) {
	binding, ok := bindingForPath(path)
	if !ok || !binding.descriptor.BuiltIn {
		return "", false
	}

	return binding.descriptor.Language, true
}

func languageForPath(path string) (string, string, unsafe.Pointer, bool) {
	binding, ok := bindingForPath(path)
	if !ok || !binding.descriptor.BuiltIn {
		return "", "", nil, false
	}

	var parser unsafe.Pointer
	if binding.parserFactory != nil {
		parser = binding.parserFactory()
	}

	return binding.descriptor.Language, binding.descriptor.Variant, parser, true
}

func bindingForPath(path string) (languageBinding, bool) {
	extension := strings.ToLower(filepath.Ext(path))
	for _, binding := range languageBindings() {
		if slices.Contains(binding.descriptor.Extensions, extension) {
			return binding, true
		}
	}

	return languageBinding{}, false
}

func languageBindings() []languageBinding {
	bindings := builtInLanguageBindings()

	return append(bindings, externalLanguageBindings()...)
}

func builtInLanguageBindings() []languageBinding {
	return []languageBinding{
		treeSitterBinding(
			LanguageGo,
			"go",
			"v0.25.0",
			[]string{".go"},
			tree_sitter_go.Language,
		),
		treeSitterBinding(
			LanguagePython,
			"python",
			"v0.25.0",
			[]string{".py"},
			tree_sitter_python.Language,
		),
		treeSitterBinding(
			LanguageJavaScript,
			"javascript",
			"v0.25.0",
			[]string{".js", ".jsx", ".mjs", ".cjs"},
			tree_sitter_javascript.Language,
		),
		treeSitterBinding(
			LanguageTypeScript,
			"typescript",
			"v0.23.2",
			[]string{".ts", ".mts", ".cts"},
			tree_sitter_typescript.LanguageTypescript,
		),
		treeSitterBinding(
			LanguageTypeScript,
			"tsx",
			"v0.23.2",
			[]string{".tsx"},
			tree_sitter_typescript.LanguageTSX,
		),
		treeSitterBinding(
			LanguageRust,
			"rust",
			"v0.23.2",
			[]string{".rs"},
			tree_sitter_rust.Language,
		),
		treeSitterBinding(
			LanguageJSON,
			"json",
			"v0.24.8",
			[]string{".json", ".jsonc"},
			tree_sitter_json.Language,
		),
		goldmarkBinding(),
		treeSitterBinding(
			LanguageShell,
			"bash",
			"v0.25.1",
			[]string{".sh", ".bash", ".zsh"},
			tree_sitter_bash.Language,
		),
		treeSitterBinding(
			LanguageTOML,
			"toml",
			"v0.7.0",
			[]string{".toml"},
			tree_sitter_toml.Language,
		),
		treeSitterBinding(
			LanguageYAML,
			"yaml",
			"v0.7.2",
			[]string{".yaml", ".yml"},
			tree_sitter_yaml.Language,
		),
	}
}

func goldmarkBinding() languageBinding {
	return languageBinding{descriptor: LanguageDescriptor{
		Language:     LanguageMarkdown,
		Variant:      "markdown",
		Extensions:   []string{".md"},
		Capabilities: []string{"symbols"},
		BuiltIn:      true,
		Extractor: ExtractorDescriptor{
			Name:        "goldmark",
			Kind:        ExtractorKindGoldmark,
			Version:     "v1.8.2",
			Fingerprint: "goldmark@v1.8.2",
		},
	}}
}

func externalLanguageBindings() []languageBinding {
	return []languageBinding{
		purrdfBinding(
			LanguageTurtle,
			"turtle",
			[]string{".ttl"},
			[]string{"semantic_graph", "subject_start_spans"},
		),
		purrdfBinding(
			LanguageTriG,
			"trig",
			[]string{".trig"},
			[]string{"semantic_graph", "subject_start_spans"},
		),
		purrdfBinding(
			LanguageNTriples,
			"ntriples",
			[]string{".nt"},
			[]string{"semantic_graph", "subject_start_spans"},
		),
		purrdfBinding(
			LanguageNQuads,
			"nquads",
			[]string{".nq"},
			[]string{"semantic_graph", "subject_start_spans"},
		),
		purrdfBinding(
			LanguageSPARQL,
			"sparql",
			[]string{".rq", ".sparql"},
			[]string{"query_algebra"},
		),
	}
}

func treeSitterBinding(
	language string,
	variant string,
	version string,
	extensions []string,
	parserFactory func() unsafe.Pointer,
) languageBinding {
	return languageBinding{
		descriptor: LanguageDescriptor{
			Language:     language,
			Variant:      variant,
			Extensions:   extensions,
			Capabilities: []string{"imports", "references", "symbols"},
			BuiltIn:      true,
			Extractor: ExtractorDescriptor{
				Name:        "tree-sitter-" + variant,
				Kind:        ExtractorKindTreeSitter,
				Version:     version,
				Fingerprint: "tree-sitter-" + variant + "@" + version,
			},
		},
		parserFactory: parserFactory,
	}
}

func purrdfBinding(
	language string,
	variant string,
	extensions []string,
	capabilities []string,
) languageBinding {
	return languageBinding{descriptor: LanguageDescriptor{
		Language:     language,
		Variant:      variant,
		Extensions:   extensions,
		Capabilities: capabilities,
		BuiltIn:      false,
		Extractor: ExtractorDescriptor{
			Name:        PurrdfExtractorName,
			Kind:        ExtractorKindExternal,
			Version:     "1",
			Fingerprint: PurrdfExtractorName + "@1+purrdf-" + PurrdfExtractorRevision,
			Protocol:    PurrdfExtractorProtocol,
		},
	}}
}
