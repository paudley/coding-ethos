// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/diagnostics"
)

const (
	defaultToolOutputMaxLines    = 80
	defaultToolOutputHead        = 32
	defaultToolOutputTail        = 32
	defaultToolOutputMaxTokens   = 2000
	defaultToolOutputHeadTokens  = 900
	defaultToolOutputTailTokens  = 900
	minPreservedLineBudget       = 2
	minToolOutputMaxLines        = 3
	minToolOutputMaxTokens       = 32
	metadataFullOutputPath       = "coding_ethos.full_output_path"
	toolOutputEvidenceMaxAge     = 24 * time.Hour
	toolOutputEvidencePattern    = "coding-ethos-tool-output-*.log"
	toolOutputEvidencePrefix     = "coding-ethos-tool-output-"
	toolOutputEvidenceSuffix     = ".log"
	tokenBudgetMarkerTokens      = 8
	tokenBudgetSplitParts        = 2
	defaultDiagnosticMaxFindings = 12
	diagnosticSummaryLineSlack   = 3
	metadataValueTrue            = "true"
	minTokenBudgetLineFragment   = 80
)

// ToolOutputDiagnosticSummaryTransform condenses parseable compiler, linter,
// and test output into a concise diagnostic table before generic output
// compression runs.
type ToolOutputDiagnosticSummaryTransform struct {
	Tool           string
	MaxFindings    int
	EvidenceMaxAge time.Duration
}

func (transform ToolOutputDiagnosticSummaryTransform) Name() string {
	return "tool-output-diagnostic-summary"
}

func (transform ToolOutputDiagnosticSummaryTransform) Apply(
	_ context.Context,
	input TransformInput,
) (TransformOutput, error) {
	tool := strings.TrimSpace(transform.Tool)
	if tool == "" || strings.TrimSpace(input.Text) == "" {
		return TransformOutput{
			Text:     input.Text,
			Metadata: cloneMetadata(input.Metadata),
			Record: TransformRecord{
				PolicyID: "proxy.diagnostic_summary",
				Decision: "allow",
				Reason:   "tool output has no diagnostic parser",
			},
		}, nil
	}

	items := diagnostics.Parse(tool, input.Text, "")
	if len(items) == 0 {
		return TransformOutput{
			Text:     input.Text,
			Metadata: cloneMetadata(input.Metadata),
			Record: TransformRecord{
				PolicyID: "proxy.diagnostic_summary",
				Decision: "allow",
				Reason:   "tool output had no parseable diagnostics",
			},
		}, nil
	}

	evidencePath, err := writeFullOutputEvidence(input.Text, transform.EvidenceMaxAge)
	if err != nil {
		return TransformOutput{}, err
	}

	output := diagnosticSummaryOutput(tool, items, transform.findingLimit(), evidencePath)
	metadata := cloneMetadata(input.Metadata)
	metadata["coding_ethos.diagnostic_summary"] = metadataValueTrue
	metadata["coding_ethos.diagnostic_count"] = strconv.Itoa(len(items))
	metadata[metadataFullOutputPath] = evidencePath

	return TransformOutput{
		Text:     output,
		Metadata: metadata,
		Record: TransformRecord{
			PolicyID:      "proxy.diagnostic_summary",
			Decision:      "summarize",
			Reason:        "tool output converted to diagnostic summary",
			EvidencePath:  evidencePath,
			FindingsCount: len(items),
		},
	}, nil
}

func (transform ToolOutputDiagnosticSummaryTransform) findingLimit() int {
	if transform.MaxFindings <= 0 {
		return defaultDiagnosticMaxFindings
	}

	return transform.MaxFindings
}

func diagnosticSummaryOutput(
	tool string,
	items []diagnostics.Diagnostic,
	limit int,
	evidencePath string,
) string {
	count := min(len(items), limit)
	lines := make([]string, 0, count+diagnosticSummaryLineSlack)
	lines = append(lines, "[coding-ethos: diagnostic summary; "+
		strconv.Itoa(len(items))+" findings parsed from "+tool+
		"; full output: "+evidencePath+"]")
	lines = append(lines, "findings["+strconv.Itoa(count)+
		"]{tool,file,line,column,severity,code,message}:")

	for _, item := range items[:count] {
		lines = append(lines, "  "+diagnosticSummaryLine(item))
	}

	if len(items) > count {
		lines = append(lines, "  "+
			strconv.Itoa(len(items)-count)+" additional findings omitted; "+
			"use full output evidence for complete diagnostics")
	}

	return strings.Join(lines, "\n")
}

