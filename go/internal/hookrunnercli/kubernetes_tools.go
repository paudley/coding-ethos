// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

func runKubeLinter(_ Config, paths []string) int {
	files := kubernetesManifestFiles(
		toolchainFiles("kube-linter", existingFiles(paths)),
	)
	if len(files) == 0 {
		return 0
	}

	return runManagedPolicyTool(
		"kube-linter",
		managedPolicyToolArgsForFiles("kube-linter", files),
	)
}

func kubernetesManifestFiles(paths []string) []string {
	files := make([]string, 0, len(paths))
	for _, path := range paths {
		if isKubernetesManifestFile(path) {
			files = append(files, path)
		}
	}

	return files
}

func isKubernetesManifestFile(path string) bool {
	payload, err := readRootedFile(path)
	if err != nil {
		return false
	}

	decoder := yaml.NewDecoder(bytes.NewReader(payload))

	for {
		var document map[string]any

		err = decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return false
		}

		if err != nil {
			return false
		}

		if hasKubernetesIdentity(document) {
			return true
		}
	}
}

func hasKubernetesIdentity(document map[string]any) bool {
	apiVersion, hasAPIVersion := document["apiVersion"].(string)
	kind, hasKind := document["kind"].(string)

	return hasAPIVersion &&
		hasKind &&
		strings.TrimSpace(apiVersion) != "" &&
		strings.TrimSpace(kind) != ""
}
