package istiocsr

import (
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type ErrorReason string

const (
	IrrecoverableError ErrorReason = "IrrecoverableError"

	RetryRequiredError ErrorReason = "RetryRequiredError"

	MultipleInstanceError ErrorReason = "MultipleInstanceError"
)

type ReconcileError struct {
	Reason  ErrorReason `json:"reason,omitempty"`
	Message string      `json:"message,omitempty"`
	Err     error       `json:"error,omitempty"`
}

var _ error = &ReconcileError{}

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

func FromError(err error, message string, args ...any) *ReconcileError {
	if err == nil {
		return nil
	}
	if IsIrrecoverableError(err) {
		return NewIrrecoverableError(err, message, args...)
	}
	return NewRetryRequiredError(err, message, args...)
}

func IsIrrecoverableError(err error) bool {
	rerr := &ReconcileError{}
	if errors.As(err, &rerr) {
		return rerr.Reason == IrrecoverableError
	}
	return false
}

func IsRetryRequiredError(err error) bool {
	rerr := &ReconcileError{}
	if errors.As(err, &rerr) {
		return rerr.Reason == RetryRequiredError
	}
	return false
}

func IsMultipleInstanceError(err error) bool {
	rerr := &ReconcileError{}
	if errors.As(err, &rerr) {
		return rerr.Reason == MultipleInstanceError
	}
	return false
}

// ReconcileError implements the ReconcileError interface.
func (e *ReconcileError) Error() string {
	return fmt.Sprintf("%s: %s", e.Message, e.Err)
}
