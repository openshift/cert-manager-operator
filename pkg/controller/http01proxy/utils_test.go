package http01proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func testProxy() *v1alpha1.HTTP01Proxy {
	p := &v1alpha1.HTTP01Proxy{}
	p.SetName(http01proxyObjectName)
	p.SetNamespace("cert-manager-operator")
	return p
}

func TestUpdateCondition(t *testing.T) {
	tests := []struct {
		name       string
		prependErr error
		statusFail bool
		wantErr    bool
		wantMsg    string
	}{
		{
			name:    "status succeeds with nil prependErr",
			wantErr: false,
		},
		{
			name:       "status succeeds with prependErr",
			prependErr: fmt.Errorf("original error"),
			wantErr:    true,
			wantMsg:    "original error",
		},
		{
			name:       "status fails with nil prependErr",
			statusFail: true,
			wantErr:    true,
			wantMsg:    "failed to update",
		},
		{
			name:       "status fails with prependErr preserves both errors",
			prependErr: fmt.Errorf("original error"),
			statusFail: true,
			wantErr:    true,
			wantMsg:    "original error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeCtrlClient{}
			if tt.statusFail {
				fakeClient.GetReturns(fmt.Errorf("simulated get error"))
			} else {
				fakeClient.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.HTTP01Proxy:
						testProxy().DeepCopyInto(o)
					}
					return nil
				})
				fakeClient.StatusUpdateReturns(nil)
			}

			r := &Reconciler{
				CtrlClient: fakeClient,
				log:        logr.Discard(),
			}

			proxy := testProxy()
			err := r.updateCondition(context.Background(), proxy, tt.prependErr)

			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantMsg)
			}
			if tt.name == "status fails with prependErr preserves both errors" {
				if !strings.Contains(err.Error(), "failed to update") {
					t.Errorf("error %q should also contain status update failure", err.Error())
				}
			}
		})
	}
}

func TestAddFinalizer(t *testing.T) {
	tests := []struct {
		name      string
		finalizer bool
		updateErr error
		getErr    error
		wantErr   bool
		wantMsg   string
	}{
		{
			name:      "already has finalizer",
			finalizer: true,
			wantErr:   false,
		},
		{
			name:    "adds finalizer successfully",
			wantErr: false,
		},
		{
			name:      "update fails",
			updateErr: fmt.Errorf("update failed"),
			wantErr:   true,
			wantMsg:   "failed to add finalizers",
		},
		{
			name:   "re-fetch fails after update",
			getErr: fmt.Errorf("get failed"),
			wantErr: true,
			wantMsg: "failed to fetch http01proxy",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeCtrlClient{}

			if tt.updateErr != nil {
				fakeClient.UpdateWithRetryReturns(tt.updateErr)
			} else {
				fakeClient.UpdateWithRetryReturns(nil)
			}

			if tt.getErr != nil {
				fakeClient.GetReturns(tt.getErr)
			} else {
				fakeClient.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.HTTP01Proxy:
						testProxy().DeepCopyInto(o)
					}
					return nil
				})
			}

			r := &Reconciler{
				CtrlClient: fakeClient,
				log:        logr.Discard(),
			}

			proxy := testProxy()
			if tt.finalizer {
				proxy.Finalizers = []string{finalizer}
			}

			err := r.addFinalizer(context.Background(), proxy)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestRemoveFinalizer(t *testing.T) {
	tests := []struct {
		name      string
		finalizer bool
		updateErr error
		wantErr   bool
		wantMsg   string
	}{
		{
			name:    "no finalizer is no-op",
			wantErr: false,
		},
		{
			name:      "removes finalizer successfully",
			finalizer: true,
			wantErr:   false,
		},
		{
			name:      "update fails",
			finalizer: true,
			updateErr: fmt.Errorf("update failed"),
			wantErr:   true,
			wantMsg:   "failed to remove finalizers",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeCtrlClient{}
			if tt.updateErr != nil {
				fakeClient.UpdateWithRetryReturns(tt.updateErr)
			} else {
				fakeClient.UpdateWithRetryReturns(nil)
			}

			r := &Reconciler{
				CtrlClient: fakeClient,
				log:        logr.Discard(),
			}

			proxy := testProxy()
			if tt.finalizer {
				proxy.Finalizers = []string{finalizer}
			}

			err := r.removeFinalizer(context.Background(), proxy)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestUpdateStatus(t *testing.T) {
	tests := []struct {
		name      string
		getErr    error
		statusErr error
		wantErr   bool
		wantMsg   string
	}{
		{
			name:    "success",
			wantErr: false,
		},
		{
			name:    "get fails",
			getErr:  fmt.Errorf("get failed"),
			wantErr: true,
			wantMsg: "failed to fetch http01proxy",
		},
		{
			name:      "status update fails",
			statusErr: fmt.Errorf("status update failed"),
			wantErr:   true,
			wantMsg:   "failed to update http01proxy",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeCtrlClient{}
			if tt.getErr != nil {
				fakeClient.GetReturns(tt.getErr)
			} else {
				fakeClient.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.HTTP01Proxy:
						testProxy().DeepCopyInto(o)
					}
					return nil
				})
			}
			if tt.statusErr != nil {
				fakeClient.StatusUpdateReturns(tt.statusErr)
			} else {
				fakeClient.StatusUpdateReturns(nil)
			}

			r := &Reconciler{
				CtrlClient: fakeClient,
				log:        logr.Discard(),
			}

			proxy := testProxy()
			err := r.updateStatus(context.Background(), proxy)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}
