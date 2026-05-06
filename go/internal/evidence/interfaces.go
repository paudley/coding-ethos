// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evidence

import "context"

type CodeFact struct {
	ID            string     `json:"id"`
	RepoID        string     `json:"repo_id,omitempty"`
	NodeKind      string     `json:"node_kind,omitempty"`
	Signature     string     `json:"signature,omitempty"`
	SearchText    string     `json:"search_text,omitempty"`
	SourceSpan    SourceSpan `json:"source_span"`
	SchemaVersion int        `json:"schema_version"`
}

type VectorRecord struct {
	Metadata      map[string]string `json:"metadata,omitempty"`
	ID            string            `json:"id"`
	Collection    string            `json:"collection"`
	ModelID       string            `json:"model_id"`
	InputKind     string            `json:"input_kind,omitempty"`
	Text          string            `json:"text,omitempty"`
	Vector        []float32         `json:"vector,omitempty"`
	Dimension     int               `json:"dimension"`
	SchemaVersion int               `json:"schema_version"`
}

type VectorQuery struct {
	Filters    map[string]string `json:"filters,omitempty"`
	Collection string            `json:"collection"`
	ModelID    string            `json:"model_id"`
	Vector     []float32         `json:"vector"`
	Limit      int               `json:"limit"`
}

type VectorMatch struct {
	Metadata map[string]string `json:"metadata,omitempty"`
	ID       string            `json:"id"`
	Score    float64           `json:"score"`
}

type VectorStats struct {
	Collections map[string]int `json:"collections,omitempty"`
	Backend     string         `json:"backend"`
	Rows        int            `json:"rows"`
}

type FindingStore interface {
	UpsertFinding(context.Context, Finding) error
	FindFinding(context.Context, string) (Finding, bool, error)
}

type CodeFactStore interface {
	UpsertCodeFact(context.Context, CodeFact) error
	FindCodeFact(context.Context, string) (CodeFact, bool, error)
}

type VectorIndex interface {
	UpsertEmbedding(context.Context, VectorRecord) error
	DeleteEmbedding(context.Context, string, string) error
	Search(context.Context, VectorQuery) ([]VectorMatch, error)
	Stats(context.Context) (VectorStats, error)
	Rebuild(context.Context, string) error
}

type TraceIngestor interface {
	IngestHookTrace(context.Context, []byte) error
	IngestLintTrace(context.Context, []byte) error
}
