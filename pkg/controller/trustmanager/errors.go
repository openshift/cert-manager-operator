package trustmanager

import (
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type errorReason string

const (
	irrecoverableError errorReason = "IrrecoverableError"

	retryRequiredError errorReason = "RetryRequiredError"

	multipleInstanceError errorReason = "MultipleInstanceError"
)

type reconcileError struct {
	Reason  errorReason `json:"reason,omitempty"`
	Message string      `json:"message,omitempty"`
	Err     error       `json:"error,omitempty"`
}

var _ error = &reconcileError{}

func newIrrecoverableError(err error, message string, args ...any) *reconcileError {
	if err == nil {
		return nil
	}
	return &reconcileError{
		Reason:  irrecoverableError,
		Message: fmt.Sprintf(message, args...),
		Err:     err,
	}
}

func newMultipleInstanceError(err error) *reconcileError {
	if err == nil {
		return nil
	}
	return &reconcileError{
		Reason:  multipleInstanceError,
		Message: fmt.Sprint(err.Error()),
		Err:     err,
	}
}

func newRetryRequiredError(err error, message string, args ...any) *reconcileError {
	if err == nil {
		return nil
	}
	return &reconcileError{
		Reason:  retryRequiredError,
		Message: fmt.Sprintf(message, args...),
		Err:     err,
	}
}

func fromClientError(err error, message string, args ...any) *reconcileError {
	if err == nil {
		return nil
	}
	if apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) || apierrors.IsInvalid(err) ||
		apierrors.IsBadRequest(err) || apierrors.IsServiceUnavailable(err) {
		return newIrrecoverableError(err, message, args...)
	}

	return newRetryRequiredError(err, message, args...)
}

func isIrrecoverableError(err error) bool {
	if rerr, ok := err.(*reconcileError); ok || errors.As(err, &rerr) {
		return rerr.Reason == irrecoverableError
	}
	return false
}

func isMultipleInstanceError(err error) bool {
	if rerr, ok := err.(*reconcileError); ok || errors.As(err, &rerr) {
		return rerr.Reason == multipleInstanceError
	}
	return false
}

// Error implements the error interface.
func (e *reconcileError) Error() string {
	return fmt.Sprintf("%s: %s", e.Message, e.Err)
}
