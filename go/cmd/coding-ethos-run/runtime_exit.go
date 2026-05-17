// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

type runtimeExit struct {
	code int
}

func requestRuntimeExit(code int) {
	panic(runtimeExit{code: code})
}

func captureRuntimeExit(code *int) {
	recovered := recover()
	if recovered == nil {
		return
	}

	exit, ok := recovered.(runtimeExit)
	if !ok {
		panic(recovered)
	}

	*code = exit.code
}