func diagnosticSummaryLine(item diagnostics.Diagnostic) string {
	return strings.Join([]string{
		summaryCell(firstNonEmptyString(item.Tool, "unknown")),
		summaryCell(item.File),
		strconv.Itoa(item.Line),
		strconv.Itoa(item.Column),
		summaryCell(item.Severity),
		summaryCell(item.Code),
		summaryCell(item.Message),
	}, ",")
}

func summaryCell(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, ",", "\\,")

	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

// ToolOutputCompressionTransform caps verbose tool output while preserving the
// beginning and ending context where command identity and terminal failures
// usually appear.
type ToolOutputCompressionTransform struct {
	MaxLines       int
	Head           int
	Tail           int
	EvidenceMaxAge time.Duration
}

func (transform ToolOutputCompressionTransform) Name() string {
	return "tool-output-compression"
}

func (transform ToolOutputCompressionTransform) Apply(
	_ context.Context,
	input TransformInput,
) (TransformOutput, error) {
	limits := transform.normalizedLimits()
	lines := splitOutputLines(input.Text)

	if len(lines) <= limits.MaxLines {
		return TransformOutput{
			Text:     input.Text,
			Metadata: cloneMetadata(input.Metadata),
			Record: TransformRecord{
				Reason: "tool output within line budget",
			},
		}, nil
	}

	evidencePath, err := writeFullOutputEvidence(input.Text, transform.EvidenceMaxAge)
	if err != nil {
		return TransformOutput{}, err
	}

	omitted := len(lines) - limits.Head - limits.Tail
	compressed := make([]string, 0, limits.Head+limits.Tail+1)
	compressed = append(compressed, lines[:limits.Head]...)
	compressed = append(
		compressed,
		strings.Join([]string{
			"[coding-ethos: compressed tool output; ",
			strconv.Itoa(omitted),
			" of ",
			strconv.Itoa(len(lines)),
			" lines omitted to save tokens; full output: ",
			evidencePath,
			"]",
		}, ""),
	)
	compressed = append(compressed, lines[len(lines)-limits.Tail:]...)

	metadata := cloneMetadata(input.Metadata)
	metadata["coding_ethos.compressed"] = metadataValueTrue
	metadata["coding_ethos.compressed_lines_omitted"] = strconv.Itoa(omitted)
	metadata[metadataFullOutputPath] = evidencePath

	return TransformOutput{
		Text:     joinOutputLines(compressed, strings.HasSuffix(input.Text, "\n")),
		Metadata: metadata,
		Record: TransformRecord{
			PolicyID:     "proxy.token_budget",
			Decision:     "truncate",
			Reason:       "tool output exceeded line budget",
			EvidencePath: evidencePath,
		},
	}, nil
}

type toolOutputCompressionLimits struct {
	MaxLines int
	Head     int
	Tail     int
}

func (
	transform ToolOutputCompressionTransform,
) normalizedLimits() toolOutputCompressionLimits {
	limits := toolOutputCompressionLimits{
		MaxLines: transform.MaxLines,
		Head:     transform.Head,
		Tail:     transform.Tail,
	}

	if limits.MaxLines <= 0 {
		limits.MaxLines = defaultToolOutputMaxLines
	}

	if limits.Head <= 0 {
		limits.Head = defaultToolOutputHead
	}

	if limits.Tail <= 0 {
		limits.Tail = defaultToolOutputTail
	}

	if limits.MaxLines < minToolOutputMaxLines {
		limits.MaxLines = minToolOutputMaxLines
	}

	if limits.Head+limits.Tail >= limits.MaxLines {
		preservedLineBudget := max(limits.MaxLines-1, minPreservedLineBudget)
		head := preservedLineBudget / minPreservedLineBudget
		tail := preservedLineBudget - head

		if limits.Head < head {
			head = limits.Head
			tail = preservedLineBudget - head
		}

		if limits.Tail < tail {
			tail = limits.Tail
			head = preservedLineBudget - tail
		}

		limits.Head = head
		limits.Tail = tail
	}

	return limits
}

