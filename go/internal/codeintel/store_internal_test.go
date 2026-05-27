// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestOpenWithStoreLockWaitRetriesLockError(t *testing.T) {
	t.Parallel()

	attempts := 0
	store, err := openWithStoreLockWait(
		context.Background(),
		"code-intel.duckdb",
		time.Second,
		time.Nanosecond,
		func(context.Context, string) (*Store, error) {
			attempts++
			if attempts == 1 {
				return nil, fmt.Errorf(
					"open code intelligence store: database/sql/driver: " +
						"could not connect to database: IO Error: Could not set lock",
				)
			}

			return &Store{}, nil
		},
	)
	if err != nil {
		t.Fatalf("open with store lock wait: %v", err)
	}
	if store == nil {
		t.Fatal("open with store lock wait returned nil store")
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestOpenWithStoreLockWaitDoesNotRetryNonLockError(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, err := openWithStoreLockWait(
		context.Background(),
		"code-intel.duckdb",
		time.Second,
		time.Nanosecond,
		func(context.Context, string) (*Store, error) {
			attempts++

			return nil, fmt.Errorf("open code intelligence store: corrupt catalog")
		},
	)
	if err == nil {
		t.Fatal("expected open error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
