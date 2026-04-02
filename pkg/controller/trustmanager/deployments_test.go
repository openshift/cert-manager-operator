package trustmanager

import (
	"context"
	"fmt"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

const testImage = "registry.redhat.io/cert-manager/cert-manager-trust-manager-rhel9:latest"

func TestDeploymentObject(t *testing.T) {
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
			wantName:      trustManagerDeploymentName,
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
			t.Setenv(trustManagerImageNameEnvVarName, testImage)
			r := testReconciler(t)
			tm := tt.tm.Build()
			dep, err := r.getDeploymentObject(tm, getResourceLabels(tm), getResourceAnnotations(tm), "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantName != "" && dep.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, dep.Name)
			}
			if tt.wantNamespace != "" && dep.Namespace != tt.wantNamespace {
				t.Errorf("expected namespace %q, got %q", tt.wantNamespace, dep.Namespace)
			}
			for key, val := range tt.wantLabels {
				if dep.Labels[key] != val {
					t.Errorf("expected label %s=%q, got %q", key, val, dep.Labels[key])
				}
			}
			for key, val := range tt.wantAnnotations {
				if dep.Annotations[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, dep.Annotations[key])
				}
			}
		})
	}
}

func TestDeploymentSpec(t *testing.T) {
	t.Setenv(trustManagerImageNameEnvVarName, testImage)
	r := testReconciler(t)
	tm := testTrustManager().Build()
	dep, err := r.getDeploymentObject(tm, getResourceLabels(tm), getResourceAnnotations(tm), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("sets correct container image", func(t *testing.T) {
		if len(dep.Spec.Template.Spec.Containers) == 0 {
			t.Fatal("expected at least one container")
		}
		if dep.Spec.Template.Spec.Containers[0].Image != testImage {
			t.Errorf("expected image %q, got %q", testImage, dep.Spec.Template.Spec.Containers[0].Image)
		}
	})

	t.Run("configures TLS volume", func(t *testing.T) {
		found := false
		for _, vol := range dep.Spec.Template.Spec.Volumes {
			if vol.Name == "tls" && vol.Secret != nil && vol.Secret.SecretName == trustManagerTLSSecretName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected TLS volume with secret name %q not found", trustManagerTLSSecretName)
		}
	})

	t.Run("sets correct service account name", func(t *testing.T) {
		if dep.Spec.Template.Spec.ServiceAccountName != trustManagerServiceAccountName {
			t.Errorf("expected serviceAccountName %q, got %q", trustManagerServiceAccountName, dep.Spec.Template.Spec.ServiceAccountName)
		}
	})

	t.Run("sets pod template labels", func(t *testing.T) {
		if dep.Spec.Template.Labels["app"] != trustManagerCommonName {
			t.Errorf("expected pod template label app=%q, got %q", trustManagerCommonName, dep.Spec.Template.Labels["app"])
		}
	})
}

func TestDeploymentContainerArgs(t *testing.T) {
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		expectedArgs    []string
		notExpectedArgs []string
	}{
		{
			name: "default values",
			expectedArgs: []string{
				"--log-level=1",
				"--log-format=text",
				"--trust-namespace=cert-manager",
				"--leader-elect=true",
				"--webhook-port=6443",
				"--metrics-port=9402",
			},
			notExpectedArgs: []string{
				"--secret-targets-enabled=true",
				"--filter-expired-certificates=true",
				fmt.Sprintf("--default-package-location=%s", defaultCAPackageLocation),
			},
		},
		{
			name:      "custom values",
			tmBuilder: testTrustManager().
				WithLogLevel(5).
				WithLogFormat("json").
				WithTrustNamespace("custom-ns").
				WithFilterExpiredCertificates(v1alpha1.FilterExpiredCertificatesPolicyEnabled),
			expectedArgs: []string{
				"--log-level=5",
				"--log-format=json",
				"--trust-namespace=custom-ns",
				"--filter-expired-certificates=true",
			},
			notExpectedArgs: []string{
				"--log-level=1",
				"--trust-namespace=cert-manager",
			},
		},
		{
			name:      "falls back to default trust namespace when empty",
			tmBuilder: testTrustManager().WithTrustNamespace(""),
			expectedArgs: []string{
				fmt.Sprintf("--trust-namespace=%s", defaultTrustNamespace),
			},
		},
		{
			name:      "includes secret-targets-enabled when policy is Custom with authorized secrets",
			tmBuilder: testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"my-bundle"}),
			expectedArgs: []string{
				"--secret-targets-enabled=true",
			},
		},
		{
			name:      "excludes secret-targets-enabled when policy is Disabled",
			tmBuilder: testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyDisabled, nil),
			notExpectedArgs: []string{
				"--secret-targets-enabled=true",
			},
		},
		{
			name:      "excludes secret-targets-enabled when policy is Custom but no authorized secrets",
			tmBuilder: testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, nil),
			notExpectedArgs: []string{
				"--secret-targets-enabled=true",
			},
		},
		{
			name:      "includes default-package-location when defaultCAPackage is Enabled",
			tmBuilder: testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			expectedArgs: []string{
				fmt.Sprintf("--default-package-location=%s", defaultCAPackageLocation),
			},
		},
		{
			name: "excludes default-package-location when defaultCAPackage is Disabled",
			notExpectedArgs: []string{
				fmt.Sprintf("--default-package-location=%s", defaultCAPackageLocation),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(trustManagerImageNameEnvVarName, testImage)
			r := testReconciler(t)

			tmBuilder := tt.tmBuilder
			if tmBuilder == nil {
				tmBuilder = testTrustManager()
			}
			tm := tmBuilder.Build()

			dep, err := r.getDeploymentObject(tm, testResourceLabels(), testResourceAnnotations(), "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			args := dep.Spec.Template.Spec.Containers[0].Args
			for _, expected := range tt.expectedArgs {
				if !slices.Contains(args, expected) {
					t.Errorf("expected arg %q not found in %v", expected, args)
				}
			}
			for _, notExpected := range tt.notExpectedArgs {
				if slices.Contains(args, notExpected) {
					t.Errorf("unexpected arg %q found in %v", notExpected, args)
				}
			}
		})
	}
}

