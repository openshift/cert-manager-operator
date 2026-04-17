package certmanager

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"
	certmanoperatorinformer "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	certmanagerinformerv1alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions/operator/v1alpha1"
)

// setupSyncedFakeCertManagerInformer builds a fake CertManagers client and a running, synced
// CertManagerInformer. It registers a watch reactor so creates after List are not missed (see
// client-go fake limitations). events receives each CertManager on add and delete; buffer size 1
// matches the tests that wait for a single add then a single delete per case.
func setupSyncedFakeCertManagerInformer(t *testing.T, ctx context.Context) (
	fakeClient *fake.Clientset,
	informer certmanagerinformerv1alpha1.CertManagerInformer,
	events <-chan *v1alpha1.CertManager,
) {
	t.Helper()

	watcherStarted := make(chan struct{})
	fakeClient = fake.NewClientset()
	fakeClient.PrependWatchReactor("certmanagers", func(action clienttesting.Action) (handled bool, ret watch.Interface, err error) {
		gvr := action.GetResource()
		ns := action.GetNamespace()
		w, err := fakeClient.Tracker().Watch(gvr, ns)
		if err != nil {
			return false, nil, err
		}
		close(watcherStarted)
		return true, w, nil
	})

	informer = certmanoperatorinformer.NewSharedInformerFactory(fakeClient, 0).Operator().V1alpha1().CertManagers()
	ch := make(chan *v1alpha1.CertManager, 1)
	_, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			ch <- obj.(*v1alpha1.CertManager)
		},
		DeleteFunc: func(obj any) {
			switch o := obj.(type) {
			case *v1alpha1.CertManager:
				ch <- o
			case cache.DeletedFinalStateUnknown:
				if cm, ok := o.Obj.(*v1alpha1.CertManager); ok {
					ch <- cm
				}
			}
		},
	})
	require.NoError(t, err)

	go informer.Informer().Run(ctx.Done())
	require.True(t, cache.WaitForCacheSync(ctx.Done(), informer.Informer().HasSynced), "failed to sync CertManager informer cache")
	select {
	case <-watcherStarted:
	case <-time.After(wait.ForeverTestTimeout):
		t.Fatal("watch reactor did not start")
	}

	return fakeClient, informer, ch
}

// withFakeCertManagerForTest creates obj in the fake API, waits for the informer add event, and
// registers t.Cleanup to delete obj and wait for the delete event.
func withFakeCertManagerForTest(t *testing.T, ctx context.Context, fakeClient *fake.Clientset, events <-chan *v1alpha1.CertManager, obj *v1alpha1.CertManager) {
	t.Helper()
	_, err := fakeClient.OperatorV1alpha1().CertManagers().Create(ctx, obj, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		err := fakeClient.OperatorV1alpha1().CertManagers().Delete(ctx, obj.Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("cert-manager delete failed: %v", err)
			return
		}
		select {
		case <-events:
		case <-time.After(wait.ForeverTestTimeout):
			t.Errorf("Informer did not get the deleted cert manager object during cleanup")
		}
	})
	select {
	case <-events:
	case <-time.After(wait.ForeverTestTimeout):
		t.Fatal("Informer did not get the added cert manager object")
	}
}

func TestMergeContainerResources(t *testing.T) {
	tests := []struct {
		name              string
		sourceResources   corev1.ResourceRequirements
		overrideResources v1alpha1.CertManagerResourceRequirements
		expected          corev1.ResourceRequirements
	}{
		{
			name: "empty override resources doesn't replace existing source resource limits",
			sourceResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
			overrideResources: v1alpha1.CertManagerResourceRequirements{},
			expected: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
		},
		{
			name: "empty override resources doesn't replace existing source resource requests",
			sourceResources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
			overrideResources: v1alpha1.CertManagerResourceRequirements{},
			expected: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
		},
		{
			name:            "override resources replaces empty source resource limits",
			sourceResources: corev1.ResourceRequirements{},
			overrideResources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
		},
		{
			name:            "override resources replaces empty source resource requests",
			sourceResources: corev1.ResourceRequirements{},
			overrideResources: v1alpha1.CertManagerResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
		},
		{
			name:            "override resources replaces empty source resources",
			sourceResources: corev1.ResourceRequirements{},
			overrideResources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
		},
		{
			name: "override resources only replaces source resource limits, doesn't replace source requests",
			sourceResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("400m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			overrideResources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
		},
		{
			name: "override resources doesn't replace source resource limits, replaces source requests",
			sourceResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("400m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			overrideResources: v1alpha1.CertManagerResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("40m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("400m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("40m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
			},
		},
		{
			name: "override resources limits and requests merge with source resource limits and requests respectively",
			sourceResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("10m"),
				},
			},
			overrideResources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("400m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("400m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
			},
		},
		{
			name: "override resources limits replaces nil source resource limits, override resource requests merges with source resource requests",
			sourceResources: corev1.ResourceRequirements{
				Limits: nil,
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("10m"),
				},
			},
			overrideResources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("400m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("400m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualResources := mergeContainerResources(tc.sourceResources, tc.overrideResources)
			require.Equal(t, tc.expected, actualResources)
		})
	}
}

