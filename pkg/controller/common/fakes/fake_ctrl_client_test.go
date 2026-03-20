package fakes

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestFakeCtrlClient_Exists documents behavior of Exists with client.ObjectKey (refactor safety).
func TestFakeCtrlClient_Exists(t *testing.T) {
	tests := []struct {
		name        string
		setupStub   func(*FakeCtrlClient)
		key         client.ObjectKey
		obj         client.Object
		expectedOk  bool
		expectError bool
		errorMsg    string
	}{
		{
			name: "happy path - stub returns true",
			setupStub: func(f *FakeCtrlClient) {
				f.ExistsReturns(true, nil)
			},
			key:         client.ObjectKey{Namespace: "ns", Name: "cm"},
			obj:         &corev1.ConfigMap{},
			expectedOk:  true,
			expectError: false,
		},
		{
			name: "happy path - stub returns false (not found)",
			setupStub: func(f *FakeCtrlClient) {
				f.ExistsReturns(false, nil)
			},
			key:         client.ObjectKey{Namespace: "ns", Name: "missing"},
			obj:         &corev1.ConfigMap{},
			expectedOk:  false,
			expectError: false,
		},
		{
			name: "error case - stub returns error",
			setupStub: func(f *FakeCtrlClient) {
				f.ExistsReturns(false, errors.New("api server error"))
			},
			key:         client.ObjectKey{Namespace: "ns", Name: "x"},
			obj:         &corev1.ConfigMap{},
			expectError: true,
			errorMsg:    "api server error",
		},
		{
			name:        "boundary - empty ObjectKey",
			setupStub:   func(f *FakeCtrlClient) { f.ExistsReturns(false, nil) },
			key:         client.ObjectKey{},
			obj:         &corev1.ConfigMap{},
			expectedOk:  false,
			expectError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &FakeCtrlClient{}
			if tt.setupStub != nil {
				tt.setupStub(fake)
			}
			ok, err := fake.Exists(context.Background(), tt.key, tt.obj)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedOk, ok)
		})
	}
}

// TestFakeCtrlClient_ExistsArgsForCall verifies ExistsCalls / ExistsArgsForCall use client.ObjectKey.
func TestFakeCtrlClient_ExistsArgsForCall(t *testing.T) {
	fake := &FakeCtrlClient{}
	fake.ExistsReturns(true, nil)
	key := client.ObjectKey{Namespace: "cert-manager", Name: "trusted-ca"}
	obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
	_, _ = fake.Exists(context.Background(), key, obj)
	require.Equal(t, 1, fake.ExistsCallCount())
	ctx, gotKey, gotObj := fake.ExistsArgsForCall(0)
	require.NotNil(t, ctx)
	assert.Equal(t, key, gotKey, "ExistsArgsForCall must return client.ObjectKey")
	assert.Same(t, obj, gotObj)
}

// TestFakeCtrlClient_Get documents behavior of Get with client.ObjectKey.
func TestFakeCtrlClient_Get(t *testing.T) {
	tests := []struct {
		name        string
		setupStub   func(*FakeCtrlClient)
		key         client.ObjectKey
		obj         client.Object
		expectError bool
		errorMsg    string
	}{
		{
			name: "happy path - stub returns nil",
			setupStub: func(f *FakeCtrlClient) {
				f.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
					cm := &corev1.ConfigMap{}
					cm.SetName("x")
					cm.SetNamespace("ns")
					cm.DeepCopyInto(obj.(*corev1.ConfigMap))
					return nil
				}
			},
			key:         client.ObjectKey{Namespace: "ns", Name: "x"},
			obj:         &corev1.ConfigMap{},
			expectError: false,
		},
		{
			name: "error case - stub returns error",
			setupStub: func(f *FakeCtrlClient) {
				f.GetReturns(errors.New("not found"))
			},
			key:         client.ObjectKey{Namespace: "ns", Name: "missing"},
			obj:         &corev1.ConfigMap{},
			expectError: true,
			errorMsg:    "not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &FakeCtrlClient{}
			if tt.setupStub != nil {
				tt.setupStub(fake)
			}
			err := fake.Get(context.Background(), tt.key, tt.obj)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestFakeCtrlClient_GetArgsForCall verifies GetCalls / GetArgsForCall use client.ObjectKey.
func TestFakeCtrlClient_GetArgsForCall(t *testing.T) {
	fake := &FakeCtrlClient{}
	fake.GetReturns(nil)
	key := client.ObjectKey{Namespace: "default", Name: "secret"}
	obj := &corev1.Secret{}
	_ = fake.Get(context.Background(), key, obj)
	require.Equal(t, 1, fake.GetCallCount())
	ctx, gotKey, gotObj := fake.GetArgsForCall(0)
	require.NotNil(t, ctx)
	assert.Equal(t, key, gotKey, "GetArgsForCall must return client.ObjectKey")
	assert.Same(t, obj, gotObj)
}
