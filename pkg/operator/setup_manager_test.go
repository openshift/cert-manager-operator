package operator

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	"github.com/openshift/cert-manager-operator/pkg/controller/trustmanager"
)

func TestBuildCacheObjectList_ConfigMapLabelMerge(t *testing.T) {
	objectList, err := buildCacheObjectList(ControllerConfig{
		EnableIstioCSR:     true,
		EnableTrustManager: true,
	})
	if err != nil {
		t.Fatalf("buildCacheObjectList: %v", err)
	}

	_, byObject, ok := findExistingCacheEntry(objectList, &corev1.ConfigMap{})
	if !ok {
		t.Fatal("expected ConfigMap cache entry")
	}

	reqs, _ := byObject.Label.Requirements()
	if len(reqs) != 1 {
		t.Fatalf("expected one label requirement, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Key() != common.ManagedResourceLabelKey || req.Operator() != selection.In {
		t.Fatalf("unexpected requirement: %v", req)
	}

	want := []string{
		istiocsr.RequestEnqueueLabelValue,
		trustmanager.RequestEnqueueLabelValue,
		common.OperatorTrustedCABundleAppLabelValue,
	}
	got := req.Values().List()
	if len(got) != len(want) {
		t.Fatalf("expected values %v, got %v", want, got)
	}
	for _, v := range want {
		if !containsString(got, v) {
			t.Fatalf("missing label value %q in %v", v, got)
		}
	}
}

func TestAppendConfigMapLabelValues(t *testing.T) {
	objectList := make(map[client.Object]cache.ByObject)
	labelReq, err := labels.NewRequirement(common.ManagedResourceLabelKey, selection.Equals, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	objectList[&corev1.ConfigMap{}] = cache.ByObject{Label: labels.NewSelector().Add(*labelReq)}

	if err := appendConfigMapLabelValues(objectList, "b", "a"); err != nil {
		t.Fatalf("appendConfigMapLabelValues: %v", err)
	}

	_, byObject, ok := findExistingCacheEntry(objectList, &corev1.ConfigMap{})
	if !ok {
		t.Fatal("expected ConfigMap entry")
	}
	reqs, _ := byObject.Label.Requirements()
	if reqs[0].Operator() != selection.In {
		t.Fatalf("expected In operator, got %v", reqs[0].Operator())
	}
	if !containsString(reqs[0].Values().List(), "b") {
		t.Fatalf("expected value b in %v", reqs[0].Values().List())
	}
}
