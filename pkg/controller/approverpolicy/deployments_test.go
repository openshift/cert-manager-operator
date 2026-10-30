package approverpolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func TestDeploymentConfiguration(t *testing.T) {
	t.Setenv(imageEnvVar, "registry.example/approver@sha256:test")

	for _, tc := range []struct {
		name   string
		mutate func(*v1alpha1.ApproverPolicyManager)
		check  func(*testing.T, *appsv1.Deployment)
	}{
		{"asset defaults are used when config is unset", func(*v1alpha1.ApproverPolicyManager) {}, func(t *testing.T, d *appsv1.Deployment) {
			container := d.Spec.Template.Spec.Containers[0]
			assert.Contains(t, container.Args, "--log-level=1")
			assert.Contains(t, container.Args, "--log-format=text")
			assert.Contains(t, container.Args, "--webhook-port=10250", "asset args are preserved")
			assert.Empty(t, container.Resources.Requests)
			assert.Equal(t, map[string]string{"kubernetes.io/os": "linux"}, d.Spec.Template.Spec.NodeSelector)
			assert.Empty(t, d.Spec.Template.Spec.Tolerations)
			assert.Nil(t, d.Spec.Template.Spec.Affinity)
			assert.True(t, *d.Spec.Template.Spec.SecurityContext.RunAsNonRoot)
			assert.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
			assert.Equal(t, d.Labels, d.Spec.Template.Labels, "pod template labels mirror the Deployment's")
			assert.Empty(t, d.Spec.Template.Annotations, "asset sets no pod template annotations")
		}},
		{"configured log level and format override the default", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.LogLevel, o.Spec.ApproverPolicyConfig.LogFormat = 5, "json"
		}, func(t *testing.T, d *appsv1.Deployment) {
			container := d.Spec.Template.Spec.Containers[0]
			assert.Contains(t, container.Args, "--log-level=5")
			assert.Contains(t, container.Args, "--log-format=json")
		}},
		{"configured resources override the empty default", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}
		}, func(t *testing.T, d *appsv1.Deployment) {
			assert.Equal(t, resource.MustParse("100m"), d.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU])
		}},
		{"configured nodeSelector is additive to the asset baseline", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.NodeSelector = map[string]string{"node-role.kubernetes.io/worker": ""}
		}, func(t *testing.T, d *appsv1.Deployment) {
			assert.Equal(t, "linux", d.Spec.Template.Spec.NodeSelector["kubernetes.io/os"])
			assert.Contains(t, d.Spec.Template.Spec.NodeSelector, "node-role.kubernetes.io/worker")
		}},
		{"configured nodeSelector wins over the asset baseline on key conflicts", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.NodeSelector = map[string]string{"kubernetes.io/os": "windows"}
		}, func(t *testing.T, d *appsv1.Deployment) {
			assert.Equal(t, "windows", d.Spec.Template.Spec.NodeSelector["kubernetes.io/os"])
		}},
		{"configured tolerations override the empty default", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.Tolerations = []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}}
		}, func(t *testing.T, d *appsv1.Deployment) {
			assert.Equal(t, []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}}, d.Spec.Template.Spec.Tolerations)
		}},
		{"configured affinity overrides the nil default", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.Affinity = &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}}
		}, func(t *testing.T, d *appsv1.Deployment) {
			assert.Equal(t, &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}}, d.Spec.Template.Spec.Affinity)
		}},
		{"configured controller annotations become the pod template annotations", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ControllerConfig.Annotations = map[string]string{"example.test/note": "keep"}
		}, func(t *testing.T, d *appsv1.Deployment) {
			assert.Equal(t, map[string]string{"example.test/note": "keep"}, d.Spec.Template.Annotations)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instance := testInstance()
			tc.mutate(instance)
			obj, err := getDeploymentObject(instance, "")
			require.NoError(t, err)
			require.Equal(t, "registry.example/approver@sha256:test", obj.Spec.Template.Spec.Containers[0].Image)
			tc.check(t, obj)
		})
	}
}

