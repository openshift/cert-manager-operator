package trustmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

func TestApplyTrustManagerWebhookTLSArgs(t *testing.T) {
	tests := []struct {
		name               string
		spec               *configv1.TLSProfileSpec
		wantKeys           []string
		wantAbsent         []string
		wantMinVer         string
		wantCipherContains string
	}{
		{
			name: "intermediate sets min version and ciphers",
			spec: &configv1.TLSProfileSpec{
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
				MinTLSVersion: configv1.VersionTLS12,
			},
			wantKeys:           []string{"--tls-min-version", "--tls-cipher-suites"},
			wantMinVer:         "VersionTLS12",
			wantCipherContains: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		},
		{
			name: "old sets min version and ciphers",
			spec: &configv1.TLSProfileSpec{
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256", "AES128-SHA"},
				MinTLSVersion: configv1.VersionTLS10,
			},
			wantKeys:           []string{"--tls-min-version", "--tls-cipher-suites"},
			wantMinVer:         "VersionTLS10",
			wantCipherContains: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		},
		{
			name: "modern tls13 omits cipher suites",
			spec: &configv1.TLSProfileSpec{
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
				MinTLSVersion: configv1.VersionTLS13,
			},
			wantKeys:   []string{"--tls-min-version"},
			wantAbsent: []string{"--tls-cipher-suites"},
			wantMinVer: "VersionTLS13",
		},
		{
			name: "custom tls12 sets min version and mapped ciphers",
			spec: &configv1.TLSProfileSpec{
				Ciphers: []string{
					"ECDHE-RSA-AES128-GCM-SHA256",
					"TLS_AES_128_GCM_SHA256",
				},
				MinTLSVersion: configv1.VersionTLS12,
			},
			wantKeys:           []string{"--tls-min-version", "--tls-cipher-suites"},
			wantMinVer:         "VersionTLS12",
			wantCipherContains: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		},
		{
			name: "custom tls13 omits cipher suites",
			spec: &configv1.TLSProfileSpec{
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
				MinTLSVersion: configv1.VersionTLS13,
			},
			wantKeys:   []string{"--tls-min-version"},
			wantAbsent: []string{"--tls-cipher-suites"},
			wantMinVer: "VersionTLS13",
		},
		{
			name: "nil spec is no-op",
			spec: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: trustManagerDeploymentName, Namespace: operandNamespace},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: trustManagerContainerName,
								Args: []string{"--webhook-port=6443", "--tls-cipher-suites=STALE"},
							}},
						},
					},
				},
			}
			if err := applyTrustManagerWebhookTLSArgs(dep, tt.spec); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			argMap := map[string]string{}
			for _, a := range dep.Spec.Template.Spec.Containers[0].Args {
				parts := strings.SplitN(a, "=", 2)
				if len(parts) == 2 {
					argMap[parts[0]] = parts[1]
				} else {
					argMap[parts[0]] = ""
				}
			}
			for _, key := range tt.wantKeys {
				if _, ok := argMap[key]; !ok {
					t.Fatalf("expected arg key %q, got %#v", key, argMap)
				}
			}
			for _, key := range tt.wantAbsent {
				if _, ok := argMap[key]; ok {
					t.Fatalf("did not expect arg key %q, got %#v", key, argMap)
				}
			}
			if tt.wantMinVer != "" && argMap["--tls-min-version"] != tt.wantMinVer {
				t.Fatalf("got min version %q want %q", argMap["--tls-min-version"], tt.wantMinVer)
			}
			if tt.wantCipherContains != "" && !strings.Contains(argMap["--tls-cipher-suites"], tt.wantCipherContains) {
				t.Fatalf("expected cipher %q in %q", tt.wantCipherContains, argMap["--tls-cipher-suites"])
			}
		})
	}
}

