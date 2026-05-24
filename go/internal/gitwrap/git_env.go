// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package gitwrap

import (
	"os"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	// WrapperAuthorizedEnv marks real Git children launched by the managed wrapper.
	WrapperAuthorizedEnv = "CODE_ETHOS_GIT_WRAPPER_AUTHORIZED"
	// WrapperPIDEnv carries the managed wrapper PID that authorized real Git.
	WrapperPIDEnv = "CODE_ETHOS_GIT_WRAPPER_PID"
)

func gitExecutionEnv(adminApproved bool) []string {
	env := cleanCodingEthosExecutionEnv(realgit.CleanGitLocalEnv(os.Environ()))

	env = append(
		env,
		WrapperAuthorizedEnv+"=1",
		WrapperPIDEnv+"="+strconv.Itoa(os.Getpid()),
	)
	if adminApproved {
		env = append(env, adminApprovedEnv+"=1")
	}

	return env
}

func cleanCodingEthosExecutionEnv(source []string) []string {
	env := make([]string, 0, len(source))
	for _, item := range source {
		name, _, found := strings.Cut(item, "=")
		if found && codingEthosExecutionEnvBlocked(name) {
			continue
		}

		env = append(env, item)
	}

	return env
}

func codingEthosExecutionEnvBlocked(name string) bool {
	switch name {
	case "CODING_ETHOS_EXEC_STACK",
		"DISPLAY",
		WrapperAuthorizedEnv,
		WrapperPIDEnv,
		"WAYLAND_DISPLAY",
		"XAUTHORITY":
		return true
	default:
		return false
	}
}
