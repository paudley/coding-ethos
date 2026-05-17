// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EvaluateFileSyntax(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	extensions := stringSliceOption(
		context.EvaluatorOptions,
		"extensions",
		[]string{".json", ".toml", ".yaml", ".yml"},
	)
	allowed := extensionSet(extensions)

	for _, file := range context.Files {
		if !allowed[strings.ToLower(filepath.Ext(file))] {
			continue
		}

		err := validateSyntaxFile(file)
		if err != nil {
			return []policy.Decision{syntaxDecision(policyDef, file, err)}, nil
		}
	}

	return nil, nil
}

func extensionSet(extensions []string) map[string]bool {
	allowed := make(map[string]bool, len(extensions))
	for _, raw := range extensions {
		extension := strings.ToLower(strings.TrimSpace(raw))
		if extension == "" {
			continue
		}

		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}

		allowed[extension] = true
	}

	return allowed
}

func validateSyntaxFile(path string) error {
	regular, err := isRegularGuardFile(path)
	if err != nil || !regular {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("read syntax file: %w", err)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return validateJSONSyntax(data)
	case ".toml":
		return validateTOMLSyntax(data)
	case ".yaml", ".yml":
		return validateYAMLSyntax(data)
	default:
		return nil
	}
}

func validateJSONSyntax(data []byte) error {
	var value any

	err := json.Unmarshal(data, &value)
	if err != nil {
		return fmt.Errorf("parse JSON syntax: %w", err)
	}

	return nil
}

func validateTOMLSyntax(data []byte) error {
	var value any

	err := toml.Unmarshal(data, &value)
	if err != nil {
		return fmt.Errorf("parse TOML syntax: %w", err)
	}

	return nil
}

func validateYAMLSyntax(data []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))

	for {
		var value any

		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("parse YAML syntax: %w", err)
		}
	}
}

func syntaxDecision(policyDef policy.Policy, file string, err error) policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Diagnostics = []diagnostics.Diagnostic{{
		Tool:     "syntax",
		File:     file,
		Severity: blockDecision,
		Code:     strings.TrimPrefix(filepath.Ext(file), "."),
		PolicyID: policyDef.ID,
		Message:  err.Error(),
		Advice:   policyDef.Suggestion,
	}}
	decision.Evidence = map[string]any{
		"file":  file,
		"error": err.Error(),
	}

	return decision
}
