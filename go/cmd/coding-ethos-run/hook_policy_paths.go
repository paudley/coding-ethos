// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import "path/filepath"

func hookPolicyBundlePath(paths runtimePaths) string {
	return hookPolicyArtifactPath(paths, "policy-bundle.json", paths.PolicyBundle)
}

func hookPolicyMetadataPath(paths runtimePaths) string {
	return hookPolicyArtifactPath(paths, "policy-metadata.json", paths.PolicyMetadata)
}

func hookPolicyArtifactPath(paths runtimePaths, name, checkoutPath string) string {
	if sameCleanPath(paths.Root, paths.EthosRoot) {
		return checkoutPath
	}

	return filepath.Join(paths.GitCommonDir, "coding-ethos-hooks", "policy", name)
}
