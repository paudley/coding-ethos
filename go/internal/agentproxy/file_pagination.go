// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/shellquote"
)

const (
	// FileReadPaginationTransformName is the stable transform identifier for
	// file-read proxy pagination evidence.
	FileReadPaginationTransformName = "file-read-pagination"
	FileReadPaginationPolicyID      = "proxy.file_pagination"

	defaultFileReadPageStart       = 1
	fileReadPageMarkerLineCapacity = 3
)

// FileReadPaginationTransform caps direct file-read output to a line-bounded,
// line-numbered page while preserving full-output evidence.
type FileReadPaginationTransform struct {
	Path      string
	PageStart int
	PageEnd   int
}

func (transform FileReadPaginationTransform) Name() string {
	return FileReadPaginationTransformName
}

func (transform FileReadPaginationTransform) Apply(
	_ context.Context,
	input TransformInput,
) (TransformOutput, error) {
	lines := splitOutputLines(input.Text)
	if len(lines) <= transform.PageEnd || transform.PageEnd <= 0 {
		return TransformOutput{
			Text:     input.Text,
			Metadata: cloneMetadata(input.Metadata),
			Record: TransformRecord{
				PolicyID: FileReadPaginationPolicyID,
				Decision: "allow",
				Reason:   "file read output within page budget",
			},
		}, nil
	}

	evidencePath, err := writeFullOutputEvidence(input.Text)
	if err != nil {
		return TransformOutput{}, err
	}

	pageStart := transform.PageStart
	if pageStart <= 0 {
		pageStart = defaultFileReadPageStart
	}

	pageEnd := min(transform.PageEnd, len(lines))
	output := fileReadPageOutput(
		lines,
		pageStart,
		pageEnd,
		transform.Path,
		evidencePath,
	)

	metadata := cloneMetadata(input.Metadata)
	metadata["coding_ethos.file_paginated"] = metadataValueTrue
	metadata["coding_ethos.file_page_start"] = strconv.Itoa(pageStart)
	metadata["coding_ethos.file_page_end"] = strconv.Itoa(pageEnd)
	metadata["coding_ethos.file_total_lines"] = strconv.Itoa(len(lines))
	metadata[metadataFullOutputPath] = evidencePath

	return TransformOutput{
		Text:     output,
		Metadata: metadata,
		Record: TransformRecord{
			PolicyID:     FileReadPaginationPolicyID,
			Decision:     "truncate",
			Reason:       "file read output paginated by line budget",
			EvidencePath: evidencePath,
		},
	}, nil
}

func fileReadPageOutput(
	lines []string,
	pageStart int,
	pageEnd int,
	path string,
	evidencePath string,
) string {
	width := len(strconv.Itoa(len(lines)))
	output := make([]string, 0, pageEnd+fileReadPageMarkerLineCapacity)
	output = append(
		output,
		"[coding-ethos: paginated file read; showing lines "+
			strconv.Itoa(pageStart)+"-"+strconv.Itoa(pageEnd)+
			" of "+strconv.Itoa(len(lines))+" for "+path+
			"; full output: "+evidencePath+"]",
	)

	for index := pageStart; index <= pageEnd; index++ {
		output = append(output, fileReadNumberedLine(index, width, lines[index-1]))
	}

	if pageEnd < len(lines) {
		output = append(
			output,
			"[coding-ethos: next page: sed -n '"+
				strconv.Itoa(pageEnd+1)+","+
				strconv.Itoa(min(pageEnd+(pageEnd-pageStart)+1, len(lines)))+
				"p' "+shellquote.Arg(path)+"]",
		)
	}

	return strings.Join(output, "\n") + "\n"
}

func fileReadNumberedLine(number, width int, line string) string {
	return leftPadInt(number, width) + " | " + line
}

func leftPadInt(value, width int) string {
	return fmt.Sprintf("%*d", width, value)
}
