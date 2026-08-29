// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"encoding/json"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

const (
	SourceV2Contract = "coding-ethos.code-intel/v2"

	SourceStatusExact   = "exact"
	SourceStatusFailed  = "failed"
	SourceStatusMissing = "missing"
	SourceStatusPartial = "partial"
	SourceStatusStale   = "stale"

	sourceV2EntryStatusIndexed = "indexed"

	VectorStatusNotEvaluated = "not_evaluated"
	VectorStatusPartial      = "partial"
	VectorStatusReady        = "ready"

	sourceV2BaseManifestKind  = "coding-ethos.code-intel.base-manifest/v2"
	sourceV2DeltaManifestKind = "coding-ethos.code-intel.delta-manifest/v2"
	sourceV2FragmentKind      = "coding-ethos.code-intel.fragment/v2"
	sourceV2StatusKind        = "coding-ethos.code-intel.status/v2"
	sourceV2SyncKind          = "coding-ethos.code-intel.sync/v2"
)

// RepositoryID identifies one origin authority and repository root-commit set.
type RepositoryID string

// SourceSnapshotID identifies exact source bytes plus extractor and config inputs.
type SourceSnapshotID string

// GenerationID identifies one immutable base-plus-delta code-intel generation.
type GenerationID string

// SourceIdentity records every deterministic input to a source generation.
type SourceIdentity struct {
	RepositoryID        RepositoryID     `json:"repository_id"`
	SourceSnapshotID    SourceSnapshotID `json:"source_snapshot_id"`
	GenerationID        GenerationID     `json:"generation_id"`
	HeadOID             string           `json:"head_oid"`
	WorktreeID          string           `json:"worktree_id"`
	WorktreeFingerprint string           `json:"worktree_fingerprint"`
	ConfigHash          string           `json:"config_hash"`
	ExtractorSetHash    string           `json:"extractor_set_hash"`
}

// LanguageCoverage accounts for every recognized source file in one language.
type LanguageCoverage struct {
	Language    string `json:"language"`
	Eligible    int    `json:"eligible"`
	Indexed     int    `json:"indexed"`
	CacheHits   int    `json:"cache_hits"`
	Unsupported int    `json:"unsupported"`
	Excluded    int    `json:"excluded"`
	Oversized   int    `json:"oversized"`
	Failed      int    `json:"failed"`
	Stale       int    `json:"stale"`
}

// SourceReadiness separates exact source coverage from vector availability.
type SourceReadiness struct {
	Identity SourceIdentity     `json:"identity"`
	Status   string             `json:"status"`
	Coverage []LanguageCoverage `json:"coverage"`
	Reasons  []string           `json:"reasons,omitempty"`
}

// VectorReadiness reports derived embedding coverage independently of source facts.
type VectorReadiness struct {
	Status         string `json:"status"`
	ReadyRecords   int    `json:"ready_records"`
	MissingVectors int    `json:"missing_vectors"`
}

// SourceV2Storage locates immutable shared products and the lane-local generation.
type SourceV2Storage struct {
	SharedRoot      string `json:"shared_root"`
	LaneRoot        string `json:"lane_root"`
	BaseManifestID  string `json:"base_manifest_id,omitempty"`
	DeltaManifestID string `json:"delta_manifest_id,omitempty"`
}

// SourceStatusReceipt is the stable machine-readable status contract.
type SourceStatusReceipt struct {
	SourceReadiness SourceReadiness `json:"source_readiness"`
	Storage         SourceV2Storage `json:"storage"`
	Contract        string          `json:"contract"`
	Kind            string          `json:"kind"`
	GeneratedAtUTC  string          `json:"generated_at_utc"`
	Repair          string          `json:"repair"`
	VectorReadiness VectorReadiness `json:"vector_readiness"`
}

// SourceSyncSummary counts the observable work performed by a v2 sync.
type SourceSyncSummary struct {
	FilesIndexed     int `json:"files_indexed"`
	FilesFailed      int `json:"files_failed"`
	FragmentsReused  int `json:"fragments_reused"`
	FragmentsWritten int `json:"fragments_written"`
}

// SourceSyncReceipt is the stable machine-readable sync contract.
type SourceSyncReceipt struct {
	Legacy          *MaintenanceSummary `json:"legacy_v1,omitempty"`
	SourceReadiness SourceReadiness     `json:"source_readiness"`
	Storage         SourceV2Storage     `json:"storage"`
	Contract        string              `json:"contract"`
	Kind            string              `json:"kind"`
	GeneratedAtUTC  string              `json:"generated_at_utc"`
	Repair          string              `json:"repair"`
	Warnings        []string            `json:"warnings,omitempty"`
	VectorReadiness VectorReadiness     `json:"vector_readiness"`
	Sync            SourceSyncSummary   `json:"sync"`
}

type sourceV2ManifestEntry struct {
	Path                 string `json:"path"`
	Language             string `json:"language"`
	ContentSHA256        string `json:"content_sha256"`
	FragmentID           string `json:"fragment_id,omitempty"`
	Status               string `json:"status"`
	ExtractorFingerprint string `json:"extractor_fingerprint"`
}

type sourceV2BaseManifest struct {
	Contract         string                  `json:"contract"`
	Kind             string                  `json:"kind"`
	ManifestID       string                  `json:"manifest_id"`
	RepositoryID     RepositoryID            `json:"repository_id"`
	HeadOID          string                  `json:"head_oid"`
	ConfigHash       string                  `json:"config_hash"`
	ExtractorSetHash string                  `json:"extractor_set_hash"`
	Entries          []sourceV2ManifestEntry `json:"entries"`
}