func TestGetOverrideResourcesFor(t *testing.T) {
	tests := []struct {
		name                      string
		certManagerObj            v1alpha1.CertManager
		deploymentName            string
		expectedOverrideResources v1alpha1.CertManagerResourceRequirements
	}{
		{
			name: "get override resources of cert manager controller config",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideResources: v1alpha1.CertManagerResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: k8sresource.MustParse("10m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
							},
						},
					},
				},
			},
			deploymentName: certmanagerControllerDeployment,
			expectedOverrideResources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("10m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
		},
		{
			name: "get override resources of cert manager webhook config",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideResources: v1alpha1.CertManagerResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: k8sresource.MustParse("10m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
							},
						},
					},
				},
			},
			deploymentName: certmanagerWebhookDeployment,
			expectedOverrideResources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("10m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
		},
		{
			name: "get override resources of cert manager cainjector config",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					CAInjectorConfig: &v1alpha1.DeploymentConfig{
						OverrideResources: v1alpha1.CertManagerResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: k8sresource.MustParse("10m"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
							},
						},
					},
				},
			},
			deploymentName: certmanagerCAinjectorDeployment,
			expectedOverrideResources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("10m"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
			},
		},
	}

	ctx := t.Context()
	fakeClient, certManagerInformers, certManagerChan := setupSyncedFakeCertManagerInformer(t, ctx)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFakeCertManagerForTest(t, ctx, fakeClient, certManagerChan, &tc.certManagerObj)

			actualOverrideResources, err := getOverrideResourcesFor(certManagerInformers, tc.deploymentName)
			assert.NoError(t, err)
			require.Equal(t, tc.expectedOverrideResources, actualOverrideResources)
		})
	}
}

func TestMergePodScheduling(t *testing.T) {
	tests := []struct {
		name               string
		sourceScheduling   v1alpha1.CertManagerScheduling
		overrideScheduling v1alpha1.CertManagerScheduling
		expected           v1alpha1.CertManagerScheduling
	}{
		{
			name: "empty override scheduling doesn't replace source scheduling",
			sourceScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
			overrideScheduling: v1alpha1.CertManagerScheduling{},
			expected: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
		},
		{
			name:             "override scheduling replaces empty source scheduling",
			sourceScheduling: v1alpha1.CertManagerScheduling{},
			overrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
			expected: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
		},
		{
			name: "override scheduling merges with source scheduling for both nodeSelector and tolerations",
			sourceScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel1": "value1",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration1",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
			overrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel2": "value2",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration2",
						Operator: "Equal",
						Effect:   "NoSchedule",
					},
				},
			},
			expected: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel1": "value1",
					"nodeLabel2": "value2",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration1",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
					{
						Key:      "toleration2",
						Operator: "Equal",
						Effect:   "NoSchedule",
					},
				},
			},
		},
		{
			name: "override scheduling overrides source scheduling for both nodeSelector and tolerations",
			sourceScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value1",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Equals",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				}},
			overrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value2",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
			expected: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value2",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
		},
		{
			name: "override scheduling overrides source scheduling only for nodeSelector",
			sourceScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value1",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Equals",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				}},
			overrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value2",
				},
				Tolerations: []corev1.Toleration{},
			},
			expected: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value2",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Equals",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				},
			},
		},
		{
			name: "override scheduling overrides source scheduling only for tolerations",
			sourceScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value1",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Equals",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				}},
			overrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
			expected: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value1",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
		},
		{
			name: "override scheduling overrides source nodeSelector and merges tolerations",
			sourceScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value1",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration1",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				}},
			overrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value2",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration2",
						Operator: "Equals",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				},
			},
			expected: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value2",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration1",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
					{
						Key:      "toleration2",
						Operator: "Equals",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				},
			},
		},
		{
			name: "override scheduling merges source tolerations with same key and Exists operator; merges nodeSelector",
			sourceScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel1": "value1",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				}},
			overrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel2": "value2",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Equals",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				},
			},
			expected: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel1": "value1",
					"nodeLabel2": "value2",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualScheduling := mergePodScheduling(tc.sourceScheduling, tc.overrideScheduling)
			require.Equal(t, tc.expected, actualScheduling)
		})
	}
}

