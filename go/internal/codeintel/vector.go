// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

type VectorBackendConfig struct {
	Backend string
	URI     string
}

func NewVectorIndex(
	ctx context.Context,
	config VectorBackendConfig,
) (evidence.VectorIndex, error) {
	backend := strings.TrimSpace(config.Backend)
	if backend == "" {
		backend = "sqlite-vec"
	}

	switch backend {
	case "sqlite", "sqlite-vec":
		return NewSQLiteVectorIndex(ctx, config.URI)
	default:
		return nil, apperror.Wrapf(
			apperror.StaticError("unsupported vector backend %q"),
			"unsupported vector backend %q",
			config.Backend,
		)
	}
}

func DefaultVectorPath(root string) string {
	return filepath.Join(root, ".coding-ethos", "code-intel-vectors.db")
}
