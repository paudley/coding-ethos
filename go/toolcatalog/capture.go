// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolcatalog

import "strings"

func (tool Tool) CaptureArgs(args []string) ([]string, bool) {
	if len(tool.CaptureOutputArgs) == 0 || tool.captureArgsMutate(args) {
		return append([]string(nil), args...), false
	}

	stripped := tool.stripCaptureOutputArgs(args)
	if len(stripped) > 0 && stringInSet(stripped[0], tool.CaptureAfterFirst) {
		return appendCopy(
			appendCopy([]string{stripped[0]}, tool.CaptureOutputArgs...),
			stripped[1:]...,
		), true
	}

	return appendCopy(tool.CaptureOutputArgs, stripped...), true
}

func (tool Tool) captureArgsMutate(args []string) bool {
	for _, arg := range args {
		if stringInSet(arg, tool.CaptureMutatingArgs) {
			return true
		}
	}

	return len(args) > 0 && stringInSet(args[0], tool.CaptureMutatingFirst)
}

func (tool Tool) stripCaptureOutputArgs(args []string) []string {
	stripped := stripArgs(args, tool.CaptureStripFlags...)

	return stripArgsWithValues(stripped, tool.CaptureStripArgs...)
}

func stripArgs(args []string, flags ...string) []string {
	stripped := []string{}
	for _, arg := range args {
		if !stringInSet(arg, flags) {
			stripped = append(stripped, arg)
		}
	}

	return stripped
}

func stripArgsWithValues(args []string, flags ...string) []string {
	stripped := []string{}
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}

		matchedFlag := false
		for _, flag := range flags {
			if arg == flag {
				matchedFlag = true
				skipNext = true

				break
			}
			if strings.HasPrefix(arg, flag+"=") {
				matchedFlag = true

				break
			}
		}
		if matchedFlag {
			continue
		}

		stripped = append(stripped, arg)
	}

	return stripped
}

func appendCopy(args []string, extra ...string) []string {
	copied := append([]string(nil), args...)

	return append(copied, extra...)
}

func stringInSet(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}

	return false
}
