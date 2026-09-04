package http01proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func newTestReconciler(client *fakes.FakeCtrlClient) *Reconciler {
	return &Reconciler{
		CtrlClient:    client,
		eventRecorder: record.NewFakeRecorder(10),
		log:           logr.Discard(),
	}
}

func TestReconcileWrongNamespace(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	r := newTestReconciler(fakeClient)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "default", Namespace: "wrong-namespace"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result, got %v", result)
	}
	if fakeClient.GetCallCount() != 0 {
		t.Errorf("expected no Get calls for wrong namespace, got %d", fakeClient.GetCallCount())
	}
}

func TestReconcileNotFound(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(errors.NewNotFound(schema.GroupResource{Group: "operator.openshift.io", Resource: "http01proxies"}, http01proxyObjectName))

	r := newTestReconciler(fakeClient)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName, Namespace: common.OperatorNamespace},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestReconcileGetError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(fmt.Errorf("api server unavailable"))

	r := newTestReconciler(fakeClient)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName, Namespace: common.OperatorNamespace},
	})

	if err == nil {
		t.Fatal("expected error for Get failure")
	}
	if !strings.Contains(err.Error(), "failed to fetch") {
		t.Errorf("error = %q, want substring %q", err.Error(), "failed to fetch")
	}
}

func TestReconcileDeletion(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	now := metav1.Now()
	fakeClient.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		proxy := obj.(*v1alpha1.HTTP01Proxy)
		proxy.Name = http01proxyObjectName
		proxy.Namespace = common.OperatorNamespace
		proxy.DeletionTimestamp = &now
		proxy.Finalizers = []string{finalizer}
		return nil
	}
	fakeClient.DeleteReturns(nil)
	fakeClient.UpdateWithRetryReturns(nil)

	r := newTestReconciler(fakeClient)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName, Namespace: common.OperatorNamespace},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result after deletion, got %v", result)
	}
	if fakeClient.DeleteCallCount() == 0 {
		t.Error("expected Delete to be called during cleanup")
	}
}

func TestCleanUpDeletesMachineConfig(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.DeleteReturns(nil)

	r := newTestReconciler(fakeClient)
	proxy := &v1alpha1.HTTP01Proxy{}
	proxy.SetName(http01proxyObjectName)
	proxy.SetNamespace(common.OperatorNamespace)

	err := r.cleanUp(context.Background(), proxy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakeClient.DeleteCallCount() != 1 {
		t.Errorf("expected 1 Delete call (MachineConfig), got %d", fakeClient.DeleteCallCount())
	}
}

func TestCleanUpMachineConfigDeleteFails(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.DeleteReturnsOnCall(0, fmt.Errorf("mc delete failed"))

	r := newTestReconciler(fakeClient)
	proxy := &v1alpha1.HTTP01Proxy{}
	proxy.SetName(http01proxyObjectName)
	proxy.SetNamespace(common.OperatorNamespace)

	err := r.cleanUp(context.Background(), proxy)
	if err == nil {
		t.Fatal("expected error when MachineConfig delete fails")
	}
	if !strings.Contains(err.Error(), "MachineConfig") {
		t.Errorf("error = %q, want substring %q", err.Error(), "MachineConfig")
	}
}

func TestCleanUpMachineConfigAlreadyGone(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.DeleteReturns(errors.NewNotFound(
		schema.GroupResource{Group: "machineconfiguration.openshift.io", Resource: "machineconfigs"},
		machineConfigName,
	))

	r := newTestReconciler(fakeClient)
	proxy := &v1alpha1.HTTP01Proxy{}
	proxy.SetName(http01proxyObjectName)
	proxy.SetNamespace(common.OperatorNamespace)

	err := r.cleanUp(context.Background(), proxy)
	if err != nil {
		t.Fatalf("cleanUp should succeed when MachineConfig is already gone, got: %v", err)
	}
}

func TestReconcileDeletionWithoutFinalizer(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	now := metav1.Now()
	fakeClient.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		proxy := obj.(*v1alpha1.HTTP01Proxy)
		proxy.Name = http01proxyObjectName
		proxy.Namespace = common.OperatorNamespace
		proxy.DeletionTimestamp = &now
		proxy.Finalizers = []string{}
		return nil
	}

	r := newTestReconciler(fakeClient)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName, Namespace: common.OperatorNamespace},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result, got %v", result)
	}
}
