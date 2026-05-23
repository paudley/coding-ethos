// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package feedback

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageRendersTOONJSONHumanAndSARIF(t *testing.T) {
	t.Parallel()

	message := Message{
		Scalars: []Scalar{
			S("status", "denied"),
			S("severity", "block"),
			S("rule_id", "coding-ethos.feedback_route"),
			S("summary", "feedback must use the central renderer"),
		},
		Tables: []Table{T(
			"repair",
			[]string{"path"},
			[][]string{{"go/internal/feedback"}},
		)},
	}

	toon, err := Render(message, FormatTOON)
	if err != nil {
		t.Fatalf("render TOON: %v", err)
	}
	for _, want := range []string{
		"status: denied",
		"severity: block",
		"repair[1]{path}:",
		"go/internal/feedback",
	} {
		if !strings.Contains(toon, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, toon)
		}
	}
	if strings.Contains(toon, "format: toon") {
		t.Fatalf("TOON output included redundant format preamble:\n%s", toon)
	}

	jsonText, err := Render(message, FormatJSON)
	if err != nil {
		t.Fatalf("render JSON: %v", err)
	}
	var jsonPayload map[string]any
	if err := json.Unmarshal([]byte(jsonText), &jsonPayload); err != nil {
		t.Fatalf("decode JSON feedback: %v\n%s", err, jsonText)
	}
	if jsonPayload["status"] != "denied" {
		t.Fatalf("JSON feedback = %#v", jsonPayload)
	}

	human, err := Render(message, FormatHuman)
	if err != nil {
		t.Fatalf("render human: %v", err)
	}
	if !strings.Contains(human, "status: denied") ||
		!strings.Contains(human, "repair:") {
		t.Fatalf("human feedback missing expected text:\n%s", human)
	}

	sarifText, err := Render(message, FormatSARIF)
	if err != nil {
		t.Fatalf("render SARIF: %v", err)
	}
	var sarif SARIFLog
	if err := json.Unmarshal([]byte(sarifText), &sarif); err != nil {
		t.Fatalf("decode SARIF feedback: %v\n%s", err, sarifText)
	}
	if sarif.Version != "2.1.0" ||
		len(sarif.Runs) != 1 ||
		len(sarif.Runs[0].Results) != 1 ||
		sarif.Runs[0].Results[0].RuleID != "coding-ethos.feedback_route" {
		t.Fatalf("SARIF feedback = %#v", sarif)
	}
}

func TestMessageExposesStructuredLogFields(t *testing.T) {
	t.Parallel()

	fields := Message{
		Scalars: []Scalar{S("status", "warning")},
		Tables:  []Table{T("guidance", []string{"message"}, [][]string{{"check output"}})},
	}.FeedbackLogFields()

	if fields["status"] != "warning" || fields["guidance_count"] != 1 {
		t.Fatalf("log fields = %#v", fields)
	}
}