func TestApplyClusterTLSProfile_adherence(t *testing.T) {
	tests := []struct {
		name               string
		apiServer          *configv1.APIServer
		wantTLSArgs        bool
		wantMinVer         string
		wantCipherKey      bool
		wantCipherContains string
	}{
		{
			name: "strict modern injects tls13 min version without ciphers",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: tlsprofile.APIServerClusterName},
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileModernType,
					},
				},
			},
			wantTLSArgs:   true,
			wantMinVer:    "VersionTLS13",
			wantCipherKey: false,
		},
		{
			name: "strict old injects min version and ciphers",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: tlsprofile.APIServerClusterName},
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileOldType,
					},
				},
			},
			wantTLSArgs:   true,
			wantMinVer:    "VersionTLS10",
			wantCipherKey: true,
		},
		{
			name: "strict custom tls12 injects mapped ciphers",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: tlsprofile.APIServerClusterName},
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileCustomType,
						Custom: &configv1.CustomTLSProfile{
							TLSProfileSpec: configv1.TLSProfileSpec{
								Ciphers: []string{
									"ECDHE-RSA-AES128-GCM-SHA256",
									"TLS_AES_128_GCM_SHA256",
								},
								MinTLSVersion: configv1.VersionTLS12,
							},
						},
					},
				},
			},
			wantTLSArgs:        true,
			wantMinVer:         "VersionTLS12",
			wantCipherKey:      true,
			wantCipherContains: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		},
		{
			name: "strict custom tls13 omits cipher suites",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: tlsprofile.APIServerClusterName},
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileCustomType,
						Custom: &configv1.CustomTLSProfile{
							TLSProfileSpec: configv1.TLSProfileSpec{
								Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
								MinTLSVersion: configv1.VersionTLS13,
							},
						},
					},
				},
			},
			wantTLSArgs:   true,
			wantMinVer:    "VersionTLS13",
			wantCipherKey: false,
		},
		{
			name: "unknown adherence treated as strict",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: tlsprofile.APIServerClusterName},
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicy("FutureStrictMode"),
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileModernType,
					},
				},
			},
			wantTLSArgs:   true,
			wantMinVer:    "VersionTLS13",
			wantCipherKey: false,
		},
		{
			name: "empty adherence skips injection",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: tlsprofile.APIServerClusterName},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileModernType,
					},
				},
			},
			wantTLSArgs: false,
		},
		{
			name: "legacy adherence skips injection",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: tlsprofile.APIServerClusterName},
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileModernType,
					},
				},
			},
			wantTLSArgs: false,
		},
		{
			name:        "missing apiserver skips injection",
			apiServer:   nil,
			wantTLSArgs: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(trustManagerImageNameEnvVarName, testImage)
			r := testReconciler(t)
			r.CtrlClient = fakeCtrlClientWithAPIServer(tt.apiServer)

			tm := testTrustManager().Build()
			dep, err := r.getDeploymentObject(tm, getResourceLabels(tm), getResourceAnnotations(tm), "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			argMap := containerArgMap(dep)
			_, hasMin := argMap["--tls-min-version"]
			_, hasCipher := argMap["--tls-cipher-suites"]
			if tt.wantTLSArgs != hasMin {
				t.Fatalf("wantTLSArgs=%v hasMin=%v args=%#v", tt.wantTLSArgs, hasMin, argMap)
			}
			if tt.wantTLSArgs && argMap["--tls-min-version"] != tt.wantMinVer {
				t.Fatalf("min version got %q want %q", argMap["--tls-min-version"], tt.wantMinVer)
			}
			if hasCipher != tt.wantCipherKey {
				t.Fatalf("wantCipherKey=%v hasCipher=%v", tt.wantCipherKey, hasCipher)
			}
			if tt.wantCipherContains != "" && !strings.Contains(argMap["--tls-cipher-suites"], tt.wantCipherContains) {
				t.Fatalf("expected cipher %q in %q", tt.wantCipherContains, argMap["--tls-cipher-suites"])
			}
		})
	}
}

func TestApplyClusterTLSProfile_invalidCustomPropagates(t *testing.T) {
	t.Setenv(trustManagerImageNameEnvVarName, testImage)
	apiServer := &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{Name: tlsprofile.APIServerClusterName},
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
			},
		},
	}
	r := testReconciler(t)
	r.CtrlClient = fakeCtrlClientWithAPIServer(apiServer)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: trustManagerDeploymentName, Namespace: operandNamespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: trustManagerContainerName}},
				},
			},
		},
	}
	if err := r.applyClusterTLSProfile(dep); err == nil {
		t.Fatal("expected error for custom profile missing custom settings")
	}
}

