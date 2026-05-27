// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

const filteredSearchFactor = 20

func normalizeVectorRecord(record evidence.VectorRecord) evidence.VectorRecord {
	record.ID = strings.TrimSpace(record.ID)
	record.Collection = strings.TrimSpace(record.Collection)
	record.ModelID = strings.TrimSpace(record.ModelID)

	record.InputKind = strings.TrimSpace(record.InputKind)
	if record.Dimension == 0 {
		record.Dimension = len(record.Vector)
	}

	if record.SchemaVersion == 0 {
		record.SchemaVersion = evidence.SchemaVersion
	}

	if record.Metadata == nil {
		record.Metadata = map[string]string{}
	}

	return record
}

func validateVectorRecord(record evidence.VectorRecord) error {
	if record.ID == "" || record.Collection == "" || record.ModelID == "" {
		return apperror.StaticError("vector id, collection, and model id are required")
	}

	if record.Dimension <= 0 {
		return apperror.StaticError("vector dimension must be positive")
	}

	if len(record.Vector) != record.Dimension {
		return apperror.Wrapf(
			apperror.StaticError("vector dimension mismatch for %q"),
			"vector dimension mismatch for %q",
			record.ID,
		)
	}

	return nil
}

func vectorTableName(dimension int) string {
	return fmt.Sprintf("vector_embeddings_vec_%d", dimension)
}

func searchCandidateLimit(limit int, filters map[string]string) int {
	if len(filters) == 0 {
		return limit
	}

	return limit * filteredSearchFactor
}

func metadataMatches(metadata, filters map[string]string) bool {
	for key, value := range filters {
		if strings.TrimSpace(value) == "" {
			continue
		}

		if metadata[key] != value {
			return false
		}
	}

	return true
}
