// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

// Package lintcapture contains Go-owned lint capture preparation primitives.
package lintcapture

type Request struct {
	ToolName      string
	OutputFormat  string
	ManagedTool   string
	InvocationCwd string
	ConsumerRoot  string
	EthosRoot     string
	TraceRoot     string
	OriginalArgv  []string
}

func (request Request) Clone() Request {
	request.OriginalArgv = append([]string(nil), request.OriginalArgv...)

	return request
}