func TestApplyClusterTLSProfile_forbiddenPropagates(t *testing.T) {
	t.Setenv(trustManagerImageNameEnvVarName, testImage)
	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	mock.GetCalls(func(_ context.Context, key client.ObjectKey, _ client.Object) error {
		return apierrors.NewForbidden(schema.GroupResource{Group: configv1.GroupName, Resource: "apiservers"}, key.Name, errors.New("denied"))
	})
	r.CtrlClient = mock

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: trustManagerDeploymentName, Namespace: operandNamespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: trustManagerContainerName}},
				},
			},
		},
	}
	if err := r.applyClusterTLSProfile(dep); err == nil {
		t.Fatal("expected Forbidden to propagate")
	}
}

func TestApplyClusterTLSProfile_nilClientIsNoop(t *testing.T) {
	r := testReconciler(t)
	r.CtrlClient = nil
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: trustManagerDeploymentName, Namespace: operandNamespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: trustManagerContainerName,
						Args: []string{"--webhook-port=6443"},
					}},
				},
			},
		},
	}
	if err := r.applyClusterTLSProfile(dep); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dep.Spec.Template.Spec.Containers[0].Args) != 1 {
		t.Fatalf("expected args unchanged, got %#v", dep.Spec.Template.Spec.Containers[0].Args)
	}
}

func TestApplyTrustManagerWebhookTLSArgs_missingContainer(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: trustManagerDeploymentName, Namespace: operandNamespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "not-trust-manager"}},
				},
			},
		},
	}
	err := applyTrustManagerWebhookTLSArgs(dep, &configv1.TLSProfileSpec{
		MinTLSVersion: configv1.VersionTLS12,
		Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
	})
	if err == nil {
		t.Fatal("expected error for missing container")
	}
}

func TestApplyClusterTLSProfile_intermediateCiphers(t *testing.T) {
	t.Setenv(trustManagerImageNameEnvVarName, testImage)
	apiServer := &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{Name: tlsprofile.APIServerClusterName},
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
		},
	}
	r := testReconciler(t)
	r.CtrlClient = fakeCtrlClientWithAPIServer(apiServer)

	tm := testTrustManager().Build()
	dep, err := r.getDeploymentObject(tm, getResourceLabels(tm), getResourceAnnotations(tm), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected, err := tlsprofile.EffectiveSpec(apiServer.Spec.TLSSecurityProfile)
	if err != nil {
		t.Fatal(err)
	}
	want := tlsprofile.TrustManagerWebhookTLSArgs(expected)
	gotArgs := dep.Spec.Template.Spec.Containers[0].Args
	for _, w := range want {
		found := false
		for _, g := range gotArgs {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected arg %q in %#v", w, gotArgs)
		}
	}
}

func fakeCtrlClientWithAPIServer(apiServer *configv1.APIServer) *fakes.FakeCtrlClient {
	mock := &fakes.FakeCtrlClient{}
	mock.GetCalls(func(_ context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Name != tlsprofile.APIServerClusterName {
			return apierrors.NewNotFound(schema.GroupResource{Group: configv1.GroupName, Resource: "apiservers"}, key.Name)
		}
		if apiServer == nil {
			return apierrors.NewNotFound(schema.GroupResource{Group: configv1.GroupName, Resource: "apiservers"}, key.Name)
		}
		dst, ok := obj.(*configv1.APIServer)
		if !ok {
			return apierrors.NewBadRequest("unexpected object type")
		}
		apiServer.DeepCopyInto(dst)
		return nil
	})
	return mock
}

func containerArgMap(dep *appsv1.Deployment) map[string]string {
	argMap := map[string]string{}
	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name != trustManagerContainerName {
			continue
		}
		for _, a := range c.Args {
			parts := strings.SplitN(a, "=", 2)
			if len(parts) == 2 {
				argMap[parts[0]] = parts[1]
			} else {
				argMap[parts[0]] = ""
			}
		}
	}
	return argMap
}
