// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package feedback

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	FormatHuman = "human"
	FormatJSON  = "json"
	FormatSARIF = "sarif"
	FormatTOON  = "toon"
)

var errUnsupportedFeedbackFormat = errors.New("unsupported feedback format")

// Payload is the central interface for agent/operator feedback.
type Payload interface {
	MarshalFeedbackJSON() any
	MarshalFeedbackTOON() string
	MarshalFeedbackHuman() string
	MarshalFeedbackSARIF() SARIFLog
	FeedbackLogFields() map[string]any
}

// Write renders payload in the requested feedback format.
func Write(writer io.Writer, payload Payload, format string) error {
	output, err := Render(payload, format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(writer, output)
	if err != nil {
		return fmt.Errorf("write feedback %s: %w", normalizedFormat(format), err)
	}

	return nil
}

// Render returns payload in the requested feedback format.
func Render(payload Payload, format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case FormatJSON:
		output, err := marshalIndented(payload.MarshalFeedbackJSON())
		if err != nil {
			return "", fmt.Errorf("encode feedback JSON: %w", err)
		}

		return output, nil
	case FormatSARIF:
		output, err := marshalIndented(payload.MarshalFeedbackSARIF())
		if err != nil {
			return "", fmt.Errorf("encode feedback SARIF: %w", err)
		}

		return output, nil
	case "", FormatTOON:
		return payload.MarshalFeedbackTOON(), nil
	case FormatHuman:
		return payload.MarshalFeedbackHuman(), nil
	default:
		return "", fmt.Errorf("%w: %q", errUnsupportedFeedbackFormat, format)
	}
}

// MustRender renders a known-good payload and panics only for programmer errors.
func MustRender(payload Payload, format string) string {
	output, err := Render(payload, format)
	if err != nil {
		panic(err)
	}

	return output
}