func TestDeploymentDefaultCAPackage(t *testing.T) {
	t.Setenv(trustManagerImageNameEnvVarName, testImage)

	t.Run("adds arg, volume, mount, and hash annotation when enabled", func(t *testing.T) {
		r := testReconciler(t)
		tm := testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled).Build()
		dep, err := r.getDeploymentObject(tm, testResourceLabels(), testResourceAnnotations(), "abc123hash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedArg := fmt.Sprintf("--default-package-location=%s", defaultCAPackageLocation)
		if !slices.Contains(dep.Spec.Template.Spec.Containers[0].Args, expectedArg) {
			t.Errorf("expected arg %q not found in %v", expectedArg, dep.Spec.Template.Spec.Containers[0].Args)
		}

		hasVolume := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == defaultCAPackageVolumeName && v.ConfigMap != nil &&
				v.ConfigMap.Name == defaultCAPackageConfigMapName {
				hasVolume = true
				break
			}
		}
		if !hasVolume {
			t.Errorf("expected volume %q with configMap %q, got volumes: %v",
				defaultCAPackageVolumeName, defaultCAPackageConfigMapName, dep.Spec.Template.Spec.Volumes)
		}

		hasMount := false
		for _, vm := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
			if vm.Name == defaultCAPackageVolumeName && vm.MountPath == defaultCAPackageMountPath && vm.ReadOnly {
				hasMount = true
				break
			}
		}
		if !hasMount {
			t.Errorf("expected volume mount %q at %q (readOnly), got mounts: %v",
				defaultCAPackageVolumeName, defaultCAPackageMountPath, dep.Spec.Template.Spec.Containers[0].VolumeMounts)
		}

		got, ok := dep.Spec.Template.Annotations[defaultCAPackageHashAnnotation]
		if !ok {
			t.Fatalf("expected pod template annotation %q to be set", defaultCAPackageHashAnnotation)
		}
		if got != "abc123hash" {
			t.Errorf("expected hash annotation %q, got %q", "abc123hash", got)
		}
	})

	t.Run("no arg, volume, or annotation when disabled", func(t *testing.T) {
		r := testReconciler(t)
		tm := testTrustManager().Build()
		dep, err := r.getDeploymentObject(tm, testResourceLabels(), testResourceAnnotations(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		notExpectedArg := fmt.Sprintf("--default-package-location=%s", defaultCAPackageLocation)
		if slices.Contains(dep.Spec.Template.Spec.Containers[0].Args, notExpectedArg) {
			t.Errorf("unexpected arg %q found in %v", notExpectedArg, dep.Spec.Template.Spec.Containers[0].Args)
		}

		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == defaultCAPackageVolumeName && v.ConfigMap != nil {
				t.Errorf("unexpected ConfigMap-backed volume %q when disabled", defaultCAPackageVolumeName)
			}
		}

		if ann := dep.Spec.Template.Annotations; ann != nil {
			if _, ok := ann[defaultCAPackageHashAnnotation]; ok {
				t.Errorf("unexpected hash annotation when disabled")
			}
		}
	})
}