func TestDeploymentModified(t *testing.T) {
	t.Setenv(imageEnvVar, "example.test/approver:test")
	instance := testInstance()
	instance.Spec.ControllerConfig.Annotations = map[string]string{"example.test/note": "keep"}
	desired, err := getDeploymentObject(instance, "")
	require.NoError(t, err)
	require.Len(t, desired.Spec.Template.Spec.Containers[0].Ports, 3, "test assumes a fixed port count in the asset")

	for _, tc := range []struct {
		name         string
		mutate       func(*appsv1.Deployment)
		wantModified bool
	}{
		{"identical object", func(*appsv1.Deployment) {}, false},
		{"API-defaulted pod/container fields are not drift", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyAlways
			d.Spec.Template.Spec.SchedulerName = "default-scheduler"
			d.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(30))
			container := &d.Spec.Template.Spec.Containers[0]
			container.ReadinessProbe.TimeoutSeconds = 1
			container.ReadinessProbe.SuccessThreshold = 1
			container.ReadinessProbe.FailureThreshold = 3
			container.ReadinessProbe.HTTPGet.Scheme = corev1.URISchemeHTTP
			for i := range container.Ports {
				container.Ports[i].Protocol = corev1.ProtocolTCP
			}
		}, false},
		{"managed label tampered", func(d *appsv1.Deployment) { d.Labels["app"] = "tampered" }, true},
		{"managed label missing", func(d *appsv1.Deployment) { delete(d.Labels, common.ManagedResourceLabelKey) }, true},
		{"replicas changed", func(d *appsv1.Deployment) { d.Spec.Replicas = ptr.To(int32(2)) }, true},
		{"selector changed", func(d *appsv1.Deployment) { d.Spec.Selector.MatchLabels["extra"] = "value" }, true},
		{"pod template label tampered", func(d *appsv1.Deployment) { d.Spec.Template.Labels["app"] = "tampered" }, true},
		{"pod template managed label missing", func(d *appsv1.Deployment) { delete(d.Spec.Template.Labels, common.ManagedResourceLabelKey) }, true},
		{"pod template annotation tampered", func(d *appsv1.Deployment) { d.Spec.Template.Annotations["example.test/note"] = "tampered" }, true},
		{"pod template annotation missing", func(d *appsv1.Deployment) { delete(d.Spec.Template.Annotations, "example.test/note") }, true},
		{"serviceAccountName changed", func(d *appsv1.Deployment) { d.Spec.Template.Spec.ServiceAccountName = "other-sa" }, true},
		{"hostNetwork changed", func(d *appsv1.Deployment) { d.Spec.Template.Spec.HostNetwork = true }, true},
		{"dnsPolicy changed", func(d *appsv1.Deployment) { d.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet }, true},
		{"nodeSelector tampered", func(d *appsv1.Deployment) { d.Spec.Template.Spec.NodeSelector["kubernetes.io/os"] = "windows" }, true},
		{"nodeSelector managed key missing", func(d *appsv1.Deployment) { delete(d.Spec.Template.Spec.NodeSelector, "kubernetes.io/os") }, true},
		{"affinity changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Affinity = &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}}
		}, true},
		{"tolerations changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Tolerations = []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}}
		}, true},
		{"pod securityContext changed", func(d *appsv1.Deployment) { d.Spec.Template.Spec.SecurityContext.RunAsNonRoot = ptr.To(false) }, true},
		{"container added", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{Name: "extra"})
		}, true},
		{"container name changed", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Name = "other" }, true},
		{"container image changed", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Image = "other:tag" }, true},
		{"container imagePullPolicy changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullAlways
		}, true},
		{"container args changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Args = append(d.Spec.Template.Spec.Containers[0].Args, "--extra")
		}, true},
		{"container resources changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}
		}, true},
		{"container securityContext changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem = ptr.To(false)
		}, true},
		{"container port removed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Ports = d.Spec.Template.Spec.Containers[0].Ports[:2]
		}, true},
		{"container port name changed", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Ports[0].Name = "other" }, true},
		{"container port number changed", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort++ }, true},
		{"container port protocol explicitly non-TCP", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Ports[0].Protocol = corev1.ProtocolUDP
		}, true},
		{"readinessProbe removed", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].ReadinessProbe = nil }, true},
		{"readinessProbe initialDelaySeconds changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds++
		}, true},
		{"readinessProbe periodSeconds changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ReadinessProbe.PeriodSeconds++
		}, true},
		{"readinessProbe httpGet removed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet = nil
		}, true},
		{"readinessProbe httpGet path changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Path = "/other"
		}, true},
		{"readinessProbe httpGet port changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Port = intstr.FromInt(9999)
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.wantModified, deploymentModified(desired, existing))
		})
	}
}

