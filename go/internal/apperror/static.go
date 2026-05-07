// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package apperror

import "fmt"

// StaticError is a constant error value for fixed application failure classes.
type StaticError string

// Error returns the static error message.
func (err StaticError) Error() string {
	return string(err)
}

// FormattedError wraps a static failure class with a formatted operator message.
type FormattedError struct {
	Cause   StaticError
	Message string
}

// Error returns the formatted operator message.
func (err FormattedError) Error() string {
	return err.Message
}

// Unwrap returns the static failure class.
func (err FormattedError) Unwrap() error {
	return err.Cause
}

// Wrapf formats an operator message while preserving a static error class.
func Wrapf(cause StaticError, format string, args ...any) error {
	return FormattedError{
		Cause:   cause,
		Message: fmt.Sprintf(format, args...),
	}
}
