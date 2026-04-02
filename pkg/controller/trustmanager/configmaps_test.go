package trustmanager

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

const testCABundle = "-----BEGIN CERTIFICATE-----\nthisIsATestCABundle\n-----END CERTIFICATE-----"

func TestFormatCAPackage(t *testing.T) {
	pkgJSON, err := formatCAPackage(testCABundle, "12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var pkg caPackage
	if err := json.Unmarshal(pkgJSON, &pkg); err != nil {
		t.Fatalf("failed to unmarshal package JSON: %v", err)
	}
	if pkg.Name != defaultCAPackageName {
		t.Errorf("expected package name %q, got %q", defaultCAPackageName, pkg.Name)
	}
	if pkg.Version != "12345" {
		t.Errorf("expected package version %q, got %q", "12345", pkg.Version)
	}
	if pkg.Bundle != testCABundle {
		t.Errorf("expected bundle to match input, got different content")
	}
}

func TestComputeCABundleHash(t *testing.T) {
	tests := []struct {
		name     string
		bundle1  string
		bundle2  string
		wantSame bool
	}{
		{
			name:     "same bundle produces same hash",
			bundle1:  testCABundle,
			bundle2:  testCABundle,
			wantSame: true,
		},
		{
			name:     "different bundles produce different hashes",
			bundle1:  testCABundle,
			bundle2:  "-----BEGIN CERTIFICATE-----\ndifferent\n-----END CERTIFICATE-----",
			wantSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := computeCABundleHash(tt.bundle1)
			hash2 := computeCABundleHash(tt.bundle2)

			if hash1 == "" {
				t.Fatal("expected non-empty hash")
			}
			if (hash1 == hash2) != tt.wantSame {
				t.Errorf("hash equality=%v, want %v (hash1=%s, hash2=%s)", hash1 == hash2, tt.wantSame, hash1, hash2)
			}
		})
	}
}

func TestBuildDefaultCAPackageConfigMap(t *testing.T) {
	pkgJSON := []byte(`{"name":"test","bundle":"test","version":"1"}`)
	labels := map[string]string{"app": "test"}
	annotations := map[string]string{"note": "test"}

	cm := buildDefaultCAPackageConfigMap(pkgJSON, labels, annotations)

	if cm.Name != defaultCAPackageConfigMapName {
		t.Errorf("expected name %q, got %q", defaultCAPackageConfigMapName, cm.Name)
	}
	if cm.Namespace != operandNamespace {
		t.Errorf("expected namespace %q, got %q", operandNamespace, cm.Namespace)
	}
	if cm.Labels["app"] != "test" {
		t.Errorf("expected label app=test, got %v", cm.Labels)
	}
	if cm.Annotations["note"] != "test" {
		t.Errorf("expected annotation note=test, got %v", cm.Annotations)
	}
	if cm.Data[defaultCAPackageFilename] != string(pkgJSON) {
		t.Errorf("expected data key %q to contain package JSON", defaultCAPackageFilename)
	}
}