// Scalar is one key/value feedback field.
type Scalar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Table is one compact feedback table.
type Table struct {
	Name    string     `json:"name"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// Message is a simple reusable Payload implementation.
type Message struct {
	Scalars []Scalar `json:"scalars"`
	Tables  []Table  `json:"tables,omitempty"`
}

// SARIFLog is a minimal SARIF-compatible envelope for recording feedback
// messages that are not lint diagnostics.
type SARIFLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []SARIFRun `json:"runs"`
}

type SARIFRun struct {
	Tool    SARIFTool     `json:"tool"`
	Results []SARIFResult `json:"results,omitempty"`
}

type SARIFTool struct {
	Driver SARIFDriver `json:"driver"`
}

//nolint:tagliatelle // SARIF standard requires camelCase keys.
type SARIFDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []SARIFRule `json:"rules,omitempty"`
}

//nolint:tagliatelle // SARIF standard requires camelCase keys.
type SARIFRule struct {
	Properties       map[string]any `json:"properties,omitempty"`
	ShortDescription SARIFMessage   `json:"shortDescription,omitzero"`
	ID               string         `json:"id"`
	Name             string         `json:"name,omitempty"`
}

//nolint:tagliatelle // SARIF standard requires camelCase keys.
type SARIFResult struct {
	Message SARIFMessage `json:"message"`
	RuleID  string       `json:"ruleId"`
	Level   string       `json:"level"`
}

//nolint:tagliatelle // SARIF standard requires camelCase keys.
type SARIFLocation struct {
	PhysicalLocation SARIFPhysicalLocation `json:"physicalLocation"`
}

//nolint:tagliatelle // SARIF standard requires camelCase keys.
type SARIFPhysicalLocation struct {
	ArtifactLocation SARIFArtifactLocation `json:"artifactLocation"`
	Region           SARIFRegion           `json:"region,omitzero"`
}

type SARIFArtifactLocation struct {
	URI string `json:"uri"`
}

//nolint:tagliatelle // SARIF standard requires camelCase keys.
type SARIFRegion struct {
	StartLine int `json:"startLine,omitempty"`
}

type SARIFMessage struct {
	Text string `json:"text"`
}

func S(key, value string) Scalar {
	return Scalar{Key: key, Value: value}
}

func T(name string, columns []string, rows [][]string) Table {
	return Table{Name: name, Columns: columns, Rows: rows}
}

func (message Message) MarshalFeedbackJSON() any {
	values := make(map[string]any, len(message.Scalars)+len(message.Tables))
	for _, scalar := range message.Scalars {
		if strings.TrimSpace(scalar.Key) != "" {
			values[scalar.Key] = scalar.Value
		}
	}

	for _, table := range message.Tables {
		if strings.TrimSpace(table.Name) != "" {
			values[table.Name] = tableRows(table)
		}
	}

	return values
}

func (message Message) MarshalFeedbackTOON() string {
	lines := make([]string, 0, len(message.Scalars)+len(message.Tables))
	for _, scalar := range message.Scalars {
		if strings.TrimSpace(scalar.Key) != "" {
			lines = append(lines, scalar.Key+": "+Cell(scalar.Value))
		}
	}

	for _, table := range message.Tables {
		if strings.TrimSpace(table.Name) == "" {
			continue
		}

		lines = append(lines, tableHeader(table))

		for _, row := range table.Rows {
			cells := make([]string, 0, len(row))
			for _, value := range row {
				cells = append(cells, Cell(value))
			}

			lines = append(lines, "  "+strings.Join(cells, ","))
		}
	}

	return strings.Join(lines, "\n")
}

func (message Message) MarshalFeedbackHuman() string {
	lines := make([]string, 0, len(message.Scalars)+len(message.Tables))
	for _, scalar := range message.Scalars {
		if strings.TrimSpace(scalar.Key) != "" {
			lines = append(lines, humanLabel(scalar.Key)+": "+scalar.Value)
		}
	}

	for _, table := range message.Tables {
		if strings.TrimSpace(table.Name) == "" {
			continue
		}

		lines = append(lines, humanLabel(table.Name)+":")
		for _, row := range table.Rows {
			lines = append(lines, "  - "+strings.Join(row, ", "))
		}
	}

	return strings.Join(lines, "\n")
}

func (message Message) MarshalFeedbackSARIF() SARIFLog {
	ruleID := scalarValue(message.Scalars, "rule_id")
	if ruleID == "" {
		ruleID = "coding-ethos.feedback"
	}

	level := scalarValue(message.Scalars, "severity")
	if level == "" {
		level = scalarValue(message.Scalars, "status")
	}

	return SARIFLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []SARIFRun{{
			Tool: SARIFTool{Driver: SARIFDriver{
				Name: "coding-ethos",
				Rules: []SARIFRule{{
					ID:   ruleID,
					Name: ruleID,
					ShortDescription: SARIFMessage{
						Text: scalarValue(message.Scalars, "summary"),
					},
				}},
			}},
			Results: []SARIFResult{{
				RuleID:  ruleID,
				Level:   sarifLevel(level),
				Message: SARIFMessage{Text: message.MarshalFeedbackHuman()},
			}},
		}},
	}
}

func (message Message) FeedbackLogFields() map[string]any {
	fields := make(map[string]any, len(message.Scalars)+len(message.Tables))
	for _, scalar := range message.Scalars {
		if strings.TrimSpace(scalar.Key) != "" {
			fields[scalar.Key] = scalar.Value
		}
	}

	for _, table := range message.Tables {
		if strings.TrimSpace(table.Name) != "" {
			fields[table.Name+"_count"] = len(table.Rows)
		}
	}

	return fields
}

// Cell escapes one TOON cell.
func Cell(value string) string {
	if value == "" {
		return ""
	}

	escaped := strings.NewReplacer(
		"\\", "\\\\",
		"\n", "\\n",
		"\r", "\\r",
		",", "\\,",
	).Replace(value)

	if needsQuote(escaped) {
		return strconv.Quote(escaped)
	}

	return escaped
}

func tableHeader(table Table) string {
	columns := strings.Join(table.Columns, ",")
	if columns == "" {
		columns = "value"
	}

	return fmt.Sprintf("%s[%d]{%s}:", table.Name, len(table.Rows), columns)
}

func tableRows(table Table) []map[string]string {
	rows := make([]map[string]string, 0, len(table.Rows))
	for _, row := range table.Rows {
		values := make(map[string]string, len(row))
		for index, value := range row {
			key := fmt.Sprintf("column_%d", index+1)
			if index < len(table.Columns) && table.Columns[index] != "" {
				key = table.Columns[index]
			}

			values[key] = value
		}

		rows = append(rows, values)
	}

	return rows
}

func scalarValue(scalars []Scalar, key string) string {
	for _, scalar := range scalars {
		if scalar.Key == key {
			return scalar.Value
		}
	}

	return ""
}

func sarifLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fatal", "fail", "failed", "error", "blocked", "denied":
		return "error"
	case "warn", "warning":
		return "warning"
	default:
		return "note"
	}
}

func marshalIndented(value any) (string, error) {
	var builder strings.Builder

	encoder := json.NewEncoder(&builder)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(value)
	if err != nil {
		return "", fmt.Errorf("encode indented JSON: %w", err)
	}

	return strings.TrimSuffix(builder.String(), "\n"), nil
}

func normalizedFormat(format string) string {
	normalized := strings.ToLower(strings.TrimSpace(format))
	if normalized == "" {
		return FormatTOON
	}

	return normalized
}

func humanLabel(key string) string {
	return strings.ReplaceAll(key, "_", " ")
}

func needsQuote(value string) bool {
	return strings.HasPrefix(value, " ") ||
		strings.HasSuffix(value, " ") ||
		strings.ContainsAny(value, `":{}[]#`)
}
