package trustmanager

import (
	"context"
	"fmt"
	"maps"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

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
			assertMapEntries(t, "label", issuer.Labels, tt.wantLabels)
			assertMapEntries(t, "annotation", issuer.Annotations, tt.wantAnnotations)
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
			assertMapEntries(t, "label", cert.Labels, tt.wantLabels)
			assertMapEntries(t, "annotation", cert.Annotations, tt.wantAnnotations)
			if cert.Spec.SecretTemplate == nil {
				t.Fatal("expected secretTemplate to be set")
			}
			assertMapEntries(t, "secretTemplate label", cert.Spec.SecretTemplate.Labels, tt.wantLabels)
			assertMapEntries(t, "secretTemplate annotation", cert.Spec.SecretTemplate.Annotations, tt.wantAnnotations)
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

	t.Run("sets secret template for direct CA injection", func(t *testing.T) {
		if cert.Spec.SecretTemplate == nil || cert.Spec.SecretTemplate.Annotations["cert-manager.io/allow-direct-injection"] != "true" {
			t.Errorf("expected secretTemplate annotation cert-manager.io/allow-direct-injection=true, got %+v", cert.Spec.SecretTemplate)
		}
	})

	t.Run("allow-direct-injection stays true when controllerConfig overrides it", func(t *testing.T) {
		overridden := testTrustManager().WithAnnotations(map[string]string{
			"cert-manager.io/allow-direct-injection": "false",
			"user-annotation":                        "preserved",
		}).Build()
		got := getCertificateObject(getResourceLabels(overridden), getResourceAnnotations(overridden))
		if got.Spec.SecretTemplate == nil {
			t.Fatal("expected secretTemplate to be set")
		}
		annotations := got.Spec.SecretTemplate.Annotations
		if annotations["cert-manager.io/allow-direct-injection"] != "true" {
			t.Errorf("expected secretTemplate annotation cert-manager.io/allow-direct-injection=true, got %q", annotations["cert-manager.io/allow-direct-injection"])
		}
		if annotations["user-annotation"] != "preserved" {
			t.Errorf("expected user annotation to be preserved, got %q", annotations["user-annotation"])
		}
	})

	t.Run("sets private key rotation policy Always", func(t *testing.T) {
		if cert.Spec.PrivateKey == nil || cert.Spec.PrivateKey.RotationPolicy != certmanagerv1.RotationPolicyAlways {
			t.Errorf("expected privateKey.rotationPolicy Always, got %+v", cert.Spec.PrivateKey)
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
		wantApplyCount  int
	}{
		{
			name: "successful apply when not found",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 1,
			wantApplyCount:  1,
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
			wantApplyCount:  0,
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
			wantApplyCount:  1,
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
			wantApplyCount:  1,
		},
		{
			name: "exists error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if Issuer",
			wantExistsCount: 1,
			wantApplyCount:  0,
		},
		{
			name: "patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.ApplyCalls(func(ctx context.Context, obj client.Object, opts ...client.ApplyOption) error {
					return errTestClient
				})
			},
			wantErr:         "failed to apply Issuer",
			wantExistsCount: 1,
			wantApplyCount:  1,
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
			if got := mock.ApplyCallCount(); got != tt.wantApplyCount {
				t.Errorf("expected %d Apply calls, got %d", tt.wantApplyCount, got)
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
		wantApplyCount  int
	}{
		{
			name: "successful apply when not found",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 1,
			wantApplyCount:  1,
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
			wantApplyCount:  0,
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
			wantApplyCount:  1,
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
			wantApplyCount:  1,
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
			wantApplyCount:  1,
		},
		{
			name: "apply when existing has issuer ref drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.IssuerRef = certmanagermetav1.IssuerReference{
						Name:  "wrong-issuer",
						Kind:  "Issuer",
						Group: "cert-manager.io",
					}
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantApplyCount:  1,
		},
		{
			name: "apply when existing is missing privateKey",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.PrivateKey = nil
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantApplyCount:  1,
		},
		{
			name: "apply when existing has privateKey rotation policy drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.PrivateKey = &certmanagerv1.CertificatePrivateKey{
						RotationPolicy: certmanagerv1.RotationPolicyNever,
					}
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantApplyCount:  1,
		},
		{
			name: "skip apply when existing privateKey has extra defaulted fields",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.PrivateKey = &certmanagerv1.CertificatePrivateKey{
						RotationPolicy: certmanagerv1.RotationPolicyAlways,
						Algorithm:      certmanagerv1.RSAKeyAlgorithm,
						Size:           2048,
					}
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantApplyCount:  0,
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
			wantApplyCount:  1,
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
			wantApplyCount:  1,
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
			wantApplyCount:  1,
		},
		{
			name: "apply when a controller-owned secretTemplate label was removed",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.SecretTemplate.Labels["removed"] = "stale"
					addOwnedSecretTemplateKey(cert, fieldOwner, "labels", "removed")
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantApplyCount:  1,
		},
		{
			name: "skip apply when an extra secretTemplate label is owned by another manager",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.SecretTemplate.Labels["extra"] = "yes"
					addOwnedSecretTemplateKey(cert, "kubectl", "labels", "extra")
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantApplyCount:  0,
		},
		{
			name: "apply when existing secretTemplate drops the direct-injection annotation",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					cert := getCertificateObject(testResourceLabels(), testResourceAnnotations())
					cert.Spec.SecretTemplate.Annotations = map[string]string{}
					cert.DeepCopyInto(obj.(*certmanagerv1.Certificate))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantApplyCount:  1,
		},
		{
			name: "exists error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if Certificate",
			wantExistsCount: 1,
			wantApplyCount:  0,
		},
		{
			name: "patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.ApplyCalls(func(ctx context.Context, obj client.Object, opts ...client.ApplyOption) error {
					return errTestClient
				})
			},
			wantErr:         "failed to apply Certificate",
			wantExistsCount: 1,
			wantApplyCount:  1,
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
			if got := mock.ApplyCallCount(); got != tt.wantApplyCount {
				t.Errorf("expected %d Apply calls, got %d", tt.wantApplyCount, got)
			}
		})
	}
}

func TestSecretTemplateModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  *certmanagerv1.CertificateSecretTemplate
		existing *certmanagerv1.CertificateSecretTemplate
		owned    []ownedSecretTemplateKey
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
			name:     "absent label with empty desired value is modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": ""}},
			existing: &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{}},
			want:     true,
		},
		{
			name:     "present label with empty value is not modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": ""}},
			existing: &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": ""}},
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
		{
			name:     "absent annotation with empty desired value is modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Annotations: map[string]string{"note": ""}},
			existing: &certmanagerv1.CertificateSecretTemplate{},
			want:     true,
		},
		{
			name:     "removed label owned by the controller is modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
			existing: &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm", "stale": "yes"}},
			owned:    []ownedSecretTemplateKey{{field: "labels", key: "stale"}},
			want:     true,
		},
		{
			name:     "removed annotation owned by the controller is modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Annotations: map[string]string{"keep": "yes"}},
			existing: &certmanagerv1.CertificateSecretTemplate{Annotations: map[string]string{"keep": "yes", "stale": "yes"}},
			owned:    []ownedSecretTemplateKey{{field: "annotations", key: "stale"}},
			want:     true,
		},
		{
			name:     "removed label owned by another manager is not modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
			existing: &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm", "extra": "yes"}},
			owned:    []ownedSecretTemplateKey{{manager: "kubectl", field: "labels", key: "extra"}},
		},
		{
			name:     "owned label that is still requested is not modified",
			desired:  &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
			existing: &certmanagerv1.CertificateSecretTemplate{Labels: map[string]string{"app": "tm"}},
			owned:    []ownedSecretTemplateKey{{field: "labels", key: "app"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var existingCert *certmanagerv1.Certificate
			if len(tt.owned) > 0 {
				existingCert = &certmanagerv1.Certificate{}
				for _, owned := range tt.owned {
					manager := owned.manager
					if manager == "" {
						manager = fieldOwner
					}
					addOwnedSecretTemplateKey(existingCert, manager, owned.field, owned.key)
				}
			}
			got := secretTemplateModified(tt.desired, tt.existing, existingCert)
			if got != tt.want {
				t.Errorf("secretTemplateModified() = %v, want %v", got, tt.want)
			}
		})
	}
}

type ownedSecretTemplateKey struct {
	manager string
	field   string
	key     string
}

func addOwnedSecretTemplateKey(cert *certmanagerv1.Certificate, manager, mapField, key string) {
	set := fieldpath.NewSet()
	set.Insert(fieldpath.MakePathOrDie("spec", "secretTemplate", mapField, key))
	raw, err := set.ToJSON()
	if err != nil {
		panic(err)
	}
	cert.SetManagedFields(append(cert.GetManagedFields(), metav1.ManagedFieldsEntry{
		Manager:  manager,
		FieldsV1: &metav1.FieldsV1{Raw: raw},
	}))
}

// assertMapEntries checks that every expected key is present in got with the same value.
// Extra keys in got are ignored.
func assertMapEntries(t *testing.T, field string, got, want map[string]string) {
	t.Helper()
	for key, val := range want {
		gotVal, ok := got[key]
		if !ok || gotVal != val {
			t.Errorf("expected %s %s=%q, got %q (present=%t)", field, key, val, gotVal, ok)
		}
	}
}

// TestSecretTemplateIndependentOfCertificateMetadata checks that spec.secretTemplate
// keeps its own copies of the label and annotation maps. Editing Certificate metadata
// afterward must not change the template applied to the webhook TLS Secret.
func TestSecretTemplateIndependentOfCertificateMetadata(t *testing.T) {
	tm := testTrustManager().
		WithLabels(map[string]string{"user-label": "label-value"}).
		WithAnnotations(map[string]string{"user-annotation": "annotation-value"}).
		Build()
	labels := getResourceLabels(tm)
	annotations := getResourceAnnotations(tm)
	cert := getCertificateObject(labels, annotations)
	if cert.Spec.SecretTemplate == nil {
		t.Fatal("expected secretTemplate to be set")
	}

	wantLabels := maps.Clone(cert.Spec.SecretTemplate.Labels)
	wantAnnotations := maps.Clone(cert.Spec.SecretTemplate.Annotations)

	// SetLabels stores this map on the Certificate, so editing cert.Labels edits labels too.
	cert.Labels["user-label"] = "mutated"
	cert.Labels["extra-label"] = "added"
	if labels["user-label"] != "mutated" {
		t.Fatal("editing certificate labels did not edit the map passed to getCertificateObject")
	}
	// updateResourceAnnotations copies this map onto the Certificate, so edit the
	// source map that certificateSecretTemplate received.
	annotations["user-annotation"] = "mutated"
	annotations["extra-annotation"] = "added"

	if !maps.Equal(cert.Spec.SecretTemplate.Labels, wantLabels) {
		t.Errorf("secretTemplate labels = %v, want %v", cert.Spec.SecretTemplate.Labels, wantLabels)
	}
	if !maps.Equal(cert.Spec.SecretTemplate.Annotations, wantAnnotations) {
		t.Errorf("secretTemplate annotations = %v, want %v", cert.Spec.SecretTemplate.Annotations, wantAnnotations)
	}
}