func TestDeploymentReady(t *testing.T) {
	t.Setenv(imageEnvVar, "example.test/approver:test")
	desired, err := getDeploymentObject(testInstance(), "")
	require.NoError(t, err)
	existing := desired.DeepCopy()
	existing.Generation = 2
	existing.Status = availableDeploymentStatus(2)

	for _, tc := range []struct {
		name   string
		mutate func(*appsv1.Deployment)
		ready  bool
	}{
		{"available", func(*appsv1.Deployment) {}, true},
		{"stale", func(o *appsv1.Deployment) { o.Status.ObservedGeneration-- }, false},
		{"old replicas", func(o *appsv1.Deployment) { o.Status.UpdatedReplicas = 0 }, false},
		{"extra replicas", func(o *appsv1.Deployment) { o.Status.Replicas = 2 }, false},
		{"not ready", func(o *appsv1.Deployment) { o.Status.ReadyReplicas = 0 }, false},
		{"not available", func(o *appsv1.Deployment) { o.Status.AvailableReplicas = 0 }, false},
		{"unavailable replicas present", func(o *appsv1.Deployment) { o.Status.UnavailableReplicas = 1 }, false},
		{"no condition", func(o *appsv1.Deployment) { o.Status.Conditions = nil }, false},
		{"terminating", func(o *appsv1.Deployment) { n := metav1.Now(); o.DeletionTimestamp = &n }, false},
		{"zero replicas requested", func(o *appsv1.Deployment) {
			o.Spec.Replicas = ptr.To(int32(0))
			o.Status = appsv1.DeploymentStatus{ObservedGeneration: 2}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := existing.DeepCopy()
			tc.mutate(obj)
			assert.Equal(t, tc.ready, deploymentReady(obj))
		})
	}
}

func TestServiceAccountUIDAnnotation(t *testing.T) {
	t.Setenv(imageEnvVar, "example.test/approver:test")
	instance := testInstance()
	instance.Spec.ControllerConfig.Annotations = map[string]string{"example.test/note": "keep"}

	for _, tc := range []struct {
		name string
		uid  string
	}{
		{"empty UID does not stamp the annotation", ""},
		{"non-empty UID stamps the annotation", "sa-uid-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj, err := getDeploymentObject(instance, types.UID(tc.uid))
			require.NoError(t, err)
			if tc.uid == "" {
				assert.NotContains(t, obj.Spec.Template.Annotations, serviceAccountUIDAnnotation)
			} else {
				assert.Equal(t, tc.uid, obj.Spec.Template.Annotations[serviceAccountUIDAnnotation])
			}
			assert.Equal(t, "keep", obj.Spec.Template.Annotations["example.test/note"],
				"stamping the ServiceAccount UID must not drop other pod template annotations")
		})
	}

	t.Run("non-empty UID initializes the annotations map when none exist", func(t *testing.T) {
		obj, err := getDeploymentObject(testInstance(), "sa-uid-1")
		require.NoError(t, err)
		assert.Equal(t, "sa-uid-1", obj.Spec.Template.Annotations[serviceAccountUIDAnnotation])
	})

	withUID1, err := getDeploymentObject(instance, "sa-uid-1")
	require.NoError(t, err)
	withUID2, err := getDeploymentObject(instance, "sa-uid-2")
	require.NoError(t, err)
	assert.True(t, deploymentModified(withUID2, withUID1),
		"a changed ServiceAccount UID must be treated as drift so the Deployment rolls")
}

func TestInvalidDeploymentConfiguration(t *testing.T) {
	t.Setenv(imageEnvVar, "example.test/approver:test")
	for _, tc := range []struct {
		name   string
		mutate func(*v1alpha1.ApproverPolicyManager)
	}{
		{"selector", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.NodeSelector = map[string]string{"invalid!": "x"}
		}},
		{"labels", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ControllerConfig.Labels = map[string]string{"invalid!": "x"}
		}},
		{"annotations", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ControllerConfig.Annotations = map[string]string{"invalid!": "x"}
		}},
		{"negative resources", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("-1")}
		}},
		{"tolerations", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.Tolerations = []corev1.Toleration{
				{Key: "dedicated", Operator: corev1.TolerationOpExists, Value: "must-be-empty-for-exists"},
			}
		}},
		{"affinity", func(o *v1alpha1.ApproverPolicyManager) {
			o.Spec.ApproverPolicyConfig.Affinity = &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{
							// "In" requires at least one Value; none supplied here is invalid.
							MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "disktype", Operator: corev1.NodeSelectorOpIn}},
						}},
					},
				},
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := testInstance()
			tc.mutate(obj)
			_, err := getDeploymentObject(obj, "")
			require.True(t, common.IsIrrecoverableError(err))
		})
	}
	t.Setenv(imageEnvVar, "")
	_, err := getDeploymentObject(testInstance(), "")
	require.True(t, common.IsIrrecoverableError(err))
}
