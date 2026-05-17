// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package apperror_test

import (
	"errors"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

func TestStaticAndFormattedErrorsPreserveFailureClass(t *testing.T) {
	t.Parallel()

	cause := apperror.StaticError("config invalid")
	err := apperror.Wrapf(cause, "config %s is invalid", "repo")

	if err.Error() != "config repo is invalid" {
		t.Fatalf("formatted error = %q", err.Error())
	}

	if !errors.Is(err, cause) {
		t.Fatalf("formatted error does not unwrap to static cause: %v", err)
	}

	if cause.Error() != "config invalid" {
		t.Fatalf("static error = %q", cause.Error())
	}
}