func splitOutputLines(text string) []string {
	if text == "" {
		return nil
	}

	lines := slices.Collect(strings.Lines(text))
	for index, line := range lines {
		lines[index] = trimLineEnding(line)
	}

	return lines
}

// OutputLineCount returns the line count used by proxy output transforms.
func OutputLineCount(text string) int {
	return len(splitOutputLines(text))
}

func trimLineEnding(line string) string {
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
}

func joinOutputLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		return ""
	}

	output := strings.Join(lines, "\n")
	if trailingNewline {
		output += "\n"
	}

	return output
}

// ToolOutputTokenBudgetTransform applies a hard token budget to tool output
// after any diagnostic-aware or line-aware compression has already run.
type ToolOutputTokenBudgetTransform struct {
	MaxTokens      int
	HeadTokens     int
	TailTokens     int
	EvidenceMaxAge time.Duration
}

func (transform ToolOutputTokenBudgetTransform) Name() string {
	return "tool-output-token-budget"
}

func (transform ToolOutputTokenBudgetTransform) Apply(
	_ context.Context,
	input TransformInput,
) (TransformOutput, error) {
	limits := transform.normalizedLimits()
	tokenizer := input.Tokenizer

	if tokenizer == nil {
		tokenizer = WhitespaceTokenizer{}
	}

	tokens := tokenizer.Count(input.Text)
	if tokens <= limits.MaxTokens {
		return TransformOutput{
			Text:     input.Text,
			Metadata: cloneMetadata(input.Metadata),
			Record: TransformRecord{
				PolicyID: "proxy.token_budget",
				Decision: "allow",
				Reason:   "tool output within token budget",
				Metadata: cloneMetadata(input.Metadata),
			},
		}, nil
	}

	evidencePath, err := originalEvidencePath(input, transform.EvidenceMaxAge)
	if err != nil {
		return TransformOutput{}, err
	}

	output := budgetedOutput(input.Text, limits, tokenizer, evidencePath)
	metadata := cloneMetadata(input.Metadata)
	metadata["coding_ethos.token_budget_exceeded"] = metadataValueTrue
	metadata["coding_ethos.input_tokens"] = strconv.Itoa(tokens)
	metadata["coding_ethos.max_tokens"] = strconv.Itoa(limits.MaxTokens)
	metadata[metadataFullOutputPath] = evidencePath

	return TransformOutput{
		Text:     output,
		Metadata: metadata,
		Record: TransformRecord{
			PolicyID:     "proxy.token_budget",
			Decision:     "truncate",
			Reason:       "tool output exceeded token budget",
			EvidencePath: evidencePath,
			Metadata:     cloneMetadata(metadata),
		},
	}, nil
}

func originalEvidencePath(input TransformInput, maxAge time.Duration) (string, error) {
	if path := input.Metadata[metadataFullOutputPath]; path != "" {
		return path, nil
	}

	return writeFullOutputEvidence(input.Text, maxAge)
}

type toolOutputTokenBudgetLimits struct {
	MaxTokens  int
	HeadTokens int
	TailTokens int
}

func (
	transform ToolOutputTokenBudgetTransform,
) normalizedLimits() toolOutputTokenBudgetLimits {
	limits := toolOutputTokenBudgetLimits{
		MaxTokens:  transform.MaxTokens,
		HeadTokens: transform.HeadTokens,
		TailTokens: transform.TailTokens,
	}

	if limits.MaxTokens <= 0 {
		limits.MaxTokens = defaultToolOutputMaxTokens
	}

	if limits.HeadTokens <= 0 {
		limits.HeadTokens = defaultToolOutputHeadTokens
	}

	if limits.TailTokens <= 0 {
		limits.TailTokens = defaultToolOutputTailTokens
	}

	if limits.MaxTokens < minToolOutputMaxTokens {
		limits.MaxTokens = minToolOutputMaxTokens
	}

	if limits.HeadTokens+limits.TailTokens >= limits.MaxTokens {
		preserved := max(
			limits.MaxTokens-tokenBudgetMarkerTokens,
			minToolOutputMaxTokens/tokenBudgetSplitParts,
		)
		limits.HeadTokens = preserved / tokenBudgetSplitParts
		limits.TailTokens = preserved - limits.HeadTokens
	}

	return limits
}

