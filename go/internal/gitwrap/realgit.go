// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"fmt"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	gitExecutableName = "git"
	realGitEnv        = realgit.Env
)

func ResolveRealGit(requested string) (string, error) {
	resolved, err := realgit.Resolve(requested)
	if err != nil {
		return "", fmt.Errorf("resolve host git executable: %w", err)
	}

	return resolved, nil
}
