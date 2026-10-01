package http01proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestReconcileHTTP01ProxyDeploymentPlatformDiscoveryError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(fmt.Errorf("infrastructure unavailable"))

	r := &Reconciler{
		CtrlClient:    fakeClient,
		eventRecorder: record.NewFakeRecorder(10),
		log:           logr.Discard(),
	}

	proxy := testProxy()
	err := r.reconcileHTTP01ProxyDeployment(context.Background(), proxy)
	if err == nil {
		t.Fatal("expected error when platform discovery fails")
	}
	if !strings.Contains(err.Error(), "failed to discover platform") {
		t.Errorf("error = %q, want substring %q", err.Error(), "failed to discover platform")
	}
}

func TestReconcileHTTP01ProxyDeploymentCleanupAfterValidationFails(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetCalls(func(_ context.Context, nn types.NamespacedName, obj client.Object) error {
		if infra, ok := obj.(*configv1.Infrastructure); ok {
			infra.SetName("cluster")
			infra.Status.PlatformStatus = &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
			}
			return nil
		}
		return errors.NewNotFound(schema.GroupResource{}, nn.Name)
	})
	fakeClient.DeleteReturns(fmt.Errorf("delete failed"))

	r := &Reconciler{
		CtrlClient:    fakeClient,
		eventRecorder: record.NewFakeRecorder(10),
		log:           logr.Discard(),
	}

	proxy := testProxy()
	err := r.reconcileHTTP01ProxyDeployment(context.Background(), proxy)
	if err == nil {
		t.Fatal("expected error when cleanup fails after platform validation")
	}
	if !strings.Contains(err.Error(), "failed to clean up resources after platform validation failure") {
		t.Errorf("error = %q, want cleanup failure message", err.Error())
	}
}

func TestReconcileHTTP01ProxyDeploymentUnsupportedPlatform(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetCalls(func(_ context.Context, nn types.NamespacedName, obj client.Object) error {
		if infra, ok := obj.(*configv1.Infrastructure); ok {
			infra.SetName("cluster")
			infra.Status.PlatformStatus = &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
			}
			return nil
		}
		return errors.NewNotFound(schema.GroupResource{}, nn.Name)
	})
	fakeClient.DeleteReturns(nil)

	r := &Reconciler{
		CtrlClient:    fakeClient,
		eventRecorder: record.NewFakeRecorder(10),
		log:           logr.Discard(),
	}

	proxy := testProxy()
	err := r.reconcileHTTP01ProxyDeployment(context.Background(), proxy)
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error = %q, want substring %q", err.Error(), "not supported")
	}
}

func TestReconcileHTTP01ProxyDeploymentHappyPath(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetCalls(func(_ context.Context, nn types.NamespacedName, obj client.Object) error {
		switch o := obj.(type) {
		case *configv1.Infrastructure:
			o.SetName("cluster")
			o.Status.PlatformStatus = &configv1.PlatformStatus{
				Type: configv1.BareMetalPlatformType,
				BareMetal: &configv1.BareMetalPlatformStatus{
					APIServerInternalIPs: []string{"192.168.1.1"},
					IngressIPs:           []string{"192.168.1.2"},
				},
			}
			return nil
		case *mcfgv1.MachineConfig:
			return errors.NewNotFound(schema.GroupResource{Group: "machineconfiguration.openshift.io", Resource: "machineconfigs"}, machineConfigName)
		default:
			return nil
		}
	})
	fakeClient.CreateReturns(nil)
	fakeClient.UpdateWithRetryReturns(nil)

	r := &Reconciler{
		CtrlClient:    fakeClient,
		eventRecorder: record.NewFakeRecorder(10),
		log:           logr.Discard(),
	}

	proxy := testProxy()
	err := r.reconcileHTTP01ProxyDeployment(context.Background(), proxy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReconcileHTTP01ProxyDeploymentMachineConfigError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetCalls(func(_ context.Context, nn types.NamespacedName, obj client.Object) error {
		switch o := obj.(type) {
		case *configv1.Infrastructure:
			o.SetName("cluster")
			o.Status.PlatformStatus = &configv1.PlatformStatus{
				Type: configv1.BareMetalPlatformType,
				BareMetal: &configv1.BareMetalPlatformStatus{
					APIServerInternalIPs: []string{"192.168.1.1"},
					IngressIPs:           []string{"192.168.1.2"},
				},
			}
			return nil
		case *mcfgv1.MachineConfig:
			return fmt.Errorf("machineconfig get failed")
		default:
			return nil
		}
	})

	r := &Reconciler{
		CtrlClient:    fakeClient,
		eventRecorder: record.NewFakeRecorder(10),
		log:           logr.Discard(),
	}

	proxy := testProxy()
	err := r.reconcileHTTP01ProxyDeployment(context.Background(), proxy)
	if err == nil {
		t.Fatal("expected error when MachineConfig operations fail")
	}
}

func TestReconcileHTTP01ProxyDeploymentAnnotationUpdateError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetCalls(func(_ context.Context, nn types.NamespacedName, obj client.Object) error {
		switch o := obj.(type) {
		case *configv1.Infrastructure:
			o.SetName("cluster")
			o.Status.PlatformStatus = &configv1.PlatformStatus{
				Type: configv1.BareMetalPlatformType,
				BareMetal: &configv1.BareMetalPlatformStatus{
					APIServerInternalIPs: []string{"192.168.1.1"},
					IngressIPs:           []string{"192.168.1.2"},
				},
			}
			return nil
		case *mcfgv1.MachineConfig:
			return errors.NewNotFound(schema.GroupResource{Group: "machineconfiguration.openshift.io", Resource: "machineconfigs"}, machineConfigName)
		default:
			return nil
		}
	})
	fakeClient.CreateReturns(nil)
	fakeClient.UpdateWithRetryReturns(fmt.Errorf("update annotation failed"))

	r := &Reconciler{
		CtrlClient:    fakeClient,
		eventRecorder: record.NewFakeRecorder(10),
		log:           logr.Discard(),
	}

	proxy := testProxy()
	err := r.reconcileHTTP01ProxyDeployment(context.Background(), proxy)
	if err == nil {
		t.Fatal("expected error when annotation update fails")
	}
	if !strings.Contains(err.Error(), "failed to update processed annotation") {
		t.Errorf("error = %q, want substring %q", err.Error(), "failed to update processed annotation")
	}
}
