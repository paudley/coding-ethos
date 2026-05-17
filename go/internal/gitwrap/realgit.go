// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package gitwrap

import (
	"context"
	"fmt"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	gitExecutableName = "git"
	realGitEnv        = realgit.Env
)

func ResolveRealGit(requested string) (string, error) {
	resolved, err := realgit.Resolve(context.Background(), requested)
	if err != nil {
		return "", fmt.Errorf("resolve host git executable: %w", err)
	}

	return resolved, nil
}