func TestDeploymentOverrides(t *testing.T) {
	tests := []struct {
		name              string
		configure         func(*v1alpha1.TrustManager)
		wantCPURequest    string
		wantMemoryLimit   string
		wantTolerationKey string
		wantNodeSelector  map[string]string
		wantAffinity      bool
	}{
		{
			name: "applies resource requirements",
			configure: func(tm *v1alpha1.TrustManager) {
				tm.Spec.TrustManagerConfig.Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				}
			},
			wantCPURequest:  "100m",
			wantMemoryLimit: "512Mi",
		},
		{
			name: "applies tolerations",
			configure: func(tm *v1alpha1.TrustManager) {
				tm.Spec.TrustManagerConfig.Tolerations = []corev1.Toleration{
					{
						Key:      "test-key",
						Operator: corev1.TolerationOpEqual,
						Value:    "test-value",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				}
			},
			wantTolerationKey: "test-key",
		},
		{
			name: "applies node selector",
			configure: func(tm *v1alpha1.TrustManager) {
				tm.Spec.TrustManagerConfig.NodeSelector = map[string]string{
					"custom-key": "custom-value",
				}
			},
			wantNodeSelector: map[string]string{"custom-key": "custom-value"},
		},
		{
			name: "applies affinity",
			configure: func(tm *v1alpha1.TrustManager) {
				tm.Spec.TrustManagerConfig.Affinity = &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{},
				}
			},
			wantAffinity: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(trustManagerImageNameEnvVarName, testImage)
			r := testReconciler(t)

			tm := testTrustManager().Build()
			tt.configure(tm)

			dep, err := r.getDeploymentObject(tm, testResourceLabels(), testResourceAnnotations(), "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			container := dep.Spec.Template.Spec.Containers[0]
			if tt.wantCPURequest != "" && container.Resources.Requests.Cpu().String() != tt.wantCPURequest {
				t.Errorf("expected CPU request %s, got %s", tt.wantCPURequest, container.Resources.Requests.Cpu().String())
			}
			if tt.wantMemoryLimit != "" && container.Resources.Limits.Memory().String() != tt.wantMemoryLimit {
				t.Errorf("expected memory limit %s, got %s", tt.wantMemoryLimit, container.Resources.Limits.Memory().String())
			}
			if tt.wantTolerationKey != "" {
				if len(dep.Spec.Template.Spec.Tolerations) == 0 {
					t.Fatal("expected tolerations to be set")
				}
				if dep.Spec.Template.Spec.Tolerations[0].Key != tt.wantTolerationKey {
					t.Errorf("expected toleration key %q, got %q", tt.wantTolerationKey, dep.Spec.Template.Spec.Tolerations[0].Key)
				}
			}
			for key, val := range tt.wantNodeSelector {
				if dep.Spec.Template.Spec.NodeSelector[key] != val {
					t.Errorf("expected nodeSelector %s=%q, got %q", key, val, dep.Spec.Template.Spec.NodeSelector[key])
				}
			}
			if tt.wantAffinity && dep.Spec.Template.Spec.Affinity == nil {
				t.Error("expected affinity to be set")
			}
		})
	}
}

