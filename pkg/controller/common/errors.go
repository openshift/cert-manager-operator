package common

import (
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ErrorReason represents the reason for a reconciliation error.
type ErrorReason string

const (
	// IrrecoverableError indicates an error that cannot be recovered by retrying.
	IrrecoverableError ErrorReason = "IrrecoverableError"

	// RetryRequiredError indicates an error that may be recovered by retrying.
	RetryRequiredError ErrorReason = "RetryRequiredError"

	// MultipleInstanceError indicates that multiple singleton instances exist.
	MultipleInstanceError ErrorReason = "MultipleInstanceError"
)

// ReconcileError represents an error that occurred during reconciliation.
type ReconcileError struct {
	Reason  ErrorReason `json:"reason,omitempty"`
	Message string      `json:"message,omitempty"`
	Err     error       `json:"error,omitempty"`
}

var _ error = &ReconcileError{}

// NewIrrecoverableError creates a new irrecoverable error.
func NewIrrecoverableError(err error, message string, args ...any) *ReconcileError {
	if err == nil {
		return nil
	}
	return &ReconcileError{
		Reason:  IrrecoverableError,
		Message: fmt.Sprintf(message, args...),
		Err:     err,
	}
}

// NewMultipleInstanceError creates a new multiple instance error.
func NewMultipleInstanceError(err error) *ReconcileError {
	if err == nil {
		return nil
	}
	return &ReconcileError{
		Reason:  MultipleInstanceError,
		Message: fmt.Sprint(err.Error()),
		Err:     err,
	}
}

// NewRetryRequiredError creates a new error that requires retry.
func NewRetryRequiredError(err error, message string, args ...any) *ReconcileError {
	if err == nil {
		return nil
	}
	return &ReconcileError{
		Reason:  RetryRequiredError,
		Message: fmt.Sprintf(message, args...),
		Err:     err,
	}
}

// FromClientError creates a ReconcileError from a Kubernetes client error.
func FromClientError(err error, message string, args ...any) *ReconcileError {
	if err == nil {
		return nil
	}
	if apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) || apierrors.IsInvalid(err) ||
		apierrors.IsBadRequest(err) || apierrors.IsServiceUnavailable(err) {
		return NewIrrecoverableError(err, message, args...)
	}

	return NewRetryRequiredError(err, message, args...)
}

// FromError creates a ReconcileError from a generic error.
func FromError(err error, message string, args ...any) *ReconcileError {
	if err == nil {
		return nil
	}
	if IsIrrecoverableError(err) {
		return NewIrrecoverableError(err, message, args...)
	}
	return NewRetryRequiredError(err, message, args...)
}

// IsIrrecoverableError checks if the error is an irrecoverable error.
func IsIrrecoverableError(err error) bool {
	rerr := &ReconcileError{}
	if errors.As(err, &rerr) {
		return rerr.Reason == IrrecoverableError
	}
	return false
}

// IsRetryRequiredError checks if the error requires retry.
func IsRetryRequiredError(err error) bool {
	rerr := &ReconcileError{}
	if errors.As(err, &rerr) {
		return rerr.Reason == RetryRequiredError
	}
	return false
}

// IsMultipleInstanceError checks if the error is a multiple instance error.
func IsMultipleInstanceError(err error) bool {
	rerr := &ReconcileError{}
	if errors.As(err, &rerr) {
		return rerr.Reason == MultipleInstanceError
	}
	return false
}

// Error implements the error interface.
func (e *ReconcileError) Error() string {
	return fmt.Sprintf("%s: %s", e.Message, e.Err)
}