func TestGetOverrideSchedulingFor(t *testing.T) {
	tests := []struct {
		name                       string
		certManagerObj             v1alpha1.CertManager
		deploymentName             string
		expectedOverrideScheduling v1alpha1.CertManagerScheduling
	}{
		{
			name: "get override scheduling of cert manager controller config",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideScheduling: v1alpha1.CertManagerScheduling{
							NodeSelector: map[string]string{
								"nodeLabel": "value",
							},
							Tolerations: []corev1.Toleration{
								{
									Key:      "toleration",
									Operator: "Exists",
									Effect:   "NoSchedule",
								},
							},
						},
					},
				},
			},
			deploymentName: certmanagerControllerDeployment,
			expectedOverrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
		},
		{
			name: "get override scheduling of cert manager webhook config",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideScheduling: v1alpha1.CertManagerScheduling{
							NodeSelector: map[string]string{
								"nodeLabel": "value",
							},
							Tolerations: []corev1.Toleration{
								{
									Key:      "toleration",
									Operator: "Exists",
									Effect:   "NoSchedule",
								},
							},
						},
					},
				},
			},
			deploymentName: certmanagerWebhookDeployment,
			expectedOverrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
		},
		{
			name: "get override scheduling of cert manager cainjector config",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					CAInjectorConfig: &v1alpha1.DeploymentConfig{
						OverrideScheduling: v1alpha1.CertManagerScheduling{
							NodeSelector: map[string]string{
								"nodeLabel": "value",
							},
							Tolerations: []corev1.Toleration{
								{
									Key:      "toleration",
									Operator: "Exists",
									Effect:   "NoSchedule",
								},
							},
						},
					},
				},
			},
			deploymentName: certmanagerCAinjectorDeployment,
			expectedOverrideScheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			},
		},
	}

	ctx := t.Context()
	fakeClient, certManagerInformers, certManagerChan := setupSyncedFakeCertManagerInformer(t, ctx)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFakeCertManagerForTest(t, ctx, fakeClient, certManagerChan, &tc.certManagerObj)

			actualOverrideScheduling, err := getOverrideSchedulingFor(certManagerInformers, tc.deploymentName)
			assert.NoError(t, err)
			require.Equal(t, tc.expectedOverrideScheduling, actualOverrideScheduling)
		})
	}
}

func TestGetOverrideReplicasFor(t *testing.T) {
	tests := []struct {
		name                     string
		certManagerObj           v1alpha1.CertManager
		deploymentName           string
		expectedOverrideReplicas *int32
	}{
		{
			name: "get override replicas of cert manager controller config",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideReplicas: ptr.To(int32(3)),
					},
				},
			},
			deploymentName:           certmanagerControllerDeployment,
			expectedOverrideReplicas: ptr.To(int32(3)),
		},
		{
			name: "get override replicas of cert manager webhook config",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideReplicas: ptr.To(int32(4)),
					},
				},
			},
			deploymentName:           certmanagerWebhookDeployment,
			expectedOverrideReplicas: ptr.To(int32(4)),
		},
		{
			name: "get override replicas of cert manager cainjector config",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					CAInjectorConfig: &v1alpha1.DeploymentConfig{
						OverrideReplicas: ptr.To(int32(2)),
					},
				},
			},
			deploymentName:           certmanagerCAinjectorDeployment,
			expectedOverrideReplicas: ptr.To(int32(2)),
		},
		{
			name: "get nil override replicas when config exists but replicas field is nil for controller",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs:     []string{"--v=3"},
						OverrideReplicas: nil,
					},
				},
			},
			deploymentName:           certmanagerControllerDeployment,
			expectedOverrideReplicas: nil,
		},
		{
			name: "get nil override replicas when config exists but replicas field is nil for webhook",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideEnv:      []corev1.EnvVar{{Name: "TEST", Value: "value"}},
						OverrideReplicas: nil,
					},
				},
			},
			deploymentName:           certmanagerWebhookDeployment,
			expectedOverrideReplicas: nil,
		},
		{
			name: "get nil override replicas when config exists but replicas field is nil for cainjector",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{
					CAInjectorConfig: &v1alpha1.DeploymentConfig{
						OverrideLabels:   map[string]string{"test": "label"},
						OverrideReplicas: nil,
					},
				},
			},
			deploymentName:           certmanagerCAinjectorDeployment,
			expectedOverrideReplicas: nil,
		},
		{
			name: "get nil override replicas when cert manager config is not set",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: v1alpha1.CertManagerSpec{},
			},
			deploymentName:           certmanagerControllerDeployment,
			expectedOverrideReplicas: nil,
		},
	}

	ctx := t.Context()
	fakeClient, certManagerInformers, certManagerChan := setupSyncedFakeCertManagerInformer(t, ctx)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFakeCertManagerForTest(t, ctx, fakeClient, certManagerChan, &tc.certManagerObj)

			actualOverrideReplicas, err := getOverrideReplicasFor(certManagerInformers, tc.deploymentName)
			assert.NoError(t, err)
			if tc.expectedOverrideReplicas == nil {
				assert.Nil(t, actualOverrideReplicas)
			} else {
				require.NotNil(t, actualOverrideReplicas)
				assert.Equal(t, *tc.expectedOverrideReplicas, *actualOverrideReplicas)
			}
		})
	}
}