func TestConfigMapModified(t *testing.T) {
	base := func() *corev1.ConfigMap {
		return &corev1.ConfigMap{
			Data: map[string]string{defaultCAPackageFilename: `{"name":"test"}`},
		}
	}

	tests := []struct {
		name     string
		desired  *corev1.ConfigMap
		existing *corev1.ConfigMap
		want     bool
	}{
		{
			name:     "identical ConfigMaps are not modified",
			desired:  base(),
			existing: base(),
			want:     false,
		},
		{
			name:    "different data is detected as modified",
			desired: base(),
			existing: func() *corev1.ConfigMap {
				cm := base()
				cm.Data[defaultCAPackageFilename] = `{"name":"changed"}`
				return cm
			}(),
			want: true,
		},
		{
			name:    "extra key in existing is detected as modified",
			desired: base(),
			existing: func() *corev1.ConfigMap {
				cm := base()
				cm.Data["extra"] = "value"
				return cm
			}(),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := configMapModified(tt.desired, tt.existing)
			if got != tt.want {
				t.Errorf("configMapModified() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultCAPackageConfigMapReconciliation(t *testing.T) {
	tests := []struct {
		name            string
		tm              *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantHash        bool
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
	}{
		{
			name: "skips when policy is Disabled",
			tm:   testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyDisabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
			},
			wantExistsCount: 0,
			wantPatchCount:  0,
		},
		{
			name: "skips when policy is unset (defaults to Disabled)",
			tm:   testTrustManager(),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
			},
			wantExistsCount: 0,
			wantPatchCount:  0,
		},
		{
			name: "returns error when injection ConfigMap is not found",
			tm:   testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					return errTestClient
				})
			},
			wantErr: "failed to read CA bundle ConfigMap",
		},
		{
			name: "returns error when CA bundle key is missing",
			tm:   testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					cm := obj.(*corev1.ConfigMap)
					cm.Data = map[string]string{}
					cm.ResourceVersion = "100"
					return nil
				})
			},
			wantErr: "does not contain key",
		},
		{
			name: "returns error when CA bundle is empty",
			tm:   testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					cm := obj.(*corev1.ConfigMap)
					cm.Data = map[string]string{common.TrustedCABundleKey: ""}
					cm.ResourceVersion = "100"
					return nil
				})
			},
			wantErr: "does not contain key",
		},
		{
			name: "creates ConfigMap and returns hash when bundle is available",
			tm:   testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					cm := obj.(*corev1.ConfigMap)
					cm.Data = map[string]string{common.TrustedCABundleKey: testCABundle}
					cm.ResourceVersion = "100"
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantHash:        true,
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "skips patch when existing ConfigMap matches desired",
			tm:   testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					cm := obj.(*corev1.ConfigMap)
					cm.Data = map[string]string{common.TrustedCABundleKey: testCABundle}
					cm.ResourceVersion = "100"
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					pkgJSON, _ := formatCAPackage(testCABundle, "100")
					cm := obj.(*corev1.ConfigMap)
					cm.Labels = testResourceLabels()
					cm.Data = map[string]string{defaultCAPackageFilename: string(pkgJSON)}
					return true, nil
				})
			},
			wantHash:        true,
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "patches when existing ConfigMap data differs",
			tm:   testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					cm := obj.(*corev1.ConfigMap)
					cm.Data = map[string]string{common.TrustedCABundleKey: testCABundle}
					cm.ResourceVersion = "100"
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cm := obj.(*corev1.ConfigMap)
					cm.Data = map[string]string{defaultCAPackageFilename: `{"name":"stale"}`}
					return true, nil
				})
			},
			wantHash:        true,
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "propagates Exists error",
			tm:   testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					cm := obj.(*corev1.ConfigMap)
					cm.Data = map[string]string{common.TrustedCABundleKey: testCABundle}
					cm.ResourceVersion = "100"
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if ConfigMap",
			wantExistsCount: 1,
		},
		{
			name: "propagates Patch error",
			tm:   testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					cm := obj.(*corev1.ConfigMap)
					cm.Data = map[string]string{common.TrustedCABundleKey: testCABundle}
					cm.ResourceVersion = "100"
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return errTestClient
				})
			},
			wantErr:         "failed to apply ConfigMap",
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			r.CtrlClient = mock

			tt.preReq(r, mock)

			tm := tt.tm.Build()
			hash, err := r.createOrApplyDefaultCAPackageConfigMap(tm, testResourceLabels(), testResourceAnnotations())
			assertError(t, err, tt.wantErr)

			if tt.wantHash && hash == "" {
				t.Error("expected non-empty hash, got empty")
			}
			if !tt.wantHash && hash != "" {
				t.Errorf("expected empty hash, got %q", hash)
			}

			if got := mock.ExistsCallCount(); got != tt.wantExistsCount {
				t.Errorf("expected %d Exists calls, got %d", tt.wantExistsCount, got)
			}
			if got := mock.PatchCallCount(); got != tt.wantPatchCount {
				t.Errorf("expected %d Patch calls, got %d", tt.wantPatchCount, got)
			}
		})
	}
}
