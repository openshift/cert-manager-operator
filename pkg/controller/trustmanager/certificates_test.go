package trustmanager

import (
	"context"
	"fmt"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestIssuerObject(t *testing.T) {
	tests := []struct {
		name            string
		tm              *trustManagerBuilder
		wantName        string
		wantNamespace   string
		wantLabels      map[string]string
		wantAnnotations map[string]string
	}{
		{
			name:          "sets correct name and namespace",
			tm:            testTrustManager(),
			wantName:      trustManagerIssuerName,
			wantNamespace: operandNamespace,
			wantLabels:    map[string]string{"app": trustManagerCommonName},
		},
		{
			name: "default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tt.tm.Build()
			issuer := getIssuerObject(getResourceLabels(tm), getResourceAnnotations(tm))

			if tt.wantName != "" && issuer.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, issuer.Name)
			}
			if tt.wantNamespace != "" && issuer.Namespace != tt.wantNamespace {
				t.Errorf("expected namespace %q, got %q", tt.wantNamespace, issuer.Namespace)
			}
			for key, val := range tt.wantLabels {
				if issuer.Labels[key] != val {
					t.Errorf("expected label %s=%q, got %q", key, val, issuer.Labels[key])
				}
			}
			for key, val := range tt.wantAnnotations {
				if issuer.Annotations[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, issuer.Annotations[key])
				}
			}
		})
	}
}

func TestCertificateObject(t *testing.T) {
	tests := []struct {
		name            string
		tm              *trustManagerBuilder
		wantName        string
		wantNamespace   string
		wantLabels      map[string]string
		wantAnnotations map[string]string
	}{
		{
			name:          "sets correct name and namespace",
			tm:            testTrustManager(),
			wantName:      trustManagerCertificateName,
			wantNamespace: operandNamespace,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tt.tm.Build()
			cert := getCertificateObject(getResourceLabels(tm), getResourceAnnotations(tm))

			if tt.wantName != "" && cert.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, cert.Name)
			}
			if tt.wantNamespace != "" && cert.Namespace != tt.wantNamespace {
				t.Errorf("expected namespace %q, got %q", tt.wantNamespace, cert.Namespace)
			}
			for key, val := range tt.wantLabels {
				if cert.Labels[key] != val {
					t.Errorf("expected label %s=%q, got %q", key, val, cert.Labels[key])
				}
			}
			for key, val := range tt.wantAnnotations {
				if cert.Annotations[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, cert.Annotations[key])
				}
			}
			if cert.Spec.SecretTemplate == nil {
				t.Fatal("expected secretTemplate to be set")
			}
			for key, val := range tt.wantLabels {
				if cert.Spec.SecretTemplate.Labels[key] != val {
					t.Errorf("expected secretTemplate label %s=%q, got %q", key, val, cert.Spec.SecretTemplate.Labels[key])
				}
			}
			for key, val := range tt.wantAnnotations {
				if cert.Spec.SecretTemplate.Annotations[key] != val {
					t.Errorf("expected secretTemplate annotation %s=%q, got %q", key, val, cert.Spec.SecretTemplate.Annotations[key])
				}
			}
		})
	}
}

func TestCertificateSpec(t *testing.T) {
	tm := testTrustManager().Build()
	cert := getCertificateObject(getResourceLabels(tm), getResourceAnnotations(tm))
	expectedDNSName := fmt.Sprintf("%s.%s.svc", trustManagerServiceName, operandNamespace)

	t.Run("sets correct common name", func(t *testing.T) {
		if cert.Spec.CommonName != expectedDNSName {
			t.Errorf("expected commonName %q, got %q", expectedDNSName, cert.Spec.CommonName)
		}
	})

	t.Run("sets correct DNS names", func(t *testing.T) {
		if len(cert.Spec.DNSNames) == 0 || cert.Spec.DNSNames[0] != expectedDNSName {
			t.Errorf("expected dnsNames to contain %q, got %v", expectedDNSName, cert.Spec.DNSNames)
		}
	})

	t.Run("sets correct secret name", func(t *testing.T) {
		if cert.Spec.SecretName != trustManagerTLSSecretName {
			t.Errorf("expected secretName %q, got %q", trustManagerTLSSecretName, cert.Spec.SecretName)
		}
	})

	t.Run("sets correct issuer reference", func(t *testing.T) {
		if cert.Spec.IssuerRef.Name != trustManagerIssuerName {
			t.Errorf("expected issuerRef.name %q, got %q", trustManagerIssuerName, cert.Spec.IssuerRef.Name)
		}
		if cert.Spec.IssuerRef.Kind != "Issuer" {
			t.Errorf("expected issuerRef.kind %q, got %q", "Issuer", cert.Spec.IssuerRef.Kind)
		}
		if cert.Spec.IssuerRef.Group != "cert-manager.io" {
			t.Errorf("expected issuerRef.group %q, got %q", "cert-manager.io", cert.Spec.IssuerRef.Group)
		}
	})

	t.Run("sets secretTemplate with operator-managed labels", func(t *testing.T) {
		if cert.Spec.SecretTemplate == nil {
			t.Fatal("expected secretTemplate to be set")
		}
		if cert.Spec.SecretTemplate.Labels["app"] != trustManagerCommonName {
			t.Errorf("expected secretTemplate label app=%q, got %q", trustManagerCommonName, cert.Spec.SecretTemplate.Labels["app"])
		}
		if cert.Spec.SecretTemplate.Labels["app.kubernetes.io/managed-by"] != "cert-manager-operator" {
			t.Errorf("expected secretTemplate managed-by label, got %q", cert.Spec.SecretTemplate.Labels["app.kubernetes.io/managed-by"])
		}
	})
}