func TestDeploymentReconciliation(t *testing.T) {
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		caBundleHash    string
		setImage        bool
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
	}{
		{
			name:     "successful apply when not found",
			setImage: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:     "skip apply when existing matches desired",
			setImage: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					dep, err := r.getDeploymentObject(testTrustManager().Build(), testResourceLabels(), testResourceAnnotations(), "")
					if err != nil {
						t.Fatalf("unexpected error building desired deployment: %v", err)
					}
					dep.DeepCopyInto(obj.(*appsv1.Deployment))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name:     "apply when existing has label drift",
			setImage: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					dep, err := r.getDeploymentObject(testTrustManager().Build(), testResourceLabels(), testResourceAnnotations(), "")
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					dep.Labels["app.kubernetes.io/instance"] = "modified-value"
					dep.DeepCopyInto(obj.(*appsv1.Deployment))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:      "apply when existing has annotation drift",
			tmBuilder: testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}),
			setImage:  true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					tm := testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}).Build()
					dep, err := r.getDeploymentObject(tm, getResourceLabels(tm), getResourceAnnotations(tm), "")
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					dep.Annotations["user-annotation"] = "tampered"
					dep.DeepCopyInto(obj.(*appsv1.Deployment))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:     "apply when defaultCAPackage changed from Enabled to Disabled",
			setImage: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					tm := testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled).Build()
					dep, err := r.getDeploymentObject(tm, testResourceLabels(), testResourceAnnotations(), "abc123hash")
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					dep.DeepCopyInto(obj.(*appsv1.Deployment))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:         "apply when existing has pod template annotation drift",
			tmBuilder:    testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled),
			caBundleHash: "abc123hash",
			setImage:     true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					tm := testTrustManager().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled).Build()
					dep, err := r.getDeploymentObject(tm, testResourceLabels(), testResourceAnnotations(), "abc123hash")
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					dep.Spec.Template.Annotations[defaultCAPackageHashAnnotation] = "stale-hash"
					dep.DeepCopyInto(obj.(*appsv1.Deployment))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:     "apply when existing has image drift",
			setImage: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					dep, err := r.getDeploymentObject(testTrustManager().Build(), testResourceLabels(), testResourceAnnotations(), "")
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					dep.Spec.Template.Spec.Containers[0].Image = "wrong-image:latest"
					dep.DeepCopyInto(obj.(*appsv1.Deployment))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:     "apply when existing has replicas drift",
			setImage: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					dep, err := r.getDeploymentObject(testTrustManager().Build(), testResourceLabels(), testResourceAnnotations(), "")
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					dep.Spec.Replicas = ptr.To(int32(5))
					dep.DeepCopyInto(obj.(*appsv1.Deployment))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:     "apply when existing has args drift",
			setImage: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					dep, err := r.getDeploymentObject(testTrustManager().Build(), testResourceLabels(), testResourceAnnotations(), "")
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					dep.Spec.Template.Spec.Containers[0].Args = append(
						dep.Spec.Template.Spec.Containers[0].Args, "--extra-arg=true",
					)
					dep.DeepCopyInto(obj.(*appsv1.Deployment))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:     "exists error propagates",
			setImage: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if deployment",
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name:     "patch error propagates",
			setImage: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return errTestClient
				})
			},
			wantErr:         "failed to apply deployment",
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:            "missing image error propagates",
			setImage:        false,
			wantErr:         "environment variable with trust-manager image not set",
			wantExistsCount: 0,
			wantPatchCount:  0,
		},
		{
			name:     "invalid toleration config returns error",
			setImage: true,
			tmBuilder: testTrustManager().WithTolerations([]corev1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpExists,
					Value:    "should-be-empty-for-exists",
				},
			}),
			wantErr:         `spec.trustManagerConfig.tolerations[0].operator: Invalid value: "should-be-empty-for-exists": value must be empty when ` + "`operator` is 'Exists'",
			wantExistsCount: 0,
			wantPatchCount:  0,
		},
		{
			name:     "invalid nodeSelector config returns error",
			setImage: true,
			tmBuilder: testTrustManager().WithNodeSelector(map[string]string{
				"node/Label/2": "value",
			}),
			wantErr:         `spec.trustManagerConfig.nodeSelector: Invalid value: "node/Label/2"`,
			wantExistsCount: 0,
			wantPatchCount:  0,
		},
		{
			name:     "invalid affinity config returns error",
			setImage: true,
			tmBuilder: testTrustManager().WithAffinity(&corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "key",
										Operator: corev1.NodeSelectorOpIn,
									},
								},
							},
						},
					},
				},
			}),
			wantErr:         `spec.trustManagerConfig.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].values: Required value: must be specified when ` + "`operator` is 'In' or 'NotIn'",
			wantExistsCount: 0,
			wantPatchCount:  0,
		},
		{
			name:     "invalid resource requirements config returns error",
			setImage: true,
			tmBuilder: testTrustManager().WithResources(corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1"),
				},
			}),
			wantErr:         `spec.trustManagerConfig.resources.requests: Invalid value: "2": must be less than or equal to cpu limit of 1`,
			wantExistsCount: 0,
			wantPatchCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setImage {
				t.Setenv(trustManagerImageNameEnvVarName, testImage)
			} else {
				t.Setenv(trustManagerImageNameEnvVarName, "")
			}
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
			err := r.createOrApplyDeployment(tm, getResourceLabels(tm), getResourceAnnotations(tm), tt.caBundleHash)
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