type sourceV2DeltaManifest struct {
	Contract         string                  `json:"contract"`
	Kind             string                  `json:"kind"`
	ManifestID       string                  `json:"manifest_id"`
	RepositoryID     RepositoryID            `json:"repository_id"`
	WorktreeID       string                  `json:"worktree_id"`
	HeadOID          string                  `json:"head_oid"`
	BaseManifestID   string                  `json:"base_manifest_id"`
	ConfigHash       string                  `json:"config_hash"`
	ExtractorSetHash string                  `json:"extractor_set_hash"`
	Entries          []sourceV2ManifestEntry `json:"entries"`
	Tombstones       []string                `json:"tombstones"`
}

// SourceSymbolFact is a path-neutral structural fact in an immutable fragment.
type SourceSymbolFact struct {
	Provenance      SourceFactProvenance `json:"provenance"`
	SymbolKind      string               `json:"symbol_kind"`
	SymbolName      string               `json:"symbol_name"`
	SymbolPath      string               `json:"symbol_path"`
	NodeKind        string               `json:"node_kind"`
	Confidence      string               `json:"confidence"`
	BaseNames       []string             `json:"base_names,omitempty"`
	CallNames       []string             `json:"call_names,omitempty"`
	ReferencedNames []string             `json:"referenced_names,omitempty"`
	StartByte       int                  `json:"start_byte"`
	EndByte         int                  `json:"end_byte"`
	StartLine       int                  `json:"start_line"`
	EndLine         int                  `json:"end_line"`
}

// SourceImportFact is a path-neutral import fact in an immutable fragment.
type SourceImportFact struct {
	Target     string               `json:"target"`
	RawText    string               `json:"raw_text"`
	Confidence string               `json:"confidence"`
	Provenance SourceFactProvenance `json:"provenance"`
}

// SourceFactProvenance records the exact deterministic extractor for a fact.
type SourceFactProvenance struct {
	Class          string `json:"class"`
	Parser         string `json:"parser"`
	ParserRevision string `json:"parser_revision"`
	SpanFidelity   string `json:"span_fidelity"`
}

type sourceV2Fragment struct {
	Extractor     astfacts.ExtractorDescriptor `json:"extractor"`
	Contract      string                       `json:"contract"`
	Kind          string                       `json:"kind"`
	FragmentID    string                       `json:"fragment_id"`
	Language      string                       `json:"language"`
	ContentSHA256 string                       `json:"content_sha256"`
	Symbols       []SourceSymbolFact           `json:"symbols,omitempty"`
	Imports       []SourceImportFact           `json:"imports,omitempty"`
	Facts         []ExternalExtractorFact      `json:"facts,omitempty"`
	LineCount     int                          `json:"line_count"`
}

// ExternalExtractorRequest is the exact PurRDF batch-extractor request envelope.
type ExternalExtractorRequest struct {
	Protocol  string                         `json:"protocol"`
	RequestID string                         `json:"request_id"`
	Files     []ExternalExtractorRequestFile `json:"files"`
}

// ExternalExtractorRequestFile carries one UTF-8 source document in a batch.
type ExternalExtractorRequestFile struct {
	Path          string `json:"path"`
	ContentSHA256 string `json:"content_sha256"`
	Content       string `json:"content"`
	BaseIRI       string `json:"base_iri,omitempty"`
}

// ExternalExtractorIdentity identifies the implementation behind a response.
type ExternalExtractorIdentity struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	PurrdfRevision string `json:"purrdf_revision"`
}

// ExternalExtractorPosition is a 1-based line/column and 0-based byte offset.
type ExternalExtractorPosition struct {
	ByteOffset int `json:"byte_offset"`
	Line       int `json:"line"`
	Column     int `json:"column"`
}

// ExternalExtractorProvenance makes parser identity and span fidelity explicit.
type ExternalExtractorProvenance struct {
	Start          *ExternalExtractorPosition `json:"start,omitempty"`
	Class          string                     `json:"class"`
	Parser         string                     `json:"parser"`
	ParserRevision string                     `json:"parser_revision"`
	SpanFidelity   string                     `json:"span_fidelity"`
	SourcePath     string                     `json:"source_path"`
}

// ExternalExtractorFact is one deterministic semantic fact from PurRDF.
type ExternalExtractorFact struct {
	Provenance ExternalExtractorProvenance `json:"provenance"`
	Attributes map[string]string           `json:"attributes,omitempty"`
	ID         string                      `json:"id"`
	Kind       string                      `json:"kind"`
	Subject    json.RawMessage             `json:"subject,omitempty"`
	Predicate  json.RawMessage             `json:"predicate,omitempty"`
	Object     json.RawMessage             `json:"object,omitempty"`
	Graph      json.RawMessage             `json:"graph,omitempty"`
	Value      json.RawMessage             `json:"value,omitempty"`
}

// ExternalExtractorResult is one file result; per-file errors keep batch exit zero.
type ExternalExtractorResult struct {
	Path          string                  `json:"path"`
	ContentSHA256 string                  `json:"content_sha256"`
	Language      string                  `json:"language"`
	Status        string                  `json:"status"`
	DocumentKind  string                  `json:"document_kind"`
	Error         string                  `json:"error,omitempty"`
	Facts         []ExternalExtractorFact `json:"facts"`
}

// ExternalExtractorResponse is the exact PurRDF batch-extractor response envelope.
type ExternalExtractorResponse struct {
	Protocol  string                    `json:"protocol"`
	RequestID string                    `json:"request_id"`
	Extractor ExternalExtractorIdentity `json:"extractor"`
	Results   []ExternalExtractorResult `json:"results"`
}
