package deployment

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"
	certmanoperatorinformer "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

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
			name: "override resources limits and requests merges with source resource limits and requests respectively",
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create channel to know when the watch has started.
	watcherStarted := make(chan struct{})
	// Create the fake client.
	fakeClient := fake.NewSimpleClientset()
	// A watch reactor for cert manager objects that allows the injection of the watcherStarted channel.
	fakeClient.PrependWatchReactor("certmanagers", func(action clienttesting.Action) (handled bool, ret watch.Interface, err error) {
		gvr := action.GetResource()
		ns := action.GetNamespace()
		watch, err := fakeClient.Tracker().Watch(gvr, ns)
		if err != nil {
			return false, nil, err
		}
		close(watcherStarted)
		return true, watch, nil
	})

	// Create cert manager informers using the fake client.
	certManagerInformers := certmanoperatorinformer.NewSharedInformerFactory(fakeClient, 0).Operator().V1alpha1().CertManagers()

	// Create a channel to receive the cert manager objects from the informer.
	certManagerChan := make(chan *v1alpha1.CertManager, 1)

	// Add event handlers to the informer to write the cert manager objects to
	// the channel received during the add and the delete events.
	certManagerInformers.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			certManagerObj := obj.(*v1alpha1.CertManager)
			t.Logf("cert manager obj added: %s", certManagerObj.Name)
			certManagerChan <- certManagerObj
		},
		DeleteFunc: func(obj interface{}) {
			certManagerObj := obj.(*v1alpha1.CertManager)
			t.Logf("cert manager obj deleted: %s", certManagerObj.Name)
			certManagerChan <- certManagerObj
		},
	})

	// Make sure informer is running.
	go certManagerInformers.Informer().Run(ctx.Done())

	// This is not required in tests, but it serves as a proof-of-concept by
	// ensuring that the informer goroutine have warmed up and called List before
	// we send any events to it.
	cache.WaitForCacheSync(ctx.Done(), certManagerInformers.Informer().HasSynced)

	// The fake client doesn't support resource version. Any writes to the client
	// after the informer's initial LIST and before the informer establishing the
	// watcher will be missed by the informer. Therefore we wait until the watcher
	// starts.
	<-watcherStarted

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create the cert manager object using the fake client.
			_, err := fakeClient.OperatorV1alpha1().CertManagers().Create(ctx, &tc.certManagerObj, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("error injecting cert manager add: %v", err)
			}

			// Wait for the informer to get the event.
			select {
			case <-certManagerChan:
			case <-time.After(wait.ForeverTestTimeout):
				t.Fatal("Informer did not get the added cert manager object")
			}

			actualOverrideResources, err := getOverrideResourcesFor(certManagerInformers, tc.deploymentName)
			assert.NoError(t, err)
			require.Equal(t, tc.expectedOverrideResources, actualOverrideResources)

			// Delete the cert manager object using the fake client.
			err = fakeClient.OperatorV1alpha1().CertManagers().Delete(ctx, tc.certManagerObj.Name, metav1.DeleteOptions{})
			if err != nil {
				t.Fatalf("error deleting cert manager add: %v", err)
			}

			// Wait for the informer to get the event.
			select {
			case <-certManagerChan:
			case <-time.After(wait.ForeverTestTimeout):
				t.Fatal("Informer did not get the deleted cert manager")
			}
		})
	}
}