func budgetedOutput(
	text string,
	limits toolOutputTokenBudgetLimits,
	tokenizer Tokenizer,
	evidencePath string,
) string {
	lines := splitOutputLines(text)
	if len(lines) == 0 {
		return ""
	}

	head, headTokens := takeHeadTokenBudget(lines, limits.HeadTokens, tokenizer)
	tail, tailTokens := takeTailTokenBudget(lines, limits.TailTokens, tokenizer)
	omittedTokens := max(
		0,
		tokenizer.Count(text)-headTokens-tailTokens,
	)

	output := make([]string, 0, len(head)+len(tail)+1)
	output = append(output, "[WARNING: Payload exceeded "+
		strconv.Itoa(limits.MaxTokens)+
		" tokens. Output truncated by proxy. "+
		"Please use a grep tool or smaller file range.]")
	output = append(output, head...)
	output = append(output, "[coding-ethos: token budget hard stop; "+
		strconv.Itoa(omittedTokens)+" tokens omitted; full output: "+
		evidencePath+"; use a narrower command]")
	output = append(output, tail...)

	return joinOutputLines(output, strings.HasSuffix(text, "\n"))
}

func writeFullOutputEvidence(text string, maxAge time.Duration) (string, error) {
	if maxAge <= 0 {
		maxAge = toolOutputEvidenceMaxAge
	}

	pruneFullOutputEvidenceFiles(time.Now(), maxAge)

	file, err := os.CreateTemp("", toolOutputEvidencePattern)
	if err != nil {
		return "", fmt.Errorf("create tool output evidence file: %w", err)
	}

	path := file.Name()
	_, writeErr := file.WriteString(text)
	closeErr := file.Close()

	if writeErr != nil {
		_ = os.Remove(path)

		return "", fmt.Errorf("write tool output evidence file %s: %w", path, writeErr)
	}

	if closeErr != nil {
		_ = os.Remove(path)

		return "", fmt.Errorf("close tool output evidence file %s: %w", path, closeErr)
	}

	return path, nil
}

func pruneFullOutputEvidenceFiles(now time.Time, maxAge time.Duration) {
	matches, err := filepath.Glob(
		filepath.Join(os.TempDir(), toolOutputEvidencePattern),
	)
	if err != nil {
		return
	}

	for _, path := range matches {
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() || !isFullOutputEvidencePath(path) {
			continue
		}

		if now.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(path)
		}
	}
}

func isFullOutputEvidencePath(path string) bool {
	name := filepath.Base(path)

	return strings.HasPrefix(name, toolOutputEvidencePrefix) &&
		strings.HasSuffix(name, toolOutputEvidenceSuffix)
}

func takeHeadTokenBudget(
	lines []string,
	budget int,
	tokenizer Tokenizer,
) ([]string, int) {
	selected := []string{}
	used := 0

	for _, line := range lines {
		lineTokens := max(1, tokenizer.Count(line))
		if used > 0 && used+lineTokens > budget {
			break
		}

		if lineTokens > budget {
			selected = append(selected, tokenBudgetLineHead(line, budget))
			used = budget
		} else {
			selected = append(selected, line)
			used += lineTokens
		}

		if used >= budget {
			break
		}
	}

	return selected, used
}

func takeTailTokenBudget(
	lines []string,
	budget int,
	tokenizer Tokenizer,
) ([]string, int) {
	selected := []string{}
	used := 0

	for index := len(lines) - 1; index >= 0; index-- {
		line := lines[index]
		lineTokens := max(1, tokenizer.Count(line))

		if used > 0 && used+lineTokens > budget {
			break
		}

		if lineTokens > budget {
			selected = append(selected, tokenBudgetLineTail(line, budget))
			used = budget
		} else {
			selected = append(selected, line)
			used += lineTokens
		}

		if used >= budget {
			break
		}
	}

	slices.Reverse(selected)

	return selected, used
}

func tokenBudgetLineHead(line string, tokens int) string {
	runes := []rune(line)
	limit := max(minTokenBudgetLineFragment, tokens*approximateTokenRuneDivisor)

	if len(runes) <= limit {
		return line
	}

	return string(runes[:limit])
}

func tokenBudgetLineTail(line string, tokens int) string {
	runes := []rune(line)
	limit := max(minTokenBudgetLineFragment, tokens*approximateTokenRuneDivisor)

	if len(runes) <= limit {
		return line
	}

	return string(runes[len(runes)-limit:])
}