func TestIssuerReconciliation(t *testing.T) {
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
	}{
		{
			name: "successful apply when not found",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "skip apply when existing matches desired",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					issuer := getIssuerObject(testResourceLabels(), testResourceAnnotations())
					issuer.DeepCopyInto(obj.(*certmanagerv1.Issuer))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "apply when existing has label drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					issuer := getIssuerObject(testResourceLabels(), testResourceAnnotations())
					issuer.Labels["app.kubernetes.io/instance"] = "modified-value"
					issuer.DeepCopyInto(obj.(*certmanagerv1.Issuer))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:      "apply when existing has annotation drift",
			tmBuilder: testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					tm := testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}).Build()
					issuer := getIssuerObject(getResourceLabels(tm), getResourceAnnotations(tm))
					issuer.Annotations["user-annotation"] = "tampered"
					issuer.DeepCopyInto(obj.(*certmanagerv1.Issuer))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "exists error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if issuer",
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return errTestClient
				})
			},
			wantErr:         "failed to apply issuer",
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.CtrlClient = mock

			tmBuilder := tt.tmBuilder
			if tmBuilder == nil {
				tmBuilder = testTrustManager()
			}
			tm := tmBuilder.Build()
			err := r.createOrApplyIssuer(tm, getResourceLabels(tm), getResourceAnnotations(tm))
			assertError(t, err, tt.wantErr)

			if got := mock.ExistsCallCount(); got != tt.wantExistsCount {
				t.Errorf("expected %d Exists calls, got %d", tt.wantExistsCount, got)
			}
			if got := mock.PatchCallCount(); got != tt.wantPatchCount {
				t.Errorf("expected %d Patch calls, got %d", tt.wantPatchCount, got)
			}
		})
	}
}

func TestCertificateReconciliation(t *testing.T) {
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
	}{
		{
			name: "successful apply when not found",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "skip apply when existing matches desired",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "apply when existing has label drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Labels["app.kubernetes.io/instance"] = "modified-value"
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:      "apply when existing has annotation drift",
			tmBuilder: testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					tm := testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}).Build()
					cert := getCertificateObject(getResourceLabels(tm), getResourceAnnotations(tm))
					cert.Annotations["user-annotation"] = "tampered"
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "apply when existing has secret name drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.SecretName = "wrong-secret"
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "apply when existing has issuer ref drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.IssuerRef = certmanagermetav1.ObjectReference{
						Name:  "wrong-issuer",
						Kind:  "Issuer",
						Group: "cert-manager.io",
					}
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "apply when existing has secretTemplate label drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.SecretTemplate.Labels["app"] = "tampered"
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "apply when existing has no secretTemplate",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.SecretTemplate = nil
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:      "apply when existing has secretTemplate annotation drift",
			tmBuilder: testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					tm := testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}).Build()
					cert := getCertificateObject(getResourceLabels(tm), getResourceAnnotations(tm))
					cert.Spec.SecretTemplate.Annotations["user-annotation"] = "tampered"
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "skip apply when existing secretTemplate annotations are empty vs nil",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.SecretTemplate.Annotations = map[string]string{}
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "exists error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if certificate",
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return errTestClient
				})
			},
			wantErr:         "failed to apply certificate",
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.CtrlClient = mock

			tmBuilder := tt.tmBuilder
			if tmBuilder == nil {
				tmBuilder = testTrustManager()
			}
			tm := tmBuilder.Build()
			err := r.createOrApplyCertificate(tm, getResourceLabels(tm), getResourceAnnotations(tm))
			assertError(t, err, tt.wantErr)

			if got := mock.ExistsCallCount(); got != tt.wantExistsCount {
				t.Errorf("expected %d Exists calls, got %d", tt.wantExistsCount, got)
			}
			if got := mock.PatchCallCount(); got != tt.wantPatchCount {
				t.Errorf("expected %d Patch calls, got %d", tt.wantPatchCount, got)
			}
		})
	}
}

func TestSecretTemplateModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  *certmanagerv1.CertificateSecretTemplate
		existing *certmanagerv1.CertificateSecretTemplate
		want     bool
	}{
		{
			name: "both nil is not modified",
		},
		{
			name:     "desired set and existing nil is modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
			existing: nil,
			want:     true,
		},
		{
			name:     "matching labels is not modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
			existing: &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
		},
		{
			name:     "label value drift is modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
			existing: &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "other"}},
			want:     true,
		},
		{
			name:     "extra existing template labels are not modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
			existing: &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm", "extra": "yes"}},
		},
		{
			name:     "nil and empty annotations are not modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
			existing: &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}, Annotations: map[string]string{}},
		},
		{
			name:     "annotation drift is modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Annotations: map[string]string{"k": "v"}},
			existing: &certmanagerv1.CertificateSecretTemplate{Annotations: map[string]string{"k": "other"}},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secretTemplateModified(tt.desired, tt.existing)
			if got != tt.want {
				t.Errorf("secretTemplateModified() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCertificateSecretTemplateClonesMaps(t *testing.T) {
	labels := map[string]string{"app": "original"}
	annotations := map[string]string{"note": "original"}
	template := certificateSecretTemplate(labels, annotations)

	labels["app"] = "mutated"
	annotations["note"] = "mutated"

	if template.Labels["app"] != "original" {
		t.Errorf("expected cloned label to stay %q, got %q", "original", template.Labels["app"])
	}
	if template.Annotations["note"] != "original" {
		t.Errorf("expected cloned annotation to stay %q, got %q", "original", template.Annotations["note"])
	}
}
