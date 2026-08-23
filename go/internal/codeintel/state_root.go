// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"os"
	"path/filepath"
	"strings"
)

// StateRootEnvironment names the private code-intel state-root override.
const StateRootEnvironment = "CODE_ETHOS_STATE_ROOT"

// ResolveStateRoot returns the configured private state root or the repository
// root when private state has not been configured.
func ResolveStateRoot(repositoryRoot string) string {
	stateRoot := strings.TrimSpace(os.Getenv(StateRootEnvironment))
	if stateRoot == "" {
		return filepath.Clean(repositoryRoot)
	}

	return filepath.Clean(stateRoot)
}
