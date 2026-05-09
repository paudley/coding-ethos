// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func gitExecutionEnv(adminApproved bool) []string {
	env := cleanCodingEthosExecutionEnv(realgit.CleanGitLocalEnv(os.Environ()))
	if adminApproved {
		env = append(env, adminApprovedEnv+"=1")
	}

	return env
}

func cleanCodingEthosExecutionEnv(source []string) []string {
	env := make([]string, 0, len(source))
	for _, item := range source {
		name, _, found := strings.Cut(item, "=")
		if found && name == "CODING_ETHOS_EXEC_STACK" {
			continue
		}

		env = append(env, item)
	}

	return env
}
