package common

import (
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestReconcileErrors(t *testing.T) {
	tests := []struct {
		name        string
		fn          func() (*ReconcileError, error)
		checkReason ErrorReason
		checkErr    bool
		wantNil     bool
	}{
		{
			name: "NewIrrecoverableError",
			fn: func() (*ReconcileError, error) {
				e := NewIrrecoverableError(fmt.Errorf("x"), "message: %s", "arg")
				return e, nil
			},
			checkReason: IrrecoverableError,
			checkErr:    true,
		},
		{
			name: "NewIrrecoverableError nil input returns nil",
			fn: func() (*ReconcileError, error) {
				return NewIrrecoverableError(nil, "msg"), nil
			},
			wantNil: true,
		},
		{
			name: "NewMultipleInstanceError",
			fn: func() (*ReconcileError, error) {
				return NewMultipleInstanceError(fmt.Errorf("multiple")), nil
			},
			checkReason: MultipleInstanceError,
			checkErr:    true,
		},
		{
			name: "NewMultipleInstanceError nil input returns nil",
			fn: func() (*ReconcileError, error) {
				return NewMultipleInstanceError(nil), nil
			},
			wantNil: true,
		},
		{
			name: "NewRetryRequiredError",
			fn: func() (*ReconcileError, error) {
				return NewRetryRequiredError(fmt.Errorf("x"), "retry: %d", 1), nil
			},
			checkReason: RetryRequiredError,
			checkErr:    true,
		},
		{
			name: "NewRetryRequiredError nil input returns nil",
			fn: func() (*ReconcileError, error) {
				return NewRetryRequiredError(nil, "msg"), nil
			},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := tt.fn()
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil error")
			}
			if tt.checkReason != "" && got.Reason != tt.checkReason {
				t.Errorf("Reason = %v, want %v", got.Reason, tt.checkReason)
			}
			if tt.checkErr && got.Err == nil {
				t.Error("Err should be set")
			}
		})
	}
}

func TestFromClientError(t *testing.T) {
	tests := []struct {
		name          string
		clientErr     error
		wantIrrecover bool
		wantRetry     bool
	}{
		{
			name:          "Unauthorized is irrecoverable",
			clientErr:     apierrors.NewUnauthorized("forbidden"),
			wantIrrecover: true,
		},
		{
			name:          "Forbidden is irrecoverable",
			clientErr:     apierrors.NewForbidden(schema.GroupResource{Resource: "test"}, "resource", fmt.Errorf("forbidden")),
			wantIrrecover: true,
		},
		{
			name:          "Invalid is irrecoverable",
			clientErr:     apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "test-pod", nil),
			wantIrrecover: true,
		},
		{
			name:          "BadRequest is irrecoverable",
			clientErr:     apierrors.NewBadRequest("bad request"),
			wantIrrecover: true,
		},
		{
			name:          "ServiceUnavailable is irrecoverable",
			clientErr:     apierrors.NewServiceUnavailable("service unavailable"),
			wantIrrecover: true,
		},
		{
			name:      "Conflict is retry required",
			clientErr: apierrors.NewConflict(schema.GroupResource{Resource: "test"}, "resource", fmt.Errorf("conflict")),
			wantRetry: true,
		},
		{
			name:      "NotFound is retry required",
			clientErr: apierrors.NewNotFound(schema.GroupResource{Resource: "test"}, "resource"),
			wantRetry: true,
		},
		{
			name:      "AlreadyExists is retry required",
			clientErr: apierrors.NewAlreadyExists(schema.GroupResource{Resource: "test"}, "resource"),
			wantRetry: true,
		},
		{
			name:      "nil error returns nil",
			clientErr: nil,
			wantRetry: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := FromClientError(tt.clientErr, "client err: %s", "context")
			if tt.clientErr == nil {
				if err != nil {
					t.Errorf("expected nil for nil input, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if tt.wantIrrecover && !IsIrrecoverableError(err) {
				t.Error("expected irrecoverable")
			}
			if tt.wantRetry && !IsRetryRequiredError(err) {
				t.Error("expected retry required")
			}
			// Verify message formatting works
			if err.Message != "client err: context" {
				t.Errorf("Message = %q, want %q", err.Message, "client err: context")
			}
		})
	}
}

func TestFromError(t *testing.T) {
	tests := []struct {
		name        string
		inputErr    error
		wantNil     bool
		wantReason  ErrorReason
		wantMessage string
	}{
		{
			name:        "preserves irrecoverable error",
			inputErr:    NewIrrecoverableError(fmt.Errorf("base"), "irrecoverable"),
			wantReason:  IrrecoverableError,
			wantMessage: "wrapped: %s",
		},
		{
			name:        "preserves retry error as retry",
			inputErr:    NewRetryRequiredError(fmt.Errorf("base"), "retry"),
			wantReason:  RetryRequiredError,
			wantMessage: "wrapped: %s",
		},
		{
			name:        "converts plain error to retry",
			inputErr:    fmt.Errorf("plain error"),
			wantReason:  RetryRequiredError,
			wantMessage: "converted",
		},
		{
			name:     "nil error returns nil",
			inputErr: nil,
			wantNil:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := FromError(tt.inputErr, tt.wantMessage, "arg")
			if tt.wantNil {
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if err.Reason != tt.wantReason {
				t.Errorf("Reason = %v, want %v", err.Reason, tt.wantReason)
			}
		})
	}
}

func TestIsIrrecoverableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "true for ReconcileError with IrrecoverableError reason",
			err:  NewIrrecoverableError(fmt.Errorf("x"), "msg"),
			want: true,
		},
		{
			name: "false for plain error",
			err:  fmt.Errorf("plain"),
			want: false,
		},
		{
			name: "false for RetryRequiredError",
			err:  NewRetryRequiredError(fmt.Errorf("x"), "msg"),
			want: false,
		},
		{
			name: "false for nil",
			err:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsIrrecoverableError(tt.err)
			if got != tt.want {
				t.Errorf("IsIrrecoverableError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRetryRequiredError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "true for RetryRequiredError",
			err:  NewRetryRequiredError(fmt.Errorf("x"), "msg"),
			want: true,
		},
		{
			name: "false for IrrecoverableError",
			err:  NewIrrecoverableError(fmt.Errorf("x"), "msg"),
			want: false,
		},
		{
			name: "false for plain error",
			err:  fmt.Errorf("plain"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryRequiredError(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryRequiredError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMultipleInstanceError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "true for MultipleInstanceError",
			err:  NewMultipleInstanceError(fmt.Errorf("multiple")),
			want: true,
		},
		{
			name: "false for IrrecoverableError",
			err:  NewIrrecoverableError(fmt.Errorf("x"), "msg"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMultipleInstanceError(tt.err)
			if got != tt.want {
				t.Errorf("IsMultipleInstanceError() = %v, want %v", got, tt.want)
			}
		})
	}
}
